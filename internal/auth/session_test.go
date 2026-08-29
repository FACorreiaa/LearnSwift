package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

func newAccount(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
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

// The property the whole scheme rests on: a stolen database gives an attacker
// nothing they can present as a cookie.
func TestOnlyTheHashOfASessionTokenIsStored(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	user := newAccount(t, pool, "hash@example.com")
	token, err := store.Create(ctx, user, "test-agent", netip.MustParseAddr("203.0.113.5"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE user_id = $1`, user).Scan(&stored); err != nil {
		t.Fatalf("read session: %v", err)
	}

	if string(stored) == token {
		t.Fatal("the raw token is in the database")
	}
	// Anything derived from it must not be reversible to the token either.
	if got := sha256.Sum256([]byte(token)); string(stored) != string(got[:]) {
		t.Error("the stored value is not the SHA-256 of the token")
	}

	// A token has to survive a round trip through a cookie header unchanged,
	// which is why it is base64url rather than raw bytes.
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Errorf("token %q is not base64url and would not survive a cookie: %v", token, err)
	}
}

func TestEachSessionTokenIsUnique(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)
	user := newAccount(t, pool, "unique@example.com")

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		token, err := store.Create(ctx, user, "", netip.Addr{})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if seen[token] {
			t.Fatal("a session token was issued twice")
		}
		seen[token] = true
	}
}

func TestLookupReturnsTheOwnerOfAValidSession(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	id := newAccount(t, pool, "owner@example.com")
	token, err := store.Create(ctx, id, "agent", netip.MustParseAddr("198.51.100.9"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	user, session, err := store.Lookup(ctx, token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if user.ID != id {
		t.Errorf("user.ID = %v, want %v", user.ID, id)
	}
	if user.Email != "owner@example.com" {
		t.Errorf("user.Email = %q", user.Email)
	}
	if session.UserID != id {
		t.Errorf("session.UserID = %v, want %v", session.UserID, id)
	}
}

// An unknown token, an empty one, and an expired one are all the same answer.
// Distinguishing them would tell a holder something about why their token
// failed, and none of those reasons are theirs to learn.
func TestAnUnusableTokenIsAlwaysJustUnauthenticated(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	t.Run("empty", func(t *testing.T) {
		if _, _, err := store.Lookup(ctx, ""); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("err = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if _, _, err := store.Lookup(ctx, "not-a-real-token"); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("err = %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		user := newAccount(t, pool, "expired@example.com")
		token, err := store.Create(ctx, user, "", netip.Addr{})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// Age the row rather than waiting out a 30-day lifetime.
		if _, err := pool.Exec(ctx,
			`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE user_id = $1`, user); err != nil {
			t.Fatalf("expire: %v", err)
		}

		if _, _, err := store.Lookup(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("err = %v, want ErrUnauthenticated for an expired session", err)
		}
	})
}

// Sliding expiry must not turn every page view into a write.
func TestTouchIsThrottledButDoesExtendAnOlderSession(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	user := newAccount(t, pool, "touch@example.com")
	token, err := store.Create(ctx, user, "", netip.Addr{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, session, err := store.Lookup(ctx, token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	before := session.ExpiresAt
	if err := store.Touch(ctx, session); err != nil {
		t.Fatalf("touch: %v", err)
	}

	_, after, _ := store.Lookup(ctx, token)
	if !after.ExpiresAt.Equal(before) {
		t.Error("a freshly created session was written again; the throttle is not working")
	}

	// Past the throttle interval, the same call must extend it.
	if _, err := pool.Exec(ctx,
		`UPDATE sessions SET last_used_at = now() - interval '2 hours' WHERE user_id = $1`, user); err != nil {
		t.Fatalf("age: %v", err)
	}
	_, stale, _ := store.Lookup(ctx, token)

	if err := store.Touch(ctx, stale); err != nil {
		t.Fatalf("touch: %v", err)
	}

	_, extended, _ := store.Lookup(ctx, token)
	if !extended.ExpiresAt.After(before) {
		t.Errorf("expires_at = %v, want later than %v — an active session was not extended",
			extended.ExpiresAt, before)
	}
}

func TestDeleteRevokesOnlyThatSession(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)
	user := newAccount(t, pool, "two@example.com")

	laptop, _ := store.Create(ctx, user, "laptop", netip.Addr{})
	phone, _ := store.Create(ctx, user, "phone", netip.Addr{})

	if err := store.Delete(ctx, laptop); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, _, err := store.Lookup(ctx, laptop); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Error("the deleted session still works")
	}
	if _, _, err := store.Lookup(ctx, phone); err != nil {
		t.Errorf("signing out on one device revoked another: %v", err)
	}
}

// This is what a password change must call. A changed password that leaves old
// sessions alive has not actually revoked anybody's access.
func TestDeleteAllForUserRevokesEverySession(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	user := newAccount(t, pool, "all@example.com")
	other := newAccount(t, pool, "other@example.com")

	var tokens []string
	for i := 0; i < 3; i++ {
		token, _ := store.Create(ctx, user, "", netip.Addr{})
		tokens = append(tokens, token)
	}
	survivor, _ := store.Create(ctx, other, "", netip.Addr{})

	if err := store.DeleteAllForUser(ctx, user); err != nil {
		t.Fatalf("delete all: %v", err)
	}

	for i, token := range tokens {
		if _, _, err := store.Lookup(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("session %d survived a full revocation", i)
		}
	}
	if _, _, err := store.Lookup(ctx, survivor); err != nil {
		t.Errorf("another user's session was revoked: %v", err)
	}
}

// The sweep must remove exactly the rows the lookup query already ignores, and
// nothing else.
func TestDeleteExpiredRemovesOnlyDeadSessions(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)
	user := newAccount(t, pool, "sweep@example.com")

	live, _ := store.Create(ctx, user, "", netip.Addr{})
	dead, _ := store.Create(ctx, user, "", netip.Addr{})

	deadHash := sha256.Sum256([]byte(dead))
	if _, err := pool.Exec(ctx,
		`UPDATE sessions SET expires_at = now() - interval '1 day' WHERE token_hash = $1`, deadHash[:]); err != nil {
		t.Fatalf("expire: %v", err)
	}

	removed, err := store.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed %d rows, want 1", removed)
	}

	if _, _, err := store.Lookup(ctx, live); err != nil {
		t.Errorf("the sweep removed a live session: %v", err)
	}
}

func TestDeletingAUserRemovesTheirSessions(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewSessionStore(pool)

	user := newAccount(t, pool, "cascade@example.com")
	if _, err := store.Create(ctx, user, "", netip.Addr{}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, user).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("%d sessions survived the account being deleted", count)
	}
}

func TestSessionLifetimeIsAnIdleTimeoutNotAHardCap(t *testing.T) {
	// A documentation test: the constant and the throttle have to stay in a
	// sane relationship, or sessions either never extend or extend on every
	// request.
	if touchInterval >= SessionLifetime {
		t.Fatal("touchInterval must be far shorter than SessionLifetime")
	}
	if touchInterval < time.Minute {
		t.Error("touchInterval is short enough that reads will cause frequent writes")
	}
}
