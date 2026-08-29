package validate

import (
	"strings"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// CheckOutput evaluates the assertions that can only be settled by running the
// code. Until Phase 2 these were unevaluable, which is why a lesson carrying
// them could only ever report "no problems found" rather than a pass.
//
// Comparison is done on trimmed output. A learner whose answer differs only by
// a trailing newline has not made a mistake worth failing them for, and
// `print` adds one whether they think about it or not.
func CheckOutput(stdout string, a lesson.Assertions) []Failure {
	var failures []Failure

	got := strings.TrimSpace(stdout)

	for _, want := range a.OutputContains {
		if !strings.Contains(got, want) {
			failures = append(failures, Failure{
				Kind:  "output_contains",
				Token: want,
				// Quoting both sides matters: "Hello, World" against
				// "Hello, World!" is otherwise an infuriating read.
				Message: "Your code should print " + quote(want) + ", but it printed " + quote(got) + ".",
			})
		}
	}

	if a.OutputEquals != "" {
		want := strings.TrimSpace(a.OutputEquals)
		if got != want {
			failures = append(failures, Failure{
				Kind:    "output_equals",
				Token:   want,
				Message: "Your code should print exactly " + quote(want) + ", but it printed " + quote(got) + ".",
			})
		}
	}

	return failures
}

func quote(s string) string {
	if s == "" {
		return "nothing"
	}
	const max = 200
	if len(s) > max {
		s = s[:max] + "…"
	}
	return `"` + s + `"`
}
