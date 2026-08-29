package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	authdb "github.com/FACorreiaa/seshat/internal/auth/db"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

type Service struct {
	q        *authdb.Queries
	sessions *SessionStore
}

func NewService(db authdb.DBTX) *Service {
	return &Service{q: authdb.New(db), sessions: NewSessionStore(db)}
}

func (s *Service) Sessions() *SessionStore { return s.sessions }

type Credentials struct {
	Email     string
	Password  string
	UserAgent string
	IP        netip.Addr
}

// Register creates an account and signs it in, returning the session token.
func (s *Service) Register(ctx context.Context, c Credentials) (User, string, error) {
	fields := apperr.FieldErrors{}
	if err := ValidateEmail(c.Email); err != nil {
		var fe apperr.FieldErrors
		if errors.As(err, &fe) {
			fields["email"] = fe["email"]
		}
	}
	if err := ValidatePassword(c.Password); err != nil {
		var fe apperr.FieldErrors
		if errors.As(err, &fe) {
			fields["password"] = fe["password"]
		}
	}
	if fields.Any() {
		return User{}, "", fields
	}

	hash, err := HashPassword(c.Password)
	if err != nil {
		return User{}, "", err
	}

	row, err := s.q.CreateUser(ctx, authdb.CreateUserParams{
		Email:        c.Email,
		PasswordHash: hash,
	})
	if err != nil {
		// 23505 is unique_violation. Letting the database decide means two
		// simultaneous registrations for one address cannot both succeed —
		// which a check-then-insert in Go would allow.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, "", apperr.FieldErrors{}.Add("email", "That address is already registered.")
		}
		return User{}, "", fmt.Errorf("auth: create user: %w", err)
	}

	user := User{ID: row.ID, Email: row.Email, CreatedAt: row.CreatedAt}

	token, err := s.sessions.Create(ctx, user.ID, c.UserAgent, c.IP)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// Login verifies credentials and issues a session.
func (s *Service) Login(ctx context.Context, c Credentials) (User, string, error) {
	row, err := s.q.GetUserByEmail(ctx, c.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A password check runs even with no user, so that "no such
			// address" and "wrong password" take the same time. Returning
			// early here would let anyone enumerate registered addresses by
			// timing the response.
			_ = CheckPassword(dummyHash, c.Password)
			return User{}, "", errInvalidCredentials()
		}
		return User{}, "", fmt.Errorf("auth: lookup user: %w", err)
	}

	if !CheckPassword(row.PasswordHash, c.Password) {
		return User{}, "", errInvalidCredentials()
	}

	user := User{ID: row.ID, Email: row.Email, CreatedAt: row.CreatedAt}

	token, err := s.sessions.Create(ctx, user.ID, c.UserAgent, c.IP)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.sessions.Delete(ctx, token)
}

// errInvalidCredentials is one message for both failures, attached to no single
// field. Saying which half was wrong tells an attacker whether an address is
// registered.
func errInvalidCredentials() error {
	return apperr.FieldErrors{}.Add("form", "That email and password do not match.")
}

// dummyHash is a bcrypt hash of a value nobody knows, compared against when no
// user matched so that the response takes the same time either way.
//
// It is computed at startup rather than pasted in as a literal, for two
// reasons: a literal cannot be checked to be well-formed, and bcrypt rejects a
// malformed hash immediately — which would silently remove the delay this
// exists to provide. Deriving it also keeps it at bcryptCost automatically if
// that is ever raised.
var dummyHash = func() string {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)

	hash, err := bcrypt.GenerateFromPassword(secret, bcryptCost)
	if err != nil {
		// Unreachable: the only documented failure is a cost outside 4..31.
		panic("auth: cannot derive timing-equalisation hash: " + err.Error())
	}
	return string(hash)
}()
