// Package executor compiles a submission and runs it.
//
// The two halves are deliberately different in kind. Compiling is expensive,
// needs a whole Swift toolchain, and is the only place untrusted input meets a
// compiler — so it happens in a container, and the result is cached by content.
// Running is cheap and needs no container at all: WebAssembly is already a
// sandbox, so wazero executes the module in this process in milliseconds, with
// no filesystem, no network, and no host calls beyond writing to a buffer.
//
// That asymmetry is what makes "try it in ten seconds" achievable. A cache hit
// skips the container entirely and the whole submission is a wasm instantiation.
package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"

	"github.com/FACorreiaa/seshat/internal/compiler"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

const (
	// runTimeout bounds the learner's program, and only that. A lesson
	// exercise prints a line; a runaway loop is the case this exists for.
	runTimeout = 5 * time.Second

	// compileTimeout bounds wazero turning wasm bytes into machine code, which
	// is a separate and much slower step: the full standard library is 7.7 MB
	// and takes about four seconds on a first encounter. Timing that together
	// with execution — as this did originally — meant a correct concurrency
	// answer could be reported as an infinite loop simply because the module
	// was large and the cache was cold.
	compileTimeout = 30 * time.Second

	// maxOutputBytes caps what a program can print. Without it, `while true {
	// print("x") }` fills memory rather than timing out.
	maxOutputBytes = 64 << 10
)

// Result is the outcome of compiling and running one submission.
type Result struct {
	// Compiled reports whether the module built. When false, Diagnostics
	// carries swiftc's errors and nothing was run.
	Compiled    bool
	Diagnostics string

	Stdout string
	Stderr string

	// ExitCode is the program's own exit status. Non-zero means it ran and
	// failed — a Swift runtime trap, for instance — which is different from
	// failing to compile.
	ExitCode uint32

	// TimedOut reports that execution was cut short.
	TimedOut bool

	// Cached reports that the module came from the cache rather than a compile.
	Cached bool
}

func (r Result) Ran() bool { return r.Compiled && !r.TimedOut }

// Cache stores compiled modules by content hash.
type Cache interface {
	Get(key string) ([]byte, bool)
	Put(key string, module []byte)
}

type Compiler interface {
	Available() bool
	Compile(ctx context.Context, source string, rt lesson.Runtime) (compiler.Result, error)
}

type Executor struct {
	compiler Compiler
	cache    Cache

	// runtimeCache lets wazero reuse the machine code it generates for a
	// module. Without it, instantiating the full-SDK module costs about four
	// seconds every time — the spike measured 4.1s cold against 21ms for the
	// embedded build — because wazero recompiles 7.7 MB of wasm on each run.
	compilationCache wazero.CompilationCache
}

func New(c Compiler, cache Cache) *Executor {
	return &Executor{
		compiler:         c,
		cache:            cache,
		compilationCache: wazero.NewCompilationCache(),
	}
}

func (e *Executor) Close(ctx context.Context) error {
	return e.compilationCache.Close(ctx)
}

func (e *Executor) Available() bool { return e.compiler != nil && e.compiler.Available() }

// CacheKey identifies a compiled module.
//
// The SDK is part of the key because the same source compiled against the
// embedded and full standard libraries produces genuinely different modules —
// different sizes, different capabilities. The toolchain version is in there so
// that upgrading Swift invalidates every entry rather than serving modules
// built by a compiler that is no longer installed.
func CacheKey(source string, rt lesson.Runtime, toolchain string) string {
	sum := sha256.Sum256([]byte(toolchain + "\x00" + string(rt) + "\x00" + source))
	return hex.EncodeToString(sum[:])
}

// Run compiles source if needed, then executes it.
func (e *Executor) Run(ctx context.Context, source string, rt lesson.Runtime) (Result, error) {
	if !e.Available() {
		return Result{}, compiler.ErrUnavailable
	}

	key := CacheKey(source, rt, "swift-6.3.1")

	module, cached := e.cache.Get(key)
	if !cached {
		out, err := e.compiler.Compile(ctx, source, rt)
		if err != nil {
			return Result{}, err
		}
		if !out.OK() {
			// A compile failure is a result, not an error: the learner needs
			// to read the diagnostics, and nothing went wrong operationally.
			return Result{Compiled: false, Diagnostics: out.Diagnostics}, nil
		}
		module = out.Module
		e.cache.Put(key, module)
	}

	result, err := e.execute(ctx, module)
	result.Compiled = true
	result.Cached = cached
	return result, err
}

// RunModule executes an already-compiled module. Exported so tooling can run a
// module through exactly the runtime that grades submissions — see
// scripts/runwasm, which the lesson checker uses.
func RunModule(ctx context.Context, module []byte) (Result, error) {
	e := &Executor{compilationCache: wazero.NewCompilationCache()}
	defer func() { _ = e.compilationCache.Close(context.WithoutCancel(ctx)) }()
	return e.execute(ctx, module)
}

func (e *Executor) execute(ctx context.Context, module []byte) (Result, error) {
	// WithCloseOnContextDone is what makes the run timeout real. Without it
	// wazero only notices cancellation at a host call, so a guest spinning in a
	// tight loop — `while true { }`, the most likely accident a learner will
	// commit — never yields, the deadline passes unobserved, and the request
	// hangs until the process dies. It costs a check in the generated code and
	// buys the difference between a timeout and a leak.
	cfg := wazero.NewRuntimeConfig().
		WithCompilationCache(e.compilationCache).
		WithCloseOnContextDone(true)

	runtimeCtx := context.WithoutCancel(ctx)
	r := wazero.NewRuntimeWithConfig(runtimeCtx, cfg)
	defer func() { _ = r.Close(context.WithoutCancel(ctx)) }()

	// WASI, and nothing else. The module gets stdout and stderr; it does not
	// get a filesystem, a clock it can use to spin, sockets, or environment.
	// This is the sandbox, and it is enforced by what is not instantiated.
	if _, err := wasi_snapshot_preview1.Instantiate(runtimeCtx, r); err != nil {
		return Result{}, fmt.Errorf("executor: wasi: %w", err)
	}

	// Compiling the module is bounded separately from running it. The
	// compilation cache means this is only slow the first time a given module
	// is seen.
	compileCtx, cancelCompile := context.WithTimeout(runtimeCtx, compileTimeout)
	compiled, err := r.CompileModule(compileCtx, module)
	cancelCompile()
	if err != nil {
		return Result{}, fmt.Errorf("executor: compile module: %w", err)
	}

	ctx, cancel := context.WithTimeout(runtimeCtx, runTimeout)
	defer cancel()

	stdout := &cappedBuffer{limit: maxOutputBytes}
	stderr := &cappedBuffer{limit: maxOutputBytes}

	modCfg := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr).
		WithArgs("exercise").
		// No environment and no filesystem are configured, so the module has
		// neither. Left explicit rather than defaulted so that a later change
		// granting one is a visible decision.
		WithSysNanotime()

	_, err = r.InstantiateModule(ctx, compiled, modCfg)

	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	switch {
	case err == nil:
		return result, nil

	case errors.Is(err, context.DeadlineExceeded):
		result.TimedOut = true
		return result, nil

	default:
		// A non-zero exit is how a Swift program reports its own failure — a
		// trap, a fatalError. It ran; it just did not succeed.
		var exit *sys.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
			return result, nil
		}
		// Anything else is the module being unloadable, which is ours.
		return result, fmt.Errorf("executor: run: %w", err)
	}
}

// cappedBuffer discards everything past a limit, so a program that prints
// without stopping cannot exhaust memory before the timeout fires.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
			c.overflow = true
		} else {
			c.buf.Write(p)
		}
	} else {
		c.overflow = true
	}
	// Always report the full length written. Reporting short would make the
	// guest see a failed write and, in Swift's case, abort with an I/O error
	// rather than simply having its output truncated.
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	if c.overflow {
		return c.buf.String() + "\n… output truncated"
	}
	return c.buf.String()
}
