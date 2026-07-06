package runs

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	workflowapp "relay/internal/app/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowReadService interface {
	ListRuns(context.Context, workflowapp.ListRunsInput) ([]workflowapp.RunSummary, error)
	GetRun(context.Context, string) (workflowapp.RunDetail, error)
	GetSpecification(context.Context, string) (workflowapp.SpecificationReview, error)
}

type WorkflowReadHandler struct {
	service WorkflowReadService
}

func NewWorkflowReadHandler(service WorkflowReadService) *WorkflowReadHandler {
	return &WorkflowReadHandler{service: service}
}

type workflowRunSummaryResponse struct {
	RunID           string                           `json:"runId"`
	FeatureSlug     string                           `json:"featureSlug"`
	RepoTarget      string                           `json:"repoTarget"`
	Status          string                           `json:"status"`
	Stage           string                           `json:"stage"`
	Branch          string                           `json:"branch"`
	BaseCommit      string                           `json:"baseCommit"`
	CanonicalSHA256 string                           `json:"canonicalSha256"`
	PlanID          string                           `json:"planId,omitempty"`
	PassID          string                           `json:"passId,omitempty"`
	PassNumber      int64                            `json:"passNumber,omitempty"`
	RemediatesRunID string                           `json:"remediatesRunId,omitempty"`
	CreatedAt       string                           `json:"createdAt"`
	UpdatedAt       string                           `json:"updatedAt"`
	CompletedAt     string                           `json:"completedAt,omitempty"`
	LatestAttempt   *workflowAttemptSummaryResponse  `json:"latestAttempt,omitempty"`
	CurrentPacket   *workflowAuditPacketLinkResponse `json:"currentPacket,omitempty"`
	LatestDecision  *workflowAuditDecisionResponse   `json:"latestDecision,omitempty"`
}

type workflowAttemptSummaryResponse struct {
	AttemptID               string                         `json:"attemptId"`
	AttemptNumber           int64                          `json:"attemptNumber"`
	Adapter                 string                         `json:"adapter"`
	Model                   string                         `json:"model"`
	Status                  string                         `json:"status"`
	CreatedAt               string                         `json:"createdAt"`
	StartedAt               string                         `json:"startedAt,omitempty"`
	FinishedAt              string                         `json:"finishedAt,omitempty"`
	CancellationRequestedAt string                         `json:"cancellationRequestedAt,omitempty"`
	Artifacts               []workflowArtifactLinkResponse `json:"artifacts"`
}

type workflowArtifactLinkResponse struct {
	ArtifactID string `json:"artifactId"`
	OwnerType  string `json:"ownerType"`
	Kind       string `json:"kind"`
	MediaType  string `json:"mediaType"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
	ContentURL string `json:"contentUrl"`
}

type workflowAuditPacketLinkResponse struct {
	AuditPacketID string `json:"auditPacketId"`
	AuditedCommit string `json:"auditedCommit"`
	PacketSHA256  string `json:"packetSha256"`
	Status        string `json:"status"`
	StaleReason   string `json:"staleReason,omitempty"`
	CreatedAt     string `json:"createdAt"`
	SupersededAt  string `json:"supersededAt,omitempty"`
}

type workflowAuditDecisionResponse struct {
	AuditDecisionID string `json:"auditDecisionId"`
	AuditedCommit   string `json:"auditedCommit"`
	PacketSHA256    string `json:"packetSha256"`
	Decision        string `json:"decision"`
	Rationale       string `json:"rationale"`
	CreatedAt       string `json:"createdAt"`
}

type workflowPlanIdentityResponse struct {
	PlanID      string `json:"planId"`
	FeatureSlug string `json:"featureSlug"`
	Status      string `json:"status"`
}

type workflowPassIdentityResponse struct {
	PassID     string `json:"passId"`
	Number     int64  `json:"number"`
	Name       string `json:"name"`
	RepoTarget string `json:"repoTarget"`
	Status     string `json:"status"`
}

func (h *WorkflowReadHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := workflowRunLimit(r)
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid limit")
		return
	}
	values, err := h.service.ListRuns(r.Context(), workflowapp.ListRunsInput{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		PlanID: strings.TrimSpace(r.URL.Query().Get("planId")),
		PassID: strings.TrimSpace(r.URL.Query().Get("passId")),
		Limit:  limit,
	})
	if err != nil {
		writeWorkflowReadError(w, err)
		return
	}
	items := make([]workflowRunSummaryResponse, 0, len(values))
	for _, value := range values {
		items = append(items, workflowRunSummaryDTO(value))
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (h *WorkflowReadHandler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetRun(r.Context(), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeWorkflowReadError(w, err)
		return
	}
	attempts := make([]workflowAttemptSummaryResponse, 0, len(detail.Attempts))
	for _, attempt := range detail.Attempts {
		attempts = append(attempts, workflowAttemptSummaryDTO(attempt))
	}
	artifacts := make([]workflowArtifactLinkResponse, 0, len(detail.Artifacts))
	for _, artifact := range detail.Artifacts {
		artifacts = append(artifacts, workflowRunArtifactDTO(artifact))
	}
	shared.JSON(w, http.StatusOK, map[string]any{
		"run":       workflowRunSummaryDTO(detail.Summary),
		"attempts":  attempts,
		"artifacts": artifacts,
	})
}

func (h *WorkflowReadHandler) GetSpecification(w http.ResponseWriter, r *http.Request) {
	review, err := h.service.GetSpecification(r.Context(), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeWorkflowReadError(w, err)
		return
	}
	response := map[string]any{
		"run":           workflowRunSummaryDTO(review.Run),
		"executionSpec": workflowRunArtifactDTO(review.ExecutionSpec),
		"executorBrief": workflowRunArtifactDTO(review.ExecutorBrief),
	}
	if review.Plan != nil {
		response["plan"] = workflowPlanIdentityResponse{
			PlanID: review.Plan.PlanID, FeatureSlug: review.Plan.FeatureSlug, Status: review.Plan.Status,
		}
	}
	if review.Pass != nil {
		response["pass"] = workflowPassIdentityResponse{
			PassID: review.Pass.PassID, Number: review.Pass.PassNumber, Name: review.Pass.Name,
			RepoTarget: review.Pass.RepoTarget, Status: review.Pass.Status,
		}
	}
	if review.RemediatesRunID != "" {
		response["remediatesRunId"] = review.RemediatesRunID
	}
	shared.JSON(w, http.StatusOK, response)
}

func workflowRunLimit(r *http.Request) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(value)
	return limit, err == nil && limit > 0
}

func workflowRunSummaryDTO(value workflowapp.RunSummary) workflowRunSummaryResponse {
	response := workflowRunSummaryResponse{
		RunID:           value.Run.RunID,
		FeatureSlug:     value.Run.FeatureSlug,
		RepoTarget:      value.Run.RepoTarget,
		Status:          value.Run.Status,
		Stage:           value.Stage,
		Branch:          value.Run.Branch,
		BaseCommit:      value.Run.BaseCommit,
		CanonicalSHA256: value.Run.CanonicalSHA256,
		PlanID:          value.PlanID,
		PassID:          value.PassID,
		PassNumber:      value.PassNumber,
		RemediatesRunID: value.RemediatesRunID,
		CreatedAt:       value.Run.CreatedAt,
		UpdatedAt:       value.Run.UpdatedAt,
	}
	if value.Run.CompletedAt.Valid {
		response.CompletedAt = value.Run.CompletedAt.String
	}
	if value.LatestAttempt != nil {
		attempt := workflowAttemptSummaryDTO(*value.LatestAttempt)
		response.LatestAttempt = &attempt
	}
	if value.CurrentPacket != nil {
		packet := workflowAuditPacketLinkResponse{
			AuditPacketID: value.CurrentPacket.AuditPacketID,
			AuditedCommit: value.CurrentPacket.AuditedCommit,
			PacketSHA256:  value.CurrentPacket.PacketSHA256,
			Status:        value.CurrentPacket.Status,
			StaleReason:   value.CurrentPacket.StaleReason,
			CreatedAt:     value.CurrentPacket.CreatedAt,
			SupersededAt:  value.CurrentPacket.SupersededAt,
		}
		response.CurrentPacket = &packet
	}
	if value.LatestDecision != nil {
		decision := workflowAuditDecisionResponse{
			AuditDecisionID: value.LatestDecision.AuditDecisionID,
			AuditedCommit:   value.LatestDecision.AuditedCommit,
			PacketSHA256:    value.LatestDecision.PacketSHA256,
			Decision:        value.LatestDecision.Decision,
			Rationale:       value.LatestDecision.Rationale,
			CreatedAt:       value.LatestDecision.CreatedAt,
		}
		response.LatestDecision = &decision
	}
	return response
}

func workflowAttemptSummaryDTO(value workflowapp.ExecutionAttemptSummary) workflowAttemptSummaryResponse {
	response := workflowAttemptSummaryResponse{
		AttemptID:               value.AttemptID,
		AttemptNumber:           value.AttemptNumber,
		Adapter:                 value.Adapter,
		Model:                   value.Model,
		Status:                  value.Status,
		CreatedAt:               value.CreatedAt,
		StartedAt:               value.StartedAt,
		FinishedAt:              value.FinishedAt,
		CancellationRequestedAt: value.CancellationRequestedAt,
		Artifacts:               make([]workflowArtifactLinkResponse, 0, len(value.Artifacts)),
	}
	for _, artifact := range value.Artifacts {
		response.Artifacts = append(response.Artifacts, workflowRunArtifactDTO(artifact))
	}
	return response
}

func workflowRunArtifactDTO(value workflowapp.ArtifactMetadata) workflowArtifactLinkResponse {
	return workflowArtifactLinkResponse{
		ArtifactID: value.ArtifactID,
		OwnerType:  value.OwnerType,
		Kind:       value.Kind,
		MediaType:  value.MediaType,
		SHA256:     value.SHA256,
		SizeBytes:  value.SizeBytes,
		CreatedAt:  value.CreatedAt,
		ContentURL: "/api/artifacts/" + value.ArtifactID + "/content",
	}
}

func writeWorkflowReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Run, Plan, or pass was not found")
	case errors.Is(err, workflowapp.ErrInvalidWorkflowRequest):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Run operation failed")
	}
}

func MountWorkflowReadRoutes(r chi.Router, handler *WorkflowReadHandler) {
	r.Get("/runs", handler.List)
	r.Get("/runs/{runID}", handler.Get)
	r.Get("/runs/{runID}/specification", handler.GetSpecification)
}
