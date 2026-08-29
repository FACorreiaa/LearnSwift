// Package config loads the application's settings from the environment.
//
// There is deliberately no package-level Config variable. Everything that needs
// a setting is handed one at construction, which keeps the dependency visible in
// the signature and lets a test build a Config without touching the process
// environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Env is "development" or "production". It is not a feature switch: it
	// decides only whether assets come from disk or the embedded FS, and
	// whether cookies are marked Secure.
	Env string

	ListenAddr        string
	MetricsListenAddr string
	BaseURL           string

	DatabaseURL string
	AutoMigrate bool

	// SwiftValidateBin is the path to the swift-validate binary that grades
	// exercises. Empty means exercise checking is unavailable — the app still
	// serves lessons, but a submission cannot be graded, which is reported as
	// such rather than silently passing.
	SwiftValidateBin string

	// ContainerRuntime and CompilerImage locate the sandbox that compiles a
	// learner's Swift. Empty CompilerImage disables running submissions: the
	// app still serves lessons and still checks them statically.
	ContainerRuntime string
	CompilerImage    string

	// ModuleCacheBytes bounds the in-process cache of compiled modules. Sized
	// in bytes rather than entries because the two Swift SDKs differ by fifty
	// times in output size.
	ModuleCacheBytes int

	// AssetDir is where assets are read from in development. Production ignores
	// it and serves the copies embedded in the binary.
	//
	// It is configurable rather than hardcoded because the default is relative
	// to the working directory: running the dev binary from anywhere but the
	// repository root otherwise serves no CSS at all, and does it silently.
	AssetDir string

	LogLevel  string
	LogFormat string

	ShutdownTimeout time.Duration
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads the environment into a Config and fails loudly on anything it
// cannot work without. A missing DATABASE_URL is worth refusing to start over:
// the alternative is an application that boots, serves a landing page, and only
// reveals the problem when the first visitor tries to sign in.
func Load() (Config, error) {
	c := Config{
		Env:               env("GO_ENV", "development"),
		ListenAddr:        env("LISTEN_ADDR", ":8090"),
		MetricsListenAddr: env("METRICS_LISTEN_ADDR", ""),
		BaseURL:           strings.TrimSuffix(env("BASE_URL", "http://localhost:8090"), "/"),
		DatabaseURL:       env("DATABASE_URL", ""),
		AssetDir:          env("ASSET_DIR", "./web/assets"),
		SwiftValidateBin:  env("SWIFT_VALIDATE_BIN", ""),
		ContainerRuntime:  env("CONTAINER_RUNTIME", "docker"),
		CompilerImage:     env("COMPILER_IMAGE", ""),
		LogLevel:          env("LOG_LEVEL", "info"),
		LogFormat:         env("LOG_FORMAT", ""),
		ShutdownTimeout:   30 * time.Second,
	}

	cacheBytes, err := envInt("MODULE_CACHE_BYTES", 256<<20)
	if err != nil {
		return Config{}, err
	}
	c.ModuleCacheBytes = cacheBytes

	autoMigrate, err := envBool("AUTO_MIGRATE", !c.IsProduction())
	if err != nil {
		return Config{}, err
	}
	c.AutoMigrate = autoMigrate

	if c.LogFormat == "" {
		// Text is readable in a terminal; JSON is what a log collector can
		// index. Defaulting by environment means neither has to be configured.
		if c.IsProduction() {
			c.LogFormat = "json"
		} else {
			c.LogFormat = "text"
		}
	}

	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q", key, raw)
	}
	return v, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean, got %q", key, raw)
	}
	return v, nil
}
