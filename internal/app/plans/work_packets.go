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

// Refactor scheduling constants. These mirror the schema-approved refactor
// candidate metadata values and the refactor backlog candidate/schedule-ref
// status strings. They are duplicated here (rather than imported from
// internal/refactors) because internal/refactors already depends on this
// package; importing it back would create an import cycle.
const (
	refactorCandidateSource            = "refactor_backlog_candidate"
	refactorSchedulingExistingPlan     = "existing_plan_bonus_pass"
	refactorSchedulingGeneratedPlan    = "generated_refactor_only_plan"
	refactorScheduleRefStatusScheduled = "scheduled"

	refactorCandidateStatusScheduled                 = "scheduled"
	refactorCandidateStatusScheduledRevisionRequired = "scheduled_revision_required"
	refactorCandidateStatusCompleted                 = "completed"
	refactorCandidateStatusCompletedWithWarnings     = "completed_with_warnings"
	refactorCandidateStatusDeferred                  = "deferred"
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
	PassID            string                         `json:"pass_id"`
	Sequence          int64                          `json:"sequence"`
	Name              string                         `json:"name"`
	Status            string                         `json:"status"`
	Goal              string                         `json:"goal,omitempty"`
	RefactorCandidate *WorkRefactorCandidateMetadata `json:"refactor_candidate,omitempty"`
}

// WorkRefactorCandidateMetadata is the bounded refactor-candidate reference
// exposed for a scheduled refactor pass.
type WorkRefactorCandidateMetadata struct {
	CandidateID            string   `json:"candidate_id"`
	Source                 string   `json:"source"`
	SchedulingMode         string   `json:"scheduling_mode"`
	SourceDiscoveryTaskIDs []string `json:"source_discovery_task_ids,omitempty"`
	CandidateStatus        string   `json:"candidate_status,omitempty"`
	ScheduleRefStatus      string   `json:"schedule_ref_status,omitempty"`
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
func (svc *OrchestratorWorkService) GetNextPassWork(ctx context.Context, req NextPassWorkRequest) (NextPassWorkResponse, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	planID := strings.TrimSpace(req.PlanID)

	if projectID == "" || planID == "" || isUnsafePath(projectID) || isUnsafePath(planID) {
		return blockerResponse(WorkBlocker{
			Code:        BlockerUnsafeRequest,
			Message:     "project_id and plan_id are required and must be safe identifiers",
			Recoverable: false,
		}), nil
	}

	project, err := svc.store.GetProjectByProjectID(projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return blockerResponse(WorkBlocker{
				Code:        string(BlockerUnknownProject),
				Message:     fmt.Sprintf("project %q is unknown", projectID),
				Recoverable: false,
			}), nil
		}
		return NextPassWorkResponse{}, fmt.Errorf("lookup project %q: %w", projectID, err)
	}

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

	if plan.ProjectRowID != project.ID {
		return blockerResponse(WorkBlocker{
			Code:        BlockerProjectPlanMismatch,
			Message:     fmt.Sprintf("plan %q does not belong to project %q", planID, projectID),
			Recoverable: false,
		}), nil
	}

	if plan.Status != "active" {
		return blockerResponse(WorkBlocker{
			Code:        BlockerPlanNotActive,
			Message:     fmt.Sprintf("plan %q has status %q; only active plans are eligible", planID, plan.Status),
			Recoverable: plan.Status != "abandoned",
		}), nil
	}

	passes, err := svc.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return NextPassWorkResponse{}, fmt.Errorf("list plan passes for plan %q: %w", planID, err)
	}

	passByID := make(map[string]*store.PlanPass, len(passes))
	for i := range passes {
		p := &passes[i]
		passByID[p.PassID] = p
	}

	for i := range passes {
		pass := &passes[i]

		switch pass.Status {
		case StatusPassCompleted, StatusPassSkipped:
			continue

		case StatusPassAuditReady:
			return blockerResponse(WorkBlocker{
				Code:        BlockerPriorPassAwaitsAudit,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q and must be audited before selecting a later pass", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassRevisionRequired:
			return blockerResponse(WorkBlocker{
				Code:        BlockerRevisionRequiredSamePass,
				Message:     fmt.Sprintf("pass %q (seq %d) requires revision before proceeding", pass.PassID, pass.Sequence),
				Recoverable: true,
			}), nil

		case StatusPassHandoffReady:
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q and is awaiting handoff submission", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassRunCreated, StatusPassInProgress:
			return blockerResponse(WorkBlocker{
				Code:        BlockerActiveRunExists,
				Message:     fmt.Sprintf("pass %q (seq %d) has an active associated run (status %q)", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: true,
			}), nil

		case StatusPassBlocked:
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) is blocked and prevents selecting a later pass", pass.PassID, pass.Sequence),
				Recoverable: true,
			}), nil

		case StatusPassPlanned, StatusPassReadyForPlanner:
			return svc.evaluateCandidate(ctx, project, plan, pass, passByID)

		default:
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) has unrecognised status %q", pass.PassID, pass.Sequence, pass.Status),
				Recoverable: false,
			}), nil
		}
	}

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
	var depIDs []string
	if err := json.Unmarshal([]byte(pass.DependenciesJson), &depIDs); err != nil {
		return blockerResponse(WorkBlocker{
			Code:        BlockerDependenciesIncomplete,
			Message:     fmt.Sprintf("pass %q dependencies JSON is malformed: %v", pass.PassID, err),
			Recoverable: false,
		}), nil
	}

	var depStatuses []WorkDependencyStatus
	for _, depID := range depIDs {
		dep, ok := passByID[depID]
		if !ok {
			depStatuses = append(depStatuses, WorkDependencyStatus{PassID: depID, Status: "unknown", Satisfied: false})
			return appendDepStatusesToBlocker(blockerResponse(WorkBlocker{
				Code:        BlockerDependenciesIncomplete,
				Message:     fmt.Sprintf("pass %q declares dependency on %q which does not exist in this plan", pass.PassID, depID),
				Recoverable: false,
			}), depStatuses), nil
		}
		satisfied := dep.Status == StatusPassCompleted || dep.Status == StatusPassSkipped
		depStatuses = append(depStatuses, WorkDependencyStatus{PassID: depID, Status: dep.Status, Satisfied: satisfied})
		if !satisfied {
			return appendDepStatusesToBlocker(blockerResponse(WorkBlocker{
				Code:        BlockerDependenciesIncomplete,
				Message:     fmt.Sprintf("pass %q must be %q or %q before pass %q can be selected (current status: %q)", depID, StatusPassCompleted, StatusPassSkipped, pass.PassID, dep.Status),
				Recoverable: true,
			}), depStatuses), nil
		}
	}

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

	var ssReqs SourceSnapshotRequirements
	if pass.SourceSnapshotRequirementsJson != "" && pass.SourceSnapshotRequirementsJson != "{}" {
		_ = json.Unmarshal([]byte(pass.SourceSnapshotRequirementsJson), &ssReqs)
	}

	var ctxPlan ContextPlan
	if pass.ContextPlanJson != "" && pass.ContextPlanJson != "{}" {
		_ = json.Unmarshal([]byte(pass.ContextPlanJson), &ctxPlan)
	}

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

	contextReady := (!requireSnapshot || snapshotID != "") && (!requirePacket || packetID != "")

	var criteria []string
	if pass.HandoffReadinessCriteriaJson != "" && pass.HandoffReadinessCriteriaJson != "[]" {
		_ = json.Unmarshal([]byte(pass.HandoffReadinessCriteriaJson), &criteria)
	}

	if len(depStatuses) == 0 {
		depStatuses = []WorkDependencyStatus{}
	}

	var terminalRunSummaries []WorkRunSummary
	for _, r := range runs {
		if terminalRunStatuses[r.Status] {
			terminalRunSummaries = append(terminalRunSummaries, buildWorkRunSummary(r))
		}
	}
	if terminalRunSummaries == nil {
		terminalRunSummaries = []WorkRunSummary{}
	}

	refMeta, refBlocker, err := validateRefactorSchedule(svc.store, project, plan, pass)
	if err != nil {
		return NextPassWorkResponse{}, err
	}
	if refBlocker != nil {
		return blockerResponse(*refBlocker), nil
	}

	selectedPass := &WorkPassSummary{
		PassID:            pass.PassID,
		Sequence:          pass.Sequence,
		Name:              pass.Name,
		Status:            pass.Status,
		Goal:              pass.Goal,
		RefactorCandidate: refMeta,
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
		SelectedPass:     selectedPass,
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

func blockerResponse(b WorkBlocker) NextPassWorkResponse {
	return NextPassWorkResponse{
		OK:       false,
		Tool:     NextPassWorkTool,
		Blockers: []WorkBlocker{b},
	}
}

func appendDepStatusesToBlocker(resp NextPassWorkResponse, deps []WorkDependencyStatus) NextPassWorkResponse {
	resp.DependencyStatus = deps
	return resp
}

func buildWorkRunSummary(r store.Run) WorkRunSummary {
	return WorkRunSummary{
		RunID:          fmt.Sprintf("%d", r.ID),
		Title:          r.Title,
		Status:         r.Status,
		LifecycleState: resolveRunLifecycleState(r.Status),
		ActiveStep:     resolveRunActiveStep(r.Status),
	}
}

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

func resolveRunActiveStep(status string) string {
	return resolveRunLifecycleState(status)
}

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

func isUnsafePath(s string) bool {
	return strings.Contains(s, "/") ||
		strings.Contains(s, "\\") ||
		strings.Contains(s, "..") ||
		strings.Contains(s, "\x00")
}

func refactorMetadataFromPass(pass *store.PlanPass) (*RefactorCandidateMetadata, error) {
	if pass == nil {
		return nil, nil
	}

	if strings.TrimSpace(pass.RawPassJson) == "" {
		if pass.PassType == "refactor" {
			return nil, fmt.Errorf("refactor pass %q is missing raw refactor metadata", pass.PassID)
		}
		return nil, nil
	}

	var raw PlanPassInput
	if err := json.Unmarshal([]byte(pass.RawPassJson), &raw); err != nil {
		if pass.PassType == "refactor" {
			return nil, fmt.Errorf("refactor pass %q raw_pass_json is malformed: %w", pass.PassID, err)
		}
		return nil, nil
	}

	if raw.RefactorCandidate == nil {
		if pass.PassType == "refactor" || raw.PassType == "refactor" {
			return nil, fmt.Errorf("refactor pass %q is missing refactor_candidate metadata", pass.PassID)
		}
		return nil, nil
	}

	meta := raw.RefactorCandidate
	if strings.TrimSpace(meta.CandidateID) == "" ||
		meta.Source != refactorCandidateSource ||
		(meta.SchedulingMode != refactorSchedulingExistingPlan && meta.SchedulingMode != refactorSchedulingGeneratedPlan) {
		return nil, fmt.Errorf("refactor pass %q has invalid refactor_candidate metadata", pass.PassID)
	}

	return meta, nil
}

func staleRefactorBlocker(reason string) *WorkBlocker {
	return &WorkBlocker{
		Code:        BlockerUnsafeRequest,
		Message:     "stale refactor scheduling reference: " + reason,
		Recoverable: true,
	}
}

func validateRefactorSchedule(st *store.Store, project *store.Project, plan *store.Plan, pass *store.PlanPass) (*WorkRefactorCandidateMetadata, *WorkBlocker, error) {
	meta, err := refactorMetadataFromPass(pass)
	if err != nil {
		return nil, staleRefactorBlocker(err.Error()), nil
	}
	if meta == nil {
		return nil, nil, nil
	}

	candidate, err := st.GetRefactorCandidateByCandidateID(project.ID, meta.CandidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, staleRefactorBlocker(fmt.Sprintf("candidate %q not found in project %q", meta.CandidateID, project.ProjectID)), nil
		}
		return nil, nil, fmt.Errorf("lookup refactor candidate %q: %w", meta.CandidateID, err)
	}

	switch candidate.Status {
	case refactorCandidateStatusScheduled, refactorCandidateStatusScheduledRevisionRequired:
	default:
		return nil, staleRefactorBlocker(fmt.Sprintf("candidate %q status is %q; expected scheduled or scheduled_revision_required", meta.CandidateID, candidate.Status)), nil
	}

	active, err := st.GetActiveRefactorCandidateScheduleRef(project.ID, candidate.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("lookup active schedule ref for candidate %q: %w", meta.CandidateID, err)
	}
	if active == nil {
		return nil, staleRefactorBlocker(fmt.Sprintf("candidate %q has no active schedule reference", meta.CandidateID)), nil
	}
	if active.Status != refactorScheduleRefStatusScheduled {
		return nil, staleRefactorBlocker(fmt.Sprintf("schedule reference for candidate %q has status %q; expected scheduled", meta.CandidateID, active.Status)), nil
	}
	if active.PlanID != plan.PlanID {
		return nil, staleRefactorBlocker(fmt.Sprintf("schedule reference plan %q does not match selected plan %q", active.PlanID, plan.PlanID)), nil
	}
	if active.PassID != pass.PassID {
		return nil, staleRefactorBlocker(fmt.Sprintf("schedule reference pass %q does not match selected pass %q", active.PassID, pass.PassID)), nil
	}
	if active.PlanRowID.Valid && active.PlanRowID.Int64 != plan.ID {
		return nil, staleRefactorBlocker(fmt.Sprintf("schedule reference plan row %d does not match selected plan row %d", active.PlanRowID.Int64, plan.ID)), nil
	}
	if active.PlanPassRowID.Valid && active.PlanPassRowID.Int64 != pass.ID {
		return nil, staleRefactorBlocker(fmt.Sprintf("schedule reference pass row %d does not match selected pass row %d", active.PlanPassRowID.Int64, pass.ID)), nil
	}

	return &WorkRefactorCandidateMetadata{
		CandidateID:            meta.CandidateID,
		Source:                 meta.Source,
		SchedulingMode:         meta.SchedulingMode,
		SourceDiscoveryTaskIDs: meta.SourceDiscoveryTaskIDs,
		CandidateStatus:        candidate.Status,
		ScheduleRefStatus:      active.Status,
	}, nil, nil
}
