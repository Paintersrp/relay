package api

import (
	"net/http"
	"strings"

	"relay/internal/refactors"
)

// ---------------------------------------------------------------------------
// PASS-004 refactor promotion / generation API handlers.
//
// These endpoints expose two reviewed actions on top of the PASS-003 refactor
// candidate lifecycle: promoting a ready candidate into an existing managed plan
// as a normal refactor pass, and generating reviewable refactor-only Plan of
// Passes artifacts. They never submit plans, create runs, dispatch executors, or
// register MCP tools.
// ---------------------------------------------------------------------------

// RefactorPromoteAPIRequest is the request payload for promoting a candidate.
type RefactorPromoteAPIRequest struct {
	PlanID                string `json:"plan_id"`
	AfterPassID           string `json:"after_pass_id"`
	UseSuggestedPlacement bool   `json:"use_suggested_placement"`
	Note                  string `json:"note"`
}

// RefactorPlacementSuggestionAPIResponse is the placement suggestion response.
type RefactorPlacementSuggestionAPIResponse struct {
	Success     bool                           `json:"success"`
	CandidateID string                         `json:"candidateId"`
	PlanID      string                         `json:"planId"`
	Suggestion  *refactors.PlacementSuggestion `json:"suggestion"`
}

// RefactorPromoteAPIResponse echoes the promotion result.
type RefactorPromoteAPIResponse struct {
	Success             bool                          `json:"success"`
	CandidateID         string                        `json:"candidateId"`
	PlanID              string                        `json:"planId"`
	PassID              string                        `json:"passId"`
	Sequence            int64                         `json:"sequence"`
	CandidateStatus     string                        `json:"candidateStatus"`
	SchedulingReference refactors.SchedulingReference `json:"schedulingReference"`
	Placement           refactors.PromotionPlacement  `json:"placement"`
	Warnings            []string                      `json:"warnings"`
}

// RefactorGeneratePlanAPIRequest is the request payload for generating a
// refactor-only plan.
type RefactorGeneratePlanAPIRequest struct {
	CandidateIDs []string `json:"candidate_ids"`
	Title        string   `json:"title"`
	Note         string   `json:"note"`
}

// RefactorGeneratePlanAPIResponse echoes the generated plan result.
type RefactorGeneratePlanAPIResponse struct {
	Success              bool     `json:"success"`
	ProjectID            string   `json:"projectId"`
	PlanID               string   `json:"planId"`
	CandidateIDs         []string `json:"candidateIds"`
	JSONArtifactPath     string   `json:"jsonArtifactPath"`
	MarkdownArtifactPath string   `json:"markdownArtifactPath"`
	SubmissionPolicy     string   `json:"submissionPolicy"`
	Warnings             []string `json:"warnings"`
}

// GetRefactorCandidatePlacementSuggestion returns a deterministic, advisory
// placement suggestion for a candidate within a plan. It does not mutate state.
func (h *APIHandler) GetRefactorCandidatePlacementSuggestion(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}
	planID := strings.TrimSpace(r.URL.Query().Get("plan_id"))

	suggestion, issues, err := h.refactorService.SuggestCandidatePlacement(r.Context(), projectID, candidateID, planID)
	if !writeRefactorOutcome(w, issues, err, "Failed to compute placement suggestion") {
		return
	}

	writeJSON(w, http.StatusOK, RefactorPlacementSuggestionAPIResponse{
		Success:     true,
		CandidateID: candidateID,
		PlanID:      planID,
		Suggestion:  suggestion,
	})
}

// PromoteRefactorCandidate promotes one ready candidate into an existing
// project-owned active plan as a normal refactor pass.
func (h *APIHandler) PromoteRefactorCandidate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	candidateID, ok := refactorRouteParam(w, r, "candidateId")
	if !ok {
		return
	}
	var req RefactorPromoteAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	result, issues, err := h.refactorService.PromoteCandidateToPlan(r.Context(), refactors.PromoteCandidateInput{
		ProjectID:             projectID,
		CandidateID:           candidateID,
		PlanID:                req.PlanID,
		AfterPassID:           req.AfterPassID,
		UseSuggestedPlacement: req.UseSuggestedPlacement,
		Note:                  req.Note,
	})
	if !writeRefactorOutcome(w, issues, err, "Failed to promote refactor candidate") {
		return
	}

	writeJSON(w, http.StatusOK, RefactorPromoteAPIResponse{
		Success:             true,
		CandidateID:         result.CandidateID,
		PlanID:              result.PlanID,
		PassID:              result.PassID,
		Sequence:            result.Sequence,
		CandidateStatus:     result.CandidateStatus,
		SchedulingReference: result.SchedulingReference,
		Placement:           result.Placement,
		Warnings:            result.Warnings,
	})
}

// GenerateRefactorOnlyPlan generates reviewable refactor-only Plan of Passes
// artifacts from selected ready candidates without submitting the plan.
func (h *APIHandler) GenerateRefactorOnlyPlan(w http.ResponseWriter, r *http.Request) {
	projectID, ok := refactorRouteParam(w, r, "projectId")
	if !ok {
		return
	}
	var req RefactorGeneratePlanAPIRequest
	if !decodeRefactorJSON(w, r, &req) {
		return
	}

	result, issues, err := h.refactorService.GenerateRefactorOnlyPlan(r.Context(), refactors.GenerateRefactorPlanInput{
		ProjectID:    projectID,
		CandidateIDs: req.CandidateIDs,
		Title:        req.Title,
		Note:         req.Note,
	})
	if !writeRefactorOutcome(w, issues, err, "Failed to generate refactor-only plan") {
		return
	}

	writeJSON(w, http.StatusOK, RefactorGeneratePlanAPIResponse{
		Success:              true,
		ProjectID:            result.ProjectID,
		PlanID:               result.PlanID,
		CandidateIDs:         result.CandidateIDs,
		JSONArtifactPath:     result.JSONArtifactPath,
		MarkdownArtifactPath: result.MarkdownArtifactPath,
		SubmissionPolicy:     result.SubmissionPolicy,
		Warnings:             result.Warnings,
	})
}
