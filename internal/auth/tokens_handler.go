package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	accountpages "github.com/FACorreiaa/seshat/web/account"
)

// tokenRoutes registers the access-token pages.
//
// Registered only when there is a store to serve them, so a build with no
// database wired — which is what the handler tests construct — has no route
// that could reach a nil pointer.
func (h *Handler) tokenRoutes(r chi.Router) {
	if h.tokens == nil {
		return
	}

	r.With(h.mw.RequireAuth).Group(func(r chi.Router) {
		r.Get("/account/tokens", h.showTokens)
		r.Post("/account/tokens", h.createToken)
		// A POST, never a link. A GET that revokes a credential can be fired
		// by any image tag on any page on the internet.
		r.Post("/account/tokens/{id}/revoke", h.revokeToken)
	})
}

func (h *Handler) showTokens(w http.ResponseWriter, r *http.Request) {
	view, ok := h.tokensView(w, r, "")
	if !ok {
		return
	}
	render(w, r, http.StatusOK, accountpages.TokensPage(view))
}

// createToken issues a token and renders it once.
//
// Deliberately not a redirect. The plaintext exists only in this response, and
// a 303 would either lose it or require stashing a live credential in a flash
// cookie — which is the one place it must never be.
func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	user := MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}

	token, _, err := h.tokens.Create(r.Context(), user.ID, r.PostFormValue("label"))
	if err != nil {
		if apperr.Is(err, apperr.ErrValidation) {
			// The validation message is the learner's own mistake described
			// back to them, so it is shown as written.
			view, ok := h.tokensView(w, r, apperr.Message(err))
			if !ok {
				return
			}
			render(w, r, http.StatusUnprocessableEntity, accountpages.TokensPage(view))
			return
		}
		middleware.FromContext(r.Context()).Error("could not create api token", slog.Any("error", err))
		http.Error(w, "could not create that token", http.StatusInternalServerError)
		return
	}

	view, ok := h.tokensView(w, r, "")
	if !ok {
		return
	}
	view.Issued = token
	render(w, r, http.StatusCreated, accountpages.TokensPage(view))
}

func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) {
	user := MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// A token that was not theirs and a token that never existed are the same
	// answer, for the same reason a failed lookup is: there is nothing useful
	// to tell apart.
	if err := h.tokens.Delete(r.Context(), id, user.ID); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		middleware.FromContext(r.Context()).Error("could not revoke api token", slog.Any("error", err))
		http.Error(w, "could not revoke that token", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/account/tokens", http.StatusSeeOther)
}

// tokensView loads the list. It reports false when it has already answered the
// request, which happens only when the list itself could not be read.
func (h *Handler) tokensView(w http.ResponseWriter, r *http.Request, errMessage string) (accountpages.View, bool) {
	user := MustUser(r.Context())

	tokens, err := h.tokens.List(r.Context(), user.ID)
	if err != nil {
		middleware.FromContext(r.Context()).Error("could not list api tokens", slog.Any("error", err))
		http.Error(w, "could not load your tokens", http.StatusInternalServerError)
		return accountpages.View{}, false
	}

	view := accountpages.View{
		Tokens: make([]accountpages.Token, 0, len(tokens)),
		MCPURL: strings.TrimSuffix(h.baseURL, "/") + "/mcp",
		Error:  errMessage,
	}
	for _, t := range tokens {
		view.Tokens = append(view.Tokens, accountpages.Token{
			ID:       t.ID.String(),
			Label:    t.Label,
			Created:  t.CreatedAt.Format(tokenDateFormat),
			LastUsed: t.LastUsedAt.Format(tokenDateFormat),
			Expires:  t.ExpiresAt.Format(tokenDateFormat),
			Expired:  t.Expired(),
		})
	}
	return view, true
}

// tokenDateFormat is a date and not a timestamp. The question a learner asks of
// this page is "which of these is the old one", and a minute-accurate stamp
// answers it no better while being harder to read.
const tokenDateFormat = "2 Jan 2006"
