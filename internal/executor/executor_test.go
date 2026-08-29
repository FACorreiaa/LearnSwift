package executor

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FACorreiaa/seshat/internal/compiler"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// compilerReady probes the container runtime once for the whole package.
//
// Probing per test was a mistake worth not repeating: an unresponsive Docker
// daemon does not fail, it hangs, so each test paid a full timeout and the
// suite stalled for many minutes before skipping anything. One short probe
// decides for everybody.
var compilerReady = sync.OnceValues(func() (string, error) {
	image := os.Getenv("SESHAT_COMPILER_IMAGE")
	if image == "" {
		image = "seshat-compiler:6.3.1"
	}
	if os.Getenv("SESHAT_SKIP_COMPILE_TESTS") != "" {
		return "", errors.New("compile tests disabled")
	}

	// Deliberately short. This asks "is the runtime answering at all", not
	// "can it compile Swift" — the tests that follow answer the second
	// question, and they are allowed to be slow.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	c := compiler.New("docker", image)
	if _, err := c.Compile(ctx, `print("probe")`, lesson.RuntimeEmbedded); err != nil {
		return "", err
	}
	return image, nil
})

// newExecutor wires a real compiler, and skips when the image is absent.
// Building it takes minutes and 4 GB, so it cannot be a precondition of
// `go test ./...`.
func newExecutor(t *testing.T) *Executor {
	t.Helper()

	image, err := compilerReady()
	if err != nil {
		t.Skipf("compiler unavailable (%v); build it with `task compiler:build`", err)
	}

	e := New(compiler.New("docker", image), NewMemoryCache(0), 0)
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	return e
}

func TestASubmissionCompilesAndRuns(t *testing.T) {
	e := newExecutor(t)

	got, err := e.Run(context.Background(), `print("Hello, World!")`, lesson.RuntimeEmbedded)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !got.Compiled {
		t.Fatalf("did not compile: %s", got.Diagnostics)
	}
	if !strings.Contains(got.Stdout, "Hello, World!") {
		t.Errorf("stdout = %q", got.Stdout)
	}
	if got.ExitCode != 0 {
		t.Errorf("exit = %d", got.ExitCode)
	}
}

// A compile failure is a result the learner reads, not an operational error.
func TestCodeThatDoesNotCompileReturnsDiagnosticsNotAnError(t *testing.T) {
	e := newExecutor(t)

	got, err := e.Run(context.Background(), `let x: Int = "not an int"`, lesson.RuntimeEmbedded)
	if err != nil {
		t.Fatalf("a compile failure must not be an error: %v", err)
	}

	if got.Compiled {
		t.Fatal("code that cannot compile reported as compiled")
	}
	if got.Diagnostics == "" {
		t.Error("no diagnostics for code that does not compile")
	}
	if !strings.Contains(got.Diagnostics, "error:") {
		t.Errorf("diagnostics do not look like swiftc output: %.200s", got.Diagnostics)
	}
}

// The second identical submission must skip the container entirely. This is
// what makes "try it in ten seconds" true for anything but the first attempt.
func TestAnIdenticalSubmissionIsServedFromCache(t *testing.T) {
	e := newExecutor(t)
	ctx := context.Background()

	first, err := e.Run(ctx, `print("cached")`, lesson.RuntimeEmbedded)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if first.Cached {
		t.Fatal("the first compile reported as cached")
	}

	start := time.Now()
	second, err := e.Run(ctx, `print("cached")`, lesson.RuntimeEmbedded)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !second.Cached {
		t.Error("an identical submission was compiled again")
	}
	if !strings.Contains(second.Stdout, "cached") {
		t.Errorf("stdout = %q", second.Stdout)
	}
	// A container round trip is seconds; a cache hit is a wasm instantiation.
	if elapsed > 3*time.Second {
		t.Errorf("cache hit took %v, which suggests it recompiled", elapsed)
	}
}

// The same source under a different SDK is a different module, so it must not
// collide in the cache.
func TestTheSameSourceUnderADifferentRuntimeIsADifferentEntry(t *testing.T) {
	source := `print("Hello")`

	embedded := CacheKey(source, lesson.RuntimeEmbedded, "swift-6.3.1")
	full := CacheKey(source, lesson.RuntimeFull, "swift-6.3.1")

	if embedded == full {
		t.Error("the embedded and full builds share a cache key")
	}
	if CacheKey(source, lesson.RuntimeEmbedded, "swift-7") == embedded {
		t.Error("a toolchain change does not invalidate the cache")
	}
}

// A learner's infinite loop must not hold a request open indefinitely.
func TestARunawayProgramIsStopped(t *testing.T) {
	e := newExecutor(t)

	start := time.Now()
	got, err := e.Run(context.Background(), `while true { }`, lesson.RuntimeEmbedded)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !got.TimedOut {
		t.Error("a runaway program was not reported as timing out")
	}
	if elapsed > runTimeout+30*time.Second {
		t.Errorf("took %v to stop a runaway program", elapsed)
	}
}

// Unbounded output must be truncated rather than allowed to exhaust memory.
func TestUnboundedOutputIsTruncated(t *testing.T) {
	e := newExecutor(t)

	got, err := e.Run(context.Background(), `while true { print("xxxxxxxxxxxxxxxx") }`, lesson.RuntimeEmbedded)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(got.Stdout) > maxOutputBytes+100 {
		t.Errorf("stdout is %d bytes, over the %d cap", len(got.Stdout), maxOutputBytes)
	}
	if !strings.Contains(got.Stdout, "truncated") {
		t.Error("truncation was not signalled to the reader")
	}
}

// The sandbox is what is *not* granted. A module gets stdout and stderr and
// nothing else — no filesystem, no network, no environment.
func TestAProgramCannotReachTheFilesystem(t *testing.T) {
	e := newExecutor(t)

	// Embedded Swift has no Foundation, so this is the full SDK.
	got, err := e.Run(context.Background(), `
import Foundation
if let data = FileManager.default.contents(atPath: "/etc/passwd") {
    print("READ \(data.count) BYTES")
} else {
    print("no access")
}
`, lesson.RuntimeFull)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got.Compiled {
		t.Skipf("sample did not compile, so the sandbox claim is untested here: %.200s", got.Diagnostics)
	}

	if strings.Contains(got.Stdout, "READ") {
		t.Errorf("the module read a host file: %q", got.Stdout)
	}
}

func TestAnUnavailableCompilerIsAnErrorNotAFailedSubmission(t *testing.T) {
	e := New(compiler.New("docker", ""), NewMemoryCache(0), 0)
	defer func() { _ = e.Close(context.Background()) }()

	if _, err := e.Run(context.Background(), `print("x")`, lesson.RuntimeEmbedded); err == nil {
		t.Fatal("a missing compiler silently produced a result")
	}
}
