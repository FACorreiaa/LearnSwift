package lessons

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

func newCrossCheckValidator(t *testing.T) *validate.Validator {
	t.Helper()

	path := os.Getenv("SWIFT_VALIDATE_BIN")
	if path == "" {
		path = filepath.Join("..", "..", "tools", "swift-validate", ".build", "release", "swift-validate")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("swift-validate is not built; run `task validate:build`")
	}
	return validate.New(path)
}

// The cross-check that keeps two graders from drifting apart.
//
// The Go lexer in internal/lessons/lesson is an approximation kept for the
// content tests, which run with no Swift toolchain. This asserts the two agree
// on every assertion in every shipped lesson — so if the approximation is ever
// wrong about real content, it fails here rather than in a learner's browser.
func TestTheGoApproximationAgreesWithTheRealParserOnShippedContent(t *testing.T) {
	v := newCrossCheckValidator(t)

	sub, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	index, err := Parse(sub)
	if err != nil {
		t.Fatalf("parse lessons: %v", err)
	}

	for _, l := range index.All() {
		for _, source := range []string{l.Starter, l.Solution} {
			if strings.TrimSpace(source) == "" {
				continue
			}

			for _, token := range append(append([]string{}, l.Assertions.MustDeclare...), l.Assertions.MustNotUse...) {
				got, err := v.Validate(context.Background(), source, lesson.Assertions{MustDeclare: []string{token}})
				if err != nil {
					t.Fatalf("%s: validate: %v", l.Slug, err)
				}

				// MustDeclare passes exactly when the token is present, so the
				// absence of a must_declare failure is the parser's answer to
				// "does this code use that token".
				swiftSaysPresent := len(got.Failures) == 0
				goSaysPresent := lesson.UsesToken(source, token)

				if swiftSaysPresent != goSaysPresent {
					t.Errorf("%s: the two graders disagree on whether %q appears in this source\n"+
						"  swift-validate: %v\n  Go approximation: %v\n  source: %q",
						l.Slug, token, swiftSaysPresent, goSaysPresent, source)
				}
			}
		}
	}
}
