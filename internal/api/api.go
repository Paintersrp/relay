package api

import (
	"log/slog"
	"net/http"

	intakeapi "relay/internal/api/intake"
	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	"relay/internal/events"
	"relay/internal/refactors"
	"relay/internal/store"
)

// APIHandler is the legacy root API adapter. Production routes now mount
// feature handlers for runs, artifacts, intake, projects, plans, and audits;
// this adapter remains only for project-scoped refactor backlog routes and the
// development smoke setup route.
type APIHandler struct {
	store           *store.Store
	log             *slog.Logger
	eventHub        *events.Hub
	refactorService *refactors.Service
}

func NewAPIHandler(s *store.Store, log *slog.Logger, hub ...*events.Hub) *APIHandler {
	var eventHub *events.Hub
	if len(hub) > 0 {
		eventHub = hub[0]
	}
	return &APIHandler{
		store:           s,
		log:             log,
		eventHub:        eventHub,
		refactorService: refactors.NewService(s),
	}
}

// CORS middleware for local frontend development origins lives in
// internal/api/shared. The legacy api.CORSMiddleware entrypoint is preserved
// in cors.go as a compatibility wrapper.

// Legacy DTO aliases preserve the api.* names used by older smoke tests and
// external package tests while keeping the canonical definitions in feature
// transport packages.
type RelayRun = runsapi.RelayRun
type RelayRunPlanContext = runsapi.RelayRunPlanContext
type RelayRunProvenance = runsapi.RelayRunProvenance
type RelayRunSourceContext = runsapi.RelayRunSourceContext
type RelayValidationResult = runsapi.RelayValidationResult
type RelayValidationIssue = runsapi.RelayValidationIssue
type RelayArtifact = runsapi.RelayArtifact
type RelayRunEvent = runsapi.RelayRunEvent
type RelayApprovalGate = runsapi.RelayApprovalGate
type RelayLogPreview = runsapi.RelayLogPreview

type PlannerHandoffIntakeRequest = intakeapi.PlannerHandoffIntakeRequest
type PlannerHandoffIntakeResponse = intakeapi.PlannerHandoffIntakeResponse

type RelayApiErrorShape struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Code    string                 `json:"code,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	shared.JSON(w, status, data)
}

func writeError(w http.ResponseWriter, status int, errStr, msg string) {
	shared.Error(w, status, errStr, msg)
}
