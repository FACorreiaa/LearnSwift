// Package validate decides whether a learner's submission satisfies a lesson's
// assertions.
//
// The parsing is done by the swift-validate binary under tools/, not here.
// Swift is the only language with a correct Swift parser, and the alternative —
// approximating a lexer in Go — is how `must_not_use: "!"` came to reject a
// correct answer because of the exclamation mark inside its printed string.
// See internal/lessons/lesson/code.go for that approximation, which now exists
// only to cross-check this one.
package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// ErrUnavailable means the validator could not be run at all. It is
// deliberately distinct from "the submission failed": a missing binary is an
// operational fault, and reporting it as a wrong answer would tell a learner
// their correct code is wrong.
var ErrUnavailable = errors.New("validate: swift-validate is unavailable")

// timeout bounds one validation. Parsing a lesson-sized file is milliseconds;
// anything approaching this is a hang, and a request handler is waiting.
const timeout = 5 * time.Second

// maxSourceBytes caps what will be sent to the parser. The editor limits input
// too, but this is the boundary that actually matters — it is the one an
// attacker would have to get past.
const maxSourceBytes = 256 << 10

type Failure struct {
	Kind    string `json:"kind"`
	Token   string `json:"token"`
	Message string `json:"message"`
}

// Diagnostic is a syntax error, with the position it was found at. This is the
// capability the Go approximation never had: it could tell you a token was
// missing, but not that line 4 does not parse.
type Diagnostic struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

type Result struct {
	OK          bool         `json:"ok"`
	Failures    []Failure    `json:"failures"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	CodeOnly    string       `json:"code_only"`
}

type request struct {
	Source     string     `json:"source"`
	Assertions assertions `json:"assertions"`
}

type assertions struct {
	MustDeclare []string `json:"must_declare"`
	MustNotUse  []string `json:"must_not_use"`
}

// Validator runs the swift-validate binary.
type Validator struct {
	// Path to the binary. Empty means validation is unavailable, which is an
	// error rather than a silent pass — see Validate.
	Path string
}

func New(path string) *Validator { return &Validator{Path: path} }

// Available reports whether validation can run at all, so a caller can decide
// what to do about it once at startup rather than per request.
func (v *Validator) Available() bool { return v.Path != "" }

// Validate checks source against a lesson's static assertions.
//
// Output-based assertions (output_contains, output_equals) are not evaluated
// here: they need the submission to have been compiled and run, which is the
// compile pipeline's job. A lesson carrying only output assertions therefore
// comes back OK from this call, and is not finished being graded.
func (v *Validator) Validate(ctx context.Context, source string, a lesson.Assertions) (Result, error) {
	if !v.Available() {
		return Result{}, ErrUnavailable
	}
	if len(source) > maxSourceBytes {
		return Result{}, fmt.Errorf("validate: source is %d bytes, over the %d limit", len(source), maxSourceBytes)
	}

	payload, err := json.Marshal(request{
		Source: source,
		Assertions: assertions{
			MustDeclare: a.MustDeclare,
			MustNotUse:  a.MustNotUse,
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("validate: encode request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// exec.CommandContext with no shell: the source is untrusted input and
	// never becomes part of a command line. It goes in on stdin, where it
	// cannot be interpreted as anything but data.
	cmd := exec.CommandContext(ctx, v.Path)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("validate: timed out after %s: %w", timeout, ctx.Err())
		}
		return Result{}, fmt.Errorf("validate: %w (stderr: %s)", err, stderr.String())
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return Result{}, fmt.Errorf("validate: decode response: %w", err)
	}
	return result, nil
}

// ErrorIsUnavailable reports whether err means the validator could not run, as
// opposed to the submission being wrong.
func ErrorIsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }
