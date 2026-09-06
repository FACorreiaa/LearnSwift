package progress

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
)

// newUser inserts a user directly. The progress slice has no business creating
// one, and going through the auth service here would couple these tests to
// password hashing for no benefit.
func newUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// The scoping in these queries is the access control — there is no second check
// anywhere else — so this is the test that matters most in the package.
func TestProgressIsScopedToItsOwner(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	alice := newUser(t, pool, "alice@example.com")
	bob := newUser(t, pool, "bob@example.com")

	if err := svc.Complete(ctx, alice, "optionals"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if _, found, err := svc.Get(ctx, bob, "optionals"); err != nil {
		t.Fatalf("get: %v", err)
	} else if found {
		t.Error("Bob can see Alice's progress")
	}

	all, err := svc.ForUser(ctx, bob)
	if err != nil {
		t.Fatalf("for user: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("Bob's progress list contains %d entries from another user", len(all))
	}
}

func TestAnUnopenedLessonIsNotAnError(t *testing.T) {
	pool := testdb.New(t)
	svc := New(pool)
	user := newUser(t, pool, "nobody@example.com")

	got, found, err := svc.Get(context.Background(), user, "never-opened")
	if err != nil {
		t.Fatalf("a lesson with no progress row must not be an error: %v", err)
	}
	if found {
		t.Error("found = true for a lesson that was never opened")
	}
	if got.Status != "" {
		t.Errorf("expected a zero LessonProgress, got %+v", got)
	}
}

func TestStartingAnAlreadyCompletedLessonDoesNotUndoIt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)
	user := newUser(t, pool, "revisit@example.com")

	if err := svc.Complete(ctx, user, "optionals"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	before, _, err := svc.Get(ctx, user, "optionals")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Re-reading a finished lesson is normal, and must not demote it.
	if err = svc.Start(ctx, user, "optionals"); err != nil {
		t.Fatalf("start: %v", err)
	}

	after, _, err := svc.Get(ctx, user, "optionals")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if after.Status != StatusCompleted {
		t.Errorf("status = %q, want completed — reopening a lesson demoted it", after.Status)
	}
	if after.CompletedAt == nil {
		t.Fatal("completed_at was cleared")
	}
	if !after.CompletedAt.Equal(*before.CompletedAt) {
		t.Error("completed_at moved; the first completion is the interesting one")
	}
}

func TestCompletingIsIdempotentAndKeepsTheFirstTimestamp(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)
	user := newUser(t, pool, "twice@example.com")

	if err := svc.Complete(ctx, user, "optionals"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	first, _, _ := svc.Get(ctx, user, "optionals")

	time.Sleep(10 * time.Millisecond)

	if err := svc.Complete(ctx, user, "optionals"); err != nil {
		t.Fatalf("complete again: %v", err)
	}
	second, _, _ := svc.Get(ctx, user, "optionals")

	if !second.CompletedAt.Equal(*first.CompletedAt) {
		t.Errorf("completed_at changed on re-completion: %v -> %v", first.CompletedAt, second.CompletedAt)
	}
}

func TestAttemptsAreScopedAndNewestFirst(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	alice := newUser(t, pool, "a@example.com")
	bob := newUser(t, pool, "b@example.com")

	for _, code := range []string{"first", "second", "third"} {
		if _, err := svc.RecordAttempt(ctx, alice, "optionals", code, false); err != nil {
			t.Fatalf("record: %v", err)
		}
		// The ordering index is on created_at, and three inserts inside one
		// clock tick would order arbitrarily.
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := svc.RecordAttempt(ctx, bob, "optionals", "bob's", true); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := svc.Attempts(ctx, alice, "optionals", 10)
	if err != nil {
		t.Fatalf("attempts: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d attempts, want 3 — Bob's may have leaked in", len(got))
	}
	if got[0].Code != "third" {
		t.Errorf("newest first: got %q, want %q", got[0].Code, "third")
	}
}

// The CHECK constraint pairing status and completed_at is what stops the two
// columns from disagreeing, so it is worth proving it is actually enforced
// rather than assumed.
func TestTheDatabaseRejectsACompletedRowWithNoTimestamp(t *testing.T) {
	pool := testdb.New(t)
	user := newUser(t, pool, "constraint@example.com")

	_, err := pool.Exec(context.Background(),
		`INSERT INTO user_lesson (user_id, lesson_slug, status, completed_at)
		 VALUES ($1, 'optionals', 'completed', NULL)`, user)
	if err == nil {
		t.Fatal("the database accepted a completed lesson with no completed_at")
	}
}

func TestDeletingAUserRemovesTheirProgress(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)
	user := newUser(t, pool, "erase@example.com")

	if err := svc.Complete(ctx, user, "optionals"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := svc.RecordAttempt(ctx, user, "optionals", "code", true); err != nil {
		t.Fatalf("record: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	for _, table := range []string{"user_lesson", "exercise_attempt"} {
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE user_id = $1`, user).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s still holds %d rows after the user was deleted", table, count)
		}
	}
}
