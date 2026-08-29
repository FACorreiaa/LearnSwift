// Package htmx answers the only question the server needs to ask about htmx:
// did this request come from it?
//
// The answer changes what a handler returns — a fragment rather than a whole
// page, an HX-Redirect header rather than a 303 — so it belongs somewhere both
// the auth middleware and every feature handler can reach without importing
// each other.
package htmx

import "net/http"

func IsRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// IsBoosted reports a request from hx-boost, which is an htmx request that
// still expects a full page rather than a fragment.
func IsBoosted(r *http.Request) bool {
	return r.Header.Get("HX-Boosted") == "true"
}

// Redirect sends the visitor to url.
//
// A 303 is correct for a normal form post and wrong for htmx: htmx follows the
// redirect itself and swaps the resulting page into whatever target the request
// named, so a sign-in page ends up nested inside a fragment. HX-Redirect with a
// 200 tells htmx to navigate the window instead.
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	if IsRequest(r) && !IsBoosted(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
