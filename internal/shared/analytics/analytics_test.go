package analytics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// An unset API key must disable the whole thing, so developing against this
// needs no credential and no network.
func TestNoAPIKeyMeansNothingIsSent(t *testing.T) {
	c := New("", "https://example.invalid", discardLogger())

	if _, ok := c.(nopClient); !ok {
		t.Fatalf("New returned %T with no API key, want the no-op client", c)
	}

	// The no-op has to be safe to use exactly like the real one: a handler that
	// captured only when configured would drift from the one that always does.
	c.Capture(Event{Name: EventCheckPassed})
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close on the no-op client returned %v", err)
	}
}

// A signed-in learner's events must join up across devices and sessions, which
// is the one thing a per-request hash cannot do.
func TestASignedInLearnerIsIdentifiedByTheirUserID(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.7")

	got := DistinctID("user-123", ip, "Firefox")
	if got != "user-123" {
		t.Errorf("DistinctID = %q, want the user id", got)
	}
}

// A guest's id is a hash, and it must be the same hash within a session or the
// funnel counts one person as many.
func TestAGuestGetsAStableAnonymousID(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.7")

	first := DistinctID("", ip, "Firefox")
	second := DistinctID("", ip, "Firefox")

	if first != second {
		t.Errorf("the same guest got two ids: %q and %q", first, second)
	}
	if !strings.HasPrefix(first, "anon_") {
		t.Errorf("DistinctID = %q, want an anon_ prefix", first)
	}
	if strings.Contains(first, "203.0.113.7") {
		t.Errorf("the address is recoverable from the id: %q", first)
	}
	if other := DistinctID("", netip.MustParseAddr("198.51.100.4"), "Firefox"); other == first {
		t.Error("two different visitors were given the same id")
	}
}

// Capture must never block the request that produced the event, so a full queue
// drops rather than waits. This is the property the whole design turns on.
func TestAFullQueueDropsRatherThanBlocking(t *testing.T) {
	// A handler that never answers, so the worker is stuck on its first send
	// and the queue fills behind it.
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	t.Cleanup(func() {
		close(blocked)
		srv.Close()
	})

	p := newPostHog("key", srv.URL, discardLogger())

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Comfortably more than the queue holds. If Capture blocked, this never
		// returns.
		for range queueDepth * 3 {
			p.Capture(Event{Name: EventCheckSubmitted, DistinctID: "anon_x"})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Capture blocked on a full queue; a slow analytics endpoint would stall every request")
	}
}

func TestEventsReachTheEndpointInPostHogsBatchShape(t *testing.T) {
	type wire struct {
		APIKey string `json:"api_key"`
		Batch  []struct {
			Event      string         `json:"event"`
			DistinctID string         `json:"distinct_id"`
			Properties map[string]any `json:"properties"`
			Timestamp  string         `json:"timestamp"`
		} `json:"batch"`
	}

	received := make(chan wire, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got wire
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		if r.URL.Path != "/batch/" {
			t.Errorf("path = %q, want /batch/", r.URL.Path)
		}
		select {
		case received <- got:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPostHog("phc_test", srv.URL, discardLogger())
	p.Capture(Event{
		Name:       EventLessonCompleted,
		DistinctID: "user-1",
		Props:      Props("lesson", "optionals", "track", "swift-basics"),
	})

	// Close flushes what is queued, so the test does not have to wait out the
	// flush interval.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case got := <-received:
		if got.APIKey != "phc_test" {
			t.Errorf("api_key = %q", got.APIKey)
		}
		if len(got.Batch) != 1 {
			t.Fatalf("batch carried %d events, want 1", len(got.Batch))
		}
		e := got.Batch[0]
		if e.Event != EventLessonCompleted || e.DistinctID != "user-1" {
			t.Errorf("event = %q, distinct_id = %q", e.Event, e.DistinctID)
		}
		if e.Properties["lesson"] != "optionals" {
			t.Errorf("properties = %v", e.Properties)
		}
		if e.Timestamp == "" {
			t.Error("no timestamp was stamped on the event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the endpoint")
	}
}

// Close must be bounded: a hung analytics endpoint cannot be allowed to hold up
// the process's shutdown.
func TestCloseGivesUpWhenTheEndpointHangs(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	t.Cleanup(func() {
		close(blocked)
		srv.Close()
	})

	p := newPostHog("key", srv.URL, discardLogger())
	p.Capture(Event{Name: EventSignedUp, DistinctID: "user-1"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := p.Close(ctx); err == nil {
		t.Error("Close reported success while the endpoint was still hanging")
	}
}

func TestPropsPairsUpKeysAndValues(t *testing.T) {
	got := Props("lesson", "optionals", "signed_in", true, "dangling")

	if got["lesson"] != "optionals" || got["signed_in"] != true {
		t.Errorf("Props = %v", got)
	}
	if len(got) != 2 {
		t.Errorf("a trailing key without a value was kept: %v", got)
	}
}
