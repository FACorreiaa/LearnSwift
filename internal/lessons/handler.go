package lessons

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	"github.com/FACorreiaa/seshat/internal/validate"
	lessonpages "github.com/FACorreiaa/seshat/web/lesson"
)

type Handler struct {
	index     *Index
	progress  *progress.Service
	validator Validator
	executor  Executor
}

// Executor compiles and runs a submission. Declared as an interface so the
// handler's decision-making can be tested without a 4 GB compiler image.
type Executor interface {
	Available() bool
	Run(ctx context.Context, source string, rt lesson.Runtime) (executor.Result, error)
}

// Validator is the subset of internal/validate this package needs. Declared
// here rather than imported as a concrete type so a test can substitute one
// without a Swift toolchain.
type Validator interface {
	Available() bool
	Validate(ctx context.Context, source string, a lesson.Assertions) (validate.Result, error)
}

func NewHandler(index *Index, prog *progress.Service, v Validator, e Executor) *Handler {
	return &Handler{index: index, progress: prog, validator: v, executor: e}
}

func (h *Handler) Routes(r chi.Router) {
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

// respond renders the result panel with the status its outcome deserves.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, l lesson.Lesson, result lessonpages.Result) {
	if user, ok := auth.UserFrom(r.Context()); ok {
		if _, err := h.progress.RecordAttempt(r.Context(), user.ID, l.Slug, result.Code, result.Passed); err != nil {
			middleware.FromContext(r.Context()).Error("could not record attempt", slog.Any("error", err))
		}
		if result.Passed {
			if err := h.progress.Complete(r.Context(), user.ID, l.Slug); err != nil {
				middleware.FromContext(r.Context()).Error("could not complete lesson", slog.Any("error", err))
			}
		}
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
