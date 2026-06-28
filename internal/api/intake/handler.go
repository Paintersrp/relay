package intake

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	runsapi "relay/internal/api/runs"
	"relay/internal/api/shared"
	appintake "relay/internal/app/intake"
	appruns "relay/internal/app/runs"
)

// Handler is the intake feature HTTP transport adapter.
type Handler struct {
	intake *appintake.Service
	runs   *appruns.Service
}

// NewHandler constructs an intake Handler.
func NewHandler(intake *appintake.Service, runs *appruns.Service) *Handler {
	return &Handler{intake: intake, runs: runs}
}

// POST /api/intake/planner-handoff
func (h *Handler) IntakePlannerHandoff(w http.ResponseWriter, r *http.Request) {
	var req PlannerHandoffIntakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	input := appintake.IntakeInput{
		Repo:                   req.Repo,
		Branch:                 req.Branch,
		HandoffPath:            req.HandoffPath,
		PacketID:               req.PacketID,
		Name:                   req.Name,
		PlannerHandoffMarkdown: req.PlannerHandoffMarkdown,
		RunID:                  req.RunID,
		RepoTarget:             req.RepoTarget,
		BranchContext:          req.BranchContext,
		Source:                 req.Source,
		ExecutorAdapter:        req.ExecutorAdapter,
		ExecutorAdapter2:       req.ExecutorAdapter2,
		ExecutorModelProfile:   req.ExecutorModelProfile,
		ExecutorModelProfile2:  req.ExecutorModelProfile2,
		RecommendedModel:       req.RecommendedModel,
		Model:                  req.Model,
		PlanID:                 req.PlanID,
		PlanIDSnake:            req.PlanIDSnake,
		PassID:                 req.PassID,
		PassIDSnake:            req.PassIDSnake,
		ContextPacketID:        req.ContextPacketID,
		ContextPacketIDSnake:   req.ContextPacketIDSnake,
		SourceSnapshotID:       req.SourceSnapshotID,
		SourceSnapshotIDSnake:  req.SourceSnapshotIDSnake,
	}

	result, err := h.intake.IntakePlannerHandoff(r.Context(), input)
	if err != nil {
		var ie *appintake.Error
		if errors.As(err, &ie) {
			shared.Error(w, ie.HTTPStatus, ie.Code, ie.Message)
			return
		}
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	details, err := h.runs.GetRun(r.Context(), result.RunID)
	if err != nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load created run")
		return
	}

	mapped := runsapi.MapRunToRelayRun(details)
	idStr := fmt.Sprintf("%d", result.RunID)

	shared.JSON(w, http.StatusOK, PlannerHandoffIntakeResponse{
		Success:        true,
		RunID:          idStr,
		RunIDSnake:     idStr,
		Status:         mapped.Status,
		LifecycleState: mapped.LifecycleState,
		CreatedAt:      mapped.CreatedAt,
		ReviewURL:      fmt.Sprintf("/runs/%s/intake", idStr),
		PlanID:         result.PlanID,
		PassID:         result.PassID,
		Artifacts:      runsapi.BuildIntakeArtifacts(idStr, details),
		Validation:     runsapi.BuildValidationResult(details),
	})
}
