package auth

import (
	"strings"
	"testing"

	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"golang.org/x/crypto/bcrypt"
)

func TestAPasswordHashIsNotThePassword(t *testing.T) {
	const plain = "correct-horse-battery"

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if strings.Contains(hash, plain) {
		t.Fatal("the hash contains the password")
	}
	if !CheckPassword(hash, plain) {
		t.Error("the password does not verify against its own hash")
	}
	if CheckPassword(hash, plain+"x") {
		t.Error("a wrong password verified")
	}
}

// Two people choosing the same password must not produce the same hash, or the
// hashes reveal that they match.
func TestTheSamePasswordHashesDifferentlyEachTime(t *testing.T) {
	a, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if a == b {
		t.Error("identical passwords produced identical hashes; the salt is not doing its job")
	}
}

func TestTheHashUsesTheConfiguredCost(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("cost: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("cost = %d, want %d", cost, bcryptCost)
	}
}

func TestShortPasswordsAreRefusedWithAMessageAboutTheField(t *testing.T) {
	err := ValidatePassword("short")
	if err == nil {
		t.Fatal("a five-character password was accepted")
	}

	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) {
		t.Fatalf("err = %T, want FieldErrors so the form can show it beside the input", err)
	}
	if fields["password"] == "" {
		t.Error("the error is not attached to the password field")
	}
	// It must also match the validation sentinel, so a handler that only cares
	// about the status code can ask that question instead.
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Error("a field error should also read as ErrValidation")
	}
}

// bcrypt silently truncates at 72 bytes. Accepting a longer password would mean
// two different passphrases sharing a hash, and the visitor never being told.
func TestOverlongPasswordsAreRefusedRatherThanTruncated(t *testing.T) {
	long := strings.Repeat("a", maxPasswordBytes+1)

	if err := ValidatePassword(long); err == nil {
		t.Fatalf("a %d-byte password was accepted; bcrypt would silently ignore the tail", len(long))
	}

	// Exactly at the limit is fine.
	if err := ValidatePassword(strings.Repeat("a", maxPasswordBytes)); err != nil {
		t.Errorf("a password of exactly %d bytes was refused: %v", maxPasswordBytes, err)
	}
}

// The minimum is counted in characters, but the maximum in bytes, because those
// are the units the two limits actually care about.
func TestLengthIsCountedInCharactersNotBytes(t *testing.T) {
	// Ten characters, but well over ten bytes in UTF-8.
	if err := ValidatePassword("héllo-wörld"); err != nil {
		t.Errorf("a valid multi-byte password was refused: %v", err)
	}

	// Nine multi-byte characters: under the character minimum despite being
	// over it in bytes.
	if err := ValidatePassword("ααααααααα"); err == nil {
		t.Error("a nine-character password passed because its bytes were counted instead")
	}
}

func TestHashPasswordRefusesWhatValidateRefuses(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Error("HashPassword accepted a password ValidatePassword rejects")
	}
}

func TestCheckPasswordRejectsAMalformedHashRatherThanPanicking(t *testing.T) {
	for _, hash := range []string{"", "not-a-hash", "$2a$12$tooshort"} {
		if CheckPassword(hash, "anything") {
			t.Errorf("a malformed hash %q verified", hash)
		}
	}
}

func TestEmailNormalizationLowercasesAndTrims(t *testing.T) {
	for input, want := range map[string]string{
		"  Learner@Example.COM ": "learner@example.com",
		"already@lower.com":      "already@lower.com",
	} {
		if got := NormalizeEmail(input); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEmailValidationIsShallowButCatchesTheObviousCases(t *testing.T) {
	valid := []string{
		"a@b.co",
		"first.last+tag@example.co.uk",
		// Deliberately accepted: a stricter pattern rejects real addresses,
		// and only sending mail proves an address exists.
		"unusual!#$%@example.com",
	}
	for _, email := range valid {
		if err := ValidateEmail(email); err != nil {
			t.Errorf("ValidateEmail(%q) rejected a valid address: %v", email, err)
		}
	}

	invalid := []string{"", "   ", "no-at-sign", "@example.com", "trailing@", "two words@example.com"}
	for _, email := range invalid {
		if err := ValidateEmail(email); err == nil {
			t.Errorf("ValidateEmail(%q) accepted an invalid address", email)
		}
	}
}

// The timing-equalisation hash must be a real, usable bcrypt hash at the same
// cost as a genuine one. A malformed value would be rejected immediately by
// bcrypt, removing exactly the delay it exists to provide.
func TestTheTimingEqualisationHashIsRealAndAtTheRightCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil {
		t.Fatalf("the dummy hash is not a valid bcrypt hash: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("dummy hash cost = %d, want %d — the fake comparison would be faster than a real one", cost, bcryptCost)
	}

	// And nothing must actually verify against it.
	if CheckPassword(dummyHash, "") || CheckPassword(dummyHash, "password") {
		t.Error("something verified against the dummy hash")
	}
}
