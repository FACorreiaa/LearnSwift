package main

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/content"
	"github.com/FACorreiaa/seshat/internal/lessons"
	"github.com/FACorreiaa/seshat/web/shared/layout"
)

// realIndex parses the lessons actually shipped, because the sitemap's whole
// job is to list them and a stub index would test the wrong thing.
func realIndex(t *testing.T) *lessons.Index {
	t.Helper()

	lessonFS, err := fs.Sub(content.Lessons, "lessons")
	if err != nil {
		t.Fatalf("lesson fs: %v", err)
	}
	index, err := lessons.Parse(lessonFS)
	if err != nil {
		t.Fatalf("parse lessons: %v", err)
	}
	return index
}

// withBaseURL sets the package-level origin the canonical tags and the sitemap
// are built from, and puts it back afterwards.
func withBaseURL(t *testing.T, u string) {
	t.Helper()
	previous := layout.BaseURL()
	layout.SetBaseURL(u)
	t.Cleanup(func() { layout.SetBaseURL(previous) })
}

// The blocker this guards: without these tags every link to this site — in
// Slack, on X, in a Discord channel — renders as a bare grey box, and Google has
// no description to show under the result.
func TestEveryPageCarriesADescriptionAndAPreviewCard(t *testing.T) {
	withBaseURL(t, "https://seshat.test")

	body := get(t, routes(testConfig(), nil, realIndex(t), newServices(nil, testConfig())), "/").Body.String()

	for _, want := range []string{
		`name="description"`,
		`property="og:title"`,
		`property="og:description"`,
		`property="og:image"`,
		`property="og:url"`,
		`name="twitter:card"`,
		`rel="canonical"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page is missing %q", want)
		}
	}

	if !strings.Contains(body, `href="https://seshat.test/"`) {
		t.Error("the canonical link is not absolute, so a crawler cannot resolve it")
	}
	if !strings.Contains(body, "https://seshat.test/assets/brand/og-default.png") {
		t.Error("og:image is not an absolute URL; scrapers will not fetch a relative one")
	}
}

// A wrong canonical is worse than no canonical: it tells a crawler the page
// lives at an address it does not.
func TestAbsoluteTagsAreOmittedWhenNoOriginIsConfigured(t *testing.T) {
	withBaseURL(t, "")

	body := get(t, routes(testConfig(), nil, realIndex(t), newServices(nil, testConfig())), "/").Body.String()

	if strings.Contains(body, `rel="canonical"`) {
		t.Error("a canonical link was emitted with no origin to build it from")
	}
	if !strings.Contains(body, `name="description"`) {
		t.Error("the description was dropped along with the absolute tags; it does not need an origin")
	}
}

func TestTheSitemapListsEveryLesson(t *testing.T) {
	withBaseURL(t, "https://seshat.test")

	index := realIndex(t)
	rec := get(t, routes(testConfig(), nil, index, newServices(nil, testConfig())), "/sitemap.xml")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "xml") {
		t.Errorf("Content-Type = %q, want XML", got)
	}

	body := rec.Body.String()
	for _, l := range index.All() {
		want := "https://seshat.test/lessons/" + l.Slug
		if !strings.Contains(body, want) {
			t.Errorf("sitemap is missing %s", want)
		}
	}
	if !strings.Contains(body, "https://seshat.test/lessons</loc>") {
		t.Error("sitemap is missing the lesson index itself")
	}
}

func TestRobotsPointsAtTheSitemapAndAllowsTheLessons(t *testing.T) {
	withBaseURL(t, "https://seshat.test")

	rec := get(t, routes(testConfig(), nil, realIndex(t), newServices(nil, testConfig())), "/robots.txt")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Sitemap: https://seshat.test/sitemap.xml") {
		t.Errorf("robots.txt does not point at the sitemap: %s", body)
	}
	if strings.Contains(body, "Disallow: /lessons") {
		t.Errorf("robots.txt blocks the lessons, which are the entire crawlable asset: %s", body)
	}
}

// The landing page's claim is that Swift runs in the browser with no account.
// The only convincing way to make it is to let a visitor do it, so the page
// carries the real exercise component posting to the real grading route.
func TestTheLandingPageEmbedsARunnableExercise(t *testing.T) {
	withBaseURL(t, "https://seshat.test")

	body := get(t, routes(testConfig(), nil, realIndex(t), newServices(nil, testConfig())), "/").Body.String()

	for _, want := range []string{
		`hx-post="/lessons/variables/check"`,
		`data-editor`,
		"/assets/js/lesson/editor.js",
		// The strongest sentence available, and it was previously unsaid.
		"No account needed",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page is missing %q", want)
		}
	}
}

// A missing featured lesson must not take the page down with it: the argument
// still stands in prose.
func TestTheLandingPageRendersWithoutAnIndex(t *testing.T) {
	rec := get(t, routes(testConfig(), nil, nil, newServices(nil, testConfig())), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Write Swift") {
		t.Error("the hero did not render without a lesson index")
	}
}

// The layout's htmx configuration is a contract between the server's status
// codes and what the visitor actually sees. Three statuses carry a rendered
// answer rather than an error, and htmx discards non-2xx responses unless told
// otherwise — so a status missing from this list is a panel nobody ever reads.
func TestHtmxSwapsEveryStatusThatCarriesAnAnswer(t *testing.T) {
	body := get(t, routes(testConfig(), nil, realIndex(t), newServices(nil, testConfig())), "/").Body.String()

	for _, code := range []string{"422", "429", "503"} {
		want := `{"code":"` + code + `","swap":true}`
		if !strings.Contains(body, want) {
			t.Errorf("htmx-config does not swap %s, so that panel is rendered and thrown away", code)
		}
	}
}
