package handlers

import (
	"OnlySats/com"
	"context"
	"database/sql"
	"net/http"
	"time"
)

type ColorsCSSHandler struct {
	Store *sql.DB
}

func (h *ColorsCSSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	css, err := com.GenerateColorsCSS(h.Store, ctx)
	if err != nil {
		http.Error(w, "failed to build colors css", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(css))
}
