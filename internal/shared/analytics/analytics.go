// Package analytics records the handful of product events that decide what gets
// built next.
//
// Three rules shape everything here. Capture never blocks a request and never
// fails one: an analytics outage must be invisible to a learner. Nothing is
// captured that a person wrote — no email, no source code — because the value of
// an event is in the shape of the funnel, not in its contents. And an unset API
// key disables the whole thing, so developing against this needs no credential
// and no network.
package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"time"
)

// Event is one thing that happened. Props carry dimensions worth slicing by —
// lesson slug, track, whether the visitor was signed in — and nothing else.
type Event struct {
	Name       string
	DistinctID string
	Props      map[string]any
	Timestamp  time.Time
}

// Client records events. Implementations must be safe for concurrent use and
// must not block the caller.
type Client interface {
	Capture(Event)
	Close(ctx context.Context) error
}

// The six events. Deliberately a closed set: a seventh event is a decision, not
// an afterthought, and every one of these answers a question already being
// asked. `returned_day_2` is derived from these at query time rather than
// emitted, because "came back" is not something a request can observe.
const (
	EventLessonViewed    = "lesson_viewed"
	EventCheckSubmitted  = "check_submitted"
	EventCheckPassed     = "check_passed"
	EventLessonCompleted = "lesson_completed"
	EventSignedUp        = "signed_up"
)

// Nop discards everything. It is what an unconfigured app gets, and what tests
// get when they do not care.
func Nop() Client { return nopClient{} }

type nopClient struct{}

func (nopClient) Capture(Event)               {}
func (nopClient) Close(context.Context) error { return nil }

// DistinctID identifies whoever made a request.
//
// A signed-in learner is their user id, so their events join up across devices
// and sessions. Everyone else gets a hash of address and user agent, salted with
// the current date: enough to count one person's session as one person, and
// deliberately not enough to follow them into next week. That expiry is the
// point — it keeps this a measurement of behaviour rather than a record of a
// person, which is also why no cookie is set to do it better.
func DistinctID(userID string, ip netip.Addr, userAgent string) string {
	if userID != "" {
		return userID
	}
	day := time.Now().UTC().Format("2006-01-02")
	sum := sha256.Sum256([]byte(day + "\x00" + ip.String() + "\x00" + userAgent))
	return "anon_" + hex.EncodeToString(sum[:8])
}

// Props is a small helper so call sites read as one line rather than five.
func Props(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			continue
		}
		m[key] = pairs[i+1]
	}
	return m
}

// New returns a PostHog client, or Nop when no API key is configured.
func New(apiKey, host string, log *slog.Logger) Client {
	if apiKey == "" {
		return Nop()
	}
	return newPostHog(apiKey, host, log)
}
