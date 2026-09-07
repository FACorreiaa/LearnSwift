package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

// The property the whole scheme rests on, and the same one the session store
// has to satisfy: a stolen database gives an attacker nothing they can present
// as a credential.
func TestOnlyTheHashOfAnAPITokenIsStored(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)

	user := newAccount(t, pool, "token-hash@example.com")
	token, _, err := store.Create(ctx, user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT token_hash FROM api_token WHERE user_id = $1`, user).Scan(&stored); err != nil {
		t.Fatalf("read token: %v", err)
	}

	if string(stored) == token {
		t.Fatal("the raw token is in the database")
	}
	if got := sha256.Sum256([]byte(token)); string(stored) != string(got[:]) {
		t.Error("the stored value is not the SHA-256 of the token")
	}

	// Nothing else in the row may hold it either — the label is learner-supplied
	// and rendered, and a token pasted in as a name would then be on a page.
	var label string
	if err := pool.QueryRow(ctx, `SELECT label FROM api_token WHERE user_id = $1`, user).Scan(&label); err != nil {
		t.Fatalf("read label: %v", err)
	}
	if strings.Contains(label, token) {
		t.Error("the token leaked into its own label")
	}
}

// The prefix is what makes the value recognisable to a learner and to a secret
// scanner. Both stop working if it drifts.
func TestAnIssuedTokenIsMarkedAsOne(t *testing.T) {
	pool := testdb.New(t)
	store := NewTokenStore(pool)

	user := newAccount(t, pool, "token-prefix@example.com")
	token, _, err := store.Create(context.Background(), user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if !strings.HasPrefix(token, APITokenPrefix) {
		t.Errorf("token %q does not start with %q", token, APITokenPrefix)
	}
	// Long enough that the prefix is not most of it.
	if len(token) < len(APITokenPrefix)+40 {
		t.Errorf("token %q is shorter than 256 bits of entropy would produce", token)
	}
}

func TestAFreshTokenAuthenticatesItsOwner(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)

	user := newAccount(t, pool, "token-ok@example.com")
	token, created, err := store.Create(ctx, user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, tok, err := store.Lookup(ctx, token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != user {
		t.Errorf("resolved to user %s, want %s", got.ID, user)
	}
	if tok.ID != created.ID {
		t.Errorf("resolved to token %s, want %s", tok.ID, created.ID)
	}
	if tok.Label != "laptop" {
		t.Errorf("label = %q, want %q", tok.Label, "laptop")
	}
}

// Every rejection has to be the same rejection. Telling the holder of a
// credential which kind of wrong it is tells an attacker which of their guesses
// was once real.
func TestEveryBadTokenIsRejectedIdentically(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	sessions := NewSessionStore(pool)

	user := newAccount(t, pool, "token-bad@example.com")

	revoked, revokedTok, err := store.Create(ctx, user, "revoked")
	if err != nil {
		t.Fatalf("create revoked: %v", err)
	}
	if delErr := store.Delete(ctx, revokedTok.ID, user); delErr != nil {
		t.Fatalf("delete: %v", delErr)
	}

	expired, _, err := store.Create(ctx, user, "expired")
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if _, expireErr := pool.Exec(ctx,
		`UPDATE api_token SET expires_at = now() - interval '1 second' WHERE label = 'expired'`); expireErr != nil {
		t.Fatalf("expire: %v", expireErr)
	}

	// A session cookie is a valid credential for the browser and must be
	// worthless here: two credential types that authenticate each other's
	// requests are one credential type with two names.
	sessionToken, err := sessions.Create(ctx, user, "", netipInvalid())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"unprefixed junk", "nonsense"},
		{"well-formed but unknown", APITokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"revoked", revoked},
		{"expired", expired},
		{"a session token", sessionToken},
		{"a session token wearing the prefix", APITokenPrefix + sessionToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := store.Lookup(ctx, tc.token); !apperrIsUnauthenticated(err) {
				t.Errorf("error = %v, want %v", err, apperr.ErrUnauthenticated)
			}
		})
	}
}

// Without the user_id in the delete, knowing any token's id would be enough to
// revoke somebody else's.
func TestATokenCanOnlyBeRevokedByItsOwner(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)

	alice := newAccount(t, pool, "revoke-alice@example.com")
	bob := newAccount(t, pool, "revoke-bob@example.com")

	token, tok, err := store.Create(ctx, alice, "alice's laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.Delete(ctx, tok.ID, bob); err == nil {
		t.Fatal("Bob revoked Alice's token")
	}
	if _, _, err := store.Lookup(ctx, token); err != nil {
		t.Fatalf("Alice's token stopped working: %v", err)
	}

	if err := store.Delete(ctx, tok.ID, alice); err != nil {
		t.Fatalf("Alice could not revoke her own token: %v", err)
	}
	if _, _, err := store.Lookup(ctx, token); !apperrIsUnauthenticated(err) {
		t.Error("revocation was not immediate")
	}
}

// Listing is the whole management page, and it must not be a way to read
// somebody else's tokens.
func TestListingShowsOnlyYourOwnTokens(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)

	alice := newAccount(t, pool, "list-alice@example.com")
	bob := newAccount(t, pool, "list-bob@example.com")

	if _, _, err := store.Create(ctx, alice, "alice one"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := store.Create(ctx, bob, "bob one"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.List(ctx, alice)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tokens, want 1 — Bob's may have leaked in", len(got))
	}
	if got[0].Label != "alice one" {
		t.Errorf("label = %q, want %q", got[0].Label, "alice one")
	}
}

func TestATokenNeedsAUsableName(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "label@example.com")

	for _, name := range []string{"", "   ", "\t\n"} {
		if _, _, err := store.Create(ctx, user, name); err == nil {
			t.Errorf("a token was created with the name %q", name)
		}
	}

	if _, _, err := store.Create(ctx, user, strings.Repeat("x", maxTokenLabel+1)); err == nil {
		t.Error("a token was created with an oversized name")
	}

	// A control character would be rendered on the management page. Stripped
	// rather than refused: the name is otherwise fine.
	_, tok, err := store.Create(ctx, user, "laptop\x00\x1b[31m")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.Label != "laptop[31m" {
		t.Errorf("label = %q, want the control characters removed", tok.Label)
	}
}

func TestExpiredTokensAreSweptAway(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "sweep@example.com")

	if _, _, err := store.Create(ctx, user, "live"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := store.Create(ctx, user, "dead"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE api_token SET expires_at = now() - interval '1 day' WHERE label = 'dead'`); err != nil {
		t.Fatalf("expire: %v", err)
	}

	n, err := store.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("swept %d rows, want 1", n)
	}

	left, err := store.List(ctx, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 1 || left[0].Label != "live" {
		t.Errorf("sweep removed the wrong row: %+v", left)
	}
}

func TestDeletingAUserRemovesTheirTokens(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "cascade@example.com")

	token, _, err := store.Create(ctx, user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	if _, _, err := store.Lookup(ctx, token); !apperrIsUnauthenticated(err) {
		t.Errorf("a deleted user's token still authenticates: %v", err)
	}
}

// Expired reports on a token the lookup would already refuse, so the management
// page can say why one stopped working rather than showing it as healthy.
func TestExpiredIsReportedOnTheManagementView(t *testing.T) {
	live := APIToken{ExpiresAt: time.Now().Add(time.Hour)}
	dead := APIToken{ExpiresAt: time.Now().Add(-time.Second)}

	if live.Expired() {
		t.Error("a token valid for another hour reported as expired")
	}
	if !dead.Expired() {
		t.Error("a token that expired a second ago reported as live")
	}
}

func TestTouchIsThrottled(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "touch@example.com")

	_, tok, err := store.Create(ctx, user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Just created, so within touchInterval: this must not write.
	if err := store.Touch(ctx, tok); err != nil {
		t.Fatalf("touch: %v", err)
	}

	// Backdate the in-memory copy past the throttle and it should write.
	tok.LastUsedAt = time.Now().Add(-2 * touchInterval)
	if err := store.Touch(ctx, tok); err != nil {
		t.Fatalf("touch after the interval: %v", err)
	}

	var lastUsed time.Time
	if err := pool.QueryRow(ctx, `SELECT last_used_at FROM api_token WHERE id = $1`, tok.ID).Scan(&lastUsed); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	if time.Since(lastUsed) > time.Minute {
		t.Errorf("last_used_at = %v, want it moved to now", lastUsed)
	}
}

func TestAnUnknownTokenIdIsNotFoundRatherThanSilentlyIgnored(t *testing.T) {
	pool := testdb.New(t)
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "missing@example.com")

	if err := store.Delete(context.Background(), uuid.New(), user); err == nil {
		t.Error("revoking a token that does not exist reported success")
	}
}

// Small helpers, kept at the bottom so the tests above read as prose.

func apperrIsUnauthenticated(err error) bool {
	return errors.Is(err, apperr.ErrUnauthenticated)
}

// netipInvalid is the zero address, which the session store treats as "not
// recorded". Spelled out so the call site reads as a deliberate omission.
func netipInvalid() netip.Addr { return netip.Addr{} }
