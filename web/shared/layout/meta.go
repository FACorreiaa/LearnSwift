package layout

import "strings"

// Meta is what a page tells search engines and link previews about itself.
//
// It exists because a title alone is not enough to be shared: a link with no
// description and no image renders as a bare grey box everywhere it is pasted,
// and a lesson with no description gives Google nothing to show under it. Every
// page has to answer these questions, so every page passes this.
type Meta struct {
	// Title is the browser tab and the headline of any preview card. Written
	// whole — "Optionals — Seshat", not "Optionals" — because it is read out of
	// context far more often than in it.
	Title string

	// Description is the sentence under the title in a search result and in a
	// preview card. Lessons already carry one: their frontmatter Summary is
	// written for exactly this and should be passed straight through.
	Description string

	// Path is this page's canonical path, beginning with a slash. It becomes
	// the canonical link and og:url once a base URL is known. Empty means the
	// page does not claim a canonical address, which is right for a fragment
	// and wrong for a page.
	Path string

	// Image is the preview card's picture, as a path under /assets. Empty falls
	// back to the site-wide card.
	Image string
}

// defaultImage is the card shown for any page that does not name its own.
const defaultImage = "/assets/brand/og-default.png"

// baseURL is the origin absolute URLs are built from. It is package state, set
// once at startup, which is a deliberate exception to this codebase's habit of
// handing every dependency in at construction: the alternative is threading a
// configuration value through every page signature in the application to answer
// one question that cannot change while the process runs.
//
// Unset, the absolute tags are omitted rather than emitted as relative URLs. A
// wrong canonical is worse than no canonical — it tells a crawler the page
// lives somewhere it does not.
var baseURL string

// SetBaseURL is called once, from main, before the server starts listening.
func SetBaseURL(u string) { baseURL = strings.TrimSuffix(u, "/") }

// BaseURL reports the configured origin. It is exported so the sitemap handler
// builds its URLs from the same value the canonical tags use, rather than from a
// second copy that can drift.
func BaseURL() string { return baseURL }

// canonical returns the absolute URL of this page, or "" when one cannot be
// built. Callers must treat "" as "emit no tag".
func (m Meta) canonical() string {
	if baseURL == "" || m.Path == "" {
		return ""
	}
	return baseURL + m.Path
}

func (m Meta) imageURL() string {
	image := m.Image
	if image == "" {
		image = defaultImage
	}
	if baseURL == "" {
		return ""
	}
	return baseURL + image
}

// description falls back to the site's own, so no page is ever shared without
// one. A missing description is the failure mode this whole type exists for.
func (m Meta) description() string {
	if m.Description != "" {
		return m.Description
	}
	return "Learn Swift and SwiftUI in the browser. Short lessons, each with an exercise you run — no Mac, no Xcode, no account needed."
}
