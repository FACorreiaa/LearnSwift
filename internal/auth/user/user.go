// Package user holds the identity of a signed-in visitor, and the only way to
// put one on a request context or read it back.
//
// It is a leaf: it imports nothing from the rest of the application. Both the
// auth slice and the templates that render a signed-in header depend on it,
// which is what keeps them from having to depend on each other.
package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User deliberately has no password field. A hash that is never loaded into the
// type it travels with cannot be logged, rendered into a template, or returned
// from a handler by accident.
type User struct {
	ID        uuid.UUID
	Email     string
	CreatedAt time.Time
}

// contextKey is an unexported struct type, so no package anywhere can construct
// the key. That is what makes From trustworthy: some other middleware cannot
// plant a user under a string key that happens to collide with this one.
type contextKey struct{}

func NewContext(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}

func From(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(contextKey{}).(User)
	return u, ok
}

// Must is for handlers mounted behind RequireAuth, where a missing user is a
// routing mistake rather than a runtime condition — so it should surface loudly
// during development instead of as a nil check in every handler.
func Must(ctx context.Context) User {
	u, ok := From(ctx)
	if !ok {
		panic("auth: no user on context; is this route behind RequireAuth?")
	}
	return u
}
