package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	CSRFCookieName = "seshat_csrf"
	CSRFFieldName  = "csrf_token"
	CSRFHeaderName = "X-CSRF-Token"

	// csrfMultipartMemory bounds what ParseMultipartForm buffers while looking
	// for the token. The route's own MaxBody still caps the request overall;
	// this only decides how much of it is held in memory rather than spilled
	// to a temporary file.
	csrfMultipartMemory = 8 << 20
)

type csrfCtxKey struct{}

// CSRF implements the double-submit cookie pattern: a random token is stored in
// a cookie and must be echoed back in the request body or a header. An attacker
// on another origin can cause the browser to *send* the cookie but cannot read
// it, so they cannot produce the matching copy.
//
// The cookie is HttpOnly. Nothing in the page needs to read it with JavaScript,
// because every form is rendered server-side with the token already in a hidden
// field, and htmx requests carry it in a header set from the same value.
func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := csrfCookie(r)
			if token == "" {
				token = rand.Text()
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
				})
			}

			if !safeMethod(r.Method) {
				sent := requestCSRFToken(r)
				// subtle.ConstantTimeCompare to keep the comparison from
				// leaking how much of the token was correct via timing.
				if sent == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(token)) != 1 {
					FromContext(r.Context()).Warn("csrf token mismatch")
					http.Error(w, "invalid CSRF token", http.StatusForbidden)
					return
				}
			}

			ctx := context.WithValue(r.Context(), csrfCtxKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFToken returns the token for this request, for rendering into a form.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfCtxKey{}).(string)
	return token
}

func safeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

func csrfCookie(r *http.Request) string {
	c, err := r.Cookie(CSRFCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func requestCSRFToken(r *http.Request) string {
	if v := r.Header.Get(CSRFHeaderName); v != "" {
		return v
	}

	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(csrfMultipartMemory); err != nil {
			return ""
		}
	default:
		if err := r.ParseForm(); err != nil {
			return ""
		}
	}
	return r.PostFormValue(CSRFFieldName)
}
