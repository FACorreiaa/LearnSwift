// Package testdb gives a test its own database.
//
// Tests run against real Postgres rather than a mock, because the things worth
// testing here are the things a mock would invent: that a query really is
// scoped to its owner, that a constraint really rejects a duplicate, that a
// cascade really removes the rows it claims to.
package testdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FACorreiaa/seshat/internal/shared/database"
)

// setupTimeout bounds everything New does: connecting, creating the database,
// and migrating it. Generous, because CREATE DATABASE is genuinely slow on some
// filesystems, but finite so a stalled server cannot hang the suite.
const setupTimeout = 60 * time.Second

// New creates a uniquely named database, applies the embedded migrations to it,
// and drops it when the test finishes.
//
// It skips rather than fails when no database is configured, so `go test ./...`
// stays useful on a machine with no Postgres running. A test that must not be
// skipped should assert on its own preconditions.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	adminURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if adminURL == "" {
		adminURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if adminURL == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL set; skipping database test")
	}

	// Bounded, because a database server that is *hung* rather than refusing
	// connections would otherwise block the test forever — which is how a
	// wedged container runtime turns a five second suite into an hour. A
	// refusal is instant; this cap is for the other case.
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	// A fresh name per test, so tests can run in parallel and one test's rows
	// can never explain another test's result.
	name := "seshat_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		// Deliberately a failure rather than a skip. Once a database URL is
		// configured, silently skipping would mean the tests that actually
		// check row scoping quietly stop running — in CI as much as locally.
		t.Fatalf("testdb: cannot reach %s\n"+
			"  Is the local stack up? `task db:up` (and check the Docker daemon is running).\n"+
			"  To run the suite without a database instead, unset DATABASE_URL and TEST_DATABASE_URL.\n"+
			"  underlying error: %v", redact(adminURL), err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("testdb: create database: %v", err)
	}
	_ = admin.Close(ctx)

	url := replaceDatabase(adminURL, name)

	if err := database.Migrate(ctx, url); err != nil {
		dropDatabase(t, adminURL, name)
		t.Fatalf("testdb: migrate: %v", err)
	}

	pool, err := database.ConnectSmall(ctx, url)
	if err != nil {
		dropDatabase(t, adminURL, name)
		t.Fatalf("testdb: connect: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		dropDatabase(t, adminURL, name)
	})

	return pool
}

func dropDatabase(t *testing.T, adminURL, name string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Logf("testdb: could not connect to drop %s: %v", name, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()

	// WITH (FORCE) terminates whatever is still attached. Without it a pool
	// connection that has not finished closing leaves the database behind, and
	// a day of running tests fills the server with them.
	if _, err := admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
		t.Logf("testdb: drop %s: %v", name, err)
	}
}

// replaceDatabase swaps the database name in a connection URL, keeping every
// other parameter (sslmode, host, credentials) exactly as given.
func replaceDatabase(url, name string) string {
	base, query, hasQuery := strings.Cut(url, "?")

	slash := strings.LastIndex(base, "/")
	if slash < 0 {
		base += "/" + name
	} else {
		base = base[:slash+1] + name
	}

	if hasQuery {
		return base + "?" + query
	}
	return base
}

// redact keeps credentials out of a failure message.
func redact(url string) string {
	at := strings.LastIndex(url, "@")
	scheme := strings.Index(url, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return url
	}
	return url[:scheme+3] + "***" + url[at:]
}
