package lessons

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/shared/analytics"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// recorder collects events instead of sending them.
type recorder struct {
	mu     sync.Mutex
	events []analytics.Event
}

func (r *recorder) Capture(e analytics.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) Close(context.Context) error { return nil }

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Name)
	}
	return out
}

func (r *recorder) has(name string) bool {
	for _, got := range r.names() {
		if got == name {
			return true
		}
	}
	return false
}

func viewRequest(t *testing.T, h *Handler, slug string) *httptest.ResponseRecorder {
	t.Helper()

	router := chi.NewRouter()
	h.Routes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lessons/"+slug, nil))
	return rec
}

// Without this there is no way to answer "did anyone finish a lesson this
// week", which is the only number that decides what gets built next.
func TestReadingALessonIsRecorded(t *testing.T) {
	rec := &recorder{}
	h := NewHandler(staticLesson(t), nil, fakeValidator{result: validate.Result{OK: true}}, nil, rec, false)

	if code := viewRequest(t, h, "x").Code; code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !rec.has(analytics.EventLessonViewed) {
		t.Errorf("no lesson_viewed event: %v", rec.names())
	}
}

func TestSubmittingAndPassingAreBothRecorded(t *testing.T) {
	rec := &recorder{}
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustDeclare: []string{"let"}}),
		nil,
		fakeValidator{result: validate.Result{OK: true}},
		nil,
		rec,
		false,
	)

	checkRequest(t, h, "x", "let a = 1")

	for _, want := range []string{analytics.EventCheckSubmitted, analytics.EventCheckPassed} {
		if !rec.has(want) {
			t.Errorf("no %s event: %v", want, rec.names())
		}
	}
}

// A failed check is still a submission. Losing it would make the pass rate look
// like 100% and hide the lessons people cannot get through, which is the single
// most useful thing this measures.
func TestAFailedCheckIsStillRecordedAsASubmission(t *testing.T) {
	rec := &recorder{}
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustDeclare: []string{"let"}}),
		nil,
		fakeValidator{result: validate.Result{OK: false}},
		nil,
		rec,
		false,
	)

	checkRequest(t, h, "x", "var a = 1")

	if !rec.has(analytics.EventCheckSubmitted) {
		t.Errorf("a failed submission was not counted: %v", rec.names())
	}
	if rec.has(analytics.EventCheckPassed) {
		t.Errorf("a failed submission was recorded as a pass: %v", rec.names())
	}
}

// The submitted source is something a person wrote. It belongs in a feedback
// table they opted into, never in an analytics payload.
func TestNoSubmittedCodeIsEverSentToAnalytics(t *testing.T) {
	rec := &recorder{}
	h := NewHandler(
		testIndex(t, lesson.Assertions{MustDeclare: []string{"let"}}),
		nil,
		fakeValidator{result: validate.Result{OK: true}},
		nil,
		rec,
		false,
	)

	const secret = "let apiKey = \"hunter2\""
	checkRequest(t, h, "x", secret)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, e := range rec.events {
		for key, value := range e.Props {
			if s, ok := value.(string); ok && s == secret {
				t.Fatalf("submitted source was captured in property %q", key)
			}
		}
	}
}

// Analytics being unreachable, unconfigured, or nil must be invisible to a
// learner. A nil client is what every test and every unconfigured deploy passes.
func TestANilAnalyticsClientDoesNotBreakAnything(t *testing.T) {
	h := NewHandler(staticLesson(t), nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)

	if code := viewRequest(t, h, "x").Code; code != http.StatusOK {
		t.Errorf("reading a lesson with no analytics client: status = %d", code)
	}
	if code := checkRequest(t, h, "x", "let a = 1").Code; code != http.StatusOK {
		t.Errorf("checking with no analytics client: status = %d", code)
	}
}
