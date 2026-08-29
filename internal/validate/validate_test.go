package validate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// newValidator finds the binary the same way the application does, and skips
// when it has not been built. Skipping rather than failing keeps `go test ./...`
// useful on a machine with no Swift toolchain — but see the cross-check test
// below, which is what stops that convenience turning into a blind spot.
func newValidator(t *testing.T) *Validator {
	t.Helper()

	path := os.Getenv("SWIFT_VALIDATE_BIN")
	if path == "" {
		path = filepath.Join("..", "..", "tools", "swift-validate", ".build", "release", "swift-validate")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("swift-validate is not built; run `task validate:build`")
	}
	return New(path)
}

// The bug this whole binary exists to fix: an exclamation mark inside a string
// is punctuation, and a substring search cannot tell it from a force unwrap.
func TestPunctuationInsideAStringIsNotAForceUnwrap(t *testing.T) {
	v := newValidator(t)

	source := "if let greeting {\n    print(\"Hello, \\(greeting)!\")\n}\n"
	got, err := v.Validate(context.Background(), source, lesson.Assertions{MustNotUse: []string{"!"}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !got.OK {
		t.Errorf("a correct answer was rejected: %+v", got.Failures)
	}
}

func TestARealForceUnwrapIsRejected(t *testing.T) {
	v := newValidator(t)

	got, err := v.Validate(context.Background(), "print(greeting!)\n", lesson.Assertions{MustNotUse: []string{"!"}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if got.OK {
		t.Fatal("a force unwrap was accepted")
	}
	if len(got.Failures) != 1 || got.Failures[0].Kind != "must_not_use" {
		t.Errorf("failures = %+v", got.Failures)
	}
}

func TestATokenOnlyInACommentDoesNotSatisfyAnAssertion(t *testing.T) {
	v := newValidator(t)

	for _, source := range []string{
		"// remember to use await\nlet x = 1\n",
		"/* await goes here */\nlet x = 1\n",
	} {
		got, err := v.Validate(context.Background(), source, lesson.Assertions{MustDeclare: []string{"await"}})
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if got.OK {
			t.Errorf("a comment satisfied must_declare: %q", source)
		}
	}
}

// The capability the Go approximation never had: saying where the code is
// broken, not just that a token is missing.
func TestSyntaxErrorsAreReportedWithAPosition(t *testing.T) {
	v := newValidator(t)

	got, err := v.Validate(context.Background(), "func broken( {\n    print(\"x\")\n", lesson.Assertions{})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if got.OK {
		t.Fatal("source that does not parse was accepted")
	}
	if len(got.Diagnostics) == 0 {
		t.Fatal("no diagnostics for unparseable source")
	}
	if got.Diagnostics[0].Line < 1 {
		t.Errorf("diagnostic has no usable line number: %+v", got.Diagnostics[0])
	}
	if got.Diagnostics[0].Message == "" {
		t.Error("diagnostic has no message")
	}
}

// Code that does not compile cannot be said to have satisfied anything, so a
// syntax error fails the exercise even when every token assertion passes.
func TestUnparseableSourceFailsEvenWhenTokensMatch(t *testing.T) {
	v := newValidator(t)

	got, err := v.Validate(context.Background(),
		"@State var count = 0\nfunc broken( {\n", lesson.Assertions{MustDeclare: []string{"@State"}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if got.OK {
		t.Error("source with a syntax error passed because its tokens matched")
	}
}

func TestAnUnavailableValidatorIsAnErrorNotAPass(t *testing.T) {
	v := New("")

	_, err := v.Validate(context.Background(), "let x = 1", lesson.Assertions{MustDeclare: []string{"nope"}})
	if err == nil {
		t.Fatal("a missing binary silently passed the submission")
	}
	if !ErrorIsUnavailable(err) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestAnOversizedSubmissionIsRefusedBeforeExec(t *testing.T) {
	v := New("/nonexistent-on-purpose")

	_, err := v.Validate(context.Background(), strings.Repeat("a", maxSourceBytes+1), lesson.Assertions{})
	if err == nil {
		t.Fatal("an oversized submission was accepted")
	}
	if ErrorIsUnavailable(err) {
		t.Error("the size check must run before the binary is consulted")
	}
}
