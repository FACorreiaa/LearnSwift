package compiler

import (
	"regexp"
	"strings"
)

// The build directory is a mktemp path inside the container, so every
// diagnostic is prefixed with something like
// /tmp/tmp.bikay9sJWu/Sources/exercise/main.swift. That path means nothing to
// the person reading it and changes on every compile.
var buildPathPattern = regexp.MustCompile(`/tmp/tmp\.[A-Za-z0-9]+/Sources/exercise/`)

// noiseMarkers are lines swiftpm emits about its own environment. They are
// consequences of the read-only sandbox, they are present on successful builds
// too, and showing them to a learner alongside a real error invites them to
// debug our container instead of their code.
var noiseMarkers = []string{
	"/home/builder/",
	"disabling user-level cache features",
	"Building for production",
	"Compiling plugin",
	"Write sources",
	"Write swift-version",
	"Write Objects.LinkFileList",
	"Build complete!",
}

// progressLine matches swiftpm's "[3/5] Compiling exercise main.swift".
var progressLine = regexp.MustCompile(`^\[\d+/\d+\]`)

// CleanDiagnostics reduces compiler output to what the learner wrote and what
// was wrong with it.
//
// Nothing is summarised or reworded: Swift's diagnostics are genuinely good,
// and a paraphrase would be worse. This only removes lines that are about the
// build environment rather than the code, and shortens the temporary path that
// prefixes every message.
func CleanDiagnostics(raw string) string {
	var kept []string

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if progressLine.MatchString(trimmed) {
			continue
		}
		if containsAny(line, noiseMarkers) {
			continue
		}
		kept = append(kept, buildPathPattern.ReplaceAllString(line, ""))
	}

	// Everything was noise: the build failed for a reason that is ours rather
	// than the learner's, and saying nothing is better than showing them an
	// empty box.
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n")
}

func containsAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
