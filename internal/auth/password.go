package auth

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
)

const (
	// MinPasswordLength is a floor, not a policy. Composition rules (a digit, a
	// symbol, a capital) push people towards predictable substitutions and
	// reused passwords; length is the property that actually costs an attacker
	// anything.
	MinPasswordLength = 10

	// bcrypt truncates silently at 72 bytes, so anything longer is refused
	// rather than quietly having its tail ignored — otherwise two different
	// long passphrases can share a hash.
	maxPasswordBytes = 72
)

// bcryptCost is the work factor. 12 is roughly a quarter-second on current
// hardware: slow enough to make offline guessing expensive, fast enough that a
// sign-in does not feel broken.
const bcryptCost = 12

func HashPassword(plain string) (string, error) {
	if err := ValidatePassword(plain); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether plain matches hash.
//
// It returns a bool rather than an error for a mismatch, because a mismatch is
// an expected outcome of signing in, not a failure of the system.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func ValidatePassword(plain string) error {
	switch {
	case utf8.RuneCountInString(plain) < MinPasswordLength:
		return apperr.FieldErrors{}.Add("password",
			fmt.Sprintf("Use at least %d characters.", MinPasswordLength))
	case len(plain) > maxPasswordBytes:
		return apperr.FieldErrors{}.Add("password",
			fmt.Sprintf("Use at most %d bytes.", maxPasswordBytes))
	}
	return nil
}

// NormalizeEmail trims and lowercases for comparison.
//
// The stored value keeps the visitor's own capitalisation — the unique index is
// on lower(email), so uniqueness does not depend on rewriting what they typed.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidateEmail(email string) error {
	e := strings.TrimSpace(email)
	at := strings.Index(e, "@")

	// Deliberately shallow. The only test that an address is real is sending
	// mail to it; a stricter pattern here rejects valid addresses (plus tags,
	// new TLDs, unicode domains) and still admits ones that do not exist.
	if e == "" || at <= 0 || at == len(e)-1 || strings.Contains(e, " ") {
		return apperr.FieldErrors{}.Add("email", "Enter a valid email address.")
	}
	return nil
}
