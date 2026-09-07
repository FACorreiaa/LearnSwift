package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FACorreiaa/seshat/internal/auth"
	"github.com/FACorreiaa/seshat/internal/grading"
	"github.com/FACorreiaa/seshat/internal/lessons"
	"github.com/FACorreiaa/seshat/internal/progress"
	"github.com/FACorreiaa/seshat/internal/shared/ratelimit"
)

const (
	// submitLimit is deliberately tighter than the browser's ninety. An agent
	// does not pause to read a compiler error; it submits, reads, and submits
	// again as fast as the transport allows, and every miss on the module cache
	// is a container start. Twenty in ten minutes is more than a learner
	// working through a lesson with help will use, and far less than a loop
	// with a bug in it manages in a second.
	//
	// Cache hits are not counted at all, which is what makes this workable:
	// iterating on the same file costs nothing and is metered as nothing.
	submitLimit  = 20
	submitWindow = 10 * time.Minute

	// serverName and serverVersion identify this server in the MCP handshake.
	// The name is what appears in a client's tool list, so it is the product's
	// name and not the package's.
	serverName    = "seshat"
	serverVersion = "0.1.0"
)

// Server exposes the lesson corpus and the grader as MCP tools.
type Server struct {
	index    *lessons.Index
	grading  *grading.Service
	progress *progress.Service
	submits  *ratelimit.Limiter
	log      *slog.Logger
}

// NewServer builds the tool set. The limiter is constructed here rather than
// passed in, matching how the browser handler owns its own: a ceiling that
// callers can choose is a ceiling that differs between deployments for no
// reason anyone wrote down.
func NewServer(index *lessons.Index, g *grading.Service, prog *progress.Service, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{
		index:    index,
		grading:  g,
		progress: prog,
		submits:  ratelimit.New(submitLimit, submitWindow),
		log:      log,
	}
}

// Handler is the HTTP endpoint, to be mounted outside the CSRF middleware and
// behind bearer authentication.
//
// The session is stateless: every tool here is a single request and a single
// response, authentication is a header on each one, and nothing needs to be
// remembered between them. That is also the direction the MCP specification
// took, and it means a restart or a second replica cannot orphan a client
// halfway through a conversation.
//
// The per-request closure is the important part. It reads the authenticated
// learner out of the request context and binds a server to them, so a tool
// handler never has to ask who is calling — it cannot be built without an
// answer.
func (s *Server) Handler() http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		user, ok := auth.UserFrom(r.Context())
		if !ok {
			// Unreachable behind RequireToken, and returning a server with no
			// tools rather than panicking if the wiring ever changes: an
			// unauthenticated caller then sees an empty tool list instead of a
			// stack trace.
			return sdk.NewServer(implementation(), nil)
		}
		return s.serverFor(user.ID)
	}, &sdk.StreamableHTTPOptions{
		Stateless: true,
		// Plain JSON rather than an event stream. Nothing here streams — a
		// verdict is one object — and text/event-stream would only add framing
		// for a client to unwrap.
		JSONResponse: true,
	})
}

func implementation() *sdk.Implementation {
	return &sdk.Implementation{Name: serverName, Version: serverVersion}
}

// serverFor builds the tool set bound to one learner.
//
// The tool descriptions carry two things a client cannot work out for itself:
// that submissions through this door are recorded as assisted, and that the
// solutions are not available here. Both are stated plainly rather than
// implied, because a description is the only documentation an agent reads.
func (s *Server) serverFor(userID uuid.UUID) *sdk.Server {
	srv := sdk.NewServer(implementation(), nil)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "list_lessons",
		Description: "List Seshat's Swift lessons by track, with this learner's status for each. " +
			"Call this first: the slugs it returns are what every other tool takes.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in ListLessonsInput) (*sdk.CallToolResult, ListLessonsOutput, error) {
		out, err := s.listLessons(ctx, userID, in)
		return nil, out, err
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_lesson",
		Description: "Fetch one lesson: its text in markdown, the starter code, what the grader will " +
			"check, and the limits of the runtime it compiles against. Read runtime_limits before " +
			"writing any code — the embedded runtime has no concurrency and no Unicode tables, and " +
			"code that ignores that will not compile. " +
			"The solution is not available through this server.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in GetLessonInput) (*sdk.CallToolResult, GetLessonOutput, error) {
		out, err := s.getLesson(ctx, userID, in)
		return nil, out, err
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "submit_solution",
		Description: "Grade a complete Swift source file against a lesson, compiling and running it. " +
			"Every submission through this tool is recorded against the learner's account and " +
			"labelled as assisted, which is counted separately from work they typed themselves. " +
			"The learner is better served by being asked a question that gets them to the answer " +
			"than by being handed one. If the result says to wait, wait: retrying immediately will " +
			"not compile any faster.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in SubmitSolutionInput) (*sdk.CallToolResult, SubmitSolutionOutput, error) {
		out, err := s.submitSolution(ctx, userID, in)
		return nil, out, err
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "get_progress",
		Description: "How far this learner has got: completed counts per track, and the next lesson to pick up.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in GetProgressInput) (*sdk.CallToolResult, GetProgressOutput, error) {
		out, err := s.getProgress(ctx, userID, in)
		return nil, out, err
	})

	return srv
}

// seconds rounds a wait to whole seconds, at least one: telling a client to
// wait zero seconds reads as "retry immediately", which is the opposite of the
// instruction.
func seconds(d time.Duration) int {
	return max(1, int(d.Round(time.Second)/time.Second))
}
