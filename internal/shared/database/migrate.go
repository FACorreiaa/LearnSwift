package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/FACorreiaa/seshat/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Migrate applies the embedded schema.
//
// The SQL is embedded rather than read from disk so the runtime image needs no
// migrations directory: `main migrate` carries its own schema, and a container
// can never apply migrations from a different commit than the binary.
func Migrate(ctx context.Context, url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("migrate: open: %w", err)
	}
	defer func() { _ = db.Close() }()

	// A session-level advisory lock, so two replicas starting at the same
	// moment cannot both try to apply the same migration.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrate: locker: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations.FS,
		goose.WithSessionLocker(locker),
		// Migrations are written on branches and merged in an order nobody
		// controls, so a migration can legitimately arrive with a timestamp
		// older than one already applied. Refusing it would block a deploy
		// over a filename.
		goose.WithAllowOutofOrder(true),
	)
	if err != nil {
		return fmt.Errorf("migrate: provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}
