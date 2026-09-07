package grading

import (
	"context"
	"log/slog"

	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/analytics"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// Validator is the subset of internal/validate this package needs. Declared as
// an interface so a test can decide what validation *means* without a Swift
// toolchain in the loop.
type Validator interface {
	Available() bool
	Validate(ctx context.Context, source string, a lesson.Assertions) (validate.Result, error)
}

// Executor compiles and runs a submission. An interface for the same reason:
// the grader's decisions are worth testing without a 4 GB compiler image.
type Executor interface {
	Available() bool
	Run(ctx context.Context, source string, rt lesson.Runtime) (executor.Result, error)

	// Cached reports whether this submission already has a compiled module.
	// The grader does not use it; callers do, to decide whether to charge
	// quota for a resubmission that costs nothing to serve.
	Cached(source string, rt lesson.Runtime) bool
}

// Service grades submissions and records what it decided.
//
// Every dependency is optional. A nil progress service grades without
// recording, a nil executor grades static assertions and reports the rest as
// pending, and a nil analytics client is replaced with one that discards. That
// is not defensiveness for its own sake: it is how the tests are written and
// how an unconfigured deploy behaves, and both are worth keeping honest.
type Service struct {
	validator Validator
	executor  Executor
	progress  *progress.Service
	analytics analytics.Client
}

func New(v Validator, e Executor, prog *progress.Service, an analytics.Client) *Service {
	if an == nil {
		an = analytics.Nop()
	}
	return &Service{validator: v, executor: e, progress: prog, analytics: an}
}

// Available reports whether anything can be graded at all.
func (s *Service) Available() bool {
	return s.validator != nil && s.validator.Available()
}

// CanExecute reports whether a lesson's runtime assertions can be settled. When
// false, such a lesson grades to OutcomePending rather than to a pass.
func (s *Service) CanExecute(rt lesson.Runtime) bool {
	return rt.Executable() && s.executor != nil && s.executor.Available()
}

// Cached reports whether this exact submission already has a compiled module.
func (s *Service) Cached(code string, rt lesson.Runtime) bool {
	return s.executor != nil && s.executor.Cached(code, rt)
}

// Grade settles a submission and records it.
//
// A returned error is an operational fault, never a verdict: the grader could
// not run, or every compile slot was busy. That distinction is the reason for
// the signature. Telling a learner their correct code is wrong because a
// container failed to start is the one outcome worth going out of the way to
// avoid, so a fault produces no Result, no recorded attempt, and no event
// beyond the submission itself.
//
// executor.ErrBusy is returned unwrapped so callers can match it with errors.Is
// and say "try again in a moment", which is true, rather than "we cannot grade
// this", which is not.
func (s *Service) Grade(ctx context.Context, sub Submission, log *slog.Logger) (Result, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	// Counted before the verdict is known. A failed submission is still a
	// submission, and losing it would make the pass rate look like 100% and
	// hide the lessons nobody can get through.
	s.capture(analytics.EventCheckSubmitted, sub)

	result, err := s.grade(ctx, sub)
	if err != nil {
		return Result{}, err
	}

	s.record(ctx, sub, result, log)
	return result, nil
}

// grade is the verdict on its own, with no recording and no events. Split out
// so the decision can be read in one screen and tested without a database.
func (s *Service) grade(ctx context.Context, sub Submission) (Result, error) {
	l := sub.Lesson

	if s.validator == nil {
		return Result{}, ErrNoValidator
	}

	got, err := s.validator.Validate(ctx, sub.Code, l.Assertions)
	if err != nil {
		return Result{}, err
	}

	result := Result{Failures: got.Failures, Diagnostics: got.Diagnostics}

	// Static checks first, and a failure there ends it: there is no point
	// compiling code that already breaks the lesson's rules.
	if !got.OK {
		result.Outcome = OutcomeStaticFailed
		return result, nil
	}

	// Everything static passed. If the lesson only asks static questions, that
	// is the whole answer.
	if !l.Assertions.NeedsExecution() {
		result.Outcome = OutcomePassed
		return result, nil
	}

	// The rest can only be settled by running the code.
	if !s.CanExecute(l.Runtime) {
		result.Outcome = OutcomePending
		return result, nil
	}

	run, err := s.executor.Run(ctx, sub.Code, l.Runtime)
	if err != nil {
		return Result{}, err
	}

	switch {
	case !run.Compiled:
		result.CompilerOutput = run.Diagnostics
		result.Outcome = OutcomeCompileFailed

	case run.TimedOut:
		result.Outcome = OutcomeTimedOut

	default:
		result.Stdout = run.Stdout
		result.Stderr = run.Stderr
		result.ExitCode = run.ExitCode
		result.Failures = append(result.Failures, validate.CheckOutput(run.Stdout, l.Assertions)...)
		// A program that trapped has not satisfied its lesson, whatever it
		// managed to print before dying.
		if len(result.Failures) == 0 && run.ExitCode == 0 {
			result.Outcome = OutcomePassed
		} else {
			result.Outcome = OutcomeOutputMismatch
		}
	}

	return result, nil
}

// record persists the attempt and fires the events its outcome earns.
//
// Nothing here can fail a submission. A learner who solved a lesson solved it
// whether or not the row was written, so a write failure is logged and the
// verdict stands.
func (s *Service) record(ctx context.Context, sub Submission, result Result, log *slog.Logger) {
	if result.Passed() {
		s.capture(analytics.EventCheckPassed, sub)
	}

	// A guest is graded but not remembered: exercise_attempt.user_id is NOT
	// NULL, so there is nobody to attribute the row to.
	if !sub.SignedIn() || s.progress == nil {
		return
	}

	if _, err := s.progress.RecordAttempt(ctx, sub.UserID, sub.Lesson.Slug, sub.Code, result.Passed(), string(sub.Provenance.OrUnknown())); err != nil {
		log.Error("could not record attempt", slog.Any("error", err))
	}

	if !result.Passed() {
		return
	}

	if err := s.progress.Complete(ctx, sub.UserID, sub.Lesson.Slug); err != nil {
		log.Error("could not complete lesson", slog.Any("error", err))
		return
	}
	s.capture(analytics.EventLessonCompleted, sub)
}

// FailedAttempts reports how many wrong answers a signed-in learner has
// submitted for a lesson. It is what gates the hint and the solution reveal.
func (s *Service) FailedAttempts(ctx context.Context, sub Submission) (int, error) {
	if !sub.SignedIn() || s.progress == nil {
		return 0, nil
	}
	return s.progress.FailedAttempts(ctx, sub.UserID, sub.Lesson.Slug)
}

// capture records one product event. It never returns an error and never fails
// a submission: an analytics outage must be invisible to a learner.
func (s *Service) capture(name string, sub Submission) {
	s.analytics.Capture(analytics.Event{
		Name:       name,
		DistinctID: sub.DistinctID,
		// The dimensions a decision gets sliced by. Never the submitted
		// source: that is something a person wrote, and it belongs in a
		// feedback table they opted into, not in an event stream.
		Props: analytics.Props(
			"lesson", sub.Lesson.Slug,
			"track", sub.Lesson.Track,
			"runtime", string(sub.Lesson.Runtime),
			"signed_in", sub.SignedIn(),
			"channel", string(sub.Channel),
		),
	})
}
