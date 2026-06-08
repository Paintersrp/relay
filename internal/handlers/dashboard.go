package handlers

import (
	"net/http"

	"relay/internal/store"
	"relay/internal/views"
)

type DashboardHandler struct {
	store *store.Store
}

func NewDashboardHandler(s *store.Store) *DashboardHandler {
	return &DashboardHandler{store: s}
}

func (h *DashboardHandler) Get(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.ListRecentRuns(20)
	if err != nil {
		http.Error(w, "failed to load runs", http.StatusInternalServerError)
		return
	}
	views.Dashboard(runs).Render(r.Context(), w)
}
