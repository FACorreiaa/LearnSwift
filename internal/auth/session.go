package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authdb "github.com/FACorreiaa/seshat/internal/auth/db"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

// sessionTokenBytes is 32 bytes = 256 bits of entropy. Guessing one is not a
// threat that needs defending against at any other layer.
const sessionTokenBytes = 32

// newSessionToken returns the token given to the browser, and the digest stored
// in the database.
//
// Only the digest is ever persisted. A leaked backup then yields nothing that
// can be replayed as a cookie.
//
// SHA-256 rather than bcrypt is correct here, and it is the opposite of the
// choice made for passwords. A password is low-entropy and human-chosen, so it
// needs a deliberately slow hash to make guessing expensive. This token is 256
// bits of uniform randomness — there is no guess to slow down — and it is
// verified on every single request, where a quarter-second would be a disaster.
func newSessionToken() (token string, digest []byte) {
	raw := make([]byte, sessionTokenBytes)
	// rand.Read from crypto/rand cannot fail; it panics internally instead.
	_, _ = rand.Read(raw)

	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:]
}

func hashSessionToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type SessionStore struct {
	q *authdb.Queries
}

func NewSessionStore(db authdb.DBTX) *SessionStore {
	return &SessionStore{q: authdb.New(db)}
}

// Create issues a session and returns the token to put in the cookie.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, userAgent string, ip netip.Addr) (string, error) {
	token, digest := newSessionToken()

	params := authdb.CreateSessionParams{
		UserID:    userID,
		TokenHash: digest,
		ExpiresAt: time.Now().Add(SessionLifetime),
	}
	if userAgent != "" {
		params.UserAgent = &userAgent
	}
	if ip.IsValid() {
		params.Ip = &ip
	}

	if _, err := s.q.CreateSession(ctx, params); err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return token, nil
}

// Lookup resolves a cookie value to its user.
//
// Expiry is handled by the query, which excludes expired rows rather than
// deleting them, so a lookup stays a pure read.
func (s *SessionStore) Lookup(ctx context.Context, token string) (User, Session, error) {
	if token == "" {
		return User{}, Session{}, apperr.ErrUnauthenticated
	}

	row, err := s.q.GetSessionWithUser(ctx, hashSessionToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// An unknown token and an expired one are the same answer. There
			// is nothing useful to tell apart, and nothing to gain by telling
			// the holder which it was.
			return User{}, Session{}, apperr.ErrUnauthenticated
		}
		return User{}, Session{}, fmt.Errorf("auth: lookup session: %w", err)
	}

	user := User{ID: row.User.ID, Email: row.User.Email, CreatedAt: row.User.CreatedAt}
	session := Session{
		ID:         row.Session.ID,
		UserID:     row.Session.UserID,
		CreatedAt:  row.Session.CreatedAt,
		LastUsedAt: row.Session.LastUsedAt,
		ExpiresAt:  row.Session.ExpiresAt,
	}
	return user, session, nil
}

// Touch slides the expiry forward, at most once per touchInterval.
//
// A failure here is logged by the caller and otherwise ignored: not extending a
// session is a much smaller problem than refusing a request that was already
// authenticated.
func (s *SessionStore) Touch(ctx context.Context, session Session) error {
	if time.Since(session.LastUsedAt) < touchInterval {
		return nil
	}
	return s.q.TouchSession(ctx, authdb.TouchSessionParams{
		ID:        session.ID,
		ExpiresAt: time.Now().Add(SessionLifetime),
	})
}

func (s *SessionStore) Delete(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.q.DeleteSession(ctx, hashSessionToken(token))
}

// DeleteAllForUser signs a user out everywhere. This is what a password change
// must call: a changed password that leaves old sessions alive has not actually
// revoked anyone's access.
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	return s.q.DeleteSessionsForUser(ctx, userID)
}

// DeleteExpired removes rows the lookup query already ignores, so the table
// does not grow without bound. Safe to run on a schedule.
func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	return s.q.DeleteExpiredSessions(ctx)
}
