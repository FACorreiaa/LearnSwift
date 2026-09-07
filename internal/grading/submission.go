// Package grading settles whether a submission answers its lesson.
//
// It exists because there is now more than one way to submit. The browser posts
// a form; an agent calls an MCP tool. Both deserve the same verdict, and the
// only way to guarantee that is for both to call the same code. What lives here
// is everything between "here is some Swift" and "here is what it did": static
// validation, compilation, execution, the outcome, and the record of it.
//
// What does not live here is anything that knows about HTTP. No request, no
// response writer, no cookie, no template. Rate limiting stays with the caller
// too — a browser is metered by address and an agent by token, with different
// ceilings, and a limiter whose key is chosen by its caller is clearer than one
// that inspects a context to guess who is asking.
package grading

import (
	"github.com/google/uuid"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// Channel is how a submission arrived. It is recorded rather than acted upon:
// the grader treats a form post and a tool call identically, and the label
// exists so that a question like "do agent-assisted learners finish tracks"
// has an answer.
type Channel string

const (
	ChannelWeb Channel = "web"
	ChannelMCP Channel = "mcp"
)

// Provenance is what is known about who wrote the code.
//
// Over MCP this is not a guess: the agent authenticated as the learner and
// called the tool, so ProvenanceAgent is a fact. In the browser it is a
// heuristic derived in the editor from paste size, which is why the middle
// values exist at all.
//
// It labels; it never refuses. No submission is rejected, delayed, or graded
// differently because of its provenance, and a learner is never shown it as a
// judgement on their work.
type Provenance string

const (
	// ProvenanceUnknown is the honest answer for anything recorded before the
	// label existed, and for a client that does not send one.
	ProvenanceUnknown Provenance = "unknown"

	ProvenanceTyped  Provenance = "typed"
	ProvenanceMixed  Provenance = "mixed"
	ProvenancePasted Provenance = "pasted"

	// ProvenanceAgent is set by the MCP channel and cannot be set by a client.
	ProvenanceAgent Provenance = "agent"
)

// Valid reports whether p is a label this package recognises. Anything else is
// treated as ProvenanceUnknown rather than refused: a client sending nonsense
// should lose its label, not its submission.
func (p Provenance) Valid() bool {
	switch p {
	case ProvenanceUnknown, ProvenanceTyped, ProvenanceMixed, ProvenancePasted, ProvenanceAgent:
		return true
	}
	return false
}

// Submission is one answer to one lesson.
type Submission struct {
	Lesson lesson.Lesson
	Code   string

	// UserID is the zero UUID for a guest. Guests are graded exactly like
	// anyone else; they simply have nowhere for the attempt to be recorded.
	UserID uuid.UUID

	// DistinctID is the analytics identity, computed by the caller because
	// deriving it for an anonymous visitor needs an address and a user agent —
	// both of which are HTTP's business, not this package's.
	DistinctID string

	Channel    Channel
	Provenance Provenance
}

// SignedIn reports whether there is anyone to record this attempt against.
func (s Submission) SignedIn() bool { return s.UserID != uuid.Nil }

// Outcome is the single word for what happened. The web layer flattens most of
// these into "not passed" for display; MCP returns them as they are, because an
// agent that can tell a compile error from a wrong answer stops guessing.
type Outcome string

const (
	// OutcomePassed means every assertion the lesson makes was satisfied.
	OutcomePassed Outcome = "passed"

	// OutcomeStaticFailed means the code broke one of the lesson's rules, so
	// nothing was compiled. Deliberately distinct: a compiler error on top of
	// a rule violation buries the simpler explanation.
	OutcomeStaticFailed Outcome = "static_failed"

	// OutcomeCompileFailed means swiftc rejected it.
	OutcomeCompileFailed Outcome = "compile_failed"

	// OutcomeTimedOut means the program ran too long and was stopped. Almost
	// always an accidental infinite loop.
	OutcomeTimedOut Outcome = "timed_out"

	// OutcomeOutputMismatch means it compiled and ran, but printed the wrong
	// thing or exited non-zero.
	OutcomeOutputMismatch Outcome = "output_mismatch"

	// OutcomePending means the static checks passed but the lesson also asks
	// questions only running the code can answer, and nothing here can run it.
	// Not a pass: a submission that was not judged has not passed.
	OutcomePending Outcome = "pending"
)

// Result is what the grader decided.
type Result struct {
	Outcome Outcome

	Failures    []validate.Failure
	Diagnostics []validate.Diagnostic

	// CompilerOutput carries swiftc's diagnostics verbatim when compilation
	// failed. Swift's compiler errors are better than anything that would
	// survive being reformatted.
	CompilerOutput string

	Stdout   string
	Stderr   string
	ExitCode uint32
}

// Passed is the one question every caller asks.
func (r Result) Passed() bool { return r.Outcome == OutcomePassed }

// Decided reports whether the submission was actually judged. A pending result
// is not a failure, and counting it as one would offer a learner the solution
// to a problem nobody checked their answer to.
func (r Result) Decided() bool { return r.Outcome != OutcomePending }

// OrUnknown reduces an unrecognised label to ProvenanceUnknown.
//
// A client sending nonsense should lose its label, not its submission, and the
// column's CHECK constraint would otherwise turn a bad string into a failed
// write on an answer that was graded correctly.
func (p Provenance) OrUnknown() Provenance {
	if !p.Valid() {
		return ProvenanceUnknown
	}
	return p
}

// ProvenanceFromClient reads a label a browser reported about itself.
//
// Only the three labels an editor can honestly derive are accepted.
// ProvenanceAgent is deliberately not among them: it means "an agent
// authenticated as this learner and called a tool", which is something the MCP
// channel knows and a form post cannot claim.
//
// This is an honour system and is meant to be read as one. A learner determined
// to misreport has to open the editor and lie about work they could have simply
// done, which is more effort than the lesson. What the label defends against is
// accident, not attack.
func ProvenanceFromClient(s string) Provenance {
	switch Provenance(s) {
	case ProvenanceTyped:
		return ProvenanceTyped
	case ProvenanceMixed:
		return ProvenanceMixed
	case ProvenancePasted:
		return ProvenancePasted
	}
	return ProvenanceUnknown
}
