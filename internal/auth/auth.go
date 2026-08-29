// Package auth owns identity: who a visitor is, and how that survives from one
// request to the next.
package auth

import (
	"time"

	"github.com/google/uuid"

	"github.com/FACorreiaa/seshat/internal/auth/user"
)

// User is re-exported so callers inside this slice can keep saying auth.User
// while the type itself lives in the leaf package that templates can import.
type User = user.User

type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

const (
	// SessionLifetime is how long a session lasts without use. It slides
	// forward on activity, so this is an idle timeout rather than a hard cap.
	SessionLifetime = 30 * 24 * time.Hour

	// touchInterval throttles the sliding-expiry write.
	//
	// Without it every authenticated request issues an UPDATE, which turns a
	// read-only page view into a write and makes the sessions table the busiest
	// one in the database. An hour of granularity costs nothing: the lifetime
	// is measured in weeks.
	touchInterval = time.Hour
)
