package compiler

import (
	"context"
	"errors"
	"testing"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// The distinction this package exists to get right: infrastructure being broken
// must never be reported to a learner as their code being wrong.
func TestRuntimeFailuresAreNotMistakenForCompileErrors(t *testing.T) {
	for _, output := range []string{
		"failed to connect to the docker API at unix:///var/run/docker.sock",
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock.",
		"docker: Error response from daemon: handle request: read response: unexpected EOF",
		"Unable to find image 'seshat-compiler:6.3.1' locally",
		"exec: \"docker\": executable file not found in $PATH",
	} {
		if !isRuntimeFailure(output) {
			t.Errorf("this would be shown to a learner as a compile error:\n  %s", output)
		}
	}
}

// And the converse: real compiler output must not be mistaken for an outage,
// or a learner never sees why their code is wrong.
func TestRealCompilerOutputIsNotMistakenForAnOutage(t *testing.T) {
	for _, output := range []string{
		"main.swift:1:14: error: cannot convert value of type 'String' to specified type 'Int'",
		"error: 'x' is not a member type of struct 'Foo'",
		"main.swift:3:5: warning: variable 'y' was never used",
		"<unknown>:0: error: could not build C module 'SwiftShims'",
	} {
		if isRuntimeFailure(output) {
			t.Errorf("real compiler output was treated as an outage:\n  %s", output)
		}
	}
}

func TestAnUnconfiguredCompilerIsUnavailable(t *testing.T) {
	c := New("docker", "")

	if c.Available() {
		t.Error("a compiler with no image reported itself available")
	}
	_, err := c.Compile(context.Background(), `print("x")`, lesson.RuntimeEmbedded)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// A lesson that cannot be executed has no SDK to compile against, and asking
// for one is a programming mistake rather than a learner's.
func TestANonExecutableRuntimeIsRefused(t *testing.T) {
	c := New("docker", "some-image")

	if _, err := c.Compile(context.Background(), `print("x")`, lesson.RuntimeNone); err == nil {
		t.Error("compiling a non-executable lesson was allowed")
	}
}

func TestEachRuntimeMapsToItsOwnSDK(t *testing.T) {
	embedded, err := sdkFor(lesson.RuntimeEmbedded)
	if err != nil {
		t.Fatal(err)
	}
	full, err := sdkFor(lesson.RuntimeFull)
	if err != nil {
		t.Fatal(err)
	}

	if embedded == full {
		t.Fatal("both runtimes map to the same SDK")
	}
	// The embedded SDK is the whole reason a lesson can be 38 KB rather than
	// 1.9 MB, so the mapping is worth pinning down.
	if embedded != "swift-6.3.1-RELEASE_wasm-embedded" {
		t.Errorf("embedded SDK = %q", embedded)
	}
}
