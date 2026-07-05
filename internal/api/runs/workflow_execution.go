package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	"relay/internal/executor"

	"github.com/go-chi/chi/v5"
)

type WorkflowExecutionService interface {
	Start(ctx context.Context, input executor.WorkflowStartInput) (executor.WorkflowStartResult, error)
	Cancel(ctx context.Context, runID, attemptID string) (executor.WorkflowCancelResult, error)
	ListAttempts(ctx context.Context, runID string) ([]executor.WorkflowAttemptView, error)
	GetAttempt(ctx context.Context, runID, attemptID string) (executor.WorkflowAttemptView, error)
}

type WorkflowExecutionHandler struct {
	service WorkflowExecutionService
}

func NewWorkflowExecutionHandler(service WorkflowExecutionService) *WorkflowExecutionHandler {
	return &WorkflowExecutionHandler{service: service}
}

type workflowStartRequest struct {
	Adapter string `json:"adapter"`
	Model   string `json:"model"`
}

type workflowAttemptResponse struct {
	AttemptID               string                     `json:"attemptId"`
	RunID                   string                     `json:"runId"`
	AttemptNumber           int64                      `json:"attemptNumber"`
	Adapter                 string                     `json:"adapter"`
	Model                   string                     `json:"model"`
	Status                  string                     `json:"status"`
	Result                  json.RawMessage            `json:"result"`
	CreatedAt               string                     `json:"createdAt"`
	StartedAt               string                     `json:"startedAt,omitempty"`
	FinishedAt              string                     `json:"finishedAt,omitempty"`
	CancellationRequestedAt string                     `json:"cancellationRequestedAt,omitempty"`
	Artifacts               []workflowArtifactResponse `json:"artifacts"`
	LiveStdout              string                     `json:"liveStdout,omitempty"`
	LiveStderr              string                     `json:"liveStderr,omitempty"`
}

type workflowArtifactResponse struct {
	ArtifactID string `json:"artifactId"`
	Kind       string `json:"kind"`
	MediaType  string `json:"mediaType"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
}

func (h *WorkflowExecutionHandler) StartAttempt(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	var request workflowStartRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid execution-attempt request")
		return
	}
	result, err := h.service.Start(r.Context(), executor.WorkflowStartInput{
		RunID:   runID,
		Adapter: request.Adapter,
		Model:   request.Model,
	})
	if err != nil {
		var preflightErr *executor.WorkflowPreflightError
		if errors.As(err, &preflightErr) {
			shared.JSON(w, http.StatusConflict, map[string]any{
				"error":     "EXECUTION_PREFLIGHT_BLOCKED",
				"message":   preflightErr.Error(),
				"preflight": preflightErr.Result,
			})
			return
		}
		writeWorkflowExecutionError(w, err)
		return
	}
	view, err := h.service.GetAttempt(r.Context(), runID, result.Attempt.AttemptID)
	if err != nil {
		writeWorkflowExecutionError(w, err)
		return
	}
	shared.JSON(w, http.StatusAccepted, map[string]any{
		"success":   true,
		"preflight": result.Preflight,
		"attempt":   workflowAttemptDTO(runID, view),
	})
}

func (h *WorkflowExecutionHandler) CancelAttempt(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	attemptID := strings.TrimSpace(chi.URLParam(r, "attemptID"))
	result, err := h.service.Cancel(r.Context(), runID, attemptID)
	if err != nil {
		writeWorkflowExecutionError(w, err)
		return
	}
	view, err := h.service.GetAttempt(r.Context(), runID, result.Attempt.AttemptID)
	if err != nil {
		writeWorkflowExecutionError(w, err)
		return
	}
	shared.JSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"attempt": workflowAttemptDTO(runID, view),
	})
}

func (h *WorkflowExecutionHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	views, err := h.service.ListAttempts(r.Context(), runID)
	if err != nil {
		writeWorkflowExecutionError(w, err)
		return
	}
	response := make([]workflowAttemptResponse, 0, len(views))
	for _, view := range views {
		response = append(response, workflowAttemptDTO(runID, view))
	}
	shared.JSON(w, http.StatusOK, response)
}

func (h *WorkflowExecutionHandler) GetAttempt(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	attemptID := strings.TrimSpace(chi.URLParam(r, "attemptID"))
	view, err := h.service.GetAttempt(r.Context(), runID, attemptID)
	if err != nil {
		writeWorkflowExecutionError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, workflowAttemptDTO(runID, view))
}

func workflowAttemptDTO(runID string, view executor.WorkflowAttemptView) workflowAttemptResponse {
	result := json.RawMessage(`{}`)
	var boundedResult map[string]any
	if json.Unmarshal([]byte(view.Attempt.ResultJSON), &boundedResult) == nil {
		delete(boundedResult, "process_identity")
		delete(boundedResult, "owner_instance_id")
		delete(boundedResult, "command_preview")
		if data, err := json.Marshal(boundedResult); err == nil {
			result = data
		}
	}
	response := workflowAttemptResponse{
		AttemptID:     view.Attempt.AttemptID,
		RunID:         runID,
		AttemptNumber: view.Attempt.AttemptNumber,
		Adapter:       view.Attempt.Adapter,
		Model:         view.Attempt.Model,
		Status:        view.Attempt.Status,
		Result:        result,
		CreatedAt:     view.Attempt.CreatedAt,
		LiveStdout:    view.LiveStdout,
		LiveStderr:    view.LiveStderr,
		Artifacts:     make([]workflowArtifactResponse, 0, len(view.Artifacts)),
	}
	if view.Attempt.StartedAt.Valid {
		response.StartedAt = view.Attempt.StartedAt.String
	}
	if view.Attempt.FinishedAt.Valid {
		response.FinishedAt = view.Attempt.FinishedAt.String
	}
	if view.Attempt.CancellationRequestedAt.Valid {
		response.CancellationRequestedAt = view.Attempt.CancellationRequestedAt.String
	}
	for _, artifact := range view.Artifacts {
		response.Artifacts = append(response.Artifacts, workflowArtifactResponse{
			ArtifactID: artifact.ArtifactID,
			Kind:       artifact.Kind,
			MediaType:  artifact.MediaType,
			SHA256:     artifact.SHA256,
			SizeBytes:  artifact.SizeBytes,
			CreatedAt:  artifact.CreatedAt,
		})
	}
	return response
}

func writeWorkflowExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Run or execution attempt was not found")
	case strings.Contains(err.Error(), "cannot start"),
		strings.Contains(err.Error(), "does not belong"),
		strings.Contains(err.Error(), "already"):
		shared.Error(w, http.StatusConflict, "EXECUTION_CONFLICT", err.Error())
	case strings.Contains(err.Error(), "required"),
		strings.Contains(err.Error(), "invalid executor adapter"),
		strings.Contains(err.Error(), "unsupported"):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Execution operation failed")
	}
}

func MountWorkflowExecutionRoutes(r chi.Router, handler *WorkflowExecutionHandler) {
	r.Post("/workflow/runs/{runID}/attempts", handler.StartAttempt)
	r.Get("/workflow/runs/{runID}/attempts", handler.ListAttempts)
	r.Get("/workflow/runs/{runID}/attempts/{attemptID}", handler.GetAttempt)
	r.Post("/workflow/runs/{runID}/attempts/{attemptID}/cancel", handler.CancelAttempt)
}
