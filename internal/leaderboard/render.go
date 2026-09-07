package leaderboard

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/FACorreiaa/seshat/internal/shared/middleware"
)

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
