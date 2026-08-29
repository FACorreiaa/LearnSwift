package lessons

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/validate"
)

// hintLesson is a checkable lesson that ships both escape hatches, grouped into
// a track so the pages that render one have something to render.
func hintLesson(t *testing.T) *Index {
	t.Helper()

	index := testIndex(t, lesson.Assertions{MustDeclare: []string{"let"}})
	l := index.bySlug["x"]
	l.Hint = "Only one of the two names ever changes."
	l.Solution = "let a = 1"
	index.bySlug["x"] = l
	index.ordered = []lesson.Lesson{l}
	index.tracks = []lesson.Track{{Slug: "swift-basics", Title: "Swift Basics", Lessons: []lesson.Lesson{l}}}
	return index
}

func failingHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(hintLesson(t), nil, fakeValidator{result: validate.Result{OK: false}}, nil, nil, false)
}

// fail submits a wrong answer, carrying the attempt-counting cookie forward the
// way a browser would.
func fail(t *testing.T, h *Handler, jar *[]*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	router := chi.NewRouter()
	h.Routes(router)

	form := url.Values{"code": {"var a = 1"}}
	req := httptest.NewRequest(http.MethodPost, "/lessons/x/check", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range *jar {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Result().Cookies(); len(got) > 0 {
		*jar = got
	}
	return rec
}

func ask(t *testing.T, h *Handler, method, path string, jar []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	router := chi.NewRouter()
	h.Routes(router)

	req := httptest.NewRequest(method, path, nil)
	for _, c := range jar {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// The first wrong answer is not being stuck, it is working. Offering help there
// would teach a learner to reach for it instead of reading the error.
func TestNoHelpIsOfferedOnTheFirstFailure(t *testing.T) {
	h := failingHandler(t)
	var jar []*http.Cookie

	body := fail(t, h, &jar).Body.String()

	if strings.Contains(body, "Give me a hint") {
		t.Errorf("a hint was offered on the first failure: %s", body)
	}
	if strings.Contains(body, "Show me the answer") {
		t.Errorf("the solution was offered on the first failure: %s", body)
	}
}

func TestAHintIsOfferedAfterTwoFailures(t *testing.T) {
	h := failingHandler(t)
	var jar []*http.Cookie

	fail(t, h, &jar)
	body := fail(t, h, &jar).Body.String()

	if !strings.Contains(body, "Give me a hint") {
		t.Errorf("no hint offered after %d failures: %s", hintAfterFailures, body)
	}
	if strings.Contains(body, "Show me the answer") {
		t.Errorf("the solution was offered as early as the hint: %s", body)
	}
}

func TestTheSolutionIsOfferedAfterFourFailures(t *testing.T) {
	h := failingHandler(t)
	var jar []*http.Cookie

	for range solutionAfterFailures {
		fail(t, h, &jar)
	}
	body := fail(t, h, &jar).Body.String()

	if !strings.Contains(body, "Show me the answer") {
		t.Errorf("no solution offered after %d failures: %s", solutionAfterFailures, body)
	}
}

// The gate that actually matters. A button not yet drawn is politeness; the
// server refusing the request is the mechanism.
func TestHelpIsRefusedByTheServerBeforeItIsEarned(t *testing.T) {
	h := failingHandler(t)
	var jar []*http.Cookie

	if code := ask(t, h, http.MethodGet, "/lessons/x/hint", jar).Code; code != http.StatusForbidden {
		t.Errorf("hint status = %d with no failures, want 403", code)
	}
	if code := ask(t, h, http.MethodPost, "/lessons/x/solution", jar).Code; code != http.StatusForbidden {
		t.Errorf("solution status = %d with no failures, want 403", code)
	}

	fail(t, h, &jar)
	fail(t, h, &jar)

	if code := ask(t, h, http.MethodGet, "/lessons/x/hint", jar).Code; code != http.StatusOK {
		t.Errorf("hint status = %d after two failures, want 200", code)
	}
	// Two failures earns a hint and nothing more.
	if code := ask(t, h, http.MethodPost, "/lessons/x/solution", jar).Code; code != http.StatusForbidden {
		t.Errorf("solution status = %d after two failures, want 403", code)
	}
}

// A hint held back by a CSS rule is a hint already given: anyone who opens the
// page source has it. Withholding has to mean not sending it.
func TestHintAndSolutionTextAreNeverSentBeforeTheyAreUnlocked(t *testing.T) {
	index := hintLesson(t)
	hint := index.bySlug["x"].Hint
	solution := index.bySlug["x"].Solution

	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: false}}, nil, nil, false)
	var jar []*http.Cookie

	// Every response up to and including the one that offers the solution
	// button must still be free of the text itself.
	for range solutionAfterFailures {
		body := fail(t, h, &jar).Body.String()
		if strings.Contains(body, hint) {
			t.Fatalf("the hint text was sent before it was asked for: %s", body)
		}
		if strings.Contains(body, solution) {
			t.Fatalf("the solution text was sent before it was asked for: %s", body)
		}
	}

	// And once asked for, it arrives.
	body := ask(t, h, http.MethodGet, "/lessons/x/hint", jar).Body.String()
	if !strings.Contains(body, hint) {
		t.Errorf("the hint was not returned when it was asked for: %s", body)
	}
}

func TestTheSolutionArrivesWhenItIsAskedForAndEarned(t *testing.T) {
	index := hintLesson(t)
	solution := index.bySlug["x"].Solution

	h := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: false}}, nil, nil, false)
	var jar []*http.Cookie

	for range solutionAfterFailures {
		fail(t, h, &jar)
	}

	rec := ask(t, h, http.MethodPost, "/lessons/x/solution", jar)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), solution) {
		t.Errorf("the solution was not returned: %s", rec.Body.String())
	}
}

// Passing clears the count, so coming back to revise does not open on an offer
// of the answer to a lesson already solved.
func TestPassingClearsTheFailureCount(t *testing.T) {
	index := hintLesson(t)
	var jar []*http.Cookie

	failing := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: false}}, nil, nil, false)
	for range solutionAfterFailures {
		fail(t, failing, &jar)
	}

	// Same learner, same cookie, now getting it right.
	passing := NewHandler(index, nil, fakeValidator{result: validate.Result{OK: true}}, nil, nil, false)
	router := chi.NewRouter()
	passing.Routes(router)

	form := url.Values{"code": {"let a = 1"}}
	req := httptest.NewRequest(http.MethodPost, "/lessons/x/check", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range jar {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if code := ask(t, failing, http.MethodGet, "/lessons/x/hint", rec.Result().Cookies()).Code; code != http.StatusForbidden {
		t.Errorf("the failure count survived a pass: hint status = %d, want 403", code)
	}
}

// A lesson with a checkable exercise and no hint has an escape hatch that never
// opens. Enforced here rather than left to whoever writes the next lesson.
func TestEveryCheckableLessonShipsAHint(t *testing.T) {
	lessonFS, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("lesson fs: %v", err)
	}
	index, err := Parse(lessonFS)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, l := range index.All() {
		if !l.Assertions.HasStatic() {
			continue
		}
		if strings.TrimSpace(l.Hint) == "" {
			t.Errorf("lesson %q can be checked but ships no hint", l.Slug)
		}
		if strings.TrimSpace(l.Solution) == "" {
			t.Errorf("lesson %q can be checked but ships no solution", l.Slug)
		}
	}
}

// A hint that quotes the answer back is not a hint.
func TestNoHintSimplyContainsItsOwnSolution(t *testing.T) {
	lessonFS, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("lesson fs: %v", err)
	}
	index, err := Parse(lessonFS)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, l := range index.All() {
		if l.Hint == "" || l.Solution == "" {
			continue
		}
		for line := range strings.SplitSeq(l.Solution, "\n") {
			line = strings.TrimSpace(line)
			// Short lines are punctuation and closing braces, which say nothing.
			if len(line) < 20 {
				continue
			}
			if strings.Contains(l.Hint, line) {
				t.Errorf("lesson %q: the hint quotes a solution line verbatim: %q", l.Slug, line)
			}
		}
	}
}

// The root is the marketing page for a stranger. Signed in it is a dashboard,
// which needs the progress service — so with none configured it must fall back
// rather than panic, which is also what the routing tests exercise.
func TestTheRootFallsBackToTheLandingPageWithoutProgress(t *testing.T) {
	h := NewHandler(hintLesson(t), nil, fakeValidator{}, nil, nil, false)

	rec := ask(t, h, http.MethodGet, "/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Write Swift") {
		t.Errorf("the root did not render the landing page: %s", rec.Body.String()[:200])
	}
}

// The featured lesson is named by slug so that reordering the curriculum cannot
// silently change what a stranger is shown first. A missing one is a warning,
// not a crash.
func TestAMissingFeaturedLessonDoesNotBreakTheRoot(t *testing.T) {
	// hintLesson holds a single lesson under the slug "x", never FeaturedSlug.
	h := NewHandler(hintLesson(t), nil, fakeValidator{}, nil, nil, false)

	view := h.LandingView()
	if view.HasLesson {
		t.Error("a lesson was featured that is not in the index")
	}
	if len(view.Tracks) == 0 {
		t.Error("the track list was dropped along with the missing lesson")
	}
}
