package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FACorreiaa/seshat/internal/compiler"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// countingCompiler reports the highest number of compiles ever in flight at
// once, which is the only thing the admission ceiling actually promises.
type countingCompiler struct {
	mu      sync.Mutex
	inside  int
	highest int

	// hold is how long each compile pretends to take. Without it every call
	// finishes before the next starts and the ceiling is never approached.
	hold time.Duration
}

func (c *countingCompiler) Available() bool { return true }

func (c *countingCompiler) Compile(_ context.Context, source string, _ lesson.Runtime) (compiler.Result, error) {
	c.mu.Lock()
	c.inside++
	if c.inside > c.highest {
		c.highest = c.inside
	}
	c.mu.Unlock()

	time.Sleep(c.hold)

	c.mu.Lock()
	c.inside--
	c.mu.Unlock()

	// Diagnostics rather than a module: Run stops at a failed compile, which
	// keeps this test away from wazero entirely.
	return compiler.Result{Diagnostics: "error: " + source}, nil
}

func (c *countingCompiler) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.highest
}

// A per-caller rate limit bounds how often one visitor may ask. This bounds how
// many visitors may be served at once — the difference between arriving on a
// front page being a slow minute and being a dead host.
func TestConcurrentCompilesNeverExceedTheCeiling(t *testing.T) {
	const (
		ceiling = 3
		callers = 40
	)

	c := &countingCompiler{hold: 2 * time.Millisecond}
	e := New(c, NewMemoryCache(0), ceiling)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Distinct sources, so every call is a cache miss and has to take a
			// slot. Identical sources would collapse onto one cache entry and
			// the test would prove nothing.
			_, _ = e.Run(context.Background(), fmt.Sprintf("let a = %d", i), lesson.RuntimeEmbedded)
		}()
	}
	wg.Wait()

	if peak := c.peak(); peak > ceiling {
		t.Errorf("%d compiles ran at once, ceiling is %d", peak, ceiling)
	}
	if c.peak() == 0 {
		t.Error("no compile was observed; the test proved nothing")
	}
}

// Zero means unbounded, which is right for a test helper and wrong for anything
// serving the internet. Worth pinning so it is not "fixed" into a default.
func TestAZeroCeilingLeavesCompilesUnbounded(t *testing.T) {
	e := New(&countingCompiler{}, NewMemoryCache(0), 0)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	if e.compileSlots != nil {
		t.Error("a zero ceiling created a semaphore, so compiles are being queued unexpectedly")
	}
}

// A caller that gives up must not leave its slot taken. The release is what
// makes the ceiling a ceiling rather than a countdown to permanent deadlock.
func TestASlotIsReturnedAfterEveryCompile(t *testing.T) {
	c := &countingCompiler{}
	e := New(c, NewMemoryCache(0), 1)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	for i := range 5 {
		if _, err := e.Run(context.Background(), fmt.Sprintf("let a = %d", i), lesson.RuntimeEmbedded); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if len(e.compileSlots) != 0 {
		t.Errorf("%d slots still held after every compile returned", len(e.compileSlots))
	}
}

// Waiting forever for a slot turns a burst into a pile-up of held connections.
// Past the timeout the honest answer is that the compiler is busy.
func TestAFullQueueEventuallyReportsBusy(t *testing.T) {
	e := New(&countingCompiler{}, NewMemoryCache(0), 1)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	// Take the only slot and keep it.
	release, err := e.admit(context.Background())
	if err != nil {
		t.Fatalf("first admit: %v", err)
	}
	defer release()

	// A cancelled context stands in for the timeout, so the test does not have
	// to wait out admissionTimeout to prove the wait is bounded.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.admit(ctx); err == nil {
		t.Error("a second admit succeeded while the only slot was held")
	}
}

// A submission still being compiled is not cached, and a submission that cannot
// run is never cached at all — the handler uses this to decide what to charge.
func TestCachedIsFalseForRuntimesThatCannotRun(t *testing.T) {
	e := New(&countingCompiler{}, NewMemoryCache(1<<20), 1)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	if e.Cached("anything", lesson.RuntimeNone) {
		t.Error("a lesson that cannot be executed reported a cached module")
	}
	if e.Cached("never compiled", lesson.RuntimeEmbedded) {
		t.Error("an uncompiled submission reported a cached module")
	}

	// A module put into the cache under the real key is found again, which is
	// what makes the quota exemption fire for a resubmission.
	source := "let a = 1"
	e.cache.Put(CacheKey(source, lesson.RuntimeEmbedded, toolchainVersion), []byte{0x00})
	if !e.Cached(source, lesson.RuntimeEmbedded) {
		t.Error("a compiled submission was not recognised as cached")
	}
}

func TestErrBusyIsMatchable(t *testing.T) {
	if !errors.Is(fmt.Errorf("wrapped: %w", ErrBusy), ErrBusy) {
		t.Error("ErrBusy does not survive wrapping, so handlers cannot tell busy from broken")
	}
}
