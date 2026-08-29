package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/shared/analytics"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"github.com/FACorreiaa/seshat/internal/shared/htmx"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	"github.com/FACorreiaa/seshat/internal/shared/ratelimit"
	authpages "github.com/FACorreiaa/seshat/web/auth"
)

const (
	// Sized to be invisible to a person who has forgotten which password they
	// used, and ruinous to anyone guessing. Ten tries in fifteen minutes is
	// roughly 960 a day against one account.
	loginAttemptLimit  = 10
	loginAttemptWindow = 15 * time.Minute

	// Registration is limited too, per address, so the endpoint cannot be used
	// to spray account-creation attempts or to probe which addresses are taken.
	registerAttemptLimit  = 5
	registerAttemptWindow = time.Hour
)

type Handler struct {
	svc       *Service
	mw        *Middleware
	analytics analytics.Client

	logins    *ratelimit.Limiter
	registers *ratelimit.Limiter

	// authOverride replaces the service call in tests. Nil in every real
	// build; see loginFunc.
	authOverride authFunc
}

func NewHandler(svc *Service, mw *Middleware, an analytics.Client) *Handler {
	if an == nil {
		an = analytics.Nop()
	}
	return &Handler{
		svc:       svc,
		mw:        mw,
		analytics: an,
		logins:    ratelimit.New(loginAttemptLimit, loginAttemptWindow),
		registers: ratelimit.New(registerAttemptLimit, registerAttemptWindow),
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/login", h.showLogin)
	r.Post("/login", h.login)
	r.Get("/register", h.showRegister)
	r.Post("/register", h.register)
	// A POST, never a link. A GET that signs the visitor out can be triggered
	// by any image tag on any page on the internet.
	r.Post("/logout", h.logout)
}

func (h *Handler) showLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/lessons", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, authpages.LoginPage(authpages.Form{}))
}

func (h *Handler) showRegister(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/lessons", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, authpages.RegisterPage(authpages.Form{}))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	// No event for a sign-in. The funnel this measures is about people arriving
	// and learning something, and a returning visitor's sign-in adds nothing to
	// it that `lesson_viewed` does not already say.
	h.submit(w, r, h.loginFunc(), h.logins, authpages.LoginForm, authpages.LoginPage, nil)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	h.submit(w, r, h.registerFunc(), h.registers, authpages.RegisterForm, authpages.RegisterPage, h.captureSignUp)
}

// captureSignUp records that an account was created. Deliberately carries no
// email address: what matters is that someone decided a guest session was worth
// keeping, not who they are.
func (h *Handler) captureSignUp(r *http.Request, user User) {
	h.analytics.Capture(analytics.Event{
		Name:       analytics.EventSignedUp,
		DistinctID: analytics.DistinctID(user.ID.String(), middleware.ClientIP(r), r.UserAgent()),
	})
}

// loginFunc and registerFunc exist so a test can drive the handler without a
// database. The rate limiting, the status codes and the re-rendering are worth
// testing on their own, and standing up Postgres to check that a 429 carries a
// Retry-After header would test the wrong thing.
func (h *Handler) loginFunc() authFunc {
	if h.authOverride != nil {
		return h.authOverride
	}
	return h.svc.Login
}

func (h *Handler) registerFunc() authFunc {
	if h.authOverride != nil {
		return h.authOverride
	}
	return h.svc.Register
}

// limitKeys returns the keys an attempt is counted against.
//
// Both the address and the client IP are limited, because they defend against
// different things: per-address stops one account being ground down from a
// changing set of addresses, and per-IP stops one source working through a list
// of accounts. Either being over the limit refuses the attempt.
func limitKeys(email string, ip netip.Addr) []string {
	keys := []string{"email:" + NormalizeEmail(email)}
	if ip.IsValid() {
		keys = append(keys, "ip:"+ip.String())
	}
	return keys
}

type authFunc func(ctx context.Context, c Credentials) (User, string, error)

type formRenderer func(authpages.Form) templ.Component

// submit is shared by sign-in and registration because the two differ only in
// which service call they make and which template they re-render. The handling
// of a failure — status code, fragment versus page, what is echoed back — is
// identical, and is the part worth having in one place.
func (h *Handler) submit(
	w http.ResponseWriter,
	r *http.Request,
	auth authFunc,
	limiter *ratelimit.Limiter,
	fragment, page formRenderer,
	onSuccess func(*http.Request, User),
) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}

	email := r.PostFormValue("email")
	ip := clientIP(r)
	creds := Credentials{
		Email:     email,
		Password:  r.PostFormValue("password"),
		UserAgent: r.UserAgent(),
		IP:        ip,
	}

	keys := limitKeys(email, ip)

	// Every key is recorded before any is checked, so a caller cannot avoid
	// being counted against their IP simply by being under the per-address
	// limit. Short-circuiting here would leave one of the two defences unarmed.
	allowed := true
	for _, key := range keys {
		if !limiter.Allow(key) {
			allowed = false
		}
	}
	if !allowed {
		var retry time.Duration
		for _, key := range keys {
			if d := limiter.RetryAfter(key); d > retry {
				retry = d
			}
		}

		middleware.FromContext(r.Context()).Warn("rate limited",
			slog.String("path", r.URL.Path),
			slog.Duration("retry_after", retry),
		)

		// A whole number of seconds, per the header's definition, and at least
		// one — a Retry-After of 0 reads as "try again immediately".
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Round(time.Second)/time.Second))))

		// 429 is swapped into the page by the htmx config in the layout, so the
		// visitor sees this rather than the response being discarded. The
		// message deliberately does not say whether the address exists.
		form := authpages.Form{
			Email:  email,
			Errors: map[string]string{"form": "Too many attempts. Try again in " + humanDuration(retry) + "."},
		}
		if htmx.IsRequest(r) {
			render(w, r, http.StatusTooManyRequests, fragment(form))
		} else {
			render(w, r, http.StatusTooManyRequests, page(form))
		}
		return
	}

	user, token, err := auth(r.Context(), creds)
	if err != nil {
		var fields apperr.FieldErrors
		if errors.As(err, &fields) {
			// 422 rather than 400: the htmx config in the layout swaps a 422
			// into the page, so the visitor sees the messages beside the
			// fields instead of the response being discarded.
			form := authpages.Form{Email: email, Errors: fields}
			if htmx.IsRequest(r) {
				render(w, r, http.StatusUnprocessableEntity, fragment(form))
			} else {
				render(w, r, http.StatusUnprocessableEntity, page(form))
			}
			return
		}

		middleware.FromContext(r.Context()).Error("authentication failed", slog.Any("error", err))
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	// Success clears the counters, so someone who mistyped their password twice
	// and then got it right is not left carrying those attempts.
	for _, key := range keys {
		limiter.Reset(key)
	}

	if onSuccess != nil {
		onSuccess(r, user)
	}

	h.mw.SetCookie(w, token)
	htmx.Redirect(w, r, "/lessons")
}

// humanDuration renders a retry delay the way a person would say it. The exact
// number of seconds is in the Retry-After header for anything automated.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "a moment"
	}
	if minutes := int(d.Round(time.Minute) / time.Minute); minutes > 1 {
		return strconv.Itoa(minutes) + " minutes"
	}
	return "a minute"
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.svc.Logout(r.Context(), cookie.Value); err != nil {
			// The cookie is cleared regardless. Leaving the visitor apparently
			// signed in because a DELETE failed is the worse outcome.
			middleware.FromContext(r.Context()).Error("could not delete session", slog.Any("error", err))
		}
	}
	h.mw.ClearCookie(w)
	htmx.Redirect(w, r, "/")
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

// clientIP records where a session was created from. It is informational — it
// is never used to authorise anything — so a proxy header that cannot be
// verified is not a security problem here, and an unparseable one just means
// the column stays null.
// clientIP is an alias kept so this package reads the same as it did when it
// owned the implementation. The logic moved to shared/middleware once the
// exercise limiter needed the identical rule — two copies of "which address do
// we count against" is how the two limiters end up disagreeing.
func clientIP(r *http.Request) netip.Addr { return middleware.ClientIP(r) }
