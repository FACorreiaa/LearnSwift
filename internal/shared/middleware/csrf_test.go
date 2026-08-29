package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// csrfHarness wires the middleware to a handler that records whether it ran, so
// each test can assert on the thing that actually matters: did the request get
// through?
func csrfHarness(secure bool) (http.Handler, *bool) {
	reached := false
	h := CSRF(secure)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_, _ = w.Write([]byte(CSRFToken(r.Context())))
	}))
	return h, &reached
}

// issuedToken mints a token the way a real visitor gets one: by making a GET
// first.
//
// That GET reaches the handler, so it sets the harness's flag. Resetting it
// here is not tidiness — without it every "was the handler reached" assertion
// afterwards is answered by this GET rather than by the request under test, and
// the rejection tests pass no matter what the middleware does.
func issuedToken(t *testing.T, h http.Handler, reached *bool) string {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			*reached = false
			return c.Value
		}
	}
	t.Fatal("no CSRF cookie was issued on a GET")
	return ""
}

func TestAGetRequestIsIssuedATokenAndPassesThrough(t *testing.T) {
	h, reached := csrfHarness(false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !*reached {
		t.Fatal("a safe method must reach the handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	token := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name != CSRFCookieName {
			continue
		}
		token = c.Value
		if !c.HttpOnly {
			t.Error("the CSRF cookie must be HttpOnly; nothing in the page reads it with JavaScript")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v, want Lax", c.SameSite)
		}
	}
	if token == "" {
		t.Fatal("no CSRF cookie was issued")
	}

	// The handler must be able to render the same token into a form, or every
	// form it produces would be rejected on submit.
	if body := rec.Body.String(); body != token {
		t.Errorf("token on context = %q, want the cookie value %q", body, token)
	}
}

func TestAPostWithNoTokenIsRejected(t *testing.T) {
	h, reached := csrfHarness(false)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if *reached {
		t.Fatal("the handler must not run for an unverified state-changing request")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// This is the attack the double-submit pattern exists to stop. A form on
// another origin causes the browser to send our cookie, but the attacker cannot
// read it, so the body they control carries the wrong value.
func TestAPostWithACookieButAWrongBodyTokenIsRejected(t *testing.T) {
	h, reached := csrfHarness(false)
	token := issuedToken(t, h, reached)

	form := url.Values{CSRFFieldName: {"not-the-token"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if *reached {
		t.Fatal("a mismatched token must not reach the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAPostWithAMatchingFormFieldIsAccepted(t *testing.T) {
	h, reached := csrfHarness(false)
	token := issuedToken(t, h, reached)

	form := url.Values{CSRFFieldName: {token}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !*reached {
		t.Fatalf("a matching token must reach the handler; status = %d", rec.Code)
	}
}

// htmx sends the token as a header rather than a field, because it posts a
// serialised form it did not necessarily build from our HTML.
func TestAPostWithAMatchingHeaderIsAccepted(t *testing.T) {
	h, reached := csrfHarness(false)
	token := issuedToken(t, h, reached)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(CSRFHeaderName, token)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !*reached {
		t.Fatalf("a matching header must reach the handler; status = %d", rec.Code)
	}
}

func TestAMultipartPostCarriesItsTokenInTheBody(t *testing.T) {
	h, reached := csrfHarness(false)
	token := issuedToken(t, h, reached)

	var body strings.Builder
	const boundary = "seshatboundary"
	body.WriteString("--" + boundary + "\r\n")
	body.WriteString(`Content-Disposition: form-data; name="` + CSRFFieldName + "\"\r\n\r\n")
	body.WriteString(token + "\r\n")
	body.WriteString("--" + boundary + "--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !*reached {
		t.Fatalf("a multipart post with a matching token must reach the handler; status = %d", rec.Code)
	}
}

func TestTheCookieIsOnlyMarkedSecureInProduction(t *testing.T) {
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
			h, _ := csrfHarness(tc.secure)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			for _, c := range rec.Result().Cookies() {
				if c.Name == CSRFCookieName && c.Secure != tc.secure {
					t.Errorf("Secure = %v, want %v", c.Secure, tc.secure)
				}
			}
		})
	}
}
