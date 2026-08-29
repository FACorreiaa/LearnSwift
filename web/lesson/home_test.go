package lesson

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
	"github.com/FACorreiaa/seshat/internal/progress"
)

func tracks() []lesson.Track {
	return []lesson.Track{
		{Slug: "swift-basics", Title: "Swift Basics", Lessons: []lesson.Lesson{
			{Slug: "variables", Title: "Learn let and var", Minutes: 2, Summary: "Two keywords."},
			{Slug: "types", Title: "Learn Type Inference", Minutes: 3},
		}},
		{Slug: "swiftui", Title: "SwiftUI Fundamentals", Lessons: []lesson.Lesson{
			{Slug: "views", Title: "Learn SwiftUI Views", Minutes: 3},
		}},
	}
}

func completed(slugs ...string) map[string]progress.LessonProgress {
	now := time.Now()
	out := map[string]progress.LessonProgress{}
	for _, s := range slugs {
		out[s] = progress.LessonProgress{
			LessonSlug:  s,
			Status:      progress.StatusCompleted,
			CompletedAt: &now,
		}
	}
	return out
}

func render(t *testing.T, v View) string {
	t.Helper()

	var sb strings.Builder
	if err := HomePage(v).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// The whole point of the page: a returning learner should not have to remember
// where they stopped.
func TestContinueIsTheFirstUnfinishedLessonInTrackOrder(t *testing.T) {
	v := WithContinue(View{Tracks: tracks(), Progress: completed("variables")})

	if !v.HasContinue {
		t.Fatal("nothing to continue with two lessons unfinished")
	}
	if v.Continue.Slug != "types" {
		t.Errorf("Continue = %q, want the next unfinished lesson in order", v.Continue.Slug)
	}
}

// Ordered by the curriculum, not by what was touched most recently: a learner
// returning after a week wants the next lesson, not the one they bounced off.
func TestContinueDoesNotSkipAheadOverAnUnfinishedLesson(t *testing.T) {
	v := WithContinue(View{Tracks: tracks(), Progress: completed("types", "views")})

	if v.Continue.Slug != "variables" {
		t.Errorf("Continue = %q, want the earliest unfinished lesson", v.Continue.Slug)
	}
}

func TestEverythingFinishedLeavesNothingToContinue(t *testing.T) {
	v := WithContinue(View{Tracks: tracks(), Progress: completed("variables", "types", "views")})

	if v.HasContinue {
		t.Errorf("Continue = %q with every lesson finished", v.Continue.Slug)
	}

	body := render(t, v)
	if !strings.Contains(body, "That is all of them") {
		t.Errorf("the finished state did not render: %s", body)
	}
}

// A learner opening their very first lesson has not left anything to come back
// to, and being told to "continue" reads as software not paying attention.
func TestAFirstVisitIsInvitedToStartRatherThanContinue(t *testing.T) {
	body := render(t, WithContinue(View{Tracks: tracks()}))

	if !strings.Contains(body, "Start the first lesson") {
		t.Errorf("a learner with no progress was not invited to start: %s", body)
	}
	if strings.Contains(body, ">Continue<") {
		t.Errorf("a learner with no progress was told to continue: %s", body)
	}
}

func TestTheHomePageLinksStraightIntoTheLesson(t *testing.T) {
	body := render(t, WithContinue(View{Tracks: tracks(), Progress: completed("variables")}))

	if !strings.Contains(body, `href="/lessons/types"`) {
		t.Errorf("the continue button does not link to the lesson: %s", body)
	}
	if !strings.Contains(body, "1 of 3 lessons done") {
		t.Errorf("the overall count is missing or wrong: %s", body)
	}
}

// Rounded down, so a track is never shown as finished until it is. Reporting
// 100% on nineteen of twenty is a small lie that undermines every other number.
func TestTrackProgressIsRoundedDown(t *testing.T) {
	v := View{Tracks: tracks(), Progress: completed("variables")}

	if got := v.PercentComplete(v.Tracks[0]); got != 50 {
		t.Errorf("PercentComplete = %d, want 50", got)
	}
	if got := v.PercentComplete(v.Tracks[1]); got != 0 {
		t.Errorf("PercentComplete = %d for an untouched track, want 0", got)
	}

	v.Progress = completed("variables", "types")
	if got := v.PercentComplete(v.Tracks[0]); got != 100 {
		t.Errorf("PercentComplete = %d for a finished track, want 100", got)
	}
}

// The bar carries the number for anyone who can see it. The meter role carries
// it for everyone else.
func TestProgressBarsAreAnnouncedNotOnlyDrawn(t *testing.T) {
	body := render(t, WithContinue(View{Tracks: tracks(), Progress: completed("variables")}))

	if !strings.Contains(body, `role="meter"`) {
		t.Errorf("progress is drawn but not announced: %s", body)
	}
	if !strings.Contains(body, `aria-label="Swift Basics progress"`) {
		t.Errorf("the meter has no accessible name: %s", body)
	}
	if !strings.Contains(body, "width: 50%") {
		t.Errorf("the bar width does not reflect progress: %s", body)
	}
}

// An empty index must not render a page claiming a learner has finished
// everything — that is a different and much more annoying wrong answer.
func TestNoTracksIsNotReportedAsFinished(t *testing.T) {
	v := WithContinue(View{})

	if v.TotalLessons() != 0 {
		t.Fatalf("TotalLessons = %d with no tracks", v.TotalLessons())
	}
	if got := v.PercentComplete(lesson.Track{}); got != 0 {
		t.Errorf("PercentComplete = %d for an empty track, want 0", got)
	}
}
