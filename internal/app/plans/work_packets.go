package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
	BlockerRequestedPassNotFound        = "requested_pass_not_found"
	BlockerRequestedPassNotEligible     = "requested_pass_not_eligible"
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
	HandoffWork              *HandoffAuthoringPacket `json:"handoff_work,omitempty"`
	HandoffAuthoringPacket   *HandoffAuthoringPacket `json:"handoff_authoring_packet,omitempty"`
	SuggestedRunSubmission   *SuggestedRunSubmission `json:"suggested_run_submission,omitempty"`
	PlannerJumpstart         *PlannerJumpstart       `json:"planner_jumpstart,omitempty"`
	Blockers                 []WorkBlocker           `json:"blockers"`
}

// NextPassWorkMCPSummary is the compact model-visible summary returned by MCP.
// It deliberately omits verbose pass goals, context prose, and seed details
// while keeping enough IDs and next-action references for follow-up tool calls.
type NextPassWorkMCPSummary struct {
	OK               bool                         `json:"ok"`
	Tool             string                       `json:"tool"`
	ProjectID        string                       `json:"project_id,omitempty"`
	PlanID           string                       `json:"plan_id,omitempty"`
	SelectedPass     *NextPassWorkSummaryPass     `json:"selected_pass,omitempty"`
	ReadinessState   string                       `json:"readiness_state,omitempty"`
	SourceSnapshotID string                       `json:"source_snapshot_id,omitempty"`
	ContextPacketID  string                       `json:"context_packet_id,omitempty"`
	ContextReady     bool                         `json:"context_ready"`
	HandoffWork      *HandoffAuthoringPacket      `json:"handoff_work,omitempty"`
	HandoffPacket    *HandoffAuthoringPacket      `json:"handoff_authoring_packet,omitempty"`
	Blockers         []NextPassWorkSummaryBlocker `json:"blockers"`
	NextActions      []NextPassWorkSummaryAction  `json:"next_actions,omitempty"`
	LocalPreviewHint string                       `json:"local_preview_hint"`
}

// NextPassWorkSummaryPass contains the selected pass fields safe for MCP text.
type NextPassWorkSummaryPass struct {
	PassID   string `json:"pass_id"`
	Sequence int64  `json:"sequence"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

// NextPassWorkSummaryBlocker contains compact blocker facts.
type NextPassWorkSummaryBlocker struct {
	Code        string `json:"code"`
	Recoverable bool   `json:"recoverable"`
}

// NextPassWorkSummaryAction describes concise follow-up guidance.
type NextPassWorkSummaryAction struct {
	Tool             string                 `json:"tool,omitempty"`
	Description      string                 `json:"description"`
	Arguments        map[string]interface{} `json:"arguments,omitempty"`
	DependsOn        string                 `json:"depends_on,omitempty"`
	ArgumentBindings map[string]string      `json:"argument_bindings,omitempty"`
}

// CompactNextPassWorkSummary returns the MCP-safe projection of the full local
// NextPassWorkResponse.
func CompactNextPassWorkSummary(resp NextPassWorkResponse) NextPassWorkMCPSummary {
	summary := NextPassWorkMCPSummary{
		OK:               resp.OK,
		Tool:             resp.Tool,
		Blockers:         []NextPassWorkSummaryBlocker{},
		LocalPreviewHint: "Use the Relay pass-detail preview for exact raw payload inspection.",
	}
	if resp.Project != nil {
		summary.ProjectID = resp.Project.ProjectID
	}
	if resp.Plan != nil {
		summary.PlanID = resp.Plan.PlanID
	}
	if resp.SelectedPass != nil {
		summary.SelectedPass = &NextPassWorkSummaryPass{
			PassID:   resp.SelectedPass.PassID,
			Sequence: resp.SelectedPass.Sequence,
			Name:     resp.SelectedPass.Name,
			Status:   resp.SelectedPass.Status,
		}
	}
	if resp.PlannerJumpstart != nil {
		summary.ReadinessState = resp.PlannerJumpstart.ReadinessState
	}
	if resp.Context != nil {
		summary.SourceSnapshotID = resp.Context.SourceSnapshotID
		summary.ContextPacketID = resp.Context.ContextPacketID
		summary.ContextReady = resp.Context.ContextReady
	}
	if resp.HandoffWork != nil {
		summary.HandoffWork = resp.HandoffWork
		summary.HandoffPacket = resp.HandoffWork
	} else if resp.HandoffAuthoringPacket != nil {
		summary.HandoffWork = resp.HandoffAuthoringPacket
		summary.HandoffPacket = resp.HandoffAuthoringPacket
	}
	for _, blocker := range resp.Blockers {
		summary.Blockers = append(summary.Blockers, NextPassWorkSummaryBlocker{
			Code:        blocker.Code,
			Recoverable: blocker.Recoverable,
		})
	}
	summary.NextActions = compactNextPassWorkActions(resp)
	return summary
}

func compactNextPassWorkActions(resp NextPassWorkResponse) []NextPassWorkSummaryAction {
	actions := []NextPassWorkSummaryAction{}
	if resp.HandoffWork != nil || resp.HandoffAuthoringPacket != nil {
		packet := resp.HandoffWork
		if packet == nil {
			packet = resp.HandoffAuthoringPacket
		}
		args := map[string]interface{}{
			"project_id": packet.ProjectID,
			"plan_id":    packet.PlanID,
			"pass_id":    packet.PassID,
		}
		if packet.SourceSnapshotID != "" {
			args["source_snapshot_id"] = packet.SourceSnapshotID
		}
		if packet.ContextPacketID != "" {
			args["context_packet_id"] = packet.ContextPacketID
		}
		actions = append(actions, NextPassWorkSummaryAction{
			Tool:        "draft_planner_handoff",
			Description: "Draft the Planner handoff from structuredContent.handoff_work; submit only after user review.",
			Arguments:   args,
		})
	}
	if resp.SuggestedRunSubmission != nil {
		actions = append(actions, NextPassWorkSummaryAction{
			Tool:        resp.SuggestedRunSubmission.Tool,
			Description: "Selected pass is ready for a Planner handoff run.",
			Arguments: map[string]interface{}{
				"plan_id": resp.SuggestedRunSubmission.Arguments.PlanID,
				"pass_id": resp.SuggestedRunSubmission.Arguments.PassID,
			},
		})
	}
	if resp.PlannerJumpstart != nil {
		for _, action := range resp.PlannerJumpstart.SuggestedContextAcquisitionActions {
			switch action.Tool {
			case "create_context_packet":
				actions = append(actions, NextPassWorkSummaryAction{
					Tool:             action.Tool,
					Description:      "Create the required context packet for the selected pass.",
					Arguments:        cloneActionArguments(action.Arguments),
					DependsOn:        action.DependsOn,
					ArgumentBindings: cloneStringMap(action.ArgumentBindings),
				})
			case "create_source_snapshot":
				actions = append(actions, NextPassWorkSummaryAction{
					Tool:        action.Tool,
					Description: "Create or select a source snapshot before context packet creation.",
					Arguments:   cloneActionArguments(action.Arguments),
				})
			default:
				actions = append(actions, NextPassWorkSummaryAction{
					Tool:        action.Tool,
					Description: "Use the local pass-detail preview for exact action arguments.",
					Arguments:   cloneActionArguments(action.Arguments),
				})
			}
		}
	}
	if len(actions) == 0 && len(resp.Blockers) > 0 {
		actions = append(actions, NextPassWorkSummaryAction{
			Description: "Resolve recoverable blockers, then call get_next_pass_work again.",
		})
	}
	return actions
}

func cloneActionArguments(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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

// PlannerJumpstart is the deterministic Planner jumpstart guidance payload
// returned for selected passes. No raw file contents are included.
type PlannerJumpstart struct {
	ReadinessState                     string                       `json:"readiness_state"`
	SelectedPassSummary                *WorkPassSummary             `json:"selected_pass_summary"`
	SourceRequirements                 *SourceSnapshotRequirements  `json:"source_requirements,omitempty"`
	ContextRequirements                ContextPlan                  `json:"context_requirements,omitempty"`
	SourceBasisReport                  *PlannerJumpstartBasisReport `json:"source_basis_report,omitempty"`
	SuggestedContextAcquisitionActions []ContextAcquisitionAction   `json:"suggested_context_acquisition_actions,omitempty"`
	HandoffPreflightChecklist          []string                     `json:"handoff_preflight_checklist,omitempty"`
}

// PlannerJumpstartBasisReport summarizes the current source snapshot and
// context packet readiness without exposing raw file contents.
type PlannerJumpstartBasisReport struct {
	SnapshotID     string `json:"snapshot_id,omitempty"`
	SnapshotStatus string `json:"snapshot_status,omitempty"`
	PacketID       string `json:"packet_id,omitempty"`
	PacketStatus   string `json:"packet_status,omitempty"`
}

// ContextAcquisitionAction describes a safe suggested MCP tool call for
// acquiring required context or source data.
type ContextAcquisitionAction struct {
	Tool             string                 `json:"tool"`
	Arguments        map[string]interface{} `json:"arguments"`
	DependsOn        string                 `json:"depends_on,omitempty"`
	ArgumentBindings map[string]string      `json:"argument_bindings,omitempty"`
}

// HandoffAuthoringPacket is the bounded, model-visible packet needed to draft
// a Planner handoff. It includes contract facts and artifact IDs, never raw
// source/context file contents.
type HandoffAuthoringPacket struct {
	ProjectID                string                           `json:"project_id"`
	PlanID                   string                           `json:"plan_id"`
	PlanTitle                string                           `json:"plan_title,omitempty"`
	PassID                   string                           `json:"pass_id"`
	PassSequence             int64                            `json:"pass_sequence"`
	PassName                 string                           `json:"pass_name"`
	PassStatus               string                           `json:"pass_status"`
	PassGoal                 string                           `json:"pass_goal,omitempty"`
	RefactorCandidate        *WorkRefactorCandidateMetadata   `json:"refactor_candidate,omitempty"`
	SourceSnapshotID         string                           `json:"source_snapshot_id,omitempty"`
	SourceSnapshotStatus     string                           `json:"source_snapshot_status,omitempty"`
	ContextPacketID          string                           `json:"context_packet_id,omitempty"`
	ContextPacketStatus      string                           `json:"context_packet_status,omitempty"`
	CoverageReportPath       string                           `json:"coverage_report_path,omitempty"`
	ContextReady             bool                             `json:"context_ready"`
	ContextPlan              ContextPlan                      `json:"context_plan"`
	ContextBudget            *ContextBudget                   `json:"context_budget,omitempty"`
	SourceRequirements       SourceSnapshotRequirements       `json:"source_snapshot_requirements"`
	HandoffReadinessCriteria []string                         `json:"handoff_readiness_criteria"`
	ReadinessCriteria        []string                         `json:"readiness_criteria"`
	ReadinessChecks          []HandoffAuthoringReadinessCheck `json:"readiness_checks"`
	ContextCoverageExpected  []string                         `json:"context_coverage_expectations,omitempty"`
	BlockedIfMissing         []string                         `json:"blocked_if_missing,omitempty"`
	SuggestedAuthoringAction string                           `json:"suggested_authoring_action"`
	SubmissionPrerequisites  []string                         `json:"submission_prerequisites"`
}

// HandoffAuthoringReadinessCheck is a compact fact used while drafting.
type HandoffAuthoringReadinessCheck struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

const (
	defaultContextPacketIncludeInventory = true
	defaultContextPacketMaxSources       = 50
	defaultContextPacketMaxTotalBytes    = 262144
	defaultSeedSearchMaxResults          = 20
	defaultSeedFileMaxBytes              = 65536
	maxContextPacketSources              = 200
	maxContextPacketTotalBytes           = 1048576
	maxSeedSearchResults                 = 200
	maxSeedFileBytes                     = 262144
)

var taskSlugUnsafeChars = regexp.MustCompile(`[^a-z0-9]+`)

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
	PassID    string // optional; empty selects earliest eligible pass
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

	if req.PassID != "" {
		return svc.getPassByIDWithSafety(ctx, project, plan, passes, passByID, req.PassID)
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

	var ctxBudget ContextBudget
	if pass.ContextBudgetJson != "" && pass.ContextBudgetJson != "{}" {
		_ = json.Unmarshal([]byte(pass.ContextBudgetJson), &ctxBudget)
	}

	requireSnapshot := (ssReqs.RequireGitStatus != nil && *ssReqs.RequireGitStatus) ||
		(ssReqs.RequireCommitSHA != nil && *ssReqs.RequireCommitSHA)

	var snapshotID string
	var snapshotStatus string
	var snapshotFound bool
	if requireSnapshot || hasRequiredContextInputs(ctxPlan) {
		snapshot, err := svc.store.GetLatestSourceSnapshotForProject(project.ID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return NextPassWorkResponse{}, fmt.Errorf("get latest source snapshot for project %q: %w", project.ProjectID, err)
			}
		} else {
			snapshotID = snapshot.SourceSnapshotID
			snapshotStatus = snapshot.Status
			snapshotFound = true
		}
	}

	requirePacket := hasRequiredContextInputs(ctxPlan)

	var packetID string
	var packetStatus string
	var coverageReportPath string
	var packetFound bool
	var packetUsable bool
	var packetUnusableReason string

	if requirePacket {
		packet, err := svc.store.GetLatestContextPacketForPass(project.ProjectID, plan.PlanID, pass.PassID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return NextPassWorkResponse{}, fmt.Errorf("get latest context packet for pass %q: %w", pass.PassID, err)
			}
		} else {
			packetID = packet.ContextPacketID
			packetStatus = packet.Status
			coverageReportPath = packet.CoverageReportPath
			packetFound = true
			packetUsable, packetUnusableReason = contextPacketUsableForHandoff(*packet, snapshotID)
		}
	}

	contextReady := (!requireSnapshot || snapshotFound) && (!requirePacket || packetUsable)

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

	repoAliases, err := svc.projectRepoAliases(project.ID)
	if err != nil {
		return NextPassWorkResponse{}, fmt.Errorf("list project repositories for %q: %w", project.ProjectID, err)
	}

	// Build the shared Planner jumpstart payload.
	jumpstart := buildPlannerJumpstart(selectedPass, project, plan.PlanID, &ssReqs, ctxPlan, &ctxBudget, repoAliases, snapshotID, snapshotStatus, packetID, packetStatus, requireSnapshot, requirePacket, snapshotFound, packetFound, packetUsable, packetUnusableReason)

	// Determine readiness state and optional blocker.
	var readinessState string
	var blocker *WorkBlocker
	switch {
	case requireSnapshot && !snapshotFound:
		readinessState = "needs_source_snapshot"
		blocker = &WorkBlocker{
			Code:        BlockerRequiredSourceContextMissing,
			Message:     fmt.Sprintf("pass %q requires a source snapshot (require_git_status or require_commit_sha) but none exists for project %q", pass.PassID, project.ProjectID),
			Recoverable: true,
		}
	case requirePacket && !packetFound:
		readinessState = "needs_context_packet"
		blocker = &WorkBlocker{
			Code:        BlockerRequiredContextPacketMissing,
			Message:     fmt.Sprintf("pass %q has required context inputs but no context packet exists for project=%q plan=%q pass=%q", pass.PassID, project.ProjectID, plan.PlanID, pass.PassID),
			Recoverable: true,
		}
	case requirePacket && !packetUsable:
		readinessState = "needs_context_packet"
		blocker = &WorkBlocker{
			Code:        BlockerRequiredContextPacketMissing,
			Message:     fmt.Sprintf("pass %q context packet %q is unusable: %s", pass.PassID, packetID, packetUnusableReason),
			Recoverable: true,
		}
	default:
		readinessState = "ready_for_handoff_authoring"
	}

	jumpstart.ReadinessState = readinessState

	resp := NextPassWorkResponse{
		OK:   blocker == nil,
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
		PlannerJumpstart:         jumpstart,
		Blockers:                 []WorkBlocker{},
	}

	if blocker != nil {
		resp.Blockers = []WorkBlocker{*blocker}
	} else {
		handoffWork := buildHandoffAuthoringPacket(project, plan, selectedPass, ctxPlan, &ctxBudget, ssReqs, criteria, snapshotID, snapshotStatus, packetID, packetStatus, coverageReportPath, contextReady, requireSnapshot, requirePacket)
		resp.HandoffWork = handoffWork
		resp.HandoffAuthoringPacket = handoffWork
	}

	return resp, nil
}

func buildHandoffAuthoringPacket(
	project *store.Project,
	plan *store.Plan,
	selectedPass *WorkPassSummary,
	ctxPlan ContextPlan,
	ctxBudget *ContextBudget,
	ssReqs SourceSnapshotRequirements,
	criteria []string,
	snapshotID, snapshotStatus, packetID, packetStatus, coverageReportPath string,
	contextReady bool,
	requireSnapshot, requirePacket bool,
) *HandoffAuthoringPacket {
	checks := []HandoffAuthoringReadinessCheck{
		{Name: "dependencies_satisfied", Ready: true, Detail: "Selected pass dependencies are satisfied."},
		{Name: "active_runs_absent", Ready: true, Detail: "No active run is associated with the selected pass."},
		{Name: "plan_active", Ready: true, Detail: "Managed plan status is active."},
	}
	if requireSnapshot {
		checks = append(checks, HandoffAuthoringReadinessCheck{Name: "source_snapshot_available", Ready: snapshotID != "", Detail: snapshotID})
	}
	if requirePacket {
		checks = append(checks, HandoffAuthoringReadinessCheck{Name: "context_packet_available", Ready: packetID != "", Detail: packetID})
	}
	checks = append(checks, HandoffAuthoringReadinessCheck{Name: "reviewed_handoff_artifact_absent", Ready: true, Detail: "Draft and review a Planner handoff before run submission."})

	copiedCriteria := append([]string(nil), criteria...)
	packet := &HandoffAuthoringPacket{
		ProjectID:                project.ProjectID,
		PlanID:                   plan.PlanID,
		PlanTitle:                plan.Title,
		PassID:                   selectedPass.PassID,
		PassSequence:             selectedPass.Sequence,
		PassName:                 selectedPass.Name,
		PassStatus:               selectedPass.Status,
		PassGoal:                 selectedPass.Goal,
		RefactorCandidate:        selectedPass.RefactorCandidate,
		SourceSnapshotID:         snapshotID,
		SourceSnapshotStatus:     snapshotStatus,
		ContextPacketID:          packetID,
		ContextPacketStatus:      packetStatus,
		CoverageReportPath:       coverageReportPath,
		ContextReady:             contextReady,
		ContextPlan:              ctxPlan,
		ContextBudget:            ctxBudget,
		SourceRequirements:       ssReqs,
		HandoffReadinessCriteria: copiedCriteria,
		ReadinessCriteria:        append([]string(nil), copiedCriteria...),
		ReadinessChecks:          checks,
		ContextCoverageExpected:  append([]string(nil), ctxPlan.ContextCoverageExpectations...),
		BlockedIfMissing:         append([]string(nil), ctxPlan.BlockedIfMissing...),
		SuggestedAuthoringAction: "draft_planner_handoff",
		SubmissionPrerequisites: []string{
			"Planner handoff markdown is drafted from this packet.",
			"User reviews and explicitly approves the handoff.",
			"create_run_from_planner_handoff receives the reviewed handoff content and artifact IDs.",
		},
	}
	return packet
}

// buildPlannerJumpstart constructs the PlannerJumpstart payload from
// the evaluated candidate data. No raw file contents are included.
func buildPlannerJumpstart(
	selectedPass *WorkPassSummary,
	project *store.Project,
	planID string,
	ssReqs *SourceSnapshotRequirements,
	ctxPlan ContextPlan,
	ctxBudget *ContextBudget,
	repoAliases map[string]string,
	snapshotID, snapshotStatus, packetID, packetStatus string,
	requireSnapshot, requirePacket, snapshotFound, packetFound bool,
	packetUsable bool,
	packetUnusableReason string,
) *PlannerJumpstart {
	var basis *PlannerJumpstartBasisReport
	if requireSnapshot || requirePacket {
		basis = &PlannerJumpstartBasisReport{
			SnapshotID:     snapshotID,
			SnapshotStatus: snapshotStatus,
			PacketID:       packetID,
			PacketStatus:   packetStatus,
		}
	}

	var actions []ContextAcquisitionAction
	var checklist []string

	if requireSnapshot && !snapshotFound {
		repoIDs := normalizeContextRepoIDs(ctxPlan.RequiredRepositories, repoAliases)
		args := map[string]interface{}{
			"project_id": project.ProjectID,
		}
		if len(repoIDs) > 0 {
			args["repo_ids"] = repoIDs
		} else {
			checklist = append(checklist, "Look up project repository IDs to pass to create_source_snapshot")
		}
		actions = append(actions, ContextAcquisitionAction{
			Tool:      "create_source_snapshot",
			Arguments: args,
		})
		checklist = append(checklist, "Source snapshot: needed — run create_source_snapshot")
	} else if snapshotFound {
		checklist = append(checklist, "Source snapshot: ready ("+snapshotID+")")
	}

	if requirePacket && (!packetFound || !packetUsable) {
		contextPacketAction := ContextAcquisitionAction{
			Tool:      "create_context_packet",
			Arguments: buildContextPacketActionArguments(project.ProjectID, planID, selectedPass.PassID, snapshotID, ctxPlan, ctxBudget, repoAliases),
		}
		if snapshotID == "" {
			if !(requireSnapshot && !snapshotFound) {
				actions = append(actions, ContextAcquisitionAction{
					Tool: "create_source_snapshot",
					Arguments: map[string]interface{}{
						"project_id": project.ProjectID,
					},
				})
				checklist = append(checklist, "Source snapshot: needed - run create_source_snapshot")
			}
			contextPacketAction.DependsOn = "create_source_snapshot"
			contextPacketAction.ArgumentBindings = map[string]string{
				"source_snapshot_id": "$.result.source_snapshot_id",
			}
		}
		actions = append(actions, contextPacketAction)
		if !packetFound {
			checklist = append(checklist, "Context packet: needed - run create_context_packet with project_id, plan_id, pass_id, task_slug, source_snapshot_id, seed_files, and seed_searches")
		} else {
			checklist = append(checklist, "Context packet: unusable - "+packetUnusableReason)
		}
	} else if requirePacket && packetFound && packetUsable {
		checklist = append(checklist, "Context packet: ready ("+packetID+")")
	}

	if selectedPass != nil {
		checklist = append(checklist,
			"Dependencies: satisfied",
			"Active runs: none",
			"Plan status: active",
			"Pass goal: "+selectedPass.Goal,
			"Pass scope: seq "+fmt.Sprintf("%d", selectedPass.Sequence)+" — "+selectedPass.Name,
			"Handoff readiness criteria: review required",
		)
	}

	return &PlannerJumpstart{
		SelectedPassSummary:                selectedPass,
		SourceRequirements:                 ssReqs,
		ContextRequirements:                ctxPlan,
		SourceBasisReport:                  basis,
		SuggestedContextAcquisitionActions: actions,
		HandoffPreflightChecklist:          checklist,
	}
}

func buildContextPacketActionArguments(projectID, planID, passID, sourceSnapshotID string, ctxPlan ContextPlan, ctxBudget *ContextBudget, repoAliases map[string]string) map[string]interface{} {
	args := map[string]interface{}{
		"project_id":        projectID,
		"plan_id":           planID,
		"pass_id":           passID,
		"task_slug":         safeTaskSlug("next-pass-work", planID, passID),
		"seed_files":        buildContextPacketSeedFiles(ctxPlan, ctxBudget, repoAliases),
		"seed_searches":     buildContextPacketSeedSearches(ctxPlan, ctxBudget, repoAliases),
		"include_inventory": defaultContextPacketIncludeInventory,
		"max_sources":       contextBudgetInt(ctxBudget, "max_files", defaultContextPacketMaxSources, maxContextPacketSources),
		"max_total_bytes":   contextBudgetInt(ctxBudget, "max_bytes", defaultContextPacketMaxTotalBytes, maxContextPacketTotalBytes),
	}
	if strings.TrimSpace(sourceSnapshotID) != "" {
		args["source_snapshot_id"] = strings.TrimSpace(sourceSnapshotID)
	}
	return args
}

func buildContextPacketSeedFiles(ctxPlan ContextPlan, ctxBudget *ContextBudget, repoAliases map[string]string) []map[string]interface{} {
	seedFiles := make([]map[string]interface{}, 0, len(ctxPlan.SeedFilesToRead))
	maxBytes := contextBudgetInt(ctxBudget, "max_bytes", defaultSeedFileMaxBytes, maxSeedFileBytes)
	for _, seed := range ctxPlan.SeedFilesToRead {
		repoID := normalizeContextRepoID(seed.RepoID, repoAliases)
		path := strings.TrimSpace(seed.Path)
		reason := strings.TrimSpace(seed.Purpose)
		if repoID == "" || path == "" || reason == "" || isLocalAbsolutePath(path) {
			continue
		}
		seedFiles = append(seedFiles, map[string]interface{}{
			"repo_id":   repoID,
			"path":      path,
			"reason":    reason,
			"required":  boolValue(seed.Required),
			"max_bytes": maxBytes,
		})
	}
	return seedFiles
}

func buildContextPacketSeedSearches(ctxPlan ContextPlan, ctxBudget *ContextBudget, repoAliases map[string]string) []map[string]interface{} {
	seedSearches := make([]map[string]interface{}, 0, len(ctxPlan.SeedSearchTerms))
	maxResults := contextBudgetInt(ctxBudget, "max_search_results", defaultSeedSearchMaxResults, maxSeedSearchResults)
	for _, seed := range ctxPlan.SeedSearchTerms {
		query := strings.TrimSpace(seed.Query)
		reason := strings.TrimSpace(seed.Purpose)
		if query == "" || reason == "" {
			continue
		}
		item := map[string]interface{}{
			"pattern":       query,
			"reason":        reason,
			"required":      boolValue(seed.Required),
			"max_results":   maxResults,
			"context_lines": 2,
		}
		if repoID := normalizeContextRepoID(seed.RepoID, repoAliases); repoID != "" {
			item["repo_ids"] = []string{repoID}
		}
		seedSearches = append(seedSearches, item)
	}
	return seedSearches
}

func (svc *OrchestratorWorkService) projectRepoAliases(projectRowID int64) (map[string]string, error) {
	repos, err := svc.store.ListProjectRepositories(projectRowID)
	if err != nil {
		return nil, err
	}
	aliases := make(map[string]string, len(repos)*2)
	ambiguous := map[string]bool{}
	for _, repo := range repos {
		for _, alias := range repoAliases(repo.RepoID) {
			if alias == "" || ambiguous[alias] {
				continue
			}
			if existing, ok := aliases[alias]; ok && existing != repo.RepoID {
				delete(aliases, alias)
				ambiguous[alias] = true
				continue
			}
			aliases[alias] = repo.RepoID
		}
	}
	return aliases, nil
}

func repoAliases(repoID string) []string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil
	}
	aliases := []string{repoID}
	if idx := strings.LastIndex(repoID, "/"); idx >= 0 && idx+1 < len(repoID) {
		aliases = append(aliases, repoID[idx+1:])
	}
	return aliases
}

func normalizeContextRepoIDs(repoIDs []string, aliases map[string]string) []string {
	out := make([]string, 0, len(repoIDs))
	seen := map[string]struct{}{}
	for _, raw := range repoIDs {
		repoID := normalizeContextRepoID(raw, aliases)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		out = append(out, repoID)
		seen[repoID] = struct{}{}
	}
	return out
}

func normalizeContextRepoID(raw string, aliases map[string]string) string {
	repoID := strings.TrimSpace(raw)
	if repoID == "" {
		return ""
	}
	if normalized, ok := aliases[repoID]; ok {
		return normalized
	}
	return repoID
}

func contextBudgetInt(ctxBudget *ContextBudget, field string, defaultValue, maxValue int) int {
	if ctxBudget == nil {
		return defaultValue
	}
	var value *int64
	switch field {
	case "max_files":
		value = ctxBudget.MaxFiles
	case "max_bytes":
		value = ctxBudget.MaxBytes
	case "max_search_results":
		value = ctxBudget.MaxSearchResults
	}
	if value == nil || *value <= 0 {
		return defaultValue
	}
	if *value > int64(maxValue) {
		return maxValue
	}
	return int(*value)
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func safeTaskSlug(parts ...string) string {
	joined := strings.ToLower(strings.Join(parts, "-"))
	slug := strings.Trim(taskSlugUnsafeChars.ReplaceAllString(joined, "-"), "-")
	if slug == "" {
		return "next-pass-work"
	}
	return slug
}

func isLocalAbsolutePath(path string) bool {
	return strings.HasPrefix(path, "/") || (len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/'))
}

// getPassByIDWithSafety validates that a requested pass can be safely
// reached (all prior passes are terminal) and then evaluates it.
// It does not bypass sequential safety.
func (svc *OrchestratorWorkService) getPassByIDWithSafety(
	ctx context.Context,
	project *store.Project,
	plan *store.Plan,
	passes []store.PlanPass,
	passByID map[string]*store.PlanPass,
	targetPassID string,
) (NextPassWorkResponse, error) {
	var targetIdx int = -1
	for i := range passes {
		if passes[i].PassID == targetPassID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return blockerResponse(WorkBlocker{
			Code:        BlockerRequestedPassNotFound,
			Message:     fmt.Sprintf("requested pass %q does not exist in plan %q", targetPassID, plan.PlanID),
			Recoverable: false,
		}), nil
	}

	for i := 0; i < targetIdx; i++ {
		prior := &passes[i]
		switch prior.Status {
		case StatusPassCompleted, StatusPassSkipped:
			continue
		case StatusPassAuditReady:
			return blockerResponse(WorkBlocker{
				Code:        BlockerPriorPassAwaitsAudit,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q and must be audited before pass %q can start", prior.PassID, prior.Sequence, prior.Status, targetPassID),
				Recoverable: true,
			}), nil
		case StatusPassRevisionRequired:
			return blockerResponse(WorkBlocker{
				Code:        BlockerRevisionRequiredSamePass,
				Message:     fmt.Sprintf("pass %q (seq %d) requires revision before pass %q can start", prior.PassID, prior.Sequence, targetPassID),
				Recoverable: true,
			}), nil
		case StatusPassHandoffReady:
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) is awaiting handoff submission; pass %q cannot start yet", prior.PassID, prior.Sequence, targetPassID),
				Recoverable: true,
			}), nil
		case StatusPassRunCreated, StatusPassInProgress:
			return blockerResponse(WorkBlocker{
				Code:        BlockerActiveRunExists,
				Message:     fmt.Sprintf("pass %q (seq %d) has an active associated run; pass %q cannot start yet", prior.PassID, prior.Sequence, targetPassID),
				Recoverable: true,
			}), nil
		case StatusPassBlocked:
			return blockerResponse(WorkBlocker{
				Code:        BlockerNoEligiblePass,
				Message:     fmt.Sprintf("pass %q (seq %d) is blocked and prevents pass %q from starting", prior.PassID, prior.Sequence, targetPassID),
				Recoverable: true,
			}), nil
		default:
			return blockerResponse(WorkBlocker{
				Code:        BlockerRequestedPassNotEligible,
				Message:     fmt.Sprintf("pass %q (seq %d) has status %q; pass %q cannot start until it is terminal", prior.PassID, prior.Sequence, prior.Status, targetPassID),
				Recoverable: true,
			}), nil
		}
	}

	target := &passes[targetIdx]
	switch target.Status {
	case StatusPassCompleted, StatusPassSkipped:
		return blockerResponse(WorkBlocker{
			Code:        BlockerRequestedPassNotEligible,
			Message:     fmt.Sprintf("requested pass %q (seq %d) has status %q and cannot be started", target.PassID, target.Sequence, target.Status),
			Recoverable: false,
		}), nil
	case StatusPassAuditReady:
		return blockerResponse(WorkBlocker{
			Code:        BlockerPriorPassAwaitsAudit,
			Message:     fmt.Sprintf("requested pass %q (seq %d) has status %q and must be audited first", target.PassID, target.Sequence, target.Status),
			Recoverable: true,
		}), nil
	case StatusPassRevisionRequired:
		return blockerResponse(WorkBlocker{
			Code:        BlockerRevisionRequiredSamePass,
			Message:     fmt.Sprintf("requested pass %q (seq %d) requires revision before proceeding", target.PassID, target.Sequence),
			Recoverable: true,
		}), nil
	case StatusPassHandoffReady:
		return blockerResponse(WorkBlocker{
			Code:        BlockerNoEligiblePass,
			Message:     fmt.Sprintf("requested pass %q (seq %d) is awaiting handoff submission", target.PassID, target.Sequence),
			Recoverable: true,
		}), nil
	case StatusPassRunCreated, StatusPassInProgress:
		return blockerResponse(WorkBlocker{
			Code:        BlockerActiveRunExists,
			Message:     fmt.Sprintf("requested pass %q (seq %d) has an active associated run", target.PassID, target.Sequence),
			Recoverable: true,
		}), nil
	case StatusPassBlocked:
		return blockerResponse(WorkBlocker{
			Code:        BlockerNoEligiblePass,
			Message:     fmt.Sprintf("requested pass %q (seq %d) is blocked", target.PassID, target.Sequence),
			Recoverable: true,
		}), nil
	case StatusPassPlanned, StatusPassReadyForPlanner:
		return svc.evaluateCandidate(ctx, project, plan, target, passByID)
	default:
		return blockerResponse(WorkBlocker{
			Code:        BlockerRequestedPassNotEligible,
			Message:     fmt.Sprintf("requested pass %q (seq %d) has unrecognised status %q", target.PassID, target.Sequence, target.Status),
			Recoverable: false,
		}), nil
	}
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

func contextPacketUsableForHandoff(packet store.ContextPacket, selectedSourceSnapshotID string) (bool, string) {
	status := strings.TrimSpace(packet.Status)
	if status != "created" {
		return false, fmt.Sprintf("packet status is %q, expected \"created\"", status)
	}
	if packet.BlockedSeedCount > 0 {
		return false, fmt.Sprintf("packet has %d blocked seeds", packet.BlockedSeedCount)
	}
	if packet.MissingSeedCount > 0 {
		return false, fmt.Sprintf("packet has %d missing seeds", packet.MissingSeedCount)
	}
	if selectedSourceSnapshotID != "" && packet.SourceSnapshotID != selectedSourceSnapshotID {
		return false, fmt.Sprintf("packet source snapshot ID %q does not match selected source snapshot ID %q", packet.SourceSnapshotID, selectedSourceSnapshotID)
	}
	return true, ""
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
