// Package middleware holds the request-scoped concerns every route shares:
// identity for the log, the log itself, panic recovery, and a body cap.
package middleware

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"time"
)

// ClientIP is the address a rate limiter counts against.
//
// It reads RemoteAddr and nothing else. X-Forwarded-For is deliberately ignored:
// it is attacker-controlled unless a proxy is known to be rewriting it, and a
// limiter keyed on a header anyone can set is not a limiter. Behind a proxy this
// wants replacing with a trusted-hop implementation, not with blind trust.
//
// The zero Addr is returned for anything unparseable, which is a usable key —
// every such caller shares one bucket, which is the conservative direction.
func ClientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

type ctxKey int

const (
	requestIDKey ctxKey = iota
	loggerKey
)

// RequestID stamps every request with an identifier and echoes it back on the
// response, so a line in the log and a report from a visitor can be joined up.
// An inbound X-Request-Id is trusted, which is what makes a trace survive a
// proxy in front of the app.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 128 {
			id = rand.Text()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// statusRecorder remembers what was written so the access log can report it.
// WriteHeader may never be called at all, hence the 200 default.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush keeps streaming working. Wrapping a ResponseWriter hides the
// http.Flusher the SSE handlers depend on, which shows up as a response that
// arrives all at once at the end instead of streaming.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Logger writes one line per request and puts a request-scoped logger on the
// context, so anything downstream can log with the request id already attached.
func Logger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			log := base.With(
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			ctx := context.WithValue(r.Context(), loggerKey, log)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			log.Info("request",
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// FromContext returns the request-scoped logger, or the default one when called
// outside a request. It never returns nil: a logging call is not worth a panic.
func FromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}

// Recover turns a panic into a 500 and a log line with a stack, so one broken
// handler cannot take the process down with it.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// A client that hangs up mid-write makes the http package panic
			// with ErrAbortHandler by design. Re-panicking lets the server
			// handle it as the ordinary event it is, rather than logging a
			// stack trace for every cancelled request.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			FromContext(r.Context()).Error("panic",
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// MaxBody caps how much a route will read.
//
// It is applied per route group rather than globally because the limits differ
// by an order of magnitude: a sign-in form needs a few kilobytes, and a Swift
// submission needs room for a file the visitor pasted.
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
