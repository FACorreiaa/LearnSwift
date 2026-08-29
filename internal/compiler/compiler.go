// Package compiler turns a learner's Swift into a WebAssembly module.
//
// The compile runs inside a container because this is the one place untrusted
// input meets a compiler. Note what is and is not being defended: the module
// produced here is never executed in the container, so the risk is swiftc's own
// handling of hostile source — a much smaller surface than general code
// execution, and the reason a container is sufficient rather than a VM.
//
// Running the compiled module is a separate concern entirely; see
// internal/executor, which runs it in-process under wazero, where WebAssembly's
// own semantics are the sandbox.
package compiler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// ErrUnavailable means no compiler could be reached at all — a missing image,
// no container runtime. Distinct from a compile failure, because telling a
// learner their code is wrong when the compiler never ran is the one outcome
// worth going out of the way to avoid.
var ErrUnavailable = errors.New("compiler: unavailable")

const (
	// Wall-clock cap on one compile. The spike measured 1.4-4.3s warm, so this
	// is generous enough not to fire on a slow machine and short enough that a
	// wedged container cannot hold a request open.
	defaultTimeout = 60 * time.Second

	// Resource caps for the container. Swift's optimiser is memory-hungry, so
	// this is well above what a lesson-sized file needs; the point is a ceiling
	// that a deliberately pathological program cannot climb past.
	memoryLimit = "2g"
	cpuLimit    = "2"
	pidsLimit   = "256"

	// The compiled module is bounded so a submission cannot fill the cache.
	//
	// Sized from measurement rather than guesswork: a plain full-SDK build is
	// ~7.7 MB, but `import Foundation` produces ~53 MB — Foundation is
	// enormous once statically linked for wasm. 64 MB admits that without
	// admitting anything unbounded.
	//
	// A lesson needing Foundation is therefore possible but a poor idea: even
	// compressed, that is a download no learner should wait for. The embedded
	// runtime, at ~38 KB compressed, is where lessons belong wherever they can.
	maxModuleBytes = 64 << 20

	// The unprivileged user in build/compiler/Dockerfile. Hardcoded because the
	// image is ours and the value has to match the tmpfs ownership below; a
	// mismatch fails at compile time with a permissions error rather than
	// anything self-explanatory.
	buildUID = "1001"
	buildGID = "1001"
)

// Result is the outcome of one compile.
type Result struct {
	// Module is the WebAssembly binary, empty when the compile failed.
	Module []byte

	// Diagnostics is swiftc's own output — the compiler errors a learner needs
	// to read. Passed through rather than parsed: Swift's diagnostics are
	// better than anything that would survive being reformatted.
	Diagnostics string
}

func (r Result) OK() bool { return len(r.Module) > 0 }

// Compiler runs compiles via a container runtime.
type Compiler struct {
	// Runtime is the container command, normally "docker".
	Runtime string

	// Image is the compiler image, built from build/compiler/Dockerfile.
	Image string

	Timeout time.Duration
}

func New(runtime, image string) *Compiler {
	if runtime == "" {
		runtime = "docker"
	}
	return &Compiler{Runtime: runtime, Image: image, Timeout: defaultTimeout}
}

func (c *Compiler) Available() bool { return c.Image != "" }

// sdkFor maps a lesson's declared runtime onto a Swift SDK.
//
// The two differ by roughly fifty times in output size — the spike measured
// 38 KB against 1.87 MB compressed — so this is not a detail. Embedded has no
// concurrency runtime and no Unicode tables, which is why a lesson declares
// what it needs rather than the compiler guessing.
func sdkFor(r lesson.Runtime) (string, error) {
	switch r {
	case lesson.RuntimeEmbedded:
		return "swift-6.3.1-RELEASE_wasm-embedded", nil
	case lesson.RuntimeFull:
		return "swift-6.3.1-RELEASE_wasm", nil
	case lesson.RuntimeNone:
		return "", fmt.Errorf("compiler: this lesson is not executable")
	default:
		return "", fmt.Errorf("compiler: unknown runtime %q", r)
	}
}

// Compile builds source and returns the module or the compiler's diagnostics.
func (c *Compiler) Compile(ctx context.Context, source string, rt lesson.Runtime) (Result, error) {
	if !c.Available() {
		return Result{}, ErrUnavailable
	}

	sdk, err := sdkFor(rt)
	if err != nil {
		return Result{}, err
	}

	// The container writes its module here. A host directory rather than a
	// volume so the bytes can be read back without another container.
	outDir, err := os.MkdirTemp("", "seshat-compile-")
	if err != nil {
		return Result{}, fmt.Errorf("compiler: scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(outDir) }()

	// World-writable because the container runs as its own uid, which will not
	// match the host user's. The directory is a per-compile temp that is
	// removed above.
	if err := os.Chmod(outDir, 0o777); err != nil {
		return Result{}, fmt.Errorf("compiler: scratch perms: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	args := []string{
		"run", "--rm", "-i",

		// No network at all. A compile has nothing to fetch — the SDKs are in
		// the image — so this closes exfiltration and dependency confusion in
		// one flag.
		"--network", "none",

		// The image is immutable at runtime; only the two mounts below can be
		// written.
		"--read-only",

		// Both tmpfs mounts are owned by the build user. Docker mounts a tmpfs
		// root-owned by default, and the container runs unprivileged, so
		// without uid/gid the compiler cannot write to its own scratch — which
		// surfaces as "Permission denied" on a module cache rather than
		// anything resembling a mount problem.
		//
		// /tmp is mounted `exec`, deliberately and with regret. swiftpm
		// compiles Package.swift into a binary under TMPDIR and then executes
		// it to read the manifest; with noexec the build dies at
		// "posix_spawn error: Permission denied". Note what still holds: no
		// network, a read-only rootfs, no capabilities, no privilege
		// escalation, a non-root uid, and hard memory and pid caps. What
		// executes here is swiftpm's own manifest binary, compiled from a
		// Package.swift this image supplies — not from anything the learner
		// wrote. Their code is only ever compiled, never run, in this
		// container.
		"--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=1g,uid=" + buildUID + ",gid=" + buildGID,

		// The clang module cache, which swiftpm writes under the invoking
		// user's home before any of our flags apply. noexec here because
		// nothing in it is ever executed.
		"--tmpfs", "/home/builder/.cache:rw,noexec,nosuid,nodev,size=512m,uid=" + buildUID + ",gid=" + buildGID,

		"-v", outDir + ":/work",

		// Caps against a program designed to exhaust the host rather than to
		// compile.
		"--memory", memoryLimit,
		"--cpus", cpuLimit,
		"--pids-limit", pidsLimit,

		// No path from the container to more privilege than it started with.
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",

		"-e", "SESHAT_SDK=" + sdk,
		c.Image,
	}

	cmd := exec.CommandContext(ctx, c.Runtime, args...)

	// Source goes in on stdin. It is never an argument, so nothing a learner
	// writes is parsed by a shell or a command line.
	cmd.Stdin = strings.NewReader(source)

	var stderr bytes.Buffer
	cmd.Stdout = &stderr // the script sends diagnostics to both
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("compiler: timed out after %s: %w", c.Timeout, ctx.Err())
	}

	module, readErr := os.ReadFile(filepath.Join(outDir, "out.wasm"))
	switch {
	case readErr == nil && len(module) > maxModuleBytes:
		return Result{}, fmt.Errorf("compiler: module is %d bytes, over the %d limit", len(module), maxModuleBytes)

	case readErr == nil:
		return Result{Module: module, Diagnostics: CleanDiagnostics(stderr.String())}, nil

	case runErr == nil:
		// Reported success and produced nothing: not a learner error.
		return Result{}, fmt.Errorf("compiler: no module produced: %s", truncate(stderr.String()))
	}

	output := stderr.String()

	// The container runtime failing must never be reported as the learner's
	// code failing. Exit 125 is docker's documented "could not run the
	// container", but a missing socket exits 1 — indistinguishable by code
	// alone from "this Swift does not compile". Getting this wrong tells
	// someone with correct code that their code is broken, which is the worst
	// outcome this package can produce, so the output is inspected too.
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 125 {
		return Result{}, fmt.Errorf("%w: %s", ErrUnavailable, truncate(output))
	}
	if isRuntimeFailure(output) {
		return Result{}, fmt.Errorf("%w: %s", ErrUnavailable, truncate(output))
	}

	// Exit 2 is the compile script's own "something is wrong here", distinct
	// from exit 1 meaning "this code does not compile".
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 2 {
		return Result{}, fmt.Errorf("compiler: %s", truncate(output))
	}

	// An ordinary compile failure. The diagnostics are the useful part.
	return Result{Diagnostics: CleanDiagnostics(output)}, nil
}

// runtimeFailureMarkers are phrases the container runtime emits when it could
// not start the container at all. None of them can appear in swiftc output,
// because swiftc never ran.
var runtimeFailureMarkers = []string{
	"Cannot connect to the Docker daemon",
	"failed to connect to the docker API",
	"error response from daemon",
	"Unable to find image",
	"docker: command not found",
	"executable file not found",
	"permission denied while trying to connect",
	"no such host",
}

func isRuntimeFailure(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range runtimeFailureMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	const max = 2000
	if len(s) <= max {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:max]) + "…"
}
