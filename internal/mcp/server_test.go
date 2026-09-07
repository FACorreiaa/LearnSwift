package mcp

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/grading"
	"github.com/FACorreiaa/seshat/internal/lessons"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// The real corpus, not a fixture. What is worth proving here is that the actual
// lessons — which do have solutions written into them — serialize without one.
func testIndex(t *testing.T) *lessons.Index {
	t.Helper()

	lessonFS, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("lesson fs: %v", err)
	}
	index, err := lessons.Parse(lessonFS)
	if err != nil {
		t.Fatalf("parse lessons: %v", err)
	}
	return index
}

type fakeValidator struct {
	result validate.Result
	err    error
}

func (f fakeValidator) Available() bool { return f.err == nil }

func (f fakeValidator) Validate(context.Context, string, lesson.Assertions) (validate.Result, error) {
	return f.result, f.err
}

type fakeExecutor struct {
	result executor.Result
	err    error
	cached bool
}

func (f fakeExecutor) Available() bool { return true }

func (f fakeExecutor) Run(context.Context, string, lesson.Runtime) (executor.Result, error) {
	return f.result, f.err
}

func (f fakeExecutor) Cached(string, lesson.Runtime) bool { return f.cached }

// connect stands the server up behind a middleware that authenticates as one
// fixed learner, which is what the bearer middleware does in production, and
// returns a live MCP client session. No network, no database.
func connect(t *testing.T, srv *Server, userID uuid.UUID) *sdk.ClientSession {
	t.Helper()

	handler := srv.Handler()
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.ContextWithUser(r.Context(), auth.User{ID: userID})
		handler.ServeHTTP(w, r.WithContext(ctx))
	})

	httpSrv := httptest.NewServer(authed)
	t.Cleanup(httpSrv.Close)

	ctx := context.Background()
	sess, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	return sess
}

// newServer builds a server with no database. A nil progress service means
// nothing is recorded, which is exactly what a grading test wants.
func newServer(t *testing.T, v grading.Validator, e grading.Executor) *Server {
	t.Helper()
	return NewServer(testIndex(t), grading.New(v, e, nil, nil), nil, nil)
}

func call[T any](t *testing.T, sess *sdk.ClientSession, name string, args map[string]any) (T, *sdk.CallToolResult) {
	t.Helper()

	res, err := sess.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}

	var out T
	if res.IsError {
		return out, res
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: remarshal: %v", name, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decode into %T: %v", name, out, err)
	}
	return out, res
}

func errorText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()

	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestTheToolSetIsWhatIsAdvertised(t *testing.T) {
	sess := connect(t, newServer(t, fakeValidator{}, fakeExecutor{}), uuid.New())

	got, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := map[string]bool{
		"list_lessons": false, "get_lesson": false,
		"submit_solution": false, "get_progress": false,
	}
	for _, tool := range got.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		want[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s has no description, which is the only documentation an agent reads", tool.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q is missing", name)
		}
	}
}

// The single most important property of this server. A tool that returns the
// answer alongside the question is a tool an agent will answer with.
func TestNoLessonPayloadEverCarriesItsSolution(t *testing.T) {
	index := testIndex(t)
	srv := NewServer(index, grading.New(fakeValidator{}, fakeExecutor{}, nil, nil), nil, nil)
	sess := connect(t, srv, uuid.New())

	checked := 0
	for _, l := range index.All() {
		if l.Solution == "" {
			continue
		}
		checked++

		_, res := call[GetLessonOutput](t, sess, "get_lesson", map[string]any{"slug": l.Slug})
		if res.IsError {
			t.Fatalf("%s: %s", l.Slug, errorText(t, res))
		}

		// Not "no field named solution" — the whole payload, as it goes over
		// the wire, must not contain the text.
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), strings.TrimSpace(l.Solution)) {
			t.Errorf("%s: the solution is in the get_lesson payload", l.Slug)
		}
		if l.Hint != "" && strings.Contains(string(raw), strings.TrimSpace(l.Hint)) {
			t.Errorf("%s: the hint is in the get_lesson payload", l.Slug)
		}
	}

	if checked == 0 {
		t.Fatal("no lesson in the corpus has a solution; this test proved nothing")
	}
}

// An agent that does not know the embedded runtime has no concurrency writes
// async Swift, reads the error, and writes more async Swift.
func TestALessonCarriesTheLimitsOfItsRuntime(t *testing.T) {
	sess := connect(t, newServer(t, fakeValidator{}, fakeExecutor{}), uuid.New())

	out, res := call[GetLessonOutput](t, sess, "get_lesson", map[string]any{"slug": "optionals"})
	if res.IsError {
		t.Fatalf("get_lesson: %s", errorText(t, res))
	}

	if out.RuntimeLimits == "" {
		t.Error("no runtime_limits on a lesson payload")
	}
	if out.Body == "" {
		t.Error("no body on a lesson payload")
	}
	if len(out.Requirements) == 0 {
		t.Error("no requirements: an agent then has to guess what is checked")
	}
	// Markdown, not the rendered HTML: an agent handed markup spends tokens on
	// tags.
	if strings.Contains(out.Body, "<p>") {
		t.Error("body is HTML; it should be the markdown as written")
	}
}

func TestListingReportsTheWholeCorpus(t *testing.T) {
	index := testIndex(t)
	srv := NewServer(index, grading.New(fakeValidator{}, fakeExecutor{}, nil, nil), nil, nil)
	sess := connect(t, srv, uuid.New())

	out, res := call[ListLessonsOutput](t, sess, "list_lessons", map[string]any{})
	if res.IsError {
		t.Fatalf("list_lessons: %s", errorText(t, res))
	}

	seen := 0
	for _, track := range out.Tracks {
		if track.Title == "" {
			t.Errorf("track %q has no title", track.Slug)
		}
		seen += len(track.Lessons)
	}
	if seen != index.Count() {
		t.Errorf("listed %d lessons, want %d", seen, index.Count())
	}

	// Narrowing must actually narrow, and must not invent a track.
	narrowed, res := call[ListLessonsOutput](t, sess, "list_lessons", map[string]any{"track": "concurrency"})
	if res.IsError {
		t.Fatalf("list_lessons(track): %s", errorText(t, res))
	}
	if len(narrowed.Tracks) != 1 || narrowed.Tracks[0].Slug != "concurrency" {
		t.Errorf("narrowing returned %d tracks", len(narrowed.Tracks))
	}

	if _, res := call[ListLessonsOutput](t, sess, "list_lessons", map[string]any{"track": "nope"}); !res.IsError {
		t.Error("an unknown track was not reported as an error")
	}
}

// A wrong slug is the most common mistake a client will make, so the message
// has to say how to recover rather than merely that it failed.
func TestAnUnknownSlugSaysHowToRecover(t *testing.T) {
	sess := connect(t, newServer(t, fakeValidator{}, fakeExecutor{}), uuid.New())

	for _, tool := range []string{"get_lesson", "submit_solution"} {
		args := map[string]any{"slug": "does-not-exist"}
		if tool == "submit_solution" {
			args["code"] = "let a = 1"
		}

		_, res := call[map[string]any](t, sess, tool, args)
		if !res.IsError {
			t.Fatalf("%s accepted an unknown slug", tool)
		}
		if got := errorText(t, res); !strings.Contains(got, "list_lessons") {
			t.Errorf("%s: %q does not point at list_lessons", tool, got)
		}
	}
}

func TestAGradedSubmissionReportsItsVerdictAndItsLabel(t *testing.T) {
	srv := newServer(t,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true, Stdout: "42\n"}},
	)
	sess := connect(t, srv, uuid.New())

	out, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals",
		"code": "let a: Int? = 42\nprint(a!)",
	})
	if res.IsError {
		t.Fatalf("submit_solution: %s", errorText(t, res))
	}

	if out.Outcome == "" {
		t.Error("no outcome word: an agent cannot then tell a compile error from a wrong answer")
	}
	// Stated in the response so the fact is not a surprise found later on a
	// leaderboard.
	if out.RecordedAs != string(grading.ProvenanceAgent) {
		t.Errorf("recorded_as = %q, want %q", out.RecordedAs, grading.ProvenanceAgent)
	}
}

func TestACompileFailureIsNotAWrongAnswer(t *testing.T) {
	srv := newServer(t,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: false, Diagnostics: "error: cannot find 'x' in scope"}},
	)
	sess := connect(t, srv, uuid.New())

	out, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals",
		"code": "print(x)",
	})
	if res.IsError {
		t.Fatalf("submit_solution: %s", errorText(t, res))
	}

	if out.Outcome != string(grading.OutcomeCompileFailed) {
		t.Errorf("outcome = %q, want %q", out.Outcome, grading.OutcomeCompileFailed)
	}
	if !strings.Contains(out.CompilerOutput, "cannot find 'x'") {
		t.Errorf("swiftc's diagnostics did not survive: %q", out.CompilerOutput)
	}
	if out.Passed {
		t.Error("code that did not compile was reported as passing")
	}
}

// The whole point of returning this as a wait rather than a failure: an agent
// in a loop must back off instead of retrying hot into a full queue.
func TestAFullCompileQueueTellsTheAgentToWait(t *testing.T) {
	srv := newServer(t,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{err: executor.ErrBusy},
	)
	sess := connect(t, srv, uuid.New())

	_, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals",
		"code": "let a: Int? = 1",
	})
	if !res.IsError {
		t.Fatal("a busy compiler was reported as a verdict")
	}

	got := errorText(t, res)
	if !strings.Contains(got, "retry") && !strings.Contains(got, "wait") {
		t.Errorf("%q does not tell the caller to wait", got)
	}
}

// An outage describes this server's state, which the caller cannot act on and
// has no business reading.
func TestAnOutageDoesNotLeakItsCause(t *testing.T) {
	srv := newServer(t,
		fakeValidator{err: errSentinel{}},
		fakeExecutor{},
	)
	sess := connect(t, srv, uuid.New())

	_, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals",
		"code": "let a: Int? = 1",
	})
	if !res.IsError {
		t.Fatal("a broken grader produced a verdict")
	}
	if got := errorText(t, res); strings.Contains(got, "exit status 127") {
		t.Errorf("the underlying failure leaked to the caller: %q", got)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "swift-validate: exit status 127" }

// Agents submit as fast as the transport allows. Cache hits are free, so the
// limit has to bite on the calls that actually start a container.
func TestSubmissionsAreMeteredButCacheHitsAreFree(t *testing.T) {
	pass := fakeValidator{result: validate.Result{OK: true}}
	ok := executor.Result{Compiled: true, Stdout: "42\n"}
	userID := uuid.New()

	t.Run("uncached submissions are eventually refused", func(t *testing.T) {
		srv := newServer(t, pass, fakeExecutor{result: ok})
		sess := connect(t, srv, userID)

		refused := ""
		for i := 0; i < submitLimit+2; i++ {
			_, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
				"slug": "optionals",
				"code": "let a: Int? = 1",
			})
			if res.IsError {
				refused = errorText(t, res)
				break
			}
		}
		if refused == "" {
			t.Fatalf("more than %d uncached submissions were admitted", submitLimit)
		}
		if !strings.Contains(refused, "seconds") {
			t.Errorf("the refusal does not say how long to wait: %q", refused)
		}
	})

	t.Run("cached submissions are never refused", func(t *testing.T) {
		srv := newServer(t, pass, fakeExecutor{result: ok, cached: true})
		sess := connect(t, srv, uuid.New())

		for i := 0; i < submitLimit*2; i++ {
			_, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
				"slug": "optionals",
				"code": "let a: Int? = 1",
			})
			if res.IsError {
				t.Fatalf("a cache hit was metered on call %d: %s", i+1, errorText(t, res))
			}
		}
	})
}

func TestAnEmptyOrOversizedSubmissionIsRefusedBeforeGrading(t *testing.T) {
	srv := newServer(t,
		fakeValidator{result: validate.Result{OK: true}},
		fakeExecutor{result: executor.Result{Compiled: true}},
	)
	sess := connect(t, srv, uuid.New())

	if _, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals", "code": "",
	}); !res.IsError {
		t.Error("an empty submission was graded")
	}

	if _, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
		"slug": "optionals", "code": strings.Repeat("x", maxSubmissionBytes+1),
	}); !res.IsError {
		t.Error("an oversized submission was graded")
	}
}

// A learner with no rows yet is not an error, and neither is a build with no
// database: both report a corpus nobody has started.
func TestProgressWithNothingRecordedIsEmptyNotBroken(t *testing.T) {
	index := testIndex(t)
	srv := NewServer(index, grading.New(fakeValidator{}, fakeExecutor{}, nil, nil), nil, nil)
	sess := connect(t, srv, uuid.New())

	out, res := call[GetProgressOutput](t, sess, "get_progress", map[string]any{})
	if res.IsError {
		t.Fatalf("get_progress: %s", errorText(t, res))
	}

	if out.Total != index.Count() {
		t.Errorf("total = %d, want %d", out.Total, index.Count())
	}
	if out.Completed != 0 {
		t.Errorf("completed = %d, want 0", out.Completed)
	}
	if out.Next == "" {
		t.Error("no next lesson offered to a learner who has done nothing")
	}
}

// Unreachable behind the bearer middleware, but the wiring must fail closed
// rather than serving a tool bound to nobody.
func TestAnUnauthenticatedCallerGetsNoTools(t *testing.T) {
	srv := newServer(t, fakeValidator{}, fakeExecutor{})

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	ctx := context.Background()
	sess, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	got, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(got.Tools) != 0 {
		t.Errorf("an unauthenticated caller was offered %d tools", len(got.Tools))
	}
}
