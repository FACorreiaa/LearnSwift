package database

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/migrations"
)

// TestEveryMigrationFilenameIsParseable guards the one migration mistake that
// produces no error at all.
//
// goose reads the leading digits of a filename as the version. Given a name it
// cannot parse — a `T` in the timestamp is the usual way — it skips the file
// and reports success. The schema is then silently missing a table, and the
// first sign of it is a query failing in production.
func TestEveryMigrationFilenameIsParseable(t *testing.T) {
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations are embedded; check the //go:embed pattern in migrations/fs.go")
	}

	seen := map[int64]string{}
	for _, name := range entries {
		version, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Errorf("%s: expected <version>_<description>.sql", name)
			continue
		}

		n, err := strconv.ParseInt(version, 10, 64)
		if err != nil {
			t.Errorf("%s: version %q is not digits-only, so goose will skip this file without reporting anything", name, version)
			continue
		}

		if other, dup := seen[n]; dup {
			t.Errorf("%s and %s share version %d", name, other, n)
			continue
		}
		seen[n] = name
	}
}

// TestEveryMigrationHasADownSection keeps a migration from being one-way by
// accident. Discovering that a release cannot be rolled back is not something
// to find out during the rollback.
func TestEveryMigrationHasADownSection(t *testing.T) {
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}

	for _, name := range entries {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(body), "+goose Down") {
			t.Errorf("%s: missing a `-- +goose Down` section", name)
		}
	}
}
