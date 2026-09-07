package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
)

// echoUser is the protected handler: it reports whoever RequireToken resolved,
// so a test can tell "let through as nobody" from "let through as Alice".
func echoUser(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("no user in context"))
		return
	}
	_, _ = w.Write([]byte(user.ID.String()))
}

func bearerRequest(t *testing.T, h http.Handler, header string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAValidBearerTokenIdentifiesItsOwner(t *testing.T) {
	pool := testdb.New(t)
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "bearer-ok@example.com")

	token, _, err := store.Create(context.Background(), user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := NewBearerMiddleware(store).RequireToken(http.HandlerFunc(echoUser))
	rec := bearerRequest(t, h, "Bearer "+token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != user.String() {
		t.Errorf("resolved to %q, want %q", got, user)
	}
}

// RFC 7235 makes the scheme case-insensitive, and a client that sends "bearer"
// is not wrong.
func TestTheBearerSchemeIsCaseInsensitive(t *testing.T) {
	pool := testdb.New(t)
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "bearer-case@example.com")

	token, _, err := store.Create(context.Background(), user, "laptop")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := NewBearerMiddleware(store).RequireToken(http.HandlerFunc(echoUser))
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		if code := bearerRequest(t, h, scheme+" "+token).Code; code != http.StatusOK {
			t.Errorf("scheme %q: status = %d, want 200", scheme, code)
		}
	}
}

// The endpoint sits outside the CSRF middleware, so it must not accept the
// credential that middleware protects. A cookie reaching it would make it
// callable from a page in another tab.
func TestASessionCookieDoesNotAuthenticateTheTokenEndpoint(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	tokens := NewTokenStore(pool)
	sessions := NewSessionStore(pool)
	user := newAccount(t, pool, "bearer-cookie@example.com")

	sessionToken, err := sessions.Create(ctx, user, "", netipInvalid())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := NewBearerMiddleware(tokens).RequireToken(http.HandlerFunc(echoUser))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionToken})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a session cookie authenticated a token endpoint: status = %d", rec.Code)
	}
}

func TestEveryRefusalLooksTheSame(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	store := NewTokenStore(pool)
	user := newAccount(t, pool, "bearer-bad@example.com")

	revoked, tok, err := store.Create(ctx, user, "revoked")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Delete(ctx, tok.ID, user); err != nil {
		t.Fatalf("delete: %v", err)
	}

	h := NewBearerMiddleware(store).RequireToken(http.HandlerFunc(echoUser))

	var bodies []string
	for _, tc := range []struct{ name, header string }{
		{"no header", ""},
		{"no scheme", "seshat_pat_whatever"},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"empty credential", "Bearer "},
		{"unknown token", "Bearer " + APITokenPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"revoked token", "Bearer " + revoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := bearerRequest(t, h, tc.header)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got == "" {
				t.Error("no WWW-Authenticate header on a 401")
			}
			bodies = append(bodies, rec.Body.String())
		})
	}

	// The point of the table: not one of these may be distinguishable from
	// the others, or the response becomes an oracle for which guesses were
	// once real credentials.
	for _, got := range bodies {
		if got != bodies[0] {
			t.Errorf("refusals differ: %q vs %q", got, bodies[0])
		}
	}
}

// The body must not name the token, the user, or the reason. A 401 that quotes
// the credential back is a 401 that puts it in a log.
func TestARefusalNeverEchoesTheCredential(t *testing.T) {
	pool := testdb.New(t)
	store := NewTokenStore(pool)

	const secret = APITokenPrefix + "SUPERSECRETVALUE"
	h := NewBearerMiddleware(store).RequireToken(http.HandlerFunc(echoUser))
	rec := bearerRequest(t, h, "Bearer "+secret)

	if strings.Contains(rec.Body.String(), secret) {
		t.Error("the 401 body echoed the token")
	}
	for key, values := range rec.Header() {
		for _, value := range values {
			if strings.Contains(value, secret) {
				t.Errorf("the token was echoed in header %q", key)
			}
		}
	}
}
