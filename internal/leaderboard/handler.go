package leaderboard

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/FACorreiaa/seshat/internal/auth"
	apperr "github.com/FACorreiaa/seshat/internal/shared/errors"
	"github.com/FACorreiaa/seshat/internal/shared/middleware"
	boardpages "github.com/FACorreiaa/seshat/web/leaderboard"
)

// Handler serves the boards.
//
// Reading them needs no account. Only names somebody asked to publish appear,
// so there is nothing here to protect — and a ranking nobody can look at
// without signing up is a ranking nobody looks at.
type Handler struct {
	svc *Service
	mw  *auth.Middleware
}

func NewHandler(svc *Service, mw *auth.Middleware) *Handler {
	return &Handler{svc: svc, mw: mw}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/leaderboard", h.show)

	// Joining and leaving are both writes, so both are POSTs behind an
	// account. A GET that puts somebody's name on a public page could be
	// triggered by any image tag anywhere.
	r.With(h.mw.RequireAuth).Post("/leaderboard/opt-in", h.optIn)
	r.With(h.mw.RequireAuth).Post("/leaderboard/opt-out", h.optOut)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	view, ok := h.view(w, r, "")
	if !ok {
		return
	}
	render(w, r, http.StatusOK, boardpages.Page(view))
}

func (h *Handler) optIn(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("display_name")

	if err := h.svc.OptIn(r.Context(), user.ID, name); err != nil {
		if apperr.Is(err, apperr.ErrValidation) {
			view, ok := h.view(w, r, apperr.Message(err))
			if !ok {
				return
			}
			// Echoed back so a rejected name can be corrected rather than
			// retyped. Safe to echo: it failed validation, so it is not on the
			// board, but it has still been through cleanDisplayName's rules
			// only in the sense of being refused by them — the template
			// escapes it either way.
			view.Name = name
			render(w, r, http.StatusUnprocessableEntity, boardpages.Page(view))
			return
		}
		middleware.FromContext(r.Context()).Error("could not opt in to the leaderboard", slog.Any("error", err))
		http.Error(w, "could not join the board", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/leaderboard", http.StatusSeeOther)
}

func (h *Handler) optOut(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := h.svc.OptOut(r.Context(), user.ID); err != nil {
		middleware.FromContext(r.Context()).Error("could not opt out of the leaderboard", slog.Any("error", err))
		http.Error(w, "could not leave the board", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/leaderboard", http.StatusSeeOther)
}

// view assembles both boards and the viewer's own position in them. It reports
// false when it has already answered the request.
func (h *Handler) view(w http.ResponseWriter, r *http.Request, errMessage string) (boardpages.View, bool) {
	solo, assisted, err := h.svc.Boards(r.Context())
	if err != nil {
		middleware.FromContext(r.Context()).Error("could not read the leaderboard", slog.Any("error", err))
		http.Error(w, "could not load the board", http.StatusInternalServerError)
		return boardpages.View{}, false
	}

	view := boardpages.View{Error: errMessage}

	if user, ok := auth.UserFrom(r.Context()); ok {
		view.SignedIn = true

		name, optedIn, standingErr := h.svc.Standing(r.Context(), user.ID)
		if standingErr != nil {
			// Not fatal. The boards were read; failing to say which row is
			// theirs is a smaller loss than refusing the page.
			middleware.FromContext(r.Context()).Warn("could not read own standing", slog.Any("error", standingErr))
		}
		view.Name = name
		view.OptedIn = optedIn
	}

	view.Solo = rank(solo, view.Name)
	view.Assisted = rank(assisted, view.Name)
	return view, true
}

// rank numbers a board and marks the viewer's own line, so a learner can find
// themselves without reading every name.
//
// Equal counts share a rank, which is what anybody reading a scoreboard
// expects: two learners on nine solves are both second, and the next is fourth.
func rank(standings []Standing, name string) []boardpages.Standing {
	out := make([]boardpages.Standing, 0, len(standings))

	for i, s := range standings {
		place := i + 1
		if i > 0 {
			previous := out[i-1]
			if previous.Solo == s.Solo && previous.Assisted == s.Assisted {
				place = previous.Rank
			}
		}

		out = append(out, boardpages.Standing{
			Rank:        place,
			DisplayName: s.DisplayName,
			Solo:        s.Solo,
			Assisted:    s.Assisted,
			You:         name != "" && s.DisplayName == name,
		})
	}
	return out
}
