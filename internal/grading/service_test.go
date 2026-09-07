package grading

import (
	"context"
	"errors"
	"testing"

	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/shared/analytics"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// fakeValidator stands in for the Swift binary. What is worth testing is the
// grader's reading of a validation result — pass, failure, or outage — and that
// reading does not need a Swift toolchain to exercise.
type fakeValidator struct {
	result validate.Result
	err    error
	calls  int
}

func (f *fakeValidator) Available() bool { return f.err == nil }

func (f *fakeValidator) Validate(context.Context, string, lesson.Assertions) (validate.Result, error) {
	f.calls++
	return f.result, f.err
}

// fakeExecutor stands in for the compile pipeline, for the same reason: a 4 GB
// image is too much to ask of a unit test.
type fakeExecutor struct {
	result    executor.Result
	err       error
	cached    bool
	available bool
	calls     int
}

func (f *fakeExecutor) Available() bool { return f.available }

func (f *fakeExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeExecutor) Cached(string, lesson.Runtime) bool { return f.cached }

// recorder collects events instead of sending them.
type recorder struct{ events []analytics.Event }

func (r *recorder) Capture(e analytics.Event)   { r.events = append(r.events, e) }
func (r *recorder) Close(context.Context) error { return nil }

func (r *recorder) names() []string {
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Name)
	}
	return out
}

func (r *recorder) has(name string) bool {
	for _, got := range r.names() {
		if got == name {
			return true
		}
	}
	return false
}

// staticOnly asks a question that can be settled without running anything.
func staticOnly() lesson.Lesson {
	return lesson.Lesson{
		Slug:       "x",
		Track:      "swift-basics",
		Runtime:    lesson.RuntimeEmbedded,
		Assertions: lesson.Assertions{MustDeclare: []string{"let"}},
	}
}

// needsOutput asks a question only running the code can answer.
func needsOutput() lesson.Lesson {
	l := staticOnly()
	l.Assertions.OutputContains = []string{"hello"}
	return l
}

func submissionFor(l lesson.Lesson) Submission {
	return Submission{
		Lesson:     l,
		Code:       "let a = 1",
		Channel:    ChannelWeb,
		Provenance: ProvenanceTyped,
		DistinctID: "anon_test",
	}
}

// The outcome is the grader's whole output, so every one of them is worth
// pinning. A new outcome that renders as an ordinary wrong answer is the bug
// this table exists to catch.
func TestEveryOutcome(t *testing.T) {
	pass := validate.Result{OK: true}

	cases := []struct {
		name     string
		lesson   lesson.Lesson
		validate validate.Result
		exec     *fakeExecutor
		want     Outcome
		wantRun  bool
	}{
		{
			name:     "a broken rule is not compiled",
			lesson:   needsOutput(),
			validate: validate.Result{OK: false, Failures: []validate.Failure{{}}},
			exec:     &fakeExecutor{available: true},
			want:     OutcomeStaticFailed,
			wantRun:  false,
		},
		{
			name:     "a static-only lesson is settled without running",
			lesson:   staticOnly(),
			validate: pass,
			exec:     &fakeExecutor{available: true},
			want:     OutcomePassed,
			wantRun:  false,
		},
		{
			name:     "no executor leaves the question open",
			lesson:   needsOutput(),
			validate: pass,
			exec:     &fakeExecutor{available: false},
			want:     OutcomePending,
			wantRun:  false,
		},
		{
			name:     "swiftc rejecting it is its own outcome",
			lesson:   needsOutput(),
			validate: pass,
			exec: &fakeExecutor{available: true, result: executor.Result{
				Compiled: false, Diagnostics: "error: expected expression",
			}},
			want:    OutcomeCompileFailed,
			wantRun: true,
		},
		{
			name:     "a runaway program is not a wrong answer",
			lesson:   needsOutput(),
			validate: pass,
			exec: &fakeExecutor{available: true, result: executor.Result{
				Compiled: true, TimedOut: true,
			}},
			want:    OutcomeTimedOut,
			wantRun: true,
		},
		{
			name:     "the wrong output is a mismatch",
			lesson:   needsOutput(),
			validate: pass,
			exec: &fakeExecutor{available: true, result: executor.Result{
				Compiled: true, Stdout: "goodbye\n",
			}},
			want:    OutcomeOutputMismatch,
			wantRun: true,
		},
		{
			name:     "a program that trapped has not passed",
			lesson:   needsOutput(),
			validate: pass,
			exec: &fakeExecutor{available: true, result: executor.Result{
				Compiled: true, Stdout: "hello\n", ExitCode: 1,
			}},
			want:    OutcomeOutputMismatch,
			wantRun: true,
		},
		{
			name:     "the right output and a clean exit is a pass",
			lesson:   needsOutput(),
			validate: pass,
			exec: &fakeExecutor{available: true, result: executor.Result{
				Compiled: true, Stdout: "hello\n", ExitCode: 0,
			}},
			want:    OutcomePassed,
			wantRun: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(&fakeValidator{result: tc.validate}, tc.exec, nil, nil)

			got, err := s.Grade(context.Background(), submissionFor(tc.lesson), nil)
			if err != nil {
				t.Fatalf("Grade: unexpected error: %v", err)
			}
			if got.Outcome != tc.want {
				t.Errorf("outcome = %q, want %q", got.Outcome, tc.want)
			}
			if got.Passed() != (tc.want == OutcomePassed) {
				t.Errorf("Passed() = %v for outcome %q", got.Passed(), got.Outcome)
			}
			if ran := tc.exec.calls > 0; ran != tc.wantRun {
				t.Errorf("executor ran = %v, want %v", ran, tc.wantRun)
			}
		})
	}
}

// The reason Grade returns an error at all: an outage must not reach a learner
// as a verdict on their code.
func TestAnOutageIsNotAVerdict(t *testing.T) {
	boom := errors.New("swift-validate: exit status 127")

	for _, tc := range []struct {
		name string
		svc  *Service
		want error
	}{
		{
			name: "a validator that cannot run",
			svc:  New(&fakeValidator{err: boom}, nil, nil, nil),
			want: boom,
		},
		{
			name: "no validator at all",
			svc:  New(nil, nil, nil, nil),
			want: ErrNoValidator,
		},
		{
			name: "an executor that cannot run",
			svc: New(
				&fakeValidator{result: validate.Result{OK: true}},
				&fakeExecutor{available: true, err: boom},
				nil, nil,
			),
			want: boom,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.svc.Grade(context.Background(), submissionFor(needsOutput()), nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if got.Outcome != "" {
				t.Errorf("an outage produced a verdict: %+v", got)
			}
		})
	}
}

// ErrBusy has to survive the trip unwrapped, because the caller's whole reason
// for matching it is to say "try again in a moment" rather than "we cannot
// grade this". One is true and the other is not.
func TestBusyIsDistinguishableFromBroken(t *testing.T) {
	s := New(
		&fakeValidator{result: validate.Result{OK: true}},
		&fakeExecutor{available: true, err: executor.ErrBusy},
		nil, nil,
	)

	if _, err := s.Grade(context.Background(), submissionFor(needsOutput()), nil); !errors.Is(err, executor.ErrBusy) {
		t.Fatalf("error = %v, want executor.ErrBusy", err)
	}
}

// A failed submission is still a submission. Losing it would make the pass rate
// look like 100% and hide the lessons nobody can get through.
func TestASubmissionIsCountedBeforeItIsJudged(t *testing.T) {
	rec := &recorder{}
	s := New(&fakeValidator{err: errors.New("down")}, nil, nil, rec)

	if _, err := s.Grade(context.Background(), submissionFor(staticOnly()), nil); err == nil {
		t.Fatal("expected an error from a validator that is down")
	}
	if !rec.has(analytics.EventCheckSubmitted) {
		t.Errorf("submission was not counted: %v", rec.names())
	}
	if rec.has(analytics.EventCheckPassed) {
		t.Errorf("an outage was recorded as a pass: %v", rec.names())
	}
}

// The channel is the point of the whole refactor: two ways in, one verdict, and
// a label that says which door was used.
func TestTheChannelIsRecorded(t *testing.T) {
	rec := &recorder{}
	s := New(&fakeValidator{result: validate.Result{OK: true}}, nil, nil, rec)

	sub := submissionFor(staticOnly())
	sub.Channel = ChannelMCP

	if _, err := s.Grade(context.Background(), sub, nil); err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if len(rec.events) == 0 {
		t.Fatal("no events captured")
	}
	for _, e := range rec.events {
		if e.Props["channel"] != string(ChannelMCP) {
			t.Errorf("%s: channel = %v, want %q", e.Name, e.Props["channel"], ChannelMCP)
		}
	}
}

// The submitted source is something a person wrote. It belongs in a feedback
// table they opted into, never in an analytics payload — and moving the capture
// into this package must not be how that guarantee gets lost.
func TestNoSubmittedCodeIsEverSentToAnalytics(t *testing.T) {
	rec := &recorder{}
	s := New(&fakeValidator{result: validate.Result{OK: true}}, nil, nil, rec)

	const secret = `let apiKey = "hunter2"`
	sub := submissionFor(staticOnly())
	sub.Code = secret

	if _, err := s.Grade(context.Background(), sub, nil); err != nil {
		t.Fatalf("Grade: %v", err)
	}
	for _, e := range rec.events {
		for key, value := range e.Props {
			if got, ok := value.(string); ok && got == secret {
				t.Fatalf("submitted source was captured in property %q", key)
			}
		}
	}
}

// A guest is graded exactly like anyone else. They simply have nowhere for the
// attempt to be recorded, and a nil progress service must not turn that into a
// crash.
func TestAGuestIsGradedWithoutBeingRecorded(t *testing.T) {
	s := New(&fakeValidator{result: validate.Result{OK: true}}, nil, nil, nil)

	sub := submissionFor(staticOnly())
	if sub.SignedIn() {
		t.Fatal("the zero UUID must not read as signed in")
	}

	got, err := s.Grade(context.Background(), sub, nil)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if !got.Passed() {
		t.Errorf("outcome = %q, want a pass", got.Outcome)
	}
}

func TestUnrecognisedProvenanceIsRejectedAsALabelNotASubmission(t *testing.T) {
	if Provenance("copilot").Valid() {
		t.Error("an unknown label must not validate")
	}
	for _, p := range []Provenance{ProvenanceUnknown, ProvenanceTyped, ProvenanceMixed, ProvenancePasted, ProvenanceAgent} {
		if !p.Valid() {
			t.Errorf("%q must validate", p)
		}
	}
}

// ProvenanceAgent means "an agent authenticated as this learner and called a
// tool". A form post cannot be that, however it labels itself, and the Solo
// board is only worth reading if the reverse is also true — a browser must not
// be able to claim a label it did not earn, in either direction.
func TestABrowserCannotClaimToBeAnAgent(t *testing.T) {
	if got := ProvenanceFromClient("agent"); got != ProvenanceUnknown {
		t.Errorf("a form post claiming to be an agent got %q, want %q", got, ProvenanceUnknown)
	}

	for _, in := range []string{"", "copilot", "AGENT", "typed ", "'; drop table --"} {
		if got := ProvenanceFromClient(in); got != ProvenanceUnknown {
			t.Errorf("ProvenanceFromClient(%q) = %q, want %q", in, got, ProvenanceUnknown)
		}
	}

	for _, in := range []Provenance{ProvenanceTyped, ProvenanceMixed, ProvenancePasted} {
		if got := ProvenanceFromClient(string(in)); got != in {
			t.Errorf("ProvenanceFromClient(%q) = %q, want it preserved", in, got)
		}
	}
}
