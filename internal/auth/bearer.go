package auth

import (
	"log/slog"
	"net/http"
	"strings"

	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
)

// BearerMiddleware authenticates a non-browser client from an Authorization
// header.
//
// Kept apart from Middleware rather than folded into it, because the two are
// opposites in the way that matters. The browser's LoadUser never rejects a
// request: a lesson has to render for a stranger. This one rejects anything it
// does not recognise, because there is no such thing as a public MCP tool call
// here — every tool either reads a learner's progress or writes to it.
//
// It also deliberately ignores cookies. An endpoint that accepts a session
// cookie is an endpoint reachable from a page in another tab, which is exactly
// what the CSRF middleware exists to prevent on every browser route. This one
// sits outside that middleware, so it must not accept the credential it
// protects.
type BearerMiddleware struct {
	tokens *TokenStore
}

func NewBearerMiddleware(tokens *TokenStore) *BearerMiddleware {
	return &BearerMiddleware{tokens: tokens}
}

// RequireToken resolves a bearer token to its user, or refuses the request.
//
// Every refusal is the same refusal: 401, one word, and a WWW-Authenticate
// header. An unknown token, an expired one, a revoked one, and a malformed
// header are indistinguishable from outside, because there is nothing useful to
// tell apart and nothing to gain by telling the holder which it was.
func (m *BearerMiddleware) RequireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w)
			return
		}

		user, apiToken, err := m.tokens.Lookup(r.Context(), token)
		if err != nil {
			// An outage is logged and still refused. Letting a request through
			// because the database was unreachable would turn a bad minute
			// into an open door.
			if !apperr.Is(err, apperr.ErrUnauthenticated) {
				middleware.FromContext(r.Context()).Error("api token lookup failed", slog.Any("error", err))
			}
			unauthorized(w)
			return
		}

		// Best effort. A token whose last-used stamp fails to move is still a
		// valid token, and refusing an authenticated request over bookkeeping
		// would be absurd.
		if err := m.tokens.Touch(r.Context(), apiToken); err != nil {
			middleware.FromContext(r.Context()).Warn("could not touch api token", slog.Any("error", err))
		}

		next.ServeHTTP(w, r.WithContext(ContextWithUser(r.Context(), user)))
	})
}

// bearerToken pulls the credential out of an Authorization header.
//
// The scheme is compared case-insensitively because RFC 7235 says it is
// case-insensitive, and a client that sends "bearer" is not wrong.
func bearerToken(header string) (string, bool) {
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func unauthorized(w http.ResponseWriter) {
	// The realm names the credential a client should go and get, which is the
	// one genuinely useful thing to say here.
	w.Header().Set("WWW-Authenticate", `Bearer realm="seshat"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// Written directly rather than through a marshaller: a fixed string cannot
	// fail to encode, and a 401 is the wrong place to discover it could.
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
