// Package database owns the connection pool and the schema.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens the pool and proves it works before returning.
//
// pgxpool connects lazily, so without the ping a misconfigured DATABASE_URL
// produces a process that starts cleanly and then fails on the first request
// that needs data. Failing at boot is the difference between a deploy that
// rolls back and one that serves errors.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return connect(ctx, url, 10, 2)
}

// ConnectSmall opens a pool sized for a single caller rather than a whole
// application. Tests use it: each one creates its own database, and a
// production-sized pool per test exhausts Postgres's connection limit long
// before it exhausts the machine — packages run in parallel, so a dozen pools
// of ten can be live at once against a server allowing a hundred.
func ConnectSmall(ctx context.Context, url string) (*pgxpool.Pool, error) {
	// MinConns 0 so an idle test database holds nothing open.
	return connect(ctx, url, 2, 0)
}

func connect(ctx context.Context, url string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	// Recycling connections bounds the damage from a server-side change that
	// a long-lived connection would otherwise never notice.
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}
