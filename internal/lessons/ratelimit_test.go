package lessons

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// cachedExecutor reports every submission as already compiled, which is the
// condition under which a check must not be charged against the quota.
type cachedExecutor struct{ runs *int }

func (c cachedExecutor) Available() bool { return true }

func (c cachedExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	if c.runs != nil {
		*c.runs++
	}
	return executor.Result{Compiled: true, Stdout: "Hello, World!"}, nil
}

func (c cachedExecutor) Cached(string, lesson.Runtime) bool { return true }

// busyExecutor stands for every compile slot being taken.
type busyExecutor struct{}

func (busyExecutor) Available() bool { return true }

func (busyExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	return executor.Result{}, executor.ErrBusy
}

func (busyExecutor) Cached(string, lesson.Runtime) bool { return false }

func staticLesson(t *testing.T) *Index {
	t.Helper()
	return testIndex(t, lesson.Assertions{MustDeclare: []string{"let"}})
}

// The blocker this guards: /check is unauthenticated and starts a Swift
// toolchain container on a cache miss. Unmetered, one script saturates the
// compiler host.
func TestAGuestIsCutOffAfterTheCheckLimit(t *testing.T) {
	h := NewHandler(staticLesson(t), nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	for i := range guestCheckLimit {
		rec := checkRequest(t, h, "x", "let a = 1")
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("cut off at submission %d, before the limit of %d", i+1, guestCheckLimit)
		}
	}

	rec := checkRequest(t, h, "x", "let a = 1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the limit is spent", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q, want a positive whole number of seconds", got)
	}
}

// Being rate limited must read as "wait a minute", not as a failed check: the
// panel swaps into the result slot so the editor and the learner's code survive.
func TestTheQuotaPanelExplainsItselfAndOffersAnAccount(t *testing.T) {
	h := NewHandler(staticLesson(t), nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	for range guestCheckLimit {
		checkRequest(t, h, "x", "let a = 1")
	}
	body := checkRequest(t, h, "x", "let a = 1").Body.String()

	if !strings.Contains(body, `id="exercise-result"`) {
		t.Errorf("quota panel does not target the result slot, so htmx will not swap it: %s", body)
	}
	if !strings.Contains(body, "/register") {
		t.Errorf("the guest panel does not mention the bigger allowance an account buys: %s", body)
	}
	if strings.Contains(body, "did not compile") || strings.Contains(body, "That's right") {
		t.Errorf("being throttled was reported as a verdict on the code: %s", body)
	}
}

// A cache hit is a hash lookup and a wasm instantiation in this process. Billing
// it would meter the one action that costs nothing, and would punish a learner
// who resubmits unchanged code to re-read its output.
func TestAnAlreadyCompiledSubmissionIsNotChargedQuota(t *testing.T) {
	runs := 0
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustNotUse: []string{"!"}, OutputContains: []string{"Hello, World!"}}),
		nil,
		fakeValidator{result: validate.Result{OK: true}},
		cachedExecutor{runs: &runs},
		nil,
		false,
	)

	// Comfortably past the guest limit. Every one of these is a cache hit.
	for i := range guestCheckLimit * 2 {
		rec := checkRequest(t, h, "x", `print("Hello, World!")`)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("a cached submission was charged quota and refused at attempt %d", i+1)
		}
	}
	if runs != guestCheckLimit*2 {
		t.Errorf("executor ran %d times, want %d", runs, guestCheckLimit*2)
	}
}

// A busy compiler is not a wrong answer. Reporting it as one would tell a
// learner their correct code failed because other people were also submitting.
func TestABusyCompilerIsReportedAsBusyNotAsAFailedCheck(t *testing.T) {
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustNotUse: []string{"!"}, OutputContains: []string{"Hello, World!"}}),
		nil,
		fakeValidator{result: validate.Result{OK: true}},
		busyExecutor{},
		nil,
		false,
	)

	rec := checkRequest(t, h, "x", `print("Hello, World!")`)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 when every compile slot is taken", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Nothing is wrong with what you wrote") {
		t.Errorf("busy panel blames the learner or is missing: %s", body)
	}
}

// unavailableExecutor stands for the compiler image being missing — the case an
// operator hits after a container runtime is reinstalled.
type unavailableExecutor struct{}

func (unavailableExecutor) Available() bool { return true }

func (unavailableExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	return executor.Result{}, errors.New("compiler: unavailable: no such image")
}

func (unavailableExecutor) Cached(string, lesson.Runtime) bool { return false }

// A grader that cannot run must say so to the learner. The panel for it already
// existed and was rendered at 503, which the layout's htmx config discarded —
// so the visible result was nothing at all happening, which is worse than the
// wrong verdict the panel was written to avoid.
func TestAnUnavailableGraderRendersAPanelThatHtmxWillSwap(t *testing.T) {
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustNotUse: []string{"!"}, OutputContains: []string{"Hello, World!"}}),
		nil,
		fakeValidator{result: validate.Result{OK: true}},
		unavailableExecutor{},
		nil,
		false,
	)

	rec := checkRequest(t, h, "x", `print("Hello, World!")`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "not with your code") {
		t.Errorf("the unavailable panel does not absolve the learner: %s", body)
	}
	if !strings.Contains(body, `id="exercise-result"`) {
		t.Errorf("the panel does not target the result slot: %s", body)
	}
}
