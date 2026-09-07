package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	authpages "github.com/FACorreiaa/seshat/web/auth"
)

// postForm submits a login the way a browser would. The rate limiting under
// test sits in front of the service, so these need no database.
func postForm(t *testing.T, h http.Handler, path, email, password, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"email": {email}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newTestHandler builds a Handler whose auth call is a stub, so a test can
// decide what "the credentials were wrong" means without a user table.
func newTestHandler(result error) (*Handler, *int) {
	calls := 0
	h := NewHandler(nil, &Middleware{}, nil, nil, "http://localhost")

	// Replace the routes with ones bound to the stub. This mirrors what
	// Routes(r) wires up, minus the service.
	stub := func(ctx context.Context, c Credentials) (User, string, error) {
		calls++
		if result != nil {
			return User{}, "", result
		}
		return User{Email: c.Email}, "session-token", nil
	}
	h.authOverride = stub
	return h, &calls
}

func loginHandler(h *Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.submit(w, r, h.authOverride, h.logins, authpages.LoginForm, authpages.LoginPage, nil)
	}
}

// The point of the limiter: guessing must stop being viable long before it
// succeeds.
func TestRepeatedFailedLoginsAreEventuallyRefused(t *testing.T) {
	h, calls := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 1; i <= loginAttemptLimit; i++ {
		rec := postForm(t, handler, "/login", "a@example.com", "wrong", "203.0.113.5:1000")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status = %d, want 422", i, rec.Code)
		}
	}

	rec := postForm(t, handler, "/login", "a@example.com", "wrong", "203.0.113.5:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once over the limit", rec.Code)
	}

	// The service must not be consulted at all for a refused attempt —
	// otherwise the limiter is not saving the expensive bcrypt comparison it
	// exists to protect.
	if *calls != loginAttemptLimit {
		t.Errorf("the auth service was called %d times, want %d — refused attempts reached it", *calls, loginAttemptLimit)
	}
}

func TestARefusedLoginSaysWhenToRetry(t *testing.T) {
	h, _ := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 0; i <= loginAttemptLimit; i++ {
		postForm(t, handler, "/login", "a@example.com", "wrong", "203.0.113.5:1000")
	}
	rec := postForm(t, handler, "/login", "a@example.com", "wrong", "203.0.113.5:1000")

	retry := rec.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("no Retry-After header on a 429")
	}
	secs, err := strconv.Atoi(retry)
	if err != nil {
		t.Fatalf("Retry-After = %q, want a whole number of seconds", retry)
	}
	// Zero would read as "retry immediately", which is the opposite of what a
	// rate limit means.
	if secs < 1 {
		t.Errorf("Retry-After = %d, want at least 1", secs)
	}

	if body := rec.Body.String(); !strings.Contains(body, "Too many attempts") {
		t.Error("the response does not tell the visitor what happened")
	}
}

// The limit must not leak whether an address is registered — the refusal has to
// look the same either way.
func TestARateLimitedResponseDoesNotRevealWhetherTheAccountExists(t *testing.T) {
	h, _ := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 0; i <= loginAttemptLimit+1; i++ {
		postForm(t, handler, "/login", "real@example.com", "wrong", "203.0.113.5:1000")
	}
	rec := postForm(t, handler, "/login", "real@example.com", "wrong", "203.0.113.5:1000")

	for _, leak := range []string{"exists", "not found", "no such", "registered"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
			t.Errorf("the 429 body contains %q, which hints at account existence", leak)
		}
	}
}

// One account being hammered must not lock out an unrelated visitor who happens
// to be signing in at the same time.
func TestOneAccountBeingAttackedDoesNotLockOutAnother(t *testing.T) {
	h, _ := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 0; i <= loginAttemptLimit+2; i++ {
		postForm(t, handler, "/login", "victim@example.com", "wrong", "198.51.100.7:1000")
	}

	// A different person, a different address, from a different IP.
	rec := postForm(t, handler, "/login", "bystander@example.com", "wrong", "203.0.113.9:1000")
	if rec.Code == http.StatusTooManyRequests {
		t.Error("an unrelated account was rate limited by someone else's attempts")
	}
}

// Rotating the source address must not reset the count for one account, or the
// limit is trivially bypassed by anyone with a proxy pool.
func TestChangingIPDoesNotResetThePerAccountLimit(t *testing.T) {
	h, _ := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 0; i <= loginAttemptLimit; i++ {
		postForm(t, handler, "/login", "victim@example.com", "wrong", "203.0.113."+strconv.Itoa(i%200+1)+":1000")
	}

	rec := postForm(t, handler, "/login", "victim@example.com", "wrong", "192.0.2.222:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — rotating the client IP bypassed the per-account limit", rec.Code)
	}
}

// Addresses are compared case-insensitively everywhere else, so the limiter
// must not treat a different capitalisation as a fresh account.
func TestTheLimitIsCaseInsensitiveOnTheAddress(t *testing.T) {
	h, _ := newTestHandler(apperr.FieldErrors{}.Add("form", "no"))
	handler := loginHandler(h)

	for i := 0; i <= loginAttemptLimit; i++ {
		postForm(t, handler, "/login", "victim@example.com", "wrong", "203.0.113.5:1000")
	}

	rec := postForm(t, handler, "/login", "VICTIM@Example.COM", "wrong", "203.0.113.5:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — changing capitalisation bypassed the limit", rec.Code)
	}
}

// Someone who mistypes a few times and then succeeds must not carry those
// attempts around for the rest of the window.
func TestASuccessfulLoginClearsTheCount(t *testing.T) {
	h, _ := newTestHandler(nil) // every call succeeds
	handler := loginHandler(h)

	// Well past the limit, but all succeeding.
	for i := 0; i <= loginAttemptLimit+5; i++ {
		rec := postForm(t, handler, "/login", "a@example.com", "right", "203.0.113.5:1000")
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was rate limited despite every attempt succeeding", i+1)
		}
	}
}
