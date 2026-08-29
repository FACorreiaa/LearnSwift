package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FACorreiaa/seshat/internal/config"
)

// testConfig builds a production config so the asset routes read from the
// embedded FS. In development they read from ./web/assets relative to the
// working directory, which during a test is this package's directory, and every
// asset would 404 for a reason that has nothing to do with the routing.
func testConfig() config.Config {
	return config.Config{
		Env:        "production",
		ListenAddr: ":0",
		BaseURL:    "http://localhost:8090",
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The pool is nil throughout: none of these routes touch the database, and
// building a real one would make the routing depend on Postgres being up.
// /healthz is deliberately not exercised here for the same reason.
func TestTheLandingPageRenders(t *testing.T) {
	rec := get(t, routes(testConfig(), nil, nil, newServices(nil, testConfig())), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Seshat",
		// The htmx configuration must be in the document, not applied later
		// from a script: htmx reads it as it initialises.
		`name="htmx-config"`,
		"/assets/js/vendor/htmx.min.js",
		"/assets/css/output.css",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page is missing %q", want)
		}
	}
}

// The 3D mascot is an enhancement layered over the SVG mark, so the page must
// carry both: the canvas for browsers that can use it, and the static mark that
// remains when JavaScript, the module import, or WebGL is unavailable.
func TestTheLandingPageShipsAMascotFallbackAlongsideTheCanvas(t *testing.T) {
	body := get(t, routes(testConfig(), nil, nil, newServices(nil, testConfig())), "/").Body.String()

	for _, want := range []string{
		"data-mascot-canvas",
		"data-mascot-fallback",
		// The loader, which decides whether to fetch three.js at all.
		"/assets/js/landing/hero.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page is missing %q", want)
		}
	}

	// three.js itself must not be referenced by the document. It is ~700 KB and
	// is fetched by a dynamic import only once the mascot is near the viewport;
	// a static reference here would defeat that entirely.
	if strings.Contains(body, "three.module") || strings.Contains(body, "three.core") {
		t.Error("three.js is referenced from the document; it should only be loaded by a dynamic import")
	}
}

func TestAssetsAreServedFromTheEmbeddedFilesystem(t *testing.T) {
	h := routes(testConfig(), nil, nil, newServices(nil, testConfig()))

	for _, path := range []string{
		"/assets/css/output.css",
		"/assets/js/vendor/htmx.min.js",
		"/assets/js/vendor/alpine.min.js",
		"/assets/js/vendor/three.module.min.js",
		"/assets/js/vendor/three.core.min.js",
		"/assets/js/landing/hero.js",
		"/assets/js/landing/mascot.js",
		"/assets/brand/favicon.svg",
		"/assets/fonts/geist/geist-variable.woff2",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, rec.Code)
		}
	}
}

func TestAssetsAreCachedImmutablyInProduction(t *testing.T) {
	rec := get(t, routes(testConfig(), nil, nil, newServices(nil, testConfig())), "/assets/css/output.css")

	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want an immutable directive", got)
	}
}

func TestAssetsAreNotCachedInDevelopment(t *testing.T) {
	cfg := testConfig()
	cfg.Env = "development"
	// Pointed at the real directory rather than left on the default, which is
	// relative to the working directory and would resolve to cmd/web here. The
	// assertion has to be made on a request that actually finds the file:
	// http.Error strips Cache-Control on the way to a 404, so testing this
	// against a miss would pass or fail for reasons unrelated to the header.
	cfg.AssetDir = "../../web/assets"

	rec := get(t, routes(cfg, nil, nil, newServices(nil, testConfig())), "/assets/css/output.css")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — has `task assets` been run?", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestFaviconRedirectsToTheBrandMark(t *testing.T) {
	rec := get(t, routes(testConfig(), nil, nil, newServices(nil, testConfig())), "/favicon.ico")

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/assets/brand/favicon.svg" {
		t.Errorf("Location = %q", got)
	}
}
