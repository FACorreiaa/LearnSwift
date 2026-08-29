package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/FACorreiaa/seshat/internal/auth/user"
)

// These cover the parts of the middleware that need no database: what a request
// with no session does, how a rejection is shaped, and the cookie attributes.
// The lookup path itself is covered in session_test.go against real Postgres.

func TestRequireAuthRejectsARequestWithNoUser(t *testing.T) {
	mw := NewMiddleware(nil, false)
	reached := false

	h := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app", nil))

	if reached {
		t.Fatal("the handler ran for an unauthenticated request")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Errorf("Location = %q, want /login", got)
	}
}

// htmx follows a 303 itself and swaps the result into whatever target the
// request named — so a plain redirect would nest the whole sign-in page inside
// a fragment. HX-Redirect tells it to navigate the window instead.
func TestRequireAuthRedirectsHtmxRequestsWithAHeaderNotAStatus(t *testing.T) {
	mw := NewMiddleware(nil, false)

	h := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("HX-Request", "true")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a 3xx would be followed and swapped by htmx", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/login" {
		t.Errorf("HX-Redirect = %q, want /login", got)
	}
}

func TestRequireAuthAdmitsARequestCarryingAUser(t *testing.T) {
	mw := NewMiddleware(nil, false)
	reached := false

	h := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if got := MustUser(r.Context()); got.Email != "a@example.com" {
			t.Errorf("MustUser returned %+v", got)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req = req.WithContext(ContextWithUser(req.Context(), User{ID: uuid.New(), Email: "a@example.com"}))

	h.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Error("an authenticated request was refused")
	}
}

// LoadUser must never reject. Public pages render differently when signed in,
// so they all pass through it — turning a missing cookie into an error would
// break the landing page for everyone signed out.
func TestLoadUserPassesThroughARequestWithNoCookie(t *testing.T) {
	mw := NewMiddleware(nil, false)
	reached := false

	h := mw.LoadUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		if _, ok := UserFrom(r.Context()); ok {
			t.Error("a user appeared on a request with no session cookie")
		}
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !reached {
		t.Fatal("LoadUser refused a request instead of passing it through")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestTheSessionCookieIsHardenedAndOnlySecureInProduction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secure bool
	}{
		// Secure would stop the cookie being set at all over plain HTTP, which
		// is how development runs.
		{"development", false},
		{"production", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mw := NewMiddleware(nil, tc.secure)

			rec := httptest.NewRecorder()
			mw.SetCookie(rec, "a-token")

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d cookies, want 1", len(cookies))
			}
			c := cookies[0]

			if c.Name != SessionCookieName {
				t.Errorf("name = %q", c.Name)
			}
			if c.Value != "a-token" {
				t.Errorf("value = %q", c.Value)
			}
			if !c.HttpOnly {
				t.Error("the session cookie must be HttpOnly; nothing reads it from JavaScript")
			}
			if c.Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", c.Secure, tc.secure)
			}
			// Strict would withhold the cookie when arriving from any other
			// site, so following a link to a lesson would land signed out.
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}
		})
	}
}

func TestClearCookieExpiresTheSessionImmediately(t *testing.T) {
	mw := NewMiddleware(nil, false)

	rec := httptest.NewRecorder()
	mw.ClearCookie(rec)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]

	if c.Value != "" {
		t.Errorf("value = %q, want empty", c.Value)
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the browser drops it now", c.MaxAge)
	}
	// The attributes have to match the cookie being replaced, or the browser
	// treats it as a different cookie and the old one survives.
	if c.Path != "/" || !c.HttpOnly {
		t.Error("the clearing cookie does not match the attributes of the one it replaces")
	}
}

// The context key is an unexported struct type in the leaf package, so no other
// package can construct it. This checks the guarantee actually holds.
func TestAUserCannotBeForgedOntoAContext(t *testing.T) {
	type impostorKey struct{}

	ctx := context.WithValue(context.Background(), impostorKey{}, User{Email: "attacker@example.com"})
	if _, ok := UserFrom(ctx); ok {
		t.Fatal("a value under a lookalike key was accepted as a signed-in user")
	}

	// A string key — the usual accidental collision — must not work either.
	ctx = context.WithValue(context.Background(), "user", User{Email: "attacker@example.com"}) //nolint:staticcheck // deliberately the wrong key type
	if _, ok := UserFrom(ctx); ok {
		t.Error("a value under a string key was accepted as a signed-in user")
	}
}

func TestMustUserPanicsWhenTheRouteIsNotBehindRequireAuth(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustUser returned a zero user instead of panicking; a mis-mounted route would go unnoticed")
		}
	}()

	MustUser(context.Background())
}

func TestTheLeafPackageAndTheSliceAgreeOnIdentity(t *testing.T) {
	u := User{ID: uuid.New(), Email: "a@example.com"}

	ctx := ContextWithUser(context.Background(), u)

	// Read back through the leaf package, which is what templates use.
	got, ok := user.From(ctx)
	if !ok {
		t.Fatal("a user set by the auth slice is invisible to the leaf package templates read through")
	}
	if got.ID != u.ID {
		t.Errorf("ID = %v, want %v", got.ID, u.ID)
	}
}
