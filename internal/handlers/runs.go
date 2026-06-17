package handlers

import (
	"log/slog"

	"relay/internal/events"
	"relay/internal/store"
)

type RunsHandler struct {
	store    *store.Store
	log      *slog.Logger
	eventHub *events.Hub
}

func NewRunsHandler(s *store.Store, log *slog.Logger, hub ...*events.Hub) *RunsHandler {
	var eventHub *events.Hub
	if len(hub) > 0 {
		eventHub = hub[0]
	}
	return &RunsHandler{
		store:    s,
		log:      log,
		eventHub: eventHub,
	}
}

func (h *RunsHandler) publishRunEvent(runID int64, kind, source, status string) {
	if h == nil || h.eventHub == nil {
		return
	}
	h.eventHub.Publish(events.RunEvent{
		RunID:  runID,
		Kind:   kind,
		Source: source,
		Status: status,
	})
}
