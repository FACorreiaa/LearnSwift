package lessons

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/analytics"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	"github.com/FACorreiaa/seshat/internal/shared/ratelimit"
	"github.com/FACorreiaa/seshat/internal/validate"
	"github.com/FACorreiaa/seshat/web/landing"
	lessonpages "github.com/FACorreiaa/seshat/web/lesson"
)

const (
	// Checking an exercise is the most expensive thing an anonymous visitor can
	// ask this application to do: it runs a grader, and on a cache miss it
	// starts a container holding a Swift toolchain. Unmetered, that is a cost
	// bomb and a denial-of-service vector at the same time.
	//
	// The numbers are sized to be invisible to a learner. Working through an
	// exercise takes a handful of submissions; thirty in ten minutes is far more
	// than anyone types by hand and far less than a script manages in a second.
	guestCheckLimit = 30

	// Signed-in learners get three times the budget. The asymmetry is the point:
	// it is the honest argument for making an account, and it is a better one
	// than a wall in front of the exercise would be.
	userCheckLimit = 90

	checkLimitWindow = 10 * time.Minute
)

type Handler struct {
	index     *Index
	progress  *progress.Service
	validator Validator
	executor  Executor
	analytics analytics.Client

	// secure marks the guest attempt-counting cookie. Mirrors the session
	// cookie's rule rather than inventing a second one.
	secure bool

	// Two limiters rather than one with a variable ceiling: a Limiter's limit is
	// fixed at construction, and keeping them separate means a guest's address
	// and a user's id can never collide in the same bucket.
	guestChecks *ratelimit.Limiter
	userChecks  *ratelimit.Limiter
}

// Executor compiles and runs a submission. Declared as an interface so the
// handler's decision-making can be tested without a 4 GB compiler image.
type Executor interface {
	Available() bool
	Run(ctx context.Context, source string, rt lesson.Runtime) (executor.Result, error)

	// Cached reports whether this submission already has a compiled module, so
	// the handler can decline to charge quota for a resubmission that costs
	// nothing to serve.
	Cached(source string, rt lesson.Runtime) bool
}

// Validator is the subset of internal/validate this package needs. Declared
// here rather than imported as a concrete type so a test can substitute one
// without a Swift toolchain.
type Validator interface {
	Available() bool
	Validate(ctx context.Context, source string, a lesson.Assertions) (validate.Result, error)
}

func NewHandler(index *Index, prog *progress.Service, v Validator, e Executor, an analytics.Client, secure bool) *Handler {
	if an == nil {
		an = analytics.Nop()
	}
	return &Handler{
		index:       index,
		progress:    prog,
		validator:   v,
		executor:    e,
		analytics:   an,
		secure:      secure,
		guestChecks: ratelimit.New(guestCheckLimit, checkLimitWindow),
		userChecks:  ratelimit.New(userCheckLimit, checkLimitWindow),
	}
}

func (h *Handler) Routes(r chi.Router) {
	// The root is the marketing page for a stranger and a dashboard for someone
	// signed in. One route rather than a redirect, so a returning learner does
	// not watch the pitch flash past on the way to their own progress.
	r.Get("/", h.Home)
	r.Get("/lessons", h.list)
	r.Get("/lessons/{slug}", h.show)
	// Recording progress needs an account, so only this route sits behind
	// RequireAuth. Reading a lesson deliberately does not: the pitch is that a
	// concept takes thirty seconds, and a sign-up wall in front of that would
	// contradict it.
	r.With(h.requireUser).Post("/lessons/{slug}/complete", h.complete)
	// Checking an answer deliberately does not require an account: the pitch is
	// that a concept takes thirty seconds, and a sign-up wall in front of the
	// exercise would contradict it. Attempts are only *recorded* when there is
	// somebody to record them against.
	r.Post("/lessons/{slug}/check", h.check)
	// Help is fetched, never embedded. Holding the hint and the solution behind
	// their own requests is what makes withholding them real: a hint sitting in
	// the page behind a CSS rule is a hint that has already been given.
	r.Get("/lessons/{slug}/hint", h.hint)
	// A POST because it is recorded. Asking to be shown the answer changes what
	// this learner's history says about the lesson, and a GET that writes is a
	// GET a prefetcher can fire on their behalf.
	r.Post("/lessons/{slug}/solution", h.solution)
}

// failures reports how many wrong answers this learner has submitted for a
// lesson. Signed in, that is a count of their recorded attempts; signed out, it
// is a cookie. Both exist so the same rule can apply to both.
func (h *Handler) failures(r *http.Request, slug string) int {
	user, ok := auth.UserFrom(r.Context())
	if !ok || h.progress == nil {
		return guestFailures(r, slug)
	}

	n, err := h.progress.FailedAttempts(r.Context(), user.ID, slug)
	if err != nil {
		// A hint withheld because a count could not be read is a small harm; an
		// error page in place of a graded exercise is a larger one.
		middleware.FromContext(r.Context()).Error("could not count attempts", slog.Any("error", err))
		return 0
	}
	return n
}

// helpFor decides what the learner may ask for, given how much they have tried.
func (h *Handler) helpFor(l lesson.Lesson, failures int) lessonpages.Help {
	return lessonpages.Help{
		Slug: l.Slug,
		// No hint written for this lesson means no hint offered. Silence beats
		// a button that answers with nothing.
		HintAvailable:     l.Hint != "" && failures >= hintAfterFailures,
		SolutionAvailable: l.Solution != "" && failures >= solutionAfterFailures,
	}
}

func (h *Handler) hint(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	l, ok := h.index.Get(slug)
	if !ok || l.Hint == "" {
		http.NotFound(w, r)
		return
	}

	failures := h.failures(r, slug)
	if failures < hintAfterFailures {
		// Refused rather than rendered. This is the check that makes the gate
		// real; the button not being drawn yet is only the polite half of it.
		http.Error(w, "keep trying first", http.StatusForbidden)
		return
	}

	help := h.helpFor(l, failures)
	help.Hint = l.Hint
	render(w, r, http.StatusOK, lessonpages.HelpPanel(help))
}

func (h *Handler) solution(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	l, ok := h.index.Get(slug)
	if !ok || l.Solution == "" {
		http.NotFound(w, r)
		return
	}

	failures := h.failures(r, slug)
	if failures < solutionAfterFailures {
		http.Error(w, "keep trying first", http.StatusForbidden)
		return
	}

	if user, ok := auth.UserFrom(r.Context()); ok && h.progress != nil {
		// Recorded before it is shown. A learner's history saying they solved
		// something they were handed would be worse than no history at all.
		if err := h.progress.RevealSolution(r.Context(), user.ID, slug); err != nil {
			middleware.FromContext(r.Context()).Error("could not record reveal", slog.Any("error", err))
		}
	}

	help := h.helpFor(l, failures)
	help.Hint = l.Hint
	help.Solution = l.Solution
	render(w, r, http.StatusOK, lessonpages.HelpPanel(help))
}

// requireUser is a thin wrapper so this package does not need the auth
// Middleware value threaded into it just to guard one route.
func (h *Handler) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.UserFrom(r.Context()); !ok {
			http.Error(w, "sign in to record progress", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FeaturedSlug is the lesson embedded on the landing page.
//
// Named by slug rather than taken by position so that reordering the curriculum
// cannot silently change what a stranger is shown first.
const FeaturedSlug = "variables"

// LandingView assembles what the marketing page needs from the lesson index.
func (h *Handler) LandingView() landing.View {
	if h.index == nil {
		return landing.View{}
	}

	l, ok := h.index.Get(FeaturedSlug)
	if !ok {
		// Not fatal. The page still makes its argument in prose, which is worth
		// a log line rather than a failed boot.
		slog.Warn("landing exercise unavailable", slog.String("slug", FeaturedSlug))
		return landing.View{Tracks: h.index.Tracks()}
	}
	return landing.View{Lesson: l, HasLesson: true, Tracks: h.index.Tracks()}
}

// Home is the marketing page for a stranger and the dashboard for a learner.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	user, signedIn := auth.UserFrom(r.Context())
	if !signedIn || h.progress == nil {
		render(w, r, http.StatusOK, landing.Page(h.LandingView()))
		return
	}

	p, err := h.progress.ForUser(r.Context(), user.ID)
	if err != nil {
		// A dashboard with no progress on it is a worse page than the landing
		// page, and both are better than an error. Fall back rather than fail.
		middleware.FromContext(r.Context()).Error("could not load progress", slog.Any("error", err))
		render(w, r, http.StatusOK, landing.Page(h.LandingView()))
		return
	}

	view := lessonpages.WithContinue(lessonpages.View{Tracks: h.index.Tracks(), Progress: p})
	render(w, r, http.StatusOK, lessonpages.HomePage(view))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	view := lessonpages.View{Tracks: h.index.Tracks()}

	if user, ok := auth.UserFrom(r.Context()); ok {
		p, err := h.progress.ForUser(r.Context(), user.ID)
		if err != nil {
			// Progress is decoration on this page. Losing it is not worth
			// refusing to show the lessons.
			middleware.FromContext(r.Context()).Error("could not load progress", slog.Any("error", err))
		} else {
			view.Progress = p
		}
	}

	render(w, r, http.StatusOK, lessonpages.IndexPage(view))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	l, ok := h.index.Get(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}

	h.capture(r, analytics.EventLessonViewed, l)

	view := lessonpages.View{Lesson: l}
	view.Next, view.HasNext = h.index.Next(slug)

	if user, ok := auth.UserFrom(r.Context()); ok {
		// Opening a lesson records that it was started, so the dashboard can
		// tell "not begun" from "begun and abandoned".
		if err := h.progress.Start(r.Context(), user.ID, slug); err != nil {
			middleware.FromContext(r.Context()).Error("could not record start", slog.Any("error", err))
		}
		if p, found, err := h.progress.Get(r.Context(), user.ID, slug); err != nil {
			middleware.FromContext(r.Context()).Error("could not load progress", slog.Any("error", err))
		} else if found {
			view.Completed = p.IsCompleted()
		}
	}

	render(w, r, http.StatusOK, lessonpages.Page(view))
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	l, ok := h.index.Get(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}

	user := auth.MustUser(r.Context())
	if err := h.progress.Complete(r.Context(), user.ID, slug); err != nil {
		middleware.FromContext(r.Context()).Error("could not complete lesson", slog.Any("error", err))
		http.Error(w, "could not save your progress", http.StatusInternalServerError)
		return
	}

	h.capture(r, analytics.EventLessonCompleted, l)

	view := lessonpages.View{Lesson: l, Completed: true}
	view.Next, view.HasNext = h.index.Next(slug)

	// The footer is returned on its own, which is the whole point of the swap:
	// marking a lesson complete should not reload the lesson.
	render(w, r, http.StatusOK, lessonpages.Footer(view))
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

// maxSubmissionBytes bounds what the editor can send. The validator has its own
// limit; this one exists so an oversized body is refused before it is read into
// memory rather than after.
const maxSubmissionBytes = 64 << 10

func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	l, ok := h.index.Get(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Gated on there being something to check, not on the lesson being
	// executable: static checking works for SwiftUI too, which cannot run.
	if !l.Assertions.HasStatic() {
		http.Error(w, "this exercise cannot be checked", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}

	code := r.PostFormValue("code")
	if len(code) > maxSubmissionBytes {
		http.Error(w, "that submission is too large", http.StatusRequestEntityTooLarge)
		return
	}

	if !h.allowCheck(w, r, l, code) {
		return
	}

	h.capture(r, analytics.EventCheckSubmitted, l)

	view := lessonpages.View{Lesson: l}
	view.Next, view.HasNext = h.index.Next(slug)
	result := lessonpages.Result{Submitted: true, Code: code}

	got, err := h.validator.Validate(r.Context(), code, l.Assertions)
	if err != nil {
		// A grader that cannot run is an operational fault, not a wrong answer.
		// Reporting it as a failure would tell a learner their correct code is
		// wrong, which is the one outcome worth going out of the way to avoid.
		middleware.FromContext(r.Context()).Error("could not validate submission", slog.Any("error", err))
		result.Unavailable = true
		view.Result = result
		render(w, r, http.StatusServiceUnavailable, lessonpages.ResultPanel(result))
		return
	}

	result.Failures = got.Failures
	result.Diagnostics = got.Diagnostics

	// Static checks first, and a failure there ends it: there is no point
	// compiling code that already breaks the lesson's rules, and a compiler
	// error would bury the simpler explanation.
	if !got.OK {
		h.respond(w, r, l, result)
		return
	}

	// Everything static passed. If the lesson only asks static questions, that
	// is the whole answer.
	if !l.Assertions.NeedsExecution() {
		result.Passed = true
		h.respond(w, r, l, result)
		return
	}

	// The rest can only be settled by running the code.
	if !l.Runtime.Executable() || h.executor == nil || !h.executor.Available() {
		// The lesson wants output but nothing can produce it. Reported as
		// pending rather than as a pass, because a submission that has not
		// been checked has not passed.
		result.Pending = true
		h.respond(w, r, l, result)
		return
	}

	run, err := h.executor.Run(r.Context(), code, l.Runtime)
	if err != nil {
		// Every compile slot was busy for longer than this submission was worth
		// waiting. Reported as busy rather than unavailable, because "try again
		// in a moment" is true and "we cannot grade this" is not.
		if errors.Is(err, executor.ErrBusy) {
			middleware.FromContext(r.Context()).Warn("compiler busy", slog.String("slug", l.Slug))
			w.Header().Set("Retry-After", "5")
			render(w, r, http.StatusTooManyRequests, lessonpages.BusyPanel())
			return
		}
		middleware.FromContext(r.Context()).Error("could not run submission", slog.Any("error", err))
		result.Unavailable = true
		view.Result = result
		render(w, r, http.StatusServiceUnavailable, lessonpages.ResultPanel(result))
		return
	}

	switch {
	case !run.Compiled:
		// swiftc's own diagnostics, passed through. They are better than
		// anything that would survive being reformatted.
		result.CompilerOutput = run.Diagnostics
		result.CompileFailed = true

	case run.TimedOut:
		result.TimedOut = true

	default:
		result.Stdout = run.Stdout
		result.Stderr = run.Stderr
		result.ExitCode = run.ExitCode
		result.Failures = append(result.Failures, validate.CheckOutput(run.Stdout, l.Assertions)...)
		// A program that trapped has not satisfied its lesson, whatever it
		// managed to print before dying.
		result.Passed = len(result.Failures) == 0 && run.ExitCode == 0
	}

	view.Result = result
	h.respond(w, r, l, result)
}

// allowCheck meters the endpoint, reporting whether the submission may proceed
// and rendering the quota panel itself when it may not.
//
// A submission that is already compiled is admitted free. That is not a
// concession — it is the whole shape of the cost. A cache hit is a hash lookup
// and a wasm instantiation in this process; charging for it would meter the one
// action that costs nothing, and would punish the learner who resubmits an
// unchanged answer to re-read the output.
func (h *Handler) allowCheck(w http.ResponseWriter, r *http.Request, l lesson.Lesson, code string) bool {
	if h.executor != nil && h.executor.Cached(code, l.Runtime) {
		return true
	}

	limiter, key := h.guestChecks, "ip:"+middleware.ClientIP(r).String()
	if user, ok := auth.UserFrom(r.Context()); ok {
		limiter, key = h.userChecks, "user:"+user.ID.String()
	}

	if limiter.Allow(key) {
		return true
	}

	retry := limiter.RetryAfter(key)
	middleware.FromContext(r.Context()).Warn("check rate limited",
		slog.String("slug", l.Slug),
		slog.Duration("retry_after", retry),
	)

	// A whole number of seconds, per the header's definition, and at least one:
	// a Retry-After of 0 reads as "try again immediately".
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Round(time.Second)/time.Second))))

	// 429 is swapped into the page by the htmx config in the layout, so the
	// learner reads this rather than the response being silently discarded.
	render(w, r, http.StatusTooManyRequests, lessonpages.QuotaPanel(retry, !isSignedIn(r)))
	return false
}

func isSignedIn(r *http.Request) bool {
	_, ok := auth.UserFrom(r.Context())
	return ok
}

// capture records one product event. It never returns an error and never fails a
// request: an analytics outage must be invisible to a learner.
func (h *Handler) capture(r *http.Request, name string, l lesson.Lesson) {
	var userID string
	if user, ok := auth.UserFrom(r.Context()); ok {
		userID = user.ID.String()
	}

	h.analytics.Capture(analytics.Event{
		Name:       name,
		DistinctID: analytics.DistinctID(userID, middleware.ClientIP(r), r.UserAgent()),
		// Slug, track and runtime — the dimensions a decision gets sliced by.
		// Never the submitted source: that is something a person wrote, and it
		// belongs in a feedback table they opted into, not in an event stream.
		Props: analytics.Props(
			"lesson", l.Slug,
			"track", l.Track,
			"runtime", string(l.Runtime),
			"signed_in", userID != "",
		),
	})
}

// respond renders the result panel with the status its outcome deserves.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, l lesson.Lesson, result lessonpages.Result) {
	if result.Passed {
		h.capture(r, analytics.EventCheckPassed, l)
	}

	signedIn := false
	if user, ok := auth.UserFrom(r.Context()); ok {
		signedIn = true
		if _, err := h.progress.RecordAttempt(r.Context(), user.ID, l.Slug, result.Code, result.Passed); err != nil {
			middleware.FromContext(r.Context()).Error("could not record attempt", slog.Any("error", err))
		}
		if result.Passed {
			if err := h.progress.Complete(r.Context(), user.ID, l.Slug); err != nil {
				middleware.FromContext(r.Context()).Error("could not complete lesson", slog.Any("error", err))
			} else {
				h.capture(r, analytics.EventLessonCompleted, l)
			}
		}
	}

	// A pending result decided nothing, so it is not a failure. Counting it
	// would offer the answer to someone whose code was never actually judged.
	switch {
	case result.Passed:
		// Clearing the count means returning to revise does not open on an
		// offer of the solution.
		if !signedIn {
			clearGuestFailures(w, r, l.Slug, h.secure)
		}
	case !result.Pending:
		failures := 0
		if signedIn {
			// Counted after the attempt was recorded, so the submission being
			// answered is included rather than the one before it.
			failures = h.failures(r, l.Slug)
		} else {
			failures = recordGuestFailure(w, r, l.Slug, h.secure)
		}
		result.Help = h.helpFor(l, failures)
	}

	// 422 for a submission that was checked and found wanting, which the
	// layout's htmx config swaps into the page. A pass and a pending result
	// are both 200: neither is a rejection of what the learner wrote.
	status := http.StatusOK
	if !result.Passed && !result.Pending {
		status = http.StatusUnprocessableEntity
	}
	render(w, r, status, lessonpages.ResultPanel(result))
}
