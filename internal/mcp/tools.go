// Package mcp lets a learner work through the lessons from their own editor.
//
// The premise is that a developer with an agent already has a place they like
// writing Swift, and asking them to leave it to type into a textarea is asking
// them to stop. So the lesson corpus and the grader are exposed as MCP tools:
// the agent fetches a lesson, the learner works on it wherever they work, and
// the submission is graded by the same pipeline the browser uses.
//
// Two things follow from that, and they are the whole design.
//
// The first is that no lesson payload here carries its solution. That is not a
// secrecy measure — the repository is public — it is that a tool which returns
// the answer alongside the question is a tool an agent will answer with.
//
// The second is that every submission arriving this way is recorded as
// assisted, and the tool descriptions say so. Seshat cannot tell whether the
// learner reasoned it out and dictated it or whether the model wrote it
// unprompted, and it does not try to. It records the door that was used, which
// it knows for certain, and lets the two counts be different numbers.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/grading"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
)

// maxSubmissionBytes bounds a tool call's source, matching the browser's cap.
// An agent can generate a great deal more Swift than a person will type, and
// the grader's own limits are not a reason to accept it first.
const maxSubmissionBytes = 64 << 10

// ListLessonsInput optionally narrows to one track.
type ListLessonsInput struct {
	Track string `json:"track,omitempty" jsonschema:"only lessons in this track, by slug; omit for all tracks"`
}

type ListLessonsOutput struct {
	Tracks []TrackView `json:"tracks"`
}

type TrackView struct {
	Slug    string          `json:"slug"`
	Title   string          `json:"title"`
	Lessons []LessonSummary `json:"lessons"`
}

type LessonSummary struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Minutes int    `json:"minutes"`
	Runtime string `json:"runtime"`

	// Status is this learner's own: "completed", "in_progress", or "" for a
	// lesson they have not opened.
	Status string `json:"status,omitempty"`
}

func (s *Server) listLessons(ctx context.Context, userID uuid.UUID, in ListLessonsInput) (ListLessonsOutput, error) {
	done, err := s.statuses(ctx, userID)
	if err != nil {
		return ListLessonsOutput{}, err
	}

	want := strings.TrimSpace(in.Track)
	out := ListLessonsOutput{}

	for _, track := range s.index.Tracks() {
		if want != "" && track.Slug != want {
			continue
		}

		view := TrackView{Slug: track.Slug, Title: track.Title}
		for _, l := range track.Lessons {
			view.Lessons = append(view.Lessons, LessonSummary{
				Slug:    l.Slug,
				Title:   l.Title,
				Summary: l.Summary,
				Minutes: l.Minutes,
				Runtime: string(l.Runtime),
				Status:  done[l.Slug],
			})
		}
		out.Tracks = append(out.Tracks, view)
	}

	if want != "" && len(out.Tracks) == 0 {
		return ListLessonsOutput{}, fmt.Errorf("no track called %q; call list_lessons with no track to see them all", want)
	}
	return out, nil
}

type GetLessonInput struct {
	Slug string `json:"slug" jsonschema:"the lesson's slug, as returned by list_lessons"`
}

type GetLessonOutput struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Track   string `json:"track"`
	Summary string `json:"summary"`
	Minutes int    `json:"minutes"`

	// Body is the lesson in markdown, as written.
	Body string `json:"body"`

	// Starter is the code the exercise begins from. A submission is expected to
	// be this, edited — not a fresh file.
	Starter string `json:"starter"`

	// Requirements describes what the grader will check, in prose. An agent
	// that knows the assertions writes to them; one that does not guesses and
	// then loops on the same failure.
	Requirements []string `json:"requirements"`

	Runtime string `json:"runtime"`

	// RuntimeLimits is the part most worth reading. The embedded runtime has no
	// concurrency and no Unicode tables, and code that ignores that does not
	// fail a check — it fails to compile, repeatedly.
	RuntimeLimits string `json:"runtime_limits"`

	// Checkable is false for a lesson with nothing the grader can settle, so a
	// client is told before it submits rather than after.
	Checkable bool `json:"checkable"`

	Status string `json:"status,omitempty"`
}

func (s *Server) getLesson(ctx context.Context, userID uuid.UUID, in GetLessonInput) (GetLessonOutput, error) {
	l, ok := s.index.Get(strings.TrimSpace(in.Slug))
	if !ok {
		return GetLessonOutput{}, fmt.Errorf("no lesson called %q; call list_lessons to see the slugs", in.Slug)
	}

	done, err := s.statuses(ctx, userID)
	if err != nil {
		return GetLessonOutput{}, err
	}

	// Solution and Hint are both absent by construction: there is no field on
	// this struct that could carry them.
	return GetLessonOutput{
		Slug:          l.Slug,
		Title:         l.Title,
		Track:         l.Track,
		Summary:       l.Summary,
		Minutes:       l.Minutes,
		Body:          l.Body,
		Starter:       l.Starter,
		Requirements:  describeAssertions(l.Assertions),
		Runtime:       string(l.Runtime),
		RuntimeLimits: runtimeLimits(l.Runtime),
		Checkable:     l.Assertions.HasStatic(),
		Status:        done[l.Slug],
	}, nil
}

type SubmitSolutionInput struct {
	Slug string `json:"slug" jsonschema:"the lesson being answered"`
	Code string `json:"code" jsonschema:"the complete Swift source to grade, not a diff"`
}

type SubmitSolutionOutput struct {
	Passed bool `json:"passed"`

	// Outcome is the verdict as one word: passed, static_failed,
	// compile_failed, timed_out, output_mismatch, or pending. An agent that
	// reads this stops treating a compiler error as a wrong answer.
	Outcome string `json:"outcome"`

	// Failures are the assertions that were not satisfied, in prose.
	Failures []string `json:"failures,omitempty"`

	// CompilerOutput is swiftc's own diagnostics, verbatim. They are better
	// than anything that would survive being reformatted.
	CompilerOutput string `json:"compiler_output,omitempty"`

	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode uint32 `json:"exit_code,omitempty"`

	// RecordedAs is the provenance this submission was stored with. Always
	// "agent" here, and stated in the response so the fact is not a surprise
	// discovered later on a leaderboard.
	RecordedAs string `json:"recorded_as"`
}

func (s *Server) submitSolution(ctx context.Context, userID uuid.UUID, in SubmitSolutionInput) (SubmitSolutionOutput, error) {
	l, ok := s.index.Get(strings.TrimSpace(in.Slug))
	if !ok {
		return SubmitSolutionOutput{}, fmt.Errorf("no lesson called %q; call list_lessons to see the slugs", in.Slug)
	}
	if !l.Assertions.HasStatic() {
		return SubmitSolutionOutput{}, fmt.Errorf("lesson %q has nothing that can be checked", l.Slug)
	}
	if in.Code == "" {
		return SubmitSolutionOutput{}, errors.New("code is empty; send the complete Swift source")
	}
	if len(in.Code) > maxSubmissionBytes {
		return SubmitSolutionOutput{}, fmt.Errorf("that submission is %d bytes; the limit is %d", len(in.Code), maxSubmissionBytes)
	}

	// Metered before anything expensive happens, and keyed on the learner
	// rather than the token: capacity is consumed per person, so minting a
	// second token must not buy a second allowance.
	//
	// A submission that is already compiled is admitted free, exactly as in the
	// browser. A cache hit is a hash lookup and a wasm instantiation, and
	// charging for it would meter the one action that costs nothing — which for
	// an agent iterating on the same file is most of them.
	if !s.grading.Cached(in.Code, l.Runtime) && !s.submits.Allow("user:"+userID.String()) {
		retry := s.submits.RetryAfter("user:" + userID.String())
		return SubmitSolutionOutput{}, fmt.Errorf(
			"submission limit reached; wait %d seconds before submitting again",
			seconds(retry),
		)
	}

	sub := grading.Submission{
		Lesson: l,
		Code:   in.Code,
		UserID: userID,
		// An agent authenticated as this learner and called this tool. That is
		// not an inference, which is what makes it worth recording.
		Channel:    grading.ChannelMCP,
		Provenance: grading.ProvenanceAgent,
		// A signed-in learner is their own analytics identity; there is no
		// address to hash because there is no anonymous caller here.
		DistinctID: userID.String(),
	}

	got, err := s.grading.Grade(ctx, sub, s.log)
	if err != nil {
		// Told as a wait rather than as a failure, so an agent in a loop backs
		// off instead of retrying hot into a full queue.
		if errors.Is(err, executor.ErrBusy) {
			return SubmitSolutionOutput{}, errors.New("every compile slot is busy; retry in 5 seconds")
		}
		// Deliberately not the underlying error. It describes this server's
		// operational state, which is not the caller's business and is not
		// something they can act on.
		return SubmitSolutionOutput{}, errors.New("the grader is unavailable; this submission was not judged")
	}

	return SubmitSolutionOutput{
		Passed:         got.Passed(),
		Outcome:        string(got.Outcome),
		Failures:       describeFailures(got),
		CompilerOutput: got.CompilerOutput,
		Stdout:         got.Stdout,
		Stderr:         got.Stderr,
		ExitCode:       got.ExitCode,
		RecordedAs:     string(grading.ProvenanceAgent),
	}, nil
}

type GetProgressInput struct{}

type GetProgressOutput struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`

	Tracks []TrackProgress `json:"tracks"`

	// Next is the lesson to pick up, or empty when there is nothing left.
	Next string `json:"next,omitempty"`
}

type TrackProgress struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
}

func (s *Server) getProgress(ctx context.Context, userID uuid.UUID, _ GetProgressInput) (GetProgressOutput, error) {
	done, err := s.statuses(ctx, userID)
	if err != nil {
		return GetProgressOutput{}, err
	}

	out := GetProgressOutput{Total: s.index.Count()}
	for _, track := range s.index.Tracks() {
		tp := TrackProgress{Slug: track.Slug, Title: track.Title, Total: len(track.Lessons)}
		for _, l := range track.Lessons {
			if done[l.Slug] == string(progress.StatusCompleted) {
				tp.Completed++
				out.Completed++
				continue
			}
			if out.Next == "" {
				out.Next = l.Slug
			}
		}
		out.Tracks = append(out.Tracks, tp)
	}
	return out, nil
}

// statuses reads this learner's progress as a slug-keyed map.
//
// A learner with no rows yet is not an error, and neither is a build with no
// progress service: both produce an empty map, and every caller then reports a
// corpus nobody has started.
func (s *Server) statuses(ctx context.Context, userID uuid.UUID) (map[string]string, error) {
	if s.progress == nil {
		return map[string]string{}, nil
	}

	rows, err := s.progress.ForUser(ctx, userID)
	if err != nil {
		// The learner cannot act on this and should not be shown its detail.
		s.log.Error("could not read progress for an mcp caller")
		return nil, errors.New("could not read your progress")
	}

	out := make(map[string]string, len(rows))
	for slug, row := range rows {
		out[slug] = string(row.Status)
	}
	return out, nil
}

// describeAssertions turns a lesson's checks into sentences.
//
// Prose rather than the raw struct because the audience is a language model
// reading a tool result: "print a line containing Hello, World!" is acted on
// correctly far more often than {"output_contains": ["Hello, World!"]}.
func describeAssertions(a lesson.Assertions) []string {
	var out []string

	for _, want := range a.MustDeclare {
		out = append(out, fmt.Sprintf("The source must use %q.", want))
	}
	for _, avoid := range a.MustNotUse {
		out = append(out, fmt.Sprintf("The source must not use %q.", avoid))
	}
	for _, want := range a.OutputContains {
		out = append(out, fmt.Sprintf("Running it must print output containing %q.", want))
	}
	if a.OutputEquals != "" {
		out = append(out, fmt.Sprintf("Running it must print exactly %q.", a.OutputEquals))
	}
	return out
}

// describeFailures flattens the grader's typed failures into the sentences they
// already carry, so a client is not asked to understand this project's internal
// shapes to read a result.
func describeFailures(got grading.Result) []string {
	var out []string
	for _, f := range got.Failures {
		if msg := strings.TrimSpace(f.Message); msg != "" {
			out = append(out, msg)
		}
	}
	for _, d := range got.Diagnostics {
		if msg := strings.TrimSpace(d.Message); msg != "" {
			out = append(out, msg)
		}
	}
	return out
}

// runtimeLimits is the single most useful sentence in a lesson payload. An
// agent that does not know the embedded runtime has no concurrency will write
// async Swift for a lesson that cannot compile it, read the error, and try
// again with more async Swift.
func runtimeLimits(rt lesson.Runtime) string {
	switch rt {
	case lesson.RuntimeEmbedded:
		return "Compiled against the embedded Swift SDK. No concurrency runtime and no Unicode " +
			"normalization tables: no async/await, no Task, no actors, and no string sorting. " +
			"No top-level var. Keep to the basics of the language."
	case lesson.RuntimeFull:
		return "Compiled against the complete standard library. async/await, Task, actors and " +
			"string comparison all work."
	case lesson.RuntimeNone:
		return "Cannot be executed anywhere: SwiftUI has no Linux or WebAssembly build. " +
			"Only the static checks apply, so write source that satisfies them; nothing will be run."
	}
	return ""
}
