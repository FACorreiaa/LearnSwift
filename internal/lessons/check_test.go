package lessons

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/compiler"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// fakeValidator stands in for the Swift binary. The handler's job is deciding
// what a validation result *means* — pass, pending, failure, or outage — and
// that decision is worth testing without a Swift toolchain in the loop.
type fakeValidator struct {
	result validate.Result
	err    error
}

func (f fakeValidator) Available() bool { return f.err == nil }

func (f fakeValidator) Validate(context.Context, string, lesson.Assertions) (validate.Result, error) {
	return f.result, f.err
}

func checkRequest(t *testing.T, h *Handler, slug, code string) *httptest.ResponseRecorder {
	t.Helper()

	r := chi.NewRouter()
	h.Routes(r)

	form := url.Values{"code": {code}}
	req := httptest.NewRequest(http.MethodPost, "/lessons/"+slug+"/check", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// testIndex builds an index holding one lesson with the given assertions.
func testIndex(t *testing.T, a lesson.Assertions) *Index {
	t.Helper()

	index := &Index{bySlug: map[string]lesson.Lesson{}}
	l := lesson.Lesson{
		Slug:       "x",
		Title:      "X",
		Track:      "swift-basics",
		Minutes:    1,
		Runtime:    lesson.RuntimeEmbedded,
		Assertions: a,
	}
	index.bySlug["x"] = l
	index.ordered = []lesson.Lesson{l}
	return index
}

// The bug this guards: a lesson whose real test is its output would otherwise
// report "that's right" the first time an unfinished starter satisfied its
// static checks, because there was nothing left to disagree with.
func TestALessonNeedingOutputIsNeverReportedAsPassedYet(t *testing.T) {
	index := testIndex(t, lesson.Assertions{
		MustNotUse:     []string{"!"},
		OutputContains: []string{"Hello, World!"},
	})
	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	rec := checkRequest(t, h, "x", "let x = 1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — passing the static checks is not a rejection", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "That's right") {
		t.Error("an ungradable submission was reported as correct")
	}
	if !strings.Contains(body, "not a pass") {
		t.Errorf("the result does not explain that it is not a pass: %s", body)
	}
}

func TestALessonWithOnlyStaticAssertionsCanActuallyPass(t *testing.T) {
	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"@State"}})
	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	rec := checkRequest(t, h, "x", "@State private var count = 0")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "That's right") {
		t.Error("a fully graded correct answer was not reported as correct")
	}
}

func TestAFailedCheckIs422SoHtmxSwapsIt(t *testing.T) {
	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"@State"}})
	h := NewHandler(index, nil, fakeValidator{result: validate.Result{
		OK:       false,
		Failures: []validate.Failure{{Kind: "must_declare", Token: "@State", Message: "Your code needs to use @State."}},
	}}, nil, nil, false)

	rec := checkRequest(t, h, "x", "var count = 0")

	// 422 rather than 400: the layout's htmx config swaps a 422 into the page,
	// so the learner sees the reason instead of the response being discarded.
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "needs to use @State") {
		t.Error("the failure message was not rendered")
	}
}

// A grader that cannot run is an outage, not a wrong answer. Reporting it as a
// failure would tell a learner their correct code is wrong.
func TestAnUnavailableGraderIsNotReportedAsAWrongAnswer(t *testing.T) {
	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"@State"}})
	h := NewHandler(index, nil, fakeValidator{err: errors.New("boom")}, nil, nil, false)

	rec := checkRequest(t, h, "x", "whatever")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "That's right") {
		t.Error("an outage was reported as a pass")
	}
	if !strings.Contains(body, "not with your code") {
		t.Errorf("the outage does not make clear the learner is not at fault: %s", body)
	}
}

func TestALessonWithNothingCheckableRefusesTheEndpoint(t *testing.T) {
	index := testIndex(t, lesson.Assertions{OutputContains: []string{"x"}})
	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	rec := checkRequest(t, h, "x", "let x = 1")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a lesson with no static assertions", rec.Code)
	}
}

func TestCheckingAnUnknownLessonIs404(t *testing.T) {
	h := NewHandler(testIndex(t, lesson.Assertions{MustDeclare: []string{"x"}}), nil,
		fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	if rec := checkRequest(t, h, "nope", "code"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestAnOversizedSubmissionIsRefused(t *testing.T) {
	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"x"}})
	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	rec := checkRequest(t, h, "x", strings.Repeat("a", maxSubmissionBytes+1))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// Static checking is about reading the code, so it must not be gated on the
// lesson being runnable — SwiftUI parses fine and simply cannot be executed.
func TestStaticCheckingWorksForALessonThatCannotBeExecuted(t *testing.T) {
	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"@State"}})
	l := index.bySlug["x"]
	l.Runtime = lesson.RuntimeNone
	index.bySlug["x"] = l

	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	if rec := checkRequest(t, h, "x", "@State var c = 0"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a non-executable lesson can still be read", rec.Code)
	}
}

// fakeExecutor stands in for the compile-and-run pipeline, so the handler's
// decisions can be tested without a 4 GB image.
type fakeExecutor struct {
	result executor.Result
	err    error
}

func (f fakeExecutor) Available() bool {
	return f.err == nil || !errors.Is(f.err, compiler.ErrUnavailable)
}

func (f fakeExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	return f.result, f.err
}

// Cached always reports false: these tests are about what the handler decides,
// and a fake that claimed a cache hit would quietly exempt every submission
// from the quota the rate-limit tests are checking.
func (f fakeExecutor) Cached(string, lesson.Runtime) bool { return false }

func outputLesson(t *testing.T) *Index {
	t.Helper()
	return testIndex(t, lesson.Assertions{
		MustNotUse:     []string{"!"},
		OutputContains: []string{"Hello, World!"},
	})
}

// The whole point of Phase 2: a lesson whose real test is its output can now
// actually be passed, rather than sitting permanently pending.
func TestALessonNeedingOutputPassesWhenTheOutputIsRight(t *testing.T) {
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true, Stdout: "Hello, World!\n"}}, nil, false)

	rec := checkRequest(t, h, "x", `print("Hello, World!")`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "That's right") {
		t.Errorf("correct output was not accepted: %s", rec.Body.String())
	}
}

func TestWrongOutputFailsAndShowsBothSides(t *testing.T) {
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true, Stdout: "Goodbye\n"}}, nil, false)

	rec := checkRequest(t, h, "x", `print("Goodbye")`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello, World!") || !strings.Contains(body, "Goodbye") {
		t.Errorf("the failure does not show what was wanted and what was printed: %s", body)
	}
}

// A program that trapped has not satisfied its lesson, whatever it printed
// before dying.
func TestANonZeroExitFailsEvenWhenTheOutputMatches(t *testing.T) {
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true, Stdout: "Hello, World!\n", ExitCode: 1}}, nil, false)

	rec := checkRequest(t, h, "x", "code")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 — the program crashed", rec.Code)
	}
}

func TestCodeThatDoesNotCompileShowsTheCompilerOutput(t *testing.T) {
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: false, Diagnostics: "main.swift:1:1: error: cannot find 'nope'"}}, nil, false)

	rec := checkRequest(t, h, "x", "nope")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cannot find") {
		t.Error("swiftc's diagnostics were not shown to the learner")
	}
}

func TestARunawayProgramIsReportedAsSuch(t *testing.T) {
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true, TimedOut: true}}, nil, false)

	rec := checkRequest(t, h, "x", "while true { }")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ran too long") {
		t.Error("a timeout was not explained to the learner")
	}
}

// Static failures end the check before compiling: there is no point building
// code that already breaks the lesson's rules, and a compiler error would bury
// the simpler explanation.
func TestAStaticFailureSkipsCompilingEntirely(t *testing.T) {
	ran := false
	h := NewHandler(outputLesson(t), nil,
		fakeValidator{result: validate.Result{
			OK:       false,
			Failures: []validate.Failure{{Kind: "must_not_use", Token: "!", Message: "This exercise asks you not to use !."}},
		}},
		trackingExecutor{ran: &ran}, nil, false)

	rec := checkRequest(t, h, "x", "print(x!)")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if ran {
		t.Error("the submission was compiled despite already failing a static check")
	}
}

type trackingExecutor struct{ ran *bool }

func (t trackingExecutor) Available() bool { return true }

func (t trackingExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	*t.ran = true
	return executor.Result{Compiled: true, Stdout: "Hello, World!"}, nil
}

func (t trackingExecutor) Cached(string, lesson.Runtime) bool { return false }

// With no executor configured, a lesson needing output cannot be settled — and
// must not be reported as passed.
func TestWithoutAnExecutorAnOutputLessonStaysPending(t *testing.T) {
	h := NewHandler(outputLesson(t), nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	rec := checkRequest(t, h, "x", "code")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "That's right") {
		t.Error("an unrun submission was reported as correct")
	}
	if !strings.Contains(body, "not a pass") {
		t.Errorf("the pending state was not explained: %s", body)
	}
}
