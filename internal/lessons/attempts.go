package lessons

import (
	"net/http"
	"strconv"
	"strings"
)

// How much failing earns help. Two is late enough that a typo does not trigger
// it and early enough that nobody is stuck for long; four is where perseverance
// has stopped being the thing being taught.
const (
	hintAfterFailures     = 2
	solutionAfterFailures = 4
)

// failureCookie tracks a signed-out learner's wrong answers, so a guest can be
// offered a hint on the same terms as anyone else.
//
// A cookie rather than server state, because the alternatives are worse: a
// per-process map is lost on deploy and shared by everyone behind one NAT, and
// a database row for someone with no account is a row nobody can ever delete.
// It is unsigned on purpose — the worst a forged value buys is an earlier hint,
// which is not a thing worth defending against, and signing it would mean
// holding a key to protect a number that helps the person holding it.
const failureCookie = "seshat_tries"

const (
	// A learner works through a handful of lessons in a sitting. Past this the
	// oldest entries are dropped, which costs someone returning to an
	// abandoned lesson a couple of extra attempts before the hint reappears.
	maxTrackedLessons = 12

	// Belt and braces against a cookie growing past what a browser will store.
	maxCookieBytes = 1024
)

// guestFailures reads the failure count for one lesson out of the request.
func guestFailures(r *http.Request, slug string) int {
	return parseFailures(r)[slug]
}

// recordGuestFailure increments the count for a lesson, writes the cookie back,
// and returns the new total.
//
// It returns the count rather than leaving the caller to re-read it, because the
// cookie it just wrote is on the *response*: reading the request again would
// return the number from before this attempt, and the hint would arrive one
// failure later than intended.
func recordGuestFailure(w http.ResponseWriter, r *http.Request, slug string, secure bool) int {
	counts := parseFailures(r)
	counts[slug]++
	writeFailures(w, counts, slug, secure)
	return counts[slug]
}

// parseFailures decodes "slug:count|slug:count". Anything malformed is skipped
// rather than rejected: this is a convenience, and a corrupt cookie should cost
// a learner a hint, not an error page.
func parseFailures(r *http.Request) map[string]int {
	out := map[string]int{}

	cookie, err := r.Cookie(failureCookie)
	if err != nil || cookie.Value == "" {
		return out
	}

	for entry := range strings.SplitSeq(cookie.Value, "|") {
		slug, raw, ok := strings.Cut(entry, ":")
		if !ok || slug == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			continue
		}
		// Capped on read as well as write, so a hand-edited cookie cannot make
		// this number mean anything the rest of the code has to handle.
		out[slug] = min(n, solutionAfterFailures)
	}
	return out
}

// writeFailures serialises the counts, keeping newest first so that trimming to
// the cap drops the lessons least recently failed.
func writeFailures(w http.ResponseWriter, counts map[string]int, newest string, secure bool) {
	parts := make([]string, 0, len(counts))
	parts = append(parts, newest+":"+strconv.Itoa(counts[newest]))
	for slug, n := range counts {
		if slug == newest {
			continue
		}
		if len(parts) >= maxTrackedLessons {
			break
		}
		parts = append(parts, slug+":"+strconv.Itoa(n))
	}

	value := strings.Join(parts, "|")
	for len(value) > maxCookieBytes && len(parts) > 1 {
		parts = parts[:len(parts)-1]
		value = strings.Join(parts, "|")
	}

	http.SetCookie(w, &http.Cookie{
		Name:  failureCookie,
		Value: value,
		Path:  "/",
		// Not a session identifier and not worth reading from JavaScript, so it
		// is given neither capability.
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

// clearGuestFailures drops a lesson's count once it has been passed, so coming
// back to revise does not open on an offer of the answer.
func clearGuestFailures(w http.ResponseWriter, r *http.Request, slug string, secure bool) {
	counts := parseFailures(r)
	if _, ok := counts[slug]; !ok {
		return
	}
	delete(counts, slug)

	parts := make([]string, 0, len(counts))
	for s, n := range counts {
		parts = append(parts, s+":"+strconv.Itoa(n))
	}

	http.SetCookie(w, &http.Cookie{
		Name:     failureCookie,
		Value:    strings.Join(parts, "|"),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}
