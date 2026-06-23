package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"relay/internal/store"
)

// Tool name constant.
const NextPassWorkTool = "get_next_pass_work"

// Blocker code constants -- all codes defined in the orchestrator work contract.
const (
	BlockerUnknownProject               = "unknown_project"
	BlockerUnknownPlan                  = "unknown_plan"
	BlockerProjectPlanMismatch          = "project_plan_mismatch"
	BlockerPlanNotActive                = "plan_not_active"
	BlockerDependenciesIncomplete       = "dependencies_incomplete"
	BlockerPriorPassAwaitsAudit         = "prior_pass_awaits_audit"
	BlockerActiveRunExists              = "active_run_exists"
	BlockerRequiredSourceContextMissing = "required_source_context_missing"
	BlockerRequiredContextPacketMissing = "required_context_packet_missing"
	BlockerRevisionRequiredSamePass     = "revision_required_same_pass"
	BlockerNoEligiblePass               = "no_eligible_pass"
	BlockerUnsafeRequest                = "unsafe_request"
)

// terminalRunStatuses are run states that do not block pass selection.
var terminalRunStatuses = map[string]bool{
	"accepted":               true,
	"accepted_with_warnings": true,
	"completed":              true,
	"closed":                 true,
}

// ----------------------------------------------------------------------------
// Response types -- snake_case JSON tags per orchestrator work contract.
// ----------------------------------------------------------------------------

// WorkBlocker describes a single business-state blocker.
type WorkBlocker struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

// NextPassWorkResponse is the top-level contract response.
type NextPassWorkResponse struct {
	OK                       bool                    `json:"ok"`
	Tool                     string                  `json:"tool"`
	Project                  *WorkProjectSummary     `json:"project,omitempty"`
	Plan                     *WorkPlanSummary        `json:"plan,omitempty"`
	SelectedPass             *WorkPassSummary        `json:"selected_pass,omitempty"`
	DependencyStatus         []WorkDependencyStatus  `json:"dependency_status,omitempty"`
	AssociatedRuns           []WorkRunSummary        `json:"associated_runs,omitempty"`
	Context                  *WorkContextSummary     `json:"context,omitempty"`
	HandoffReadinessCriteria []string                `json:"handoff_readiness_criteria,omitempty"`
	SuggestedRunSubmission   *SuggestedRunSubmission `json:"suggested_run_submission,omitempty"`
	Blockers                 []WorkBlocker           `json:"blockers"`
}

// WorkProjectSummary contains bounded project metadata.
type WorkProjectSummary struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

// WorkPlanSummary contains bounded plan metadata.
type WorkPlanSummary struct {
	PlanID string `json:"plan_id"`
	Status string `json:"status"`
	Title  string `json:"title,omitempty"`
}

// WorkPassSummary contains bounded pass metadata.
type WorkPassSummary struct {
	PassID   string `json:"pass_id"`
	Sequence int64  `json:"sequence"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Goal     string `json:"goal,omitempty"`
}

// WorkDependencyStatus describes one dependency pass's readiness.
type WorkDependencyStatus struct {
	PassID    string `json:"pass_id"`
	Status    string `json:"status"`
	Satisfied bool   `json:"satisfied"`
}

// WorkRunSummary contains bounded run metadata; no logs or full content.
type WorkRunSummary struct {
	RunID          string `json:"run_id"`
	Title          string `json:"title,omitempty"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycle_state"`
	ActiveStep     string `json:"active_step"`
}

// WorkContextSummary contains IDs and readiness flags; never raw file contents.
type WorkContextSummary struct {
	ContextPlan          ContextPlan `json:"context_plan"`
	SourceSnapshotID     string      `json:"source_snapshot_id,omitempty"`
	SourceSnapshotStatus string      `json:"source_snapshot_status,omitempty"`
	ContextPacketID      string      `json:"context_packet_id,omitempty"`
	ContextPacketStatus  string      `json:"context_packet_status,omitempty"`
	CoverageReportPath   string      `json:"coverage_report_path,omitempty"`
	ContextReady         bool        `json:"context_ready"`
}

// SuggestedRunSubmission contains only tool name and plan/pass IDs.
// It must not include handoff Markdown, source contents, or audit fields.
type SuggestedRunSubmission struct {
	Tool      string                `json:"tool"`
	Arguments SuggestedRunArguments `json:"arguments"`
}

// SuggestedRunArguments contains only plan_id and pass_id.
type SuggestedRunArguments struct {
	PlanID string `json:"plan_id"`
	PassID string `json:"pass_id"`
}

// ----------------------------------------------------------------------------
// Service
// ----------------------------------------------------------------------------

// OrchestratorWorkService resolves the next eligible Planner work packet
// for a project-scoped managed plan. All operations are read-only.
type OrchestratorWorkService struct {
	store *store.Store
}

// NewOrchestratorWorkService constructs an OrchestratorWorkService.
func NewOrchestratorWorkService(s *store.Store) *OrchestratorWorkService {
	return &OrchestratorWorkService{store: s}
}

// NextPassWorkRequest is the input for GetNextPassWork.
type NextPassWorkRequest struct {
	ProjectID string
	PlanID    string
}

// GetNextPassWork returns the next eligible Planner work packet or structured blockers.
// It is read-only: it never creates runs, submits handoffs, creates source snapshots,
// creates context packets, mutates git, or invokes MCP tools.
func (svc *OrchestratorWorkService) GetNextPassWork(ctx context.Context, req NextPassWorkRequest) (NextPassWorkResponse, error) {
	// S1: Validate inputs -- trim and reject empty or path-like values.
	projectID := strings.TrimSpace(req.ProjectID)
	planID := strings.TrimSpace(req.PlanID)

	if projectID == "" || planID == "" || isUnsafePath(projectID) || isUnsafePath(planID) {
		return blockerResponse(WorkBlocker{
			Code:        BlockerUnsafeRequest,
			Message:     "project_id and plan_id are required and must be safe identifiers",
			Recoverable: false,
		}), nil
	}

	// S2: Load project.
	project, err := svc.store.GetProjectByProjectID(projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return blockerResponse(WorkBlocker{
				Code:        BlockerUnknownProject,
				Message:     fmt.Sprintf("project %q is unknown", projectID),
				Recoverable: false,
			}), nil
		}
		return NextPassWorkResponse{}, fmt.Errorf("lookup project %q: %w", projectID, err)
	}

	// S3: Load plan.
	plan, err := svc.store.GetPlanByPlanID(planID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return blockerResponse(WorkBlocker{
				Code:        BlockerUnknownPlan,
				Message:     fmt.Sprintf("plan %q is unknown", planID),
				Recoverable: false,
			}), nil
		}
		return NextPassWorkResponse{}, fmt.Errorf("lookup plan %q: %w", planID, err)
	}

	// S4: Verify plan belongs to this project.
	if plan.ProjectRowID != project.ID {
		return blockerResponse(WorkBlocker{
			Code:        BlockerProjectPlanMismatch,
			Message:     fmt.Sprintf("plan %q does not belong to project %q", planID, projectID),
			Recoverable: false,
		}), nil
	}

	// S5: Verify plan is active.
	if plan.Status != "active" {
		return blockerResponse(WorkBlocker{
			Code:        BlockerPlanNotActive,
			Message:     fmt.Sprintf("plan %q has status %q; only active plans are eligible", planID, plan.Status),
			Recoverable: plan.Status != "abandoned",
		}), nil
	}

	// S6: Load ordered passes (sequence ASC -- ListPlanPassesByPlan returns them ordered).
	passes, err := svc.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return NextPassWorkResponse{}, fmt.Errorf("list plan passes for plan %q: %w", planID, err)
	}

	// Build a pass-by-ID index for dependency resolution.
	passByID := make(map[string]*store.PlanPass, len(passes))
	for i := range passes {
		p := &passes[i]
		passByID[p.PassID] = p
	}

	// S7: Walk passes in sequence order, find the first unresolved pass.
	for i := range passes {
		pass := &passes[i]

		switch pass.Status {
		case StatusPassCompleted, StatusPassSkipped:
			// Terminal -- dependency-satisfying; skip.
			continue

		case StatusPassAuditReady:
			// An earlier pass is awaiting audit; block advancement.
			return blockerResponse(WorkBlocker{
				Code:        BlockerPriorPassAwaitsAudit,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q and must be audited before selecting a later pass", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassRevisionRequired:
			// Revision required; do not advance.
			return blockerResponse(WorkBlocker{
				Code:        BlockerRevisionRequiredSamePass,
				Message:     fmt.Sprintf("pass %q (seq %d) requires revision before proceeding", pass.PassID, pass.Sequence),
				Recoverable: true,
			}), nil

		case StatusPassHandoffReady:
			// A handoff exists but is awaiting submission -- not Planner-selectable.
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q and is awaiting handoff submission", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassRunCreated, StatusPassInProgress:
			// An active run exists for this pass.
			return blockerResponse(WorkBlocker{
				Code:        BlockerActiveRunExists,
				Message:     fmt.Sprintf("pass %q (seq %d) has an active associated run (status %q)", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassBlocked:
			// This pass is blocked; continue scanning.
			continue

		case StatusPassPlanned, StatusPassReadyForPlanner:
			// Candidate -- check dependencies, runs, and context.
			return svc.evaluateCandidate(ctx, project, plan, pass, passByID)

		default:
			// Unknown status -- treat as no_eligible_pass to be safe.
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) has unrecognised status %q", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: false,
			}), nil
		}
	}

	// No eligible pass found.
	return blockerResponse(WorkBlocker{
		Code:        BlockerNoEligiblePass,
		Message:     fmt.Sprintf("no eligible pass found for plan %q; all passes may be completed, skipped, or blocked", planID),
		Recoverable: false,
	}), nil
}

// evaluateCandidate checks dependency satisfaction, active runs, and context
// readiness for a candidate pass with status planned or ready_for_planner.
func (svc *OrchestratorWorkService) evaluateCandidate(
	ctx context.Context,
	project *store.Project,
	plan *store.Plan,
	pass *store.PlanPass,
	passByID map[string]*store.PlanPass,
) (NextPassWorkResponse, error) {
	// Parse dependency list from JSON.
	var depIDs []string
	if err := json.Unmarshal([]byte(pass.DependenciesJson), &depIDs); err != nil {
		// Treat unparseable dependencies as incomplete (fail-closed).
		return blockerResponse(WorkBlocker{
			Code:        BlockerDependenciesIncomplete,
			Message:     fmt.Sprintf("pass %q dependencies JSON is malformed: %v", pass.PassID, err),
			Recoverable: false,
		}), nil
	}

	// Check each declared dependency.
	var depStatuses []WorkDependencyStatus
	for _, depID := range depIDs {
		dep, ok := passByID[depID]
		if !ok {
			// Dependency not found in plan -- treat as incomplete.
			depStatuses = append(depStatuses, WorkDependencyStatus{
				PassID:    depID,
				Status:    "unknown",
				Satisfied: false,
			})
			return appendDepStatusesToBlocker(
				blockerResponse(WorkBlocker{
					Code:        BlockerDependenciesIncomplete,
					Message:     fmt.Sprintf("pass %q declares dependency on %q which does not exist in this plan", pass.PassID, depID),
					Recoverable: false,
				}),
				depStatuses,
			), nil
		}
		satisfied := dep.Status == StatusPassCompleted || dep.Status == StatusPassSkipped
		depStatuses = append(depStatuses, WorkDependencyStatus{
			PassID:    depID,
			Status:    dep.Status,
			Satisfied: satisfied,
		})
		if !satisfied {
			return appendDepStatusesToBlocker(
				blockerResponse(WorkBlocker{
					Code:        BlockerDependenciesIncomplete,
					Message:     fmt.Sprintf("pass %q must be %q or %q before pass %q can be selected (current status: %q)", depID, StatusPassCompleted, StatusPassSkipped, pass.PassID, dep.Status),
					Recoverable: true,
				}),
				depStatuses,
			), nil
		}
	}

	// Check for active associated runs on the candidate pass.
	runs, err := svc.store.ListRunsByPlanPass(pass.ID)
	if err != nil {
		return NextPassWorkResponse{}, fmt.Errorf("list runs for pass %q: %w", pass.PassID, err)
	}

	var activeRuns []WorkRunSummary
	for _, r := range runs {
		if !terminalRunStatuses[r.Status] {
			activeRuns = append(activeRuns, buildWorkRunSummary(r))
		}
	}
	if len(activeRuns) > 0 {
		resp := blockerResponse(WorkBlocker{
			Code:        BlockerActiveRunExists,
			Message:     fmt.Sprintf("pass %q has %d active associated run(s)", pass.PassID, len(activeRuns)),
			Recoverable: true,
		})
		resp.AssociatedRuns = activeRuns
		return resp, nil
	}

	// Parse source snapshot requirements.
	var ssReqs SourceSnapshotRequirements
	if pass.SourceSnapshotRequirementsJson != "" && pass.SourceSnapshotRequirementsJson != "{}" {
		_ = json.Unmarshal([]byte(pass.SourceSnapshotRequirementsJson), &ssReqs)
	}

	// Parse context plan.
	var ctxPlan ContextPlan
	if pass.ContextPlanJson != "" && pass.ContextPlanJson != "{}" {
		_ = json.Unmarshal([]byte(pass.ContextPlanJson), &ctxPlan)
	}

	// Check source snapshot requirement.
	requireSnapshot := (ssReqs.RequireGitStatus != nil && *ssReqs.RequireGitStatus) ||
		(ssReqs.RequireCommitSHA != nil && *ssReqs.RequireCommitSHA)

	var snapshotID string
	var snapshotStatus string
	if requireSnapshot {
		snapshot, err := svc.store.GetLatestSourceSnapshotForProject(project.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return blockerResponse(WorkBlocker{
					Code:        BlockerRequiredSourceContextMissing,
					Message:     fmt.Sprintf("pass %q requires a source snapshot (require_git_status or require_commit_sha) but none exists for project %q", pass.PassID, project.ProjectID),
					Recoverable: true,
				}), nil
			}
			return NextPassWorkResponse{}, fmt.Errorf("get latest source snapshot for project %q: %w", project.ProjectID, err)
		}
		snapshotID = snapshot.SourceSnapshotID
		snapshotStatus = snapshot.Status
	}

	// Check context packet requirement.
	requirePacket := hasRequiredContextInputs(ctxPlan)

	var packetID string
	var packetStatus string
	var coverageReportPath string
	if requirePacket {
		packet, err := svc.store.GetLatestContextPacketForPass(project.ProjectID, plan.PlanID, pass.PassID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return blockerResponse(WorkBlocker{
					Code:        BlockerRequiredContextPacketMissing,
					Message:     fmt.Sprintf("pass %q has required context inputs but no context packet exists for project=%q plan=%q pass=%q", pass.PassID, project.ProjectID, plan.PlanID, pass.PassID),
					Recoverable: true,
				}), nil
			}
			return NextPassWorkResponse{}, fmt.Errorf("get latest context packet for pass %q: %w", pass.PassID, err)
		}
		packetID = packet.ContextPacketID
		packetStatus = packet.Status
		coverageReportPath = packet.CoverageReportPath
	}

	// All checks passed -- build success response.
	contextReady := (!requireSnapshot || snapshotID != "") && (!requirePacket || packetID != "")

	// Parse handoff readiness criteria.
	var criteria []string
	if pass.HandoffReadinessCriteriaJson != "" && pass.HandoffReadinessCriteriaJson != "[]" {
		_ = json.Unmarshal([]byte(pass.HandoffReadinessCriteriaJson), &criteria)
	}

	if len(depStatuses) == 0 {
		depStatuses = []WorkDependencyStatus{}
	}

	// Build terminal run summaries for display.
	var terminalRunSummaries []WorkRunSummary
	for _, r := range runs {
		if terminalRunStatuses[r.Status] {
			terminalRunSummaries = append(terminalRunSummaries, buildWorkRunSummary(r))
		}
	}
	if terminalRunSummaries == nil {
		terminalRunSummaries = []WorkRunSummary{}
	}

	return NextPassWorkResponse{
		OK:   true,
		Tool: NextPassWorkTool,
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
			PassID:   pass.PassID,
			Sequence: pass.Sequence,
			Name:     pass.Name,
			Status:   pass.Status,
			Goal:     pass.Goal,
		},
		DependencyStatus: depStatuses,
		AssociatedRuns:   terminalRunSummaries,
		Context: &WorkContextSummary{
			ContextPlan:          ctxPlan,
			SourceSnapshotID:     snapshotID,
			SourceSnapshotStatus: snapshotStatus,
			ContextPacketID:      packetID,
			ContextPacketStatus:  packetStatus,
			CoverageReportPath:   coverageReportPath,
			ContextReady:         contextReady,
		},
		HandoffReadinessCriteria: criteria,
		SuggestedRunSubmission: &SuggestedRunSubmission{
			Tool: "create_run_from_planner_handoff",
			Arguments: SuggestedRunArguments{
				PlanID: plan.PlanID,
				PassID: pass.PassID,
			},
		},
		Blockers: []WorkBlocker{},
	}, nil
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// blockerResponse builds a failed NextPassWorkResponse with a single blocker.
func blockerResponse(b WorkBlocker) NextPassWorkResponse {
	return NextPassWorkResponse{
		OK:       false,
		Tool:     NextPassWorkTool,
		Blockers: []WorkBlocker{b},
	}
}

// appendDepStatusesToBlocker attaches dependency status to a blocker response.
func appendDepStatusesToBlocker(resp NextPassWorkResponse, deps []WorkDependencyStatus) NextPassWorkResponse {
	resp.DependencyStatus = deps
	return resp
}

// buildWorkRunSummary maps a store.Run to a bounded WorkRunSummary.
func buildWorkRunSummary(r store.Run) WorkRunSummary {
	return WorkRunSummary{
		RunID:          fmt.Sprintf("%d", r.ID),
		Title:          r.Title,
		Status:         r.Status,
		LifecycleState: resolveRunLifecycleState(r.Status),
		ActiveStep:     resolveRunActiveStep(r.Status),
	}
}

// resolveRunLifecycleState maps a run status to a lifecycle phase label.
func resolveRunLifecycleState(status string) string {
	switch status {
	case "accepted", "accepted_with_warnings", "completed", "closed":
		return "completed"
	case "audit_ready", "audit_ready_for_review", "audit_generated", "audit_submitted",
		"audit_approved", "audit_approved_with_warnings", "audit_revision_requested", "audit_closed",
		"revision_required":
		return "audit"
	case "executor_dispatched", "executor_running", "executor_done", "executor_blocked",
		"executor_error", "executor_cancelled", "agent_done", "agent_blocked",
		"agent_result_needs_review", "local_validation_running", "approved_for_executor":
		return "execute"
	case "approved_for_prepare", "packet_ready", "packet_validated", "packet_validation_failed",
		"brief_ready_for_review", "brief_validation_failed", "repair_validated":
		return "prepare"
	default:
		return "intake"
	}
}

// resolveRunActiveStep maps a run status to an active step label.
func resolveRunActiveStep(status string) string {
	return resolveRunLifecycleState(status)
}

// hasRequiredContextInputs returns true when the context plan declares at least
// one required seed file or required search term.
func hasRequiredContextInputs(cp ContextPlan) bool {
	for _, f := range cp.SeedFilesToRead {
		if f.Required != nil && *f.Required {
			return true
		}
	}
	for _, s := range cp.SeedSearchTerms {
		if s.Required != nil && *s.Required {
			return true
		}
	}
	return false
}

// isUnsafePath returns true when the value contains path traversal sequences
// or path separators that would be unsafe in an identifier context.
func isUnsafePath(s string) bool {
	return strings.Contains(s, "/") ||
		strings.Contains(s, "\\") ||
		strings.Contains(s, "..") ||
		strings.Contains(s, "\x00")
}