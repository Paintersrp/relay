package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"relay/internal/store"
)

// Tool name constant.
const NextAuditWorkTool = "get_next_audit_work"

// Additional blocker code constants.
const (
	BlockerUnknownPass           = "unknown_pass"
	BlockerUnknownRun            = "unknown_run"
	BlockerRunNotInProjectPlan   = "run_not_in_project_plan"
	BlockerRunNotAuditReady      = "run_not_audit_ready"
	BlockerAuditEvidenceMissing  = "audit_evidence_missing"
	BlockerAuditAlreadyFinalized = "audit_already_finalized"
	BlockerNoAuditWork           = "no_audit_work"
)

// NextAuditWorkRequest is the input for GetNextAuditWork.
type NextAuditWorkRequest struct {
	ProjectID string
	PlanID    string
	PassID    string
	RunID     string
}

// NextAuditWorkResponse is the response shape returned by GetNextAuditWork.
type NextAuditWorkResponse struct {
	OK                            bool                          `json:"ok"`
	Tool                          string                        `json:"tool"`
	Project                       *WorkProjectSummary           `json:"project,omitempty"`
	Plan                          *WorkPlanSummary              `json:"plan,omitempty"`
	SelectedPass                  *WorkPassSummary              `json:"selected_pass,omitempty"`
	SelectedRun                   *AuditWorkRunSummary          `json:"selected_run,omitempty"`
	ExecutorResultReferences      []WorkArtifactReference       `json:"executor_result_references,omitempty"`
	ValidationReportReferences    []WorkArtifactReference       `json:"validation_report_references,omitempty"`
	AuditPacketReferences         []WorkArtifactReference       `json:"audit_packet_references,omitempty"`
	DiffEvidenceReferences        []WorkArtifactReference       `json:"diff_evidence_references,omitempty"`
	PriorPassContext              *AuditPriorPassContext        `json:"prior_pass_context,omitempty"`
	AllowedDecisions              []string                      `json:"allowed_decisions,omitempty"`
	SubmitDecisionPayloadGuidance *AuditDecisionPayloadGuidance `json:"submit_decision_payload_guidance,omitempty"`
	Blockers                      []WorkBlocker                 `json:"blockers"`
}

type AuditWorkRunSummary struct {
	RunID          string `json:"run_id"`
	Title          string `json:"title,omitempty"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	ActiveStep     string `json:"active_step"`
	WorkbenchPath  string `json:"workbench_path,omitempty"`
}

type WorkArtifactReference struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Filename   string `json:"filename"`
	ContentURL string `json:"content_url"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at,omitempty"`
}

type AuditPriorPassContext struct {
	PriorPasses []WorkPassSummary `json:"prior_passes"`
}

type AuditDecisionPayloadGuidance struct {
	PrimaryRoute      AuditDecisionRoute   `json:"primary_route"`
	ConvenienceRoutes []AuditDecisionRoute `json:"convenience_routes,omitempty"`
}

type AuditDecisionRoute struct {
	Method           string      `json:"method"`
	Path             string      `json:"path"`
	BodyShape        interface{} `json:"body_shape,omitempty"`
	AllowedDecisions []string    `json:"allowed_decisions,omitempty"`
	Decision         string      `json:"decision,omitempty"`
}

// GetNextAuditWork returns the next eligible audit work packet or structured blockers.
// It is a read-only method on OrchestratorWorkService.
func (svc *OrchestratorWorkService) GetNextAuditWork(ctx context.Context, req NextAuditWorkRequest) (NextAuditWorkResponse, error) {
	// S1: Validate inputs -- trim and reject empty or path-like values.
	projectID := strings.TrimSpace(req.ProjectID)
	planID := strings.TrimSpace(req.PlanID)

	if projectID == "" || planID == "" || isUnsafePath(projectID) || isUnsafePath(planID) {
		return auditBlockerResponse(WorkBlocker{
			Code:        BlockerUnsafeRequest,
			Message:     "project_id and plan_id are required and must be safe identifiers",
			Recoverable: false,
		}), nil
	}

	// S2: Load project.
	project, err := svc.store.GetProjectByProjectID(projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerUnknownProject,
				Message:     fmt.Sprintf("project %q is unknown", projectID),
				Recoverable: false,
			}), nil
		}
		return NextAuditWorkResponse{}, fmt.Errorf("lookup project %q: %w", projectID, err)
	}

	// S3: Load plan.
	plan, err := svc.store.GetPlanByPlanID(planID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerUnknownPlan,
				Message:     fmt.Sprintf("plan %q is unknown", planID),
				Recoverable: false,
			}), nil
		}
		return NextAuditWorkResponse{}, fmt.Errorf("lookup plan %q: %w", planID, err)
	}

	// S4: Verify plan belongs to project.
	if plan.ProjectRowID != project.ID {
		return auditBlockerResponse(WorkBlocker{
			Code:        BlockerProjectPlanMismatch,
			Message:     fmt.Sprintf("plan %q does not belong to project %q", planID, projectID),
			Recoverable: false,
		}), nil
	}

	// S5: Load ordered passes.
	passes, err := svc.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return NextAuditWorkResponse{}, fmt.Errorf("list plan passes for plan %q: %w", planID, err)
	}

	// Index passes by PassID.
	passByID := make(map[string]*store.PlanPass)
	for i := range passes {
		passByID[passes[i].PassID] = &passes[i]
	}

	var selectedPass *store.PlanPass
	var selectedRun *store.Run

	// S6: Check overrides or select automatically.
	if req.RunID != "" {
		// Explicit RunID override.
		runID, err := strconv.ParseInt(strings.TrimSpace(req.RunID), 10, 64)
		if err != nil || runID <= 0 {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerUnsafeRequest,
				Message:     fmt.Sprintf("run_id %q is not a valid positive integer", req.RunID),
				Recoverable: false,
			}), nil
		}

		run, err := svc.store.GetRun(runID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return auditBlockerResponse(WorkBlocker{
					Code:        BlockerUnknownRun,
					Message:     fmt.Sprintf("run %d is unknown", runID),
					Recoverable: false,
				}), nil
			}
			return NextAuditWorkResponse{}, fmt.Errorf("lookup run %d: %w", runID, err)
		}

		// Verify run is associated with this plan.
		if !run.PlanRowID.Valid || run.PlanRowID.Int64 != plan.ID {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerRunNotInProjectPlan,
				Message:     fmt.Sprintf("run %d is not associated with plan %q", runID, planID),
				Recoverable: false,
			}), nil
		}

		// Verify run is associated with a pass under this plan.
		if !run.PlanPassRowID.Valid {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerRunNotInProjectPlan,
				Message:     fmt.Sprintf("run %d is not associated with any pass", runID),
				Recoverable: false,
			}), nil
		}

		pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
		if err != nil {
			return NextAuditWorkResponse{}, fmt.Errorf("lookup run pass %d: %w", run.PlanPassRowID.Int64, err)
		}

		// Verify pass belongs to plan.
		if pass.PlanRowID != plan.ID {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerRunNotInProjectPlan,
				Message:     fmt.Sprintf("run %d associated pass %q does not belong to plan %q", runID, pass.PassID, planID),
				Recoverable: false,
			}), nil
		}

		// If PassID override is also set, verify run pass matches it.
		if req.PassID != "" {
			if pass.PassID != req.PassID {
				return auditBlockerResponse(WorkBlocker{
					Code:        BlockerRunNotInProjectPlan,
					Message:     fmt.Sprintf("run %d associated pass %q does not match requested pass %q", runID, pass.PassID, req.PassID),
					Recoverable: false,
				}), nil
			}
		}

		selectedPass = pass
		selectedRun = run

	} else if req.PassID != "" {
		// Explicit PassID override.
		passID := strings.TrimSpace(req.PassID)
		if passID == "" || isUnsafePath(passID) {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerUnsafeRequest,
				Message:     "pass_id must be a safe identifier",
				Recoverable: false,
			}), nil
		}

		pass, ok := passByID[passID]
		if !ok {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerUnknownPass,
				Message:     fmt.Sprintf("pass %q is unknown in plan %q", passID, planID),
				Recoverable: false,
			}), nil
		}

		// Select latest run for this pass.
		runs, err := svc.store.ListRunsByPlanPass(pass.ID)
		if err != nil {
			return NextAuditWorkResponse{}, fmt.Errorf("list runs for pass %q: %w", passID, err)
		}

		if len(runs) == 0 {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerNoAuditWork,
				Message:     fmt.Sprintf("no runs found for pass %q", passID),
				Recoverable: true,
			}), nil
		}

		// Find the latest run (highest ID).
		var latestRun store.Run
		for i := range runs {
			if latestRun.ID == 0 || runs[i].ID > latestRun.ID {
				latestRun = runs[i]
			}
		}

		selectedPass = pass
		selectedRun = &latestRun

	} else {
		// Automatic selection.
		for i := range passes {
			pass := &passes[i]

			// C7: revision_required must block plan advancement and must not select a later pass.
			if pass.Status == StatusPassRevisionRequired {
				return auditBlockerResponse(WorkBlocker{
					Code:        BlockerRevisionRequiredSamePass,
					Message:     fmt.Sprintf("pass %q (seq %d) requires revision before proceeding", pass.PassID, pass.Sequence),
					Recoverable: true,
				}), nil
			}

			// Load runs for this pass.
			runs, err := svc.store.ListRunsByPlanPass(pass.ID)
			if err != nil {
				return NextAuditWorkResponse{}, fmt.Errorf("list runs for pass %q: %w", pass.PassID, err)
			}

			// Find latest audit-ready run that is not finalized.
			var candidate *store.Run
			for j := range runs {
				r := &runs[j]
				if r.Status == "audit_ready" || r.Status == "audit_ready_for_review" {
					finalized, err := svc.isRunFinalized(r, pass.Status)
					if err != nil {
						return NextAuditWorkResponse{}, err
					}
					if !finalized {
						if candidate == nil || r.ID > candidate.ID {
							candidate = r
						}
					}
				}
			}

			if candidate != nil {
				selectedPass = pass
				selectedRun = candidate
				break
			}
		}

		if selectedRun == nil {
			return auditBlockerResponse(WorkBlocker{
				Code:        BlockerNoAuditWork,
				Message:     fmt.Sprintf("no eligible audit work found for plan %q", planID),
				Recoverable: true,
			}), nil
		}
	}

	// S7: Load artifacts for the selected run.
	artifacts, err := svc.store.ListArtifactsByRun(selectedRun.ID)
	if err != nil {
		return NextAuditWorkResponse{}, fmt.Errorf("list artifacts for run %d: %w", selectedRun.ID, err)
	}

	hasDecision := false
	hasPacket := false
	hasEvidenceManifest := false

	for _, art := range artifacts {
		if art.Kind == "audit_decision_json" {
			hasDecision = true
		} else if art.Kind == "audit_packet" {
			hasPacket = true
		} else if art.Kind == "audit_evidence_manifest_json" {
			hasEvidenceManifest = true
		}
	}

	// S8: Check if run is finalized.
	finalizedStatus := selectedRun.Status == "accepted" ||
		selectedRun.Status == "accepted_with_warnings" ||
		selectedRun.Status == "completed" ||
		selectedPass.Status == "completed" ||
		selectedPass.Status == "skipped" ||
		hasDecision

	if finalizedStatus {
		return auditBlockerResponse(WorkBlocker{
			Code:        BlockerAuditAlreadyFinalized,
			Message:     fmt.Sprintf("audit for run %d is already finalized", selectedRun.ID),
			Recoverable: false,
		}), nil
	}

	// S9: Check if selected run is in audit-ready status.
	if selectedRun.Status != "audit_ready" && selectedRun.Status != "audit_ready_for_review" {
		return auditBlockerResponse(WorkBlocker{
			Code:        BlockerRunNotAuditReady,
			Message:     fmt.Sprintf("run %d status is %q; expected audit_ready or audit_ready_for_review", selectedRun.ID, selectedRun.Status),
			Recoverable: false,
		}), nil
	}

	// S10: Require evidence references.
	if !hasPacket || !hasEvidenceManifest {
		return auditBlockerResponse(WorkBlocker{
			Code:        BlockerAuditEvidenceMissing,
			Message:     fmt.Sprintf("run %d is missing required audit evidence (audit_packet=%t, audit_evidence_manifest_json=%t)", selectedRun.ID, hasPacket, hasEvidenceManifest),
			Recoverable: true,
		}), nil
	}

	// S11: Build successful response.
	var priorPasses []WorkPassSummary
	for i := range passes {
		p := &passes[i]
		if p.Sequence < selectedPass.Sequence {
			priorPasses = append(priorPasses, WorkPassSummary{
				PassID:   p.PassID,
				Sequence: p.Sequence,
				Name:     p.Name,
				Status:   p.Status,
				Goal:     p.Goal,
			})
		}
	}
	if priorPasses == nil {
		priorPasses = []WorkPassSummary{}
	}

	var executorRefs []WorkArtifactReference
	var validationRefs []WorkArtifactReference
	var auditRefs []WorkArtifactReference
	var diffRefs []WorkArtifactReference

	for _, art := range artifacts {
		ref := WorkArtifactReference{
			Kind:       art.Kind,
			Label:      formatArtifactLabel(art.Kind),
			Filename:   filepath.Base(art.Path),
			ContentURL: fmt.Sprintf("/api/runs/%d/artifacts/%s", selectedRun.ID, art.Kind),
			Status:     "ready",
			CreatedAt:  art.CreatedAt,
		}

		if isExecutorResult(art.Kind) {
			executorRefs = append(executorRefs, ref)
		} else if isValidationReport(art.Kind) {
			validationRefs = append(validationRefs, ref)
		} else if isAuditPacket(art.Kind) {
			auditRefs = append(auditRefs, ref)
		} else if isDiffEvidence(art.Kind) {
			diffRefs = append(diffRefs, ref)
		}
	}

	decisions := []string{
		"accepted",
		"accepted_with_warnings",
		"revision_required",
		"blocked",
		"manual_review_required",
	}

	runIDStr := fmt.Sprintf("%d", selectedRun.ID)

	payloadGuidance := &AuditDecisionPayloadGuidance{
		PrimaryRoute: AuditDecisionRoute{
			Method: "POST",
			Path:   fmt.Sprintf("/api/runs/%s/audit/submit", runIDStr),
			BodyShape: map[string]string{
				"audit_packet_markdown": "string required when submitting manual packet text",
				"decision":               "accepted | accepted_with_warnings | revision_required | blocked | manual_review_required",
				"notes":                  "string optional",
			},
		},
		ConvenienceRoutes: []AuditDecisionRoute{
			{
				Method:           "POST",
				Path:             fmt.Sprintf("/api/runs/%s/audit/approve", runIDStr),
				AllowedDecisions: []string{"accepted", "accepted_with_warnings"},
			},
			{
				Method:   "POST",
				Path:     fmt.Sprintf("/api/runs/%s/audit/request-revision", runIDStr),
				Decision: "revision_required",
			},
		},
	}

	return NextAuditWorkResponse{
		OK:   true,
		Tool: NextAuditWorkTool,
		Project: &WorkProjectSummary{
			ProjectID: project.ProjectID,
			Name:      project.Name,
		},
		Plan: &WorkPlanSummary{
			PlanID: plan.PlanID,
			Status: plan.Status,
			Title:  plan.Title,
		},
		SelectedPass: &WorkPassSummary{
			PassID:   selectedPass.PassID,
			Sequence: selectedPass.Sequence,
			Name:     selectedPass.Name,
			Status:   selectedPass.Status,
			Goal:     selectedPass.Goal,
		},
		SelectedRun: &AuditWorkRunSummary{
			RunID:          runIDStr,
			Title:          selectedRun.Title,
			Status:         selectedRun.Status,
			LifecycleState: "audit",
			ActiveStep:     "audit",
			WorkbenchPath:  fmt.Sprintf("/runs/%s/audit", runIDStr),
		},
		ExecutorResultReferences:   executorRefs,
		ValidationReportReferences: validationRefs,
		AuditPacketReferences:      auditRefs,
		DiffEvidenceReferences:     diffRefs,
		PriorPassContext: &AuditPriorPassContext{
			PriorPasses: priorPasses,
		},
		AllowedDecisions:              decisions,
		SubmitDecisionPayloadGuidance: payloadGuidance,
		Blockers:                      []WorkBlocker{},
	}, nil
}

func (svc *OrchestratorWorkService) isRunFinalized(run *store.Run, passStatus string) (bool, error) {
	if run.Status == "accepted" || run.Status == "accepted_with_warnings" || run.Status == "completed" {
		return true, nil
	}
	if passStatus == "completed" || passStatus == "skipped" {
		return true, nil
	}
	artifacts, err := svc.store.ListArtifactsByRun(run.ID)
	if err != nil {
		return false, err
	}
	for _, art := range artifacts {
		if art.Kind == "audit_decision_json" {
			return true, nil
		}
	}
	return false, nil
}

func auditBlockerResponse(b WorkBlocker) NextAuditWorkResponse {
	return NextAuditWorkResponse{
		OK:       false,
		Tool:     NextAuditWorkTool,
		Blockers: []WorkBlocker{b},
	}
}

func formatArtifactLabel(kind string) string {
	words := strings.Split(kind, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func isExecutorResult(kind string) bool {
	switch kind {
	case "agent_result_raw", "agent_result_json", "opencode_stdout", "opencode_stderr", "opencode_combined", "executor_result":
		return true
	default:
		return false
	}
}

func isValidationReport(kind string) bool {
	switch kind {
	case "validation_run_json", "validation_stdout", "validation_stderr", "validation_failure_acceptance_json":
		return true
	default:
		return false
	}
}

func isAuditPacket(kind string) bool {
	switch kind {
	case "audit_input_summary", "audit_evidence_manifest_json", "audit_packet":
		return true
	default:
		return false
	}
}

func isDiffEvidence(kind string) bool {
	switch kind {
	case "git_status", "git_diff_stat", "git_diff_patch", "git_diff_name_status", "audit_patch":
		return true
	default:
		return false
	}
}
