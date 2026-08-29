package validate

import (
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

func TestOutputContainsIsSatisfiedBySubstring(t *testing.T) {
	got := CheckOutput("Hello, World!\n", lesson.Assertions{OutputContains: []string{"Hello, World!"}})
	if len(got) != 0 {
		t.Errorf("correct output was rejected: %+v", got)
	}
}

func TestOutputContainsFailsWithBothSidesQuoted(t *testing.T) {
	got := CheckOutput("Goodbye\n", lesson.Assertions{OutputContains: []string{"Hello, World!"}})
	if len(got) != 1 {
		t.Fatalf("failures = %+v", got)
	}

	// A learner needs to see what they printed as well as what was wanted;
	// "expected Hello, World!" alone sends them hunting.
	msg := got[0].Message
	if !strings.Contains(msg, `"Hello, World!"`) || !strings.Contains(msg, `"Goodbye"`) {
		t.Errorf("message does not show both sides: %q", msg)
	}
}

// A trailing newline is print's doing, not the learner's mistake.
func TestTrailingWhitespaceDoesNotFailAnAnswer(t *testing.T) {
	for _, stdout := range []string{"42", "42\n", "  42  \n\n"} {
		if got := CheckOutput(stdout, lesson.Assertions{OutputEquals: "42"}); len(got) != 0 {
			t.Errorf("output %q was rejected: %+v", stdout, got)
		}
	}
}

func TestOutputEqualsIsExactAfterTrimming(t *testing.T) {
	if got := CheckOutput("42 plus", lesson.Assertions{OutputEquals: "42"}); len(got) != 1 {
		t.Errorf("a superset of the expected output passed an exact match: %+v", got)
	}
}

func TestNoOutputAssertionsMeansNothingToFail(t *testing.T) {
	if got := CheckOutput("anything", lesson.Assertions{MustDeclare: []string{"x"}}); len(got) != 0 {
		t.Errorf("static-only assertions produced output failures: %+v", got)
	}
}

func TestEmptyOutputIsDescribedAsNothing(t *testing.T) {
	got := CheckOutput("", lesson.Assertions{OutputContains: []string{"x"}})
	if len(got) != 1 {
		t.Fatalf("failures = %+v", got)
	}
	if !strings.Contains(got[0].Message, "nothing") {
		t.Errorf("empty output should read as 'nothing', got: %q", got[0].Message)
	}
}
