package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/FACorreiaa/seshat/internal/auth/user"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"github.com/FACorreiaa/seshat/internal/shared/htmx"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
)

const SessionCookieName = "seshat_session"

type Middleware struct {
	sessions *SessionStore
	secure   bool
}

func NewMiddleware(sessions *SessionStore, secure bool) *Middleware {
	return &Middleware{sessions: sessions, secure: secure}
}

// LoadUser attaches the signed-in user when there is one, and otherwise does
// nothing at all. It never rejects a request — that is RequireAuth's job — so
// public pages can render differently for a signed-in visitor without every
// route having to opt in.
func (m *Middleware) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		user, session, err := m.sessions.Lookup(r.Context(), cookie.Value)
		if err != nil {
			if apperr.Is(err, apperr.ErrUnauthenticated) {
				// The cookie names a session that no longer exists. Clearing it
				// stops the browser presenting it on every subsequent request.
				m.ClearCookie(w)
			} else {
				middleware.FromContext(r.Context()).Error("session lookup failed", slog.Any("error", err))
			}
			next.ServeHTTP(w, r)
			return
		}

		// Best effort: a session that fails to slide forward is still valid,
		// and refusing the request over it would be absurd.
		if err := m.sessions.Touch(r.Context(), session); err != nil {
			middleware.FromContext(r.Context()).Warn("could not extend session", slog.Any("error", err))
		}

		next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
	})
}

// RequireAuth rejects anyone LoadUser did not recognise.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			// htmx.Redirect picks HX-Redirect over a 303 for an htmx request:
			// htmx would otherwise follow the redirect itself and swap the
			// whole sign-in page into whatever fragment target was named.
			htmx.Redirect(w, r, "/login")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ContextWithUser(ctx context.Context, u User) context.Context {
	return user.NewContext(ctx, u)
}

func UserFrom(ctx context.Context) (User, bool) {
	return user.From(ctx)
}

func MustUser(ctx context.Context) User {
	return user.Must(ctx)
}

func (m *Middleware) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		// Lax rather than Strict on purpose. Strict withholds the cookie when
		// arriving from any other site, so a visitor following a link to a
		// lesson from anywhere else would land signed out for one request and
		// conclude the session had dropped.
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(SessionLifetime),
	})
}

func (m *Middleware) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
