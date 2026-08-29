package lessons

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/lessons/lesson"
)

// realLessons parses the content that actually ships.
func realLessons(t *testing.T) *Index {
	t.Helper()

	sub, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("sub content/lessons: %v", err)
	}
	index, err := Parse(sub)
	if err != nil {
		t.Fatalf("the shipped lessons do not parse: %v", err)
	}
	return index
}

// This is the test that keeps a broken lesson from reaching a reader. Content
// is edited far more often than the parser, so the parser being correct is not
// the interesting property — the files being valid is.
func TestTheShippedLessonsAllParse(t *testing.T) {
	index := realLessons(t)

	if index.Count() == 0 {
		t.Fatal("no lessons were parsed")
	}

	for _, l := range index.All() {
		if l.Summary == "" {
			t.Errorf("%s: no summary; it is what the track listing shows", l.Slug)
		}
		if strings.TrimSpace(l.BodyHTML) == "" {
			t.Errorf("%s: rendered to an empty body", l.Slug)
		}
		if l.Starter == "" {
			t.Errorf("%s: no starter code, so the exercise panel would be blank", l.Slug)
		}
		if l.Solution == "" {
			t.Errorf("%s: no solution; nothing can verify the assertions accept a correct answer", l.Slug)
		}
		if l.Assertions.Empty() {
			t.Errorf("%s: no assertions, so its exercise can never be passed or failed", l.Slug)
		}
	}
}

// The starter must not already satisfy the exercise, or the reader is asked to
// fix something that is not broken. This checks the assertions that can be
// evaluated without running Swift; the output-based ones wait for Phase 2.
func TestAStarterDoesNotAlreadySatisfyItsStaticAssertions(t *testing.T) {
	for _, l := range realLessons(t).All() {
		for _, want := range l.Assertions.MustDeclare {
			if lesson.UsesToken(l.Starter, want) {
				t.Errorf("%s: the starter already contains %q, which the exercise asks the reader to add",
					l.Slug, want)
			}
		}
		for _, banned := range l.Assertions.MustNotUse {
			if lesson.UsesToken(l.Solution, banned) {
				t.Errorf("%s: the solution uses %q, which its own assertions forbid", l.Slug, banned)
			}
		}
	}
}

func TestASolutionSatisfiesItsOwnStaticAssertions(t *testing.T) {
	for _, l := range realLessons(t).All() {
		for _, want := range l.Assertions.MustDeclare {
			if !lesson.UsesToken(l.Solution, want) {
				t.Errorf("%s: the solution is missing %q, which its assertions require", l.Slug, want)
			}
		}
	}
}

func TestLessonsAreGroupedIntoOrderedTracks(t *testing.T) {
	index := realLessons(t)

	if len(index.Tracks()) == 0 {
		t.Fatal("no tracks")
	}

	for _, track := range index.Tracks() {
		if track.Title == "" {
			t.Errorf("track %q has no title", track.Slug)
		}
		for n := 1; n < len(track.Lessons); n++ {
			if track.Lessons[n-1].Order > track.Lessons[n].Order {
				t.Errorf("track %s is not in order", track.Slug)
			}
		}
	}
}

func TestNextWalksReadingOrderAndStopsAtTheEnd(t *testing.T) {
	index := realLessons(t)
	all := index.All()

	for n := 0; n < len(all)-1; n++ {
		next, ok := index.Next(all[n].Slug)
		if !ok {
			t.Fatalf("%s: expected a following lesson", all[n].Slug)
		}
		if next.Slug != all[n+1].Slug {
			t.Errorf("after %s: got %s, want %s", all[n].Slug, next.Slug, all[n+1].Slug)
		}
	}

	if _, ok := index.Next(all[len(all)-1].Slug); ok {
		t.Error("the last lesson must not report a next one")
	}
}

// --- parser behaviour, on fixtures rather than shipped content ---

const validBody = `---
title: A Lesson
track: swift-basics
order: 1
minutes: 3
summary: Something.
runtime: embedded
starter: "x"
---

Body text.
`

func TestAFileWithNoFrontmatterIsRejected(t *testing.T) {
	_, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte("# Just markdown")}})
	if err == nil {
		t.Fatal("expected an error for a file with no frontmatter")
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("error should name the problem, got: %v", err)
	}
}

func TestAnUnclosedFrontmatterBlockIsRejected(t *testing.T) {
	_, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte("---\ntitle: X\n")}})
	if err == nil {
		t.Fatal("expected an error for an unclosed frontmatter block")
	}
}

// A track name that is not in trackOrder must fail the build rather than
// parsing happily into a lesson that is then unreachable from every page.
func TestAnUnknownTrackIsRejected(t *testing.T) {
	body := strings.Replace(validBody, "track: swift-basics", "track: swift-basicz", 1)

	_, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte(body)}})
	if err == nil {
		t.Fatal("expected an error for an unknown track")
	}
	if !strings.Contains(err.Error(), "swift-basicz") {
		t.Errorf("error should name the offending track, got: %v", err)
	}
}

func TestRequiredFrontmatterFieldsAreEnforced(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"title", "title: A Lesson", "title: \"\""},
		{"track", "track: swift-basics", "track: \"\""},
		{"minutes", "minutes: 3", "minutes: 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(validBody, tc.from, tc.to, 1)

			_, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte(body)}})
			if err == nil {
				t.Fatalf("expected an error when %s is missing", tc.name)
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Errorf("error should name %s, got: %v", tc.name, err)
			}
		})
	}
}

func TestTheSlugComesFromTheFilename(t *testing.T) {
	index, err := Parse(fstest.MapFS{"my-lesson.md": &fstest.MapFile{Data: []byte(validBody)}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := index.Get("my-lesson"); !ok {
		t.Errorf("expected slug my-lesson, got %+v", index.All())
	}
}

// Swift samples are highlighted at build time, so nothing has to ship a syntax
// highlighter to the browser.
func TestCodeBlocksAreHighlightedAtBuildTime(t *testing.T) {
	body := validBody + "\n```swift\nlet x = 1\n```\n"

	index, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte(body)}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	l, _ := index.Get("a")
	if !strings.Contains(l.BodyHTML, "<span") {
		t.Errorf("expected highlighted markup, got: %.200s", l.BodyHTML)
	}

	// Classes, not inline styles. An inline style attribute pins one palette
	// into the lesson's markup, so every code block would keep a white
	// background in dark mode — which is exactly what this configuration was
	// changed to avoid, and a test that only looked for <span> would not
	// notice it coming back.
	if !strings.Contains(l.BodyHTML, `class="chroma"`) {
		t.Errorf("expected chroma class markup, got: %.300s", l.BodyHTML)
	}
	if strings.Contains(l.BodyHTML, "background-color:") {
		t.Error("inline colour styles are present; highlighting must emit classes so the theme can drive it")
	}
}

// Every lesson must declare how — or whether — its exercise can run. Getting
// this wrong is not cosmetic: an executable-by-default SwiftUI lesson would
// offer a Run button that can never work, because SwiftUI has no WebAssembly
// build at all.
func TestEveryLessonDeclaresAValidRuntime(t *testing.T) {
	for _, l := range realLessons(t).All() {
		if !l.Runtime.Valid() {
			t.Errorf("%s: runtime %q is not one of embedded, full, none", l.Slug, l.Runtime)
		}
	}
}

func TestAMissingOrUnknownRuntimeIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, runtime string }{
		{"missing", ""},
		{"unknown", "wasm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(validBody, "runtime: embedded", "runtime: "+tc.runtime, 1)

			_, err := Parse(fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte(body)}})
			if err == nil {
				t.Fatal("expected an error; a lesson must state its runtime rather than defaulting")
			}
			if !strings.Contains(err.Error(), "runtime") {
				t.Errorf("error should name the field, got: %v", err)
			}
		})
	}
}

// The two constraints the spike measured, encoded so a future lesson cannot
// quietly violate them.
func TestLessonsDoNotAskForARuntimeThatCannotRunThem(t *testing.T) {
	for _, l := range realLessons(t).All() {
		source := l.Starter + "\n" + l.Solution

		// Embedded Swift has no _Concurrency module.
		if l.Runtime == lesson.RuntimeEmbedded && lesson.UsesToken(source, "await") {
			t.Errorf("%s: declares the embedded runtime but uses await, which embedded Swift cannot compile", l.Slug)
		}

		// SwiftUI has no WebAssembly build under any SDK.
		if l.Track == "swiftui" && l.Runtime.Executable() {
			t.Errorf("%s: a SwiftUI lesson cannot be executable; SwiftUI has no WebAssembly build", l.Slug)
		}
	}
}
