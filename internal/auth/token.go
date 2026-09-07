package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authdb "github.com/FACorreiaa/seshat/internal/auth/db"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

const (
	// apiTokenBytes matches sessionTokenBytes: 256 bits of entropy, so guessing
	// a token is not a threat that needs defending against at another layer.
	apiTokenBytes = 32

	// APITokenPrefix marks the string as what it is. Two audiences read it: a
	// learner who needs to recognise the value they pasted into a config file,
	// and the secret scanners that watch public repositories for exactly this
	// shape. Both are served by the prefix being boring and fixed.
	APITokenPrefix = "seshat_pat_"

	// APITokenLifetime is deliberately shorter than a session's, and for the
	// opposite reason. A session slides forward on use because signing a
	// learner out of a lesson they are mid-way through is hostile. A token
	// lives in a config file on a machine, so the interesting risk is the one
	// nobody remembers to revoke, and an expiry is what eventually closes it.
	APITokenLifetime = 90 * 24 * time.Hour

	// maxTokenLabel bounds what goes on the management page. Long enough to
	// say "laptop, Claude Code"; short enough not to be a place to store
	// paragraphs.
	maxTokenLabel = 60
)

// newAPIToken returns the token handed to the learner, and the digest stored in
// the database.
//
// Only the digest is ever persisted, exactly as for sessions: a leaked backup
// then yields nothing that can be replayed as a credential. SHA-256 rather than
// bcrypt for the same reason as there — this is 256 bits of uniform randomness,
// so there is no guess to slow down, and it is verified on every request.
//
// The digest covers the prefixed string. Hashing the random part alone would
// mean two different strings authenticating the same row, which is one more
// thing to be careful about for no gain.
func newAPIToken() (token string, digest []byte) {
	raw := make([]byte, apiTokenBytes)
	// rand.Read from crypto/rand cannot fail; it panics internally instead.
	_, _ = rand.Read(raw)

	token = APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, sum[:]
}

func hashAPIToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// APIToken is one credential, without its secret. There is no field for the
// token itself because no row ever holds one: a struct that cannot carry the
// plaintext cannot accidentally render it.
type APIToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Label      string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

// Expired reports whether this token would still be accepted. The lookup query
// already excludes expired rows; this is for the management page, which lists
// them so a learner can see why one stopped working.
func (t APIToken) Expired() bool { return !t.ExpiresAt.After(time.Now()) }

type TokenStore struct {
	q *authdb.Queries
}

func NewTokenStore(db authdb.DBTX) *TokenStore {
	return &TokenStore{q: authdb.New(db)}
}

// Create issues a token and returns the plaintext, which is the only time it
// exists. The caller must show it once and then forget it.
func (s *TokenStore) Create(ctx context.Context, userID uuid.UUID, label string) (string, APIToken, error) {
	label, err := cleanTokenLabel(label)
	if err != nil {
		return "", APIToken{}, err
	}

	token, digest := newAPIToken()

	row, err := s.q.CreateAPIToken(ctx, authdb.CreateAPITokenParams{
		UserID:    userID,
		TokenHash: digest,
		Label:     label,
		ExpiresAt: time.Now().Add(APITokenLifetime),
	})
	if err != nil {
		return "", APIToken{}, fmt.Errorf("auth: create api token: %w", err)
	}

	return token, toAPIToken(row), nil
}

// Lookup resolves a bearer token to its user.
//
// Every rejection is the same rejection. An unknown token, an expired one, and
// one revoked a second ago are indistinguishable from the outside, because
// there is nothing useful to tell apart and nothing to gain by telling the
// holder of a credential which kind of wrong it is.
func (s *TokenStore) Lookup(ctx context.Context, token string) (User, APIToken, error) {
	// Checked before the query so that a stray cookie value, a copied session
	// token, or an empty header never reaches the database as a lookup.
	if !strings.HasPrefix(token, APITokenPrefix) {
		return User{}, APIToken{}, apperr.ErrUnauthenticated
	}

	row, err := s.q.GetAPITokenWithUser(ctx, hashAPIToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, APIToken{}, apperr.ErrUnauthenticated
		}
		return User{}, APIToken{}, fmt.Errorf("auth: lookup api token: %w", err)
	}

	user := User{ID: row.User.ID, Email: row.User.Email, CreatedAt: row.User.CreatedAt}
	return user, toAPIToken(row.ApiToken), nil
}

// Touch records that a token was used, at most once per touchInterval.
//
// Unlike a session's touch this does not extend anything — the expiry is fixed
// at creation. It exists so the management page can answer "is this the one I
// set up on the old laptop", which is the question that makes a token safe to
// revoke.
//
// A failure is the caller's to log and otherwise ignore: refusing a request
// that was already authenticated because a bookkeeping write failed would be a
// far worse outcome than a stale timestamp.
func (s *TokenStore) Touch(ctx context.Context, token APIToken) error {
	if time.Since(token.LastUsedAt) < touchInterval {
		return nil
	}
	return s.q.TouchAPIToken(ctx, token.ID)
}

func (s *TokenStore) List(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	rows, err := s.q.ListAPITokensForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list api tokens: %w", err)
	}

	out := make([]APIToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, APIToken{
			ID:         row.ID,
			UserID:     row.UserID,
			Label:      row.Label,
			CreatedAt:  row.CreatedAt,
			LastUsedAt: row.LastUsedAt,
			ExpiresAt:  row.ExpiresAt,
		})
	}
	return out, nil
}

// Delete revokes a token. Effective immediately: nothing caches a lookup, so
// there is no window in which a deleted token still authenticates.
//
// Scoped by user as well as id, so knowing another learner's token id is not
// enough to revoke it. A row that was not theirs reports ErrNotFound rather
// than success, because silently doing nothing would tell them their token was
// revoked when it was not.
func (s *TokenStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := s.q.DeleteAPIToken(ctx, authdb.DeleteAPITokenParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("auth: delete api token: %w", err)
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// DeleteExpired removes rows the lookup already ignores, so the table does not
// grow without bound. Safe to run on a schedule.
func (s *TokenStore) DeleteExpired(ctx context.Context) (int64, error) {
	return s.q.DeleteExpiredAPITokens(ctx)
}

func toAPIToken(row authdb.ApiToken) APIToken {
	return APIToken{
		ID:         row.ID,
		UserID:     row.UserID,
		Label:      row.Label,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt,
		ExpiresAt:  row.ExpiresAt,
	}
}

// cleanTokenLabel makes a label fit to display. Control characters are stripped
// rather than escaped at render time: the label is shown in several places, and
// a value that is safe everywhere is better than remembering to escape it in
// each one.
func cleanTokenLabel(label string) (string, error) {
	label = strings.Map(func(r rune) rune {
		if r == '\t' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, label)
	label = strings.TrimSpace(label)

	if label == "" {
		return "", fmt.Errorf("%w: a token needs a name", apperr.ErrValidation)
	}
	if len([]rune(label)) > maxTokenLabel {
		return "", fmt.Errorf("%w: that name is too long", apperr.ErrValidation)
	}
	return label, nil
}
