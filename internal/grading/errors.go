package grading

import "errors"

// ErrNoValidator means the grader was asked to settle a submission with nothing
// to settle it. An unconfigured deploy, not a wrong answer, and reported as an
// outage for the same reason a failing validator is.
var ErrNoValidator = errors.New("grading: no validator configured")
