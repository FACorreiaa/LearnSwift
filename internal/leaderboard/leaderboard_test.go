package leaderboard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

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

// solve records a passing attempt and the completion it implies, which is what
// the application does. Written as raw SQL rather than through the progress
// service so a test can say exactly which provenance and which order it means.
func solve(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, slug, provenance string) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO exercise_attempt (user_id, lesson_slug, code, passed, provenance)
		 VALUES ($1, $2, 'let a = 1', true, $3)`, user, slug, provenance); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_lesson (user_id, lesson_slug, status, completed_at)
		 VALUES ($1, $2, 'completed', now())
		 ON CONFLICT (user_id, lesson_slug) DO UPDATE
		 SET status = 'completed', completed_at = coalesce(user_lesson.completed_at, now())`,
		user, slug); err != nil {
		t.Fatalf("upsert progress: %v", err)
	}
}

func reveal(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, slug string) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`UPDATE user_lesson SET solution_revealed_at = now()
		 WHERE user_id = $1 AND lesson_slug = $2`, user, slug); err != nil {
		t.Fatalf("mark revealed: %v", err)
	}
}

func standingFor(t *testing.T, board []Standing, name string) Standing {
	t.Helper()

	for _, s := range board {
		if s.DisplayName == name {
			return s
		}
	}
	t.Fatalf("%q is not on the board: %+v", name, board)
	return Standing{}
}

// The board is consent, not a census. A learner who never opted in must not
// appear on it however many lessons they finish.
func TestOnlyLearnersWhoOptedInAreRanked(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	shy := newUser(t, pool, "shy@example.com")
	keen := newUser(t, pool, "keen@example.com")

	solve(t, pool, shy, "optionals", "typed")
	solve(t, pool, keen, "optionals", "typed")

	if err := svc.OptIn(ctx, keen, "Keen"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	solo, assisted, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}
	if len(solo) != 1 || len(assisted) != 1 {
		t.Fatalf("board has %d solo and %d assisted rows, want 1 each", len(solo), len(assisted))
	}
	if solo[0].DisplayName != "Keen" {
		t.Errorf("ranked %q, want %q", solo[0].DisplayName, "Keen")
	}
}

// The distinction the whole feature exists to make.
func TestSoloCountsOnlyUnaidedSolves(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "counts@example.com")
	if err := svc.OptIn(ctx, user, "Counted"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	// One of each, so a miscount in either direction shows up.
	solve(t, pool, user, "variables", "typed")
	solve(t, pool, user, "types", "typed")
	solve(t, pool, user, "optionals", "agent")
	solve(t, pool, user, "closures", "pasted")
	solve(t, pool, user, "functions", "mixed")
	solve(t, pool, user, "control-flow", "unknown")

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}

	got := standingFor(t, solo, "Counted")
	if got.Solo != 2 {
		t.Errorf("solo = %d, want 2 — only the typed solves count", got.Solo)
	}
	if got.Assisted != 4 {
		t.Errorf("assisted = %d, want 4 — everything that is not a typed solve", got.Assisted)
	}
}

// A lesson finished after reading the solution is still finished. It is not
// solved unaided, and a board that said otherwise would be the kind of number
// that is worse than none.
func TestRevealingTheSolutionDisqualifiesALessonFromSolo(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "revealed@example.com")
	if err := svc.OptIn(ctx, user, "Revealed"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	solve(t, pool, user, "variables", "typed")
	solve(t, pool, user, "optionals", "typed")
	reveal(t, pool, user, "optionals")

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}

	got := standingFor(t, solo, "Revealed")
	if got.Solo != 1 {
		t.Errorf("solo = %d, want 1 — the revealed lesson must not count", got.Solo)
	}
	if got.Assisted != 1 {
		t.Errorf("assisted = %d, want 1 — it still counts, just not as solo", got.Assisted)
	}
}

// Only the first passing attempt's provenance means anything. A learner who
// solves a lesson unaided and later pastes the same answer back has not
// retroactively cheated; one who pastes it first has not earned the solo count
// by retyping it afterwards.
func TestOnlyTheFirstPassingAttemptDecidesTheLabel(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	honest := newUser(t, pool, "typed-first@example.com")
	if err := svc.OptIn(ctx, honest, "TypedFirst"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	solve(t, pool, honest, "optionals", "typed")
	solve(t, pool, honest, "optionals", "pasted")

	cheeky := newUser(t, pool, "pasted-first@example.com")
	if err := svc.OptIn(ctx, cheeky, "PastedFirst"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	solve(t, pool, cheeky, "optionals", "pasted")
	solve(t, pool, cheeky, "optionals", "typed")

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}

	if got := standingFor(t, solo, "TypedFirst"); got.Solo != 1 || got.Assisted != 0 {
		t.Errorf("typed first: solo = %d, assisted = %d; want 1 and 0", got.Solo, got.Assisted)
	}
	if got := standingFor(t, solo, "PastedFirst"); got.Solo != 0 || got.Assisted != 1 {
		t.Errorf("pasted first: solo = %d, assisted = %d; want 0 and 1", got.Solo, got.Assisted)
	}
}

// A failed attempt is not a solve, whoever typed it.
func TestFailedAttemptsAreNotCounted(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "failing@example.com")
	if err := svc.OptIn(ctx, user, "Failing"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO exercise_attempt (user_id, lesson_slug, code, passed, provenance)
		 VALUES ($1, 'optionals', 'nope', false, 'typed')`, user); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}
	if got := standingFor(t, solo, "Failing"); got.Solo != 0 || got.Assisted != 0 {
		t.Errorf("solo = %d, assisted = %d; want 0 and 0", got.Solo, got.Assisted)
	}
}

// Somebody who opted in before solving anything belongs on the board at zero,
// not missing from it: the LEFT JOIN is what makes that true.
func TestAnOptedInLearnerWithNoSolvesIsRankedAtZero(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "fresh@example.com")
	if err := svc.OptIn(ctx, user, "Fresh"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}
	if got := standingFor(t, solo, "Fresh"); got.Solo != 0 || got.Assisted != 0 {
		t.Errorf("solo = %d, assisted = %d; want 0 and 0", got.Solo, got.Assisted)
	}
}

func TestTheTwoBoardsAreOrderedIndependently(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	typist := newUser(t, pool, "typist@example.com")
	driver := newUser(t, pool, "driver@example.com")
	if err := svc.OptIn(ctx, typist, "Typist"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	if err := svc.OptIn(ctx, driver, "Driver"); err != nil {
		t.Fatalf("opt in: %v", err)
	}

	solve(t, pool, typist, "variables", "typed")
	solve(t, pool, typist, "types", "typed")

	for _, slug := range []string{"variables", "types", "optionals", "closures"} {
		solve(t, pool, driver, slug, "agent")
	}

	solo, assisted, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}

	if solo[0].DisplayName != "Typist" {
		t.Errorf("solo board led by %q, want Typist", solo[0].DisplayName)
	}
	if assisted[0].DisplayName != "Driver" {
		t.Errorf("assisted board led by %q, want Driver", assisted[0].DisplayName)
	}
}

func TestOptingOutRemovesALearnerFromTheBoards(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "leaving@example.com")
	if err := svc.OptIn(ctx, user, "Leaving"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	solve(t, pool, user, "optionals", "typed")

	if err := svc.OptOut(ctx, user); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}
	if len(solo) != 0 {
		t.Errorf("board still lists %d rows", len(solo))
	}

	// Leaving a ranking is not leaving the course.
	var attempts int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM exercise_attempt WHERE user_id = $1`, user).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 1 {
		t.Errorf("opting out destroyed %d attempts", 1-attempts)
	}
}

func TestANameCanBeChangedButNotStolen(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	alice := newUser(t, pool, "name-alice@example.com")
	bob := newUser(t, pool, "name-bob@example.com")

	if err := svc.OptIn(ctx, alice, "Alice"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	// Renaming is the same call, so it must not collide with the row it
	// replaces.
	if err := svc.OptIn(ctx, alice, "Alice Again"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	name, ok, err := svc.Standing(ctx, alice)
	if err != nil {
		t.Fatalf("standing: %v", err)
	}
	if !ok || name != "Alice Again" {
		t.Errorf("standing = %q, %v; want the new name", name, ok)
	}

	// Case-insensitively unique: two learners distinguishable only by
	// capitalisation is an impersonation on a page whose content is names.
	if err := svc.OptIn(ctx, bob, "alice again"); !errors.Is(err, apperr.ErrValidation) {
		t.Errorf("Bob took Alice's name: %v", err)
	}
}

func TestALearnerWhoNeverOptedInHasNoStanding(t *testing.T) {
	pool := testdb.New(t)
	store := New(pool)
	user := newUser(t, pool, "nostanding@example.com")

	name, ok, err := store.Standing(context.Background(), user)
	if err != nil {
		t.Fatalf("standing: %v", err)
	}
	if ok || name != "" {
		t.Errorf("standing = %q, %v; want empty and false", name, ok)
	}
}

func TestDisplayNamesAreConstrained(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)
	user := newUser(t, pool, "names@example.com")

	for _, name := range []string{
		"",
		"ab",
		strings.Repeat("x", maxDisplayName+1),
		// A right-to-left override can rewrite the line it sits on.
		"Al\u202eice",
		// A zero-width joiner is invisible and defeats the unique index.
		"Ali\u200dce",
		"Alice  Twospace",
		"alice@example.com",
		"<script>alert(1)</script>",
	} {
		if err := svc.OptIn(ctx, user, name); !errors.Is(err, apperr.ErrValidation) {
			t.Errorf("OptIn(%q) was accepted", name)
		}
	}

	for _, name := range []string{"Alice", "alice_99", "Ana-Maria", "Ana Maria", "日本語だ"} {
		if err := svc.OptIn(ctx, user, name); err != nil {
			t.Errorf("OptIn(%q) was refused: %v", name, err)
		}
	}
}

func TestDeletingAUserRemovesTheirOptIn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := New(pool)

	user := newUser(t, pool, "cascade-board@example.com")
	if err := svc.OptIn(ctx, user, "Cascading"); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	solo, _, err := svc.Boards(ctx)
	if err != nil {
		t.Fatalf("boards: %v", err)
	}
	if len(solo) != 0 {
		t.Errorf("a deleted user is still ranked: %+v", solo)
	}
}
