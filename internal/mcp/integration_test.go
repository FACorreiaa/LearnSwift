package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/executor"
	"github.com/FACorreiaa/seshat/internal/grading"
	"github.com/FACorreiaa/seshat/internal/leaderboard"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/database/testdb"
	"github.com/FACorreiaa/seshat/internal/validate"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The whole path, with nothing stubbed but the Swift toolchain: a real access
// token, the real bearer middleware, the real grader, a real database, and the
// real leaderboard query reading what the tool call wrote.
//
// The compiler is the one thing faked. It needs a 2 GB image and a container
// runtime, and what is under test here is the wiring rather than swiftc.
func TestAnAgentCompletesALessonEndToEnd(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	users := auth.NewTokenStore(pool)
	prog := progress.New(pool)
	board := leaderboard.New(pool)

	learner := newLearner(t, pool, "e2e@example.com")
	token, _, err := users.Create(ctx, learner, "claude code")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	index := testIndex(t)

	// The output the real lesson demands, read off the lesson rather than
	// copied into this file: a test that hardcodes it starts lying the day
	// somebody edits the exercise.
	target, ok := index.Get(lessonUnderTest)
	if !ok {
		t.Fatalf("no lesson called %q", lessonUnderTest)
	}

	srv := NewServer(
		index,
		grading.New(
			fakeValidator{result: validate.Result{OK: true}},
			fakeExecutor{result: executor.Result{Compiled: true, Stdout: wantedOutput(target)}},
			prog,
			nil,
		),
		prog,
		nil,
	)

	// Mounted exactly as cmd/web does it: bearer authentication in front, no
	// cookie handling, no CSRF.
	endpoint := httptest.NewServer(auth.NewBearerMiddleware(users).RequireToken(srv.Handler()))
	defer endpoint.Close()

	t.Run("no token gets nowhere", func(t *testing.T) {
		res, reqErr := http.Post(endpoint.URL, "application/json", http.NoBody)
		if reqErr != nil {
			t.Fatalf("post: %v", reqErr)
		}
		defer func() { _ = res.Body.Close() }()

		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", res.StatusCode)
		}
	})

	sess := connectWithToken(t, endpoint.URL, token)

	t.Run("the agent can see the corpus", func(t *testing.T) {
		out, res := call[ListLessonsOutput](t, sess, "list_lessons", map[string]any{})
		if res.IsError {
			t.Fatalf("list_lessons: %s", errorText(t, res))
		}
		if len(out.Tracks) == 0 {
			t.Fatal("no tracks")
		}
	})

	t.Run("a submission is graded and recorded as assisted", func(t *testing.T) {
		out, res := call[SubmitSolutionOutput](t, sess, "submit_solution", map[string]any{
			"slug": lessonUnderTest,
			"code": "let greeting: String? = \"World\"\nif let greeting { print(\"Hello, \\(greeting)!\") }",
		})
		if res.IsError {
			t.Fatalf("submit_solution: %s", errorText(t, res))
		}
		if !out.Passed {
			t.Fatalf("outcome = %q, want a pass", out.Outcome)
		}

		// The row, not the response: what matters is what was written down.
		var provenance string
		var passed bool
		if err := pool.QueryRow(ctx,
			`SELECT provenance, passed FROM exercise_attempt
			 WHERE user_id = $1 AND lesson_slug = $2`, learner, lessonUnderTest,
		).Scan(&provenance, &passed); err != nil {
			t.Fatalf("read attempt: %v", err)
		}
		if provenance != string(grading.ProvenanceAgent) {
			t.Errorf("provenance = %q, want %q", provenance, grading.ProvenanceAgent)
		}
		if !passed {
			t.Error("the attempt was not recorded as passing")
		}
	})

	t.Run("the lesson shows completed to the browser", func(t *testing.T) {
		got, found, progErr := prog.Get(ctx, learner, lessonUnderTest)
		if progErr != nil {
			t.Fatalf("progress: %v", progErr)
		}
		if !found || !got.IsCompleted() {
			t.Errorf("progress = %+v, found = %v; want a completed lesson", got, found)
		}
	})

	t.Run("get_progress agrees with the database", func(t *testing.T) {
		out, res := call[GetProgressOutput](t, sess, "get_progress", map[string]any{})
		if res.IsError {
			t.Fatalf("get_progress: %s", errorText(t, res))
		}
		if out.Completed != 1 {
			t.Errorf("completed = %d, want 1", out.Completed)
		}
	})

	t.Run("the leaderboard counts it as assisted, not solo", func(t *testing.T) {
		if optErr := board.OptIn(ctx, learner, "Agent Driver"); optErr != nil {
			t.Fatalf("opt in: %v", optErr)
		}

		solo, _, boardErr := board.Boards(ctx)
		if boardErr != nil {
			t.Fatalf("boards: %v", boardErr)
		}
		if len(solo) != 1 {
			t.Fatalf("board has %d rows, want 1", len(solo))
		}
		if solo[0].Solo != 0 {
			t.Errorf("solo = %d, want 0 — an agent's submission is not an unaided solve", solo[0].Solo)
		}
		if solo[0].Assisted != 1 {
			t.Errorf("assisted = %d, want 1", solo[0].Assisted)
		}
	})

	t.Run("revoking the token closes the door immediately", func(t *testing.T) {
		listed, listErr := users.List(ctx, learner)
		if listErr != nil || len(listed) != 1 {
			t.Fatalf("list tokens: %v (%d rows)", listErr, len(listed))
		}
		if delErr := users.Delete(ctx, listed[0].ID, learner); delErr != nil {
			t.Fatalf("revoke: %v", delErr)
		}

		// Presenting the revoked token, not an absent one: the question is
		// whether revocation bites, and a request with no credential would
		// have been refused before it too.
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, http.NoBody)
		if reqErr != nil {
			t.Fatalf("request: %v", reqErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			t.Fatalf("post: %v", doErr)
		}
		defer func() { _ = res.Body.Close() }()

		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("a revoked token still authenticated: status = %d", res.StatusCode)
		}
	})
}

func newLearner(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, 'x') RETURNING id`, email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// connectWithToken is how a real client connects: a bearer header on every
// request the transport makes.
func connectWithToken(t *testing.T, endpoint, token string) *sdk.ClientSession {
	t.Helper()

	transport := &sdk.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{token: token, next: http.DefaultTransport},
		},
	}

	sess, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	return sess
}

type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// lessonUnderTest is a lesson that both compiles and asserts on its output, so
// the end-to-end path actually reaches the executor rather than short-circuiting
// on static checks alone.
const lessonUnderTest = "optionals"

// wantedOutput is what a correct answer prints, according to the lesson.
func wantedOutput(l lesson.Lesson) string {
	if l.Assertions.OutputEquals != "" {
		return l.Assertions.OutputEquals
	}
	if len(l.Assertions.OutputContains) > 0 {
		return l.Assertions.OutputContains[0] + "\n"
	}
	return ""
}
