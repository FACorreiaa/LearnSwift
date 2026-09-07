// Package apperr is the application's error vocabulary.
//
// Handlers need to answer one question about every error a service returns:
// what status code does the visitor see? Sentinel errors answer it without
// handlers having to know anything about the service that produced them.
package apperr

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrValidation means the request was understood and refused. It renders
	// as 422 so htmx swaps the re-rendered form rather than discarding it.
	ErrValidation = errors.New("validation failed")

	// ErrUnauthenticated means there is no usable session.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrNotFound means the thing does not exist, or does not exist for this
	// user. Handlers must not distinguish the two: telling a stranger that a
	// record exists but is not theirs is itself a disclosure.
	ErrNotFound = errors.New("not found")

	// ErrRateLimited renders as 429 into the quota panel.
	ErrRateLimited = errors.New("rate limited")
)

func Is(err, target error) bool { return errors.Is(err, target) }
func As(err error, t any) bool  { return errors.As(err, t) }
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// FieldErrors is a validation failure that knows which inputs were wrong, so a
// form can be re-rendered with the messages beside the fields rather than in a
// banner at the top.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	return fmt.Sprintf("validation failed on %d field(s)", len(f))
}

// Unwrap reports FieldErrors as an ErrValidation, so a handler that only cares
// about the status code can match the sentinel and ignore the detail.
func (f FieldErrors) Unwrap() error { return ErrValidation }

func (f FieldErrors) Add(field, msg string) FieldErrors {
	if f == nil {
		f = FieldErrors{}
	}
	f[field] = msg
	return f
}

func (f FieldErrors) Any() bool { return len(f) > 0 }

// Message is the part of a wrapped error meant for a person.
//
// A validation error is built as `fmt.Errorf("%w: a token needs a name", ...)`,
// which reads correctly in a log and badly on a page — the sentinel's own text
// is in front of the sentence. This drops it, leaving what a service actually
// wrote for the reader.
//
// Only ever called on an error already known to be ErrValidation. Anything
// else describes the application's internal state and does not belong in a
// response at all.
func Message(err error) string {
	if err == nil {
		return ""
	}
	if _, after, found := strings.Cut(err.Error(), ": "); found {
		return after
	}
	return err.Error()
}
