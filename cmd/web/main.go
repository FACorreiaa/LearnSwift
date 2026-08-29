package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/compiler"
	"github.com/FACorreiaa/seshat/internal/config"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/database"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	"github.com/FACorreiaa/seshat/internal/validate"
	"github.com/FACorreiaa/seshat/web/assets"
	"github.com/FACorreiaa/seshat/web/landing"
)

func main() {
	// os.Exit only here. Calling it from run would skip every deferred close
	// in the process, including the connection pool's.
	if err := dispatch(); err != nil {
		slog.Error("fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func dispatch() error {
	loadDotEnv()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(newLogger(cfg))

	// A subcommand rather than a second binary, so the image cannot apply a
	// schema built from a different commit than the application serving it.
	// In-cluster this runs as a PreSync hook with AUTO_MIGRATE=false.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
		slog.Info("migrations applied")
		return nil
	}

	return run(cfg)
}

func run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.AutoMigrate {
		if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Lessons are parsed once, at startup, so a malformed lesson stops a deploy
	// instead of producing a page that fails for a reader.
	lessonFS, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		return fmt.Errorf("lessons: %w", err)
	}
	index, err := lessons.Parse(lessonFS)
	if err != nil {
		return err
	}
	slog.Info("lessons loaded", slog.Int("count", index.Count()))

	svc := newServices(pool, cfg)
	defer func() { _ = svc.executor.Close(context.Background()) }()

	if !svc.executor.Available() {
		// A warning, not a refusal to start. Lessons still read, and static
		// checking still works; only the exercises needing output go pending.
		slog.Warn("running submissions is disabled; set COMPILER_IMAGE (see: task compiler:build)")
	}
	if !svc.validator.Available() {
		// A warning rather than a refusal to start: lessons still read fine,
		// and an operator who has not built the binary should find out from a
		// log line rather than from the app declining to boot.
		slog.Warn("exercise checking is disabled; set SWIFT_VALIDATE_BIN (see: task validate:build)")
	}

	// Expired sessions are already ignored by the lookup query; this only stops
	// the table growing without bound. Bound to the signal context so it stops
	// with the process.
	go auth.SweepExpiredSessions(ctx, svc.auth.Sessions(), slog.Default())

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: routes(cfg, pool, index, svc),

		// A header the client never finishes sending would otherwise hold a
		// connection open indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,

		// WriteTimeout is deliberately unset. It is a deadline on the whole
		// response, which severs a Server-Sent Events stream the moment it
		// expires — and Phase 2 streams compile output over SSE. Handlers
		// bound their own work with context deadlines instead.
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", slog.String("addr", cfg.ListenAddr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// services are the slices the router wires together. They are built once, by
// run, so that anything needing one outside a request — the session sweep, and
// the compile worker in a later phase — shares the instance the handlers use
// rather than constructing a second one against the same pool.
type services struct {
	auth      *auth.Service
	progress  *progress.Service
	validator *validate.Validator
	executor  *executor.Executor
}

func newServices(pool *pgxpool.Pool, cfg config.Config) *services {
	svc := &services{
		validator: validate.New(cfg.SwiftValidateBin),
		executor: executor.New(
			compiler.New(cfg.ContainerRuntime, cfg.CompilerImage),
			executor.NewMemoryCache(cfg.ModuleCacheBytes),
		),
	}
	if pool != nil {
		svc.auth = auth.NewService(pool)
		svc.progress = progress.New(pool)
	}
	return svc
}

func routes(cfg config.Config, pool *pgxpool.Pool, index *lessons.Index, svc *services) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(slog.Default()))
	r.Use(middleware.Recover)

	// Liveness sits outside every group below: a probe should not be asked for
	// a CSRF token, and it should keep answering while the rest is broken.
	r.Get("/healthz", healthz(pool))

	// nil pool is the routing tests, which exercise the static routes without
	// standing up a database. Building the slices against a nil pool would
	// panic on the first query rather than at construction.
	var (
		authHandler   *auth.Handler
		lessonHandler *lessons.Handler
		authMW        *auth.Middleware
	)
	if pool != nil {
		authMW = auth.NewMiddleware(svc.auth.Sessions(), cfg.IsProduction())
		authHandler = auth.NewHandler(svc.auth, authMW)
		lessonHandler = lessons.NewHandler(index, svc.progress, svc.validator, svc.executor)
	}

	// The browser group. Non-browser callers — the MCP endpoint and the compile
	// API in later phases — get their own sibling groups outside CSRF, because
	// a client that holds a bearer token has no cookie to double-submit.
	r.Group(func(r chi.Router) {
		r.Use(middleware.MaxBody(1 << 20))
		r.Use(middleware.CSRF(cfg.IsProduction()))
		if authMW != nil {
			// LoadUser, not RequireAuth: these routes are all public, but they
			// render differently for someone signed in.
			r.Use(authMW.LoadUser)
		}

		mountAssets(r, cfg)

		r.Handle("GET /", templ.Handler(landing.Page()))

		if authHandler != nil {
			authHandler.Routes(r)
			lessonHandler.Routes(r)
		}
	})

	return r
}

func mountAssets(r chi.Router, cfg config.Config) {
	var fileServer http.Handler
	var cacheControl string

	if cfg.IsProduction() {
		fileServer = http.FileServer(http.FS(assets.Assets))
		// Safe because a rebuild changes the URL: component scripts are
		// cache-busted by utils.ScriptURL, and the stylesheet is rebuilt into
		// the binary rather than edited in place.
		cacheControl = "public, max-age=31536000, immutable"
	} else {
		fileServer = http.FileServer(http.Dir(cfg.AssetDir))
		cacheControl = "no-store"
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// The vendored ESM bundles are served as modules; Go's sniffing gets
		// this right for .js already, but being explicit costs nothing.
		if strings.HasSuffix(req.URL.Path, ".js") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		w.Header().Set("Cache-Control", cacheControl)
		fileServer.ServeHTTP(w, req)
	})

	r.Handle("GET /assets/*", http.StripPrefix("/assets/", handler))
	r.Get("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/assets/brand/favicon.svg", http.StatusMovedPermanently)
	})
}

func healthz(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		body := map[string]string{"status": "ok", "database": "ok"}
		status := http.StatusOK

		if err := pool.Ping(ctx); err != nil {
			body["status"] = "degraded"
			body["database"] = "unreachable"
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

// loadDotEnv is best-effort. In a container there is no .env file and there is
// nothing wrong with that, so a missing file is not worth a warning.
func loadDotEnv() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "note: could not load .env: %v\n", err)
		}
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
