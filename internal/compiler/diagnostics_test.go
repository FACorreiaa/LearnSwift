package compiler

import (
	"strings"
	"testing"
)

const realOutput = `warning: /home/builder/.swiftpm/configuration is not accessible or not writable, disabling user-level cache features.
warning: /home/builder/.swiftpm/security is not accessible or not writable, disabling user-level cache features.
Building for production...
[0/4] Write sources
[3/5] Compiling exercise main.swift
/tmp/tmp.bikay9sJWu/Sources/exercise/main.swift:1:5: error: var 'greeting' is not concurrency-safe because it is nonisolated global shared mutable state
1 | var greeting: String? = "World"
  |     |- error: var 'greeting' is not concurrency-safe`

func TestTheLearnerSeesTheirErrorAndNotOurContainer(t *testing.T) {
	got := CleanDiagnostics(realOutput)

	// The actual error must survive, verbatim.
	if !strings.Contains(got, "not concurrency-safe") {
		t.Errorf("the real error was removed:\n%s", got)
	}
	// And the source line, which is most of what makes Swift's diagnostics
	// readable.
	if !strings.Contains(got, `var greeting: String? = "World"`) {
		t.Errorf("the quoted source line was removed:\n%s", got)
	}

	for _, noise := range []string{"/home/builder/", "Building for production", "[3/5]", "cache features"} {
		if strings.Contains(got, noise) {
			t.Errorf("container noise reached the learner: %q in\n%s", noise, got)
		}
	}
}

// The temporary build path changes on every compile and means nothing to the
// reader, so it is shortened to the filename they recognise.
func TestTheTemporaryBuildPathIsRemoved(t *testing.T) {
	got := CleanDiagnostics(realOutput)

	if strings.Contains(got, "/tmp/tmp.") {
		t.Errorf("the container's temp path is still shown:\n%s", got)
	}
	if !strings.Contains(got, "main.swift:1:5:") {
		t.Errorf("the file and position were lost:\n%s", got)
	}
}

// Output that is nothing but noise means the build failed for a reason of ours.
// An empty string lets the caller say so, rather than showing an empty box.
func TestOutputThatIsEntirelyNoiseBecomesEmpty(t *testing.T) {
	only := "warning: /home/builder/.swiftpm/security is not accessible\nBuilding for production...\n[0/4] Write sources"

	if got := CleanDiagnostics(only); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestASuccessfulBuildLeavesNothingToShow(t *testing.T) {
	success := "Building for production...\n[3/5] Compiling exercise main.swift\n[4/5] Linking exercise.wasm\nBuild complete! (0.74s)"

	if got := CleanDiagnostics(success); got != "" {
		t.Errorf("a clean build produced diagnostics: %q", got)
	}
}
