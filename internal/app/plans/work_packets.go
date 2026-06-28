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

	BlockerSourceSnapshotAcquisitionFailed   = "source_snapshot_acquisition_failed"
	BlockerSourceSnapshotMetadataMissing     = "source_snapshot_metadata_missing"
	BlockerSourceSnapshotRequiredSeedMissing = "source_snapshot_required_seed_missing"
	BlockerContextPacketAcquisitionFailed    = "context_packet_acquisition_failed"
	BlockerContextCoverageIncomplete         = "context_coverage_incomplete"
	BlockerContextPacketTruncated            = "context_packet_truncated"
	BlockerContextPacketUnusable             = "context_packet_unusable"
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
	OK                       bool                      `json:"ok"`
	Tool                     string                    `json:"tool"`
	Project                  *WorkProjectSummary       `json:"project,omitempty"`
	Plan                     *WorkPlanSummary          `json:"plan,omitempty"`
	SelectedPass             *WorkPassSummary          `json:"selected_pass,omitempty"`
	DependencyStatus         []WorkDependencyStatus    `json:"dependency_status,omitempty"`
	AssociatedRuns           []WorkRunSummary          `json:"associated_runs,omitempty"`
	Context                  *WorkContextSummary       `json:"context,omitempty"`
	HandoffReadinessCriteria []string                  `json:"handoff_readiness_criteria,omitempty"`
	HandoffWork              *HandoffAuthoringPacket   `json:"handoff_work,omitempty"`
	HandoffAuthoringPacket   *HandoffAuthoringPacket   `json:"handoff_authoring_packet,omitempty"`
	SuggestedRunSubmission   *SuggestedRunSubmission   `json:"suggested_run_submission,omitempty"`
	PlannerJumpstart         *PlannerJumpstart         `json:"planner_jumpstart,omitempty"`
	Blockers                 []WorkBlocker             `json:"blockers"`
	AcquisitionSummary       *AcquisitionSummary       `json:"acquisition_summary,omitempty"`
	AcquisitionFailureReport *AcquisitionFailureReport `json:"acquisition_failure_report,omitempty"`
}

// NextPassWorkMCPSummary is the compact model-visible summary returned by MCP.
// It deliberately omits verbose pass goals, context prose, and seed details
// while keeping enough IDs and next-action references for follow-up tool calls.
type NextPassWorkMCPSummary struct {
	OK                       bool                         `json:"ok"`
	Tool                     string                       `json:"tool"`
	ProjectID                string                       `json:"project_id,omitempty"`
	PlanID                   string                       `json:"plan_id,omitempty"`
	SelectedPass             *NextPassWorkSummaryPass     `json:"selected_pass,omitempty"`
	ReadinessState           string                       `json:"readiness_state,omitempty"`
	SourceSnapshotID         string                       `json:"source_snapshot_id,omitempty"`
	ContextPacketID          string                       `json:"context_packet_id,omitempty"`
	ContextReady             bool                         `json:"context_ready"`
	HandoffWork              *HandoffAuthoringPacket      `json:"handoff_work,omitempty"`
	HandoffPacket            *HandoffAuthoringPacket      `json:"handoff_authoring_packet,omitempty"`
	Blockers                 []NextPassWorkSummaryBlocker `json:"blockers"`
	NextActions              []NextPassWorkSummaryAction  `json:"next_actions,omitempty"`
	LocalPreviewHint         string                       `json:"local_preview_hint"`
	AcquisitionSummary       *AcquisitionSummary          `json:"acquisition_summary,omitempty"`
	AcquisitionFailureReport *AcquisitionFailureReport    `json:"acquisition_failure_report,omitempty"`
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

// AcquisitionSummary reports what was acquired or attempted during a
// get_next_pass_work call.
type AcquisitionSummary struct {
	SourceSnapshotAcquired bool   `json:"source_snapshot_acquired"`
	SourceSnapshotID       string `json:"source_snapshot_id,omitempty"`
	ContextPacketCreated   bool   `json:"context_packet_created"`
	ContextPacketID        string `json:"context_packet_id,omitempty"`
	ContextPacketStatus    string `json:"context_packet_status,omitempty"`
}

// AcquisitionFailureReport contains bounded terminal context-acquisition
// diagnostics. It never includes source contents.
type AcquisitionFailureReport struct {
	Stage                     string                            `json:"stage"`
	FailureCode               string                            `json:"failure_code"`
	ReadinessState            string                            `json:"readiness_state"`
	SourceSnapshotID          string                            `json:"source_snapshot_id,omitempty"`
	ContextPacketID           string                            `json:"context_packet_id,omitempty"`
	ContextPacketStatus       string                            `json:"context_packet_status,omitempty"`
	TerminalReason            string                            `json:"terminal_reason"`
	AttemptedStrategies       []AcquisitionAttemptReport        `json:"attempted_strategies"`
	PacketSummary             *ContextPacketDiagnosticSummary   `json:"packet_summary,omitempty"`
	CoverageSummary           *ContextCoverageDiagnosticSummary `json:"coverage_summary,omitempty"`
	RecommendedOperatorAction string                            `json:"recommended_operator_action"`
}

type AcquisitionAttemptReport struct {
	Strategy            AcquisitionAttemptStrategy      `json:"strategy"`
	ContextPacketID     string                          `json:"context_packet_id,omitempty"`
	ContextPacketStatus string                          `json:"context_packet_status,omitempty"`
	FailureCode         string                          `json:"failure_code,omitempty"`
	TerminalReason      string                          `json:"terminal_reason,omitempty"`
	PacketSummary       *ContextPacketDiagnosticSummary `json:"packet_summary,omitempty"`
	LimitHit            string                          `json:"limit_hit,omitempty"`
}

type AcquisitionAttemptStrategy struct {
	Name             string `json:"name"`
	IncludeInventory bool   `json:"include_inventory"`
	MaxSources       int    `json:"max_sources"`
	MaxTotalBytes    int    `json:"max_total_bytes"`
	MaxSearchResults int    `json:"max_search_results"`
	ContextLines     int    `json:"context_lines"`
}

type ContextPacketDiagnosticSummary struct {
	MaxSources        int    `json:"max_sources"`
	MaxTotalBytes     int    `json:"max_total_bytes"`
	TotalSourceBytes  int    `json:"total_source_bytes"`
	SourceCount       int    `json:"source_count"`
	CoveredSeedCount  int    `json:"covered_seed_count"`
	BlockedSeedCount  int    `json:"blocked_seed_count"`
	MissingSeedCount  int    `json:"missing_seed_count"`
	Truncated         bool   `json:"truncated"`
	InventoryIncluded bool   `json:"inventory_included"`
	LimitHit          string `json:"limit_hit"`
}

type ContextCoverageDiagnosticSummary struct {
	EntryCount      int                         `json:"entry_count"`
	CoveredCount    int                         `json:"covered_count"`
	PartialCount    int                         `json:"partial_count"`
	BlockedCount    int                         `json:"blocked_count"`
	MissingCount    int                         `json:"missing_count"`
	TruncatedCount  int                         `json:"truncated_count"`
	RequiredCount   int                         `json:"required_count"`
	RequiredCovered int                         `json:"required_covered_count"`
	Entries         []ContextCoverageDiagnostic `json:"entries,omitempty"`
}

type ContextCoverageDiagnostic struct {
	SeedID       string             `json:"seed_id"`
	SeedType     string             `json:"seed_type"`
	Required     bool               `json:"required"`
	Path         string             `json:"path,omitempty"`
	Pattern      string             `json:"pattern,omitempty"`
	Reason       string             `json:"reason,omitempty"`
	Status       string             `json:"status"`
	Truncated    bool               `json:"truncated"`
	MissingCause string             `json:"missing_cause,omitempty"`
	Blockers     []CtxSourceBlocker `json:"blockers,omitempty"`
	SourceIDs    []string           `json:"source_ids,omitempty"`
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
	if resp.AcquisitionSummary != nil {
		summary.AcquisitionSummary = resp.AcquisitionSummary
	}
	if resp.AcquisitionFailureReport != nil {
		summary.AcquisitionFailureReport = resp.AcquisitionFailureReport
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
// for a project-scoped managed plan. When source and context packet services
// are provided, the service performs bounded backend acquisition (creating
// source snapshots and context packets as needed) before returning handoff
// readiness; without them it falls back to retrieval-only readiness checks.
type OrchestratorWorkService struct {
	store             *store.Store
	sourcesSvc        sourceSnapshotAcquirer
	contextPacketsSvc contextPacketAcquirer
}

// sourceSnapshotAcquirer abstracts source snapshot creation for the
// acquisition coordinator.
type sourceSnapshotAcquirer interface {
	CreateSourceSnapshot(ctx context.Context, projectID string, repoIDs []string, includeFileMetadata bool) (snapshotID, status string, includedFileCount int, err error)
}

// contextPacketAcquirer abstracts context packet creation for the
// acquisition coordinator.
type contextPacketAcquirer interface {
	CreateContextPacket(ctx context.Context, input CtxPacketInput) (*CtxPacketResult, error)
}

// CtxPacketInput mirrors contextpackets.ContextPacketInput without importing
// that package (avoids import cycle with projects/plans through test chain).
type CtxPacketInput struct {
	ProjectID        string
	PlanID           string
	PassID           string
	TaskSlug         string
	SourceSnapshotID string
	SeedFiles        []CtxSeedFile
	SeedSearches     []CtxSeedSearch
	IncludeInventory bool
	MaxSources       int
	MaxTotalBytes    int
}

// CtxSeedFile mirrors contextpackets.ContextSeedFile.
type CtxSeedFile struct {
	RepoID   string
	Path     string
	Reason   string
	Required bool
}

// CtxSeedSearch mirrors contextpackets.ContextSeedSearch.
type CtxSeedSearch struct {
	RepoIDs      []string
	Pattern      string
	Reason       string
	Required     bool
	MaxResults   int
	ContextLines int
}

// CtxPacketResult mirrors contextpackets.ContextPacketResult.
type CtxPacketResult struct {
	ContextPacketID    string
	Status             string
	CoverageReportPath string
	BlockedSeedCount   int
	MissingSeedCount   int
	Truncated          bool
	SourceSnapshotID   string
	SourceCount        int
	Summary            CtxPacketSummary
	Coverage           []CtxCoverageEntry
	LimitHit           string
}

type CtxPacketSummary struct {
	SourceCount       int
	CoveredSeedCount  int
	BlockedSeedCount  int
	MissingSeedCount  int
	Truncated         bool
	MaxSources        int
	MaxTotalBytes     int
	TotalSourceBytes  int
	InventoryIncluded bool
}

type CtxCoverageEntry struct {
	SeedID       string
	SeedType     string
	Required     bool
	Status       string
	Path         string
	Pattern      string
	Reason       string
	SourceIDs    []string
	Truncated    bool
	Blockers     []CtxSourceBlocker
	MissingCause string
}

type CtxSourceBlocker struct {
	RepoID  string `json:"repo_id,omitempty"`
	Path    string `json:"path,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// NewOrchestratorWorkService constructs an OrchestratorWorkService.
func NewOrchestratorWorkService(s *store.Store) *OrchestratorWorkService {
	return &OrchestratorWorkService{store: s}
}

// SetSourceService configures the optional source service for bounded
// source snapshot acquisition during GetNextPassWork.
func (svc *OrchestratorWorkService) SetSourceService(s sourceSnapshotAcquirer) {
	svc.sourcesSvc = s
}

// SetContextPacketService configures the optional context packet service
// for bounded context packet acquisition during GetNextPassWork.
func (svc *OrchestratorWorkService) SetContextPacketService(s contextPacketAcquirer) {
	svc.contextPacketsSvc = s
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

	requireSnapshot := (ssReqs.RequireGitStatus != nil && *ssReqs.RequireGitStatus) ||
		(ssReqs.RequireCommitSHA != nil && *ssReqs.RequireCommitSHA)

	var snapshotID string
	var snapshotStatus string
	var snapshotFound bool
	var snapshotAcquired bool

	contextPacketRequired := contextPlanRequiresPacket(ctxPlan)
	sourceSnapshotNeeded := requireSnapshot || contextPacketRequired

	if sourceSnapshotNeeded {
		if svc.sourcesSvc != nil && svc.contextPacketsSvc != nil && svc.store != nil {
			var acqBlocker *WorkBlocker
			snapshotID, snapshotStatus, snapshotFound, snapshotAcquired, acqBlocker = svc.acquireSourceSnapshot(
				ctx, project, plan, pass, requireSnapshot, &ssReqs, ctxPlan, repoAliases)
			if acqBlocker != nil {
				jumpstart := buildPlannerJumpstart(selectedPass, project, plan.PlanID, &ssReqs, ctxPlan, &ctxBudget, repoAliases, snapshotID, snapshotStatus, "", "", requireSnapshot, false, snapshotFound, false, false, "")
				jumpstart.ReadinessState = "needs_source_snapshot"
				resp := NextPassWorkResponse{
					OK:   false,
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
						ContextReady:         false,
					},
					PlannerJumpstart: jumpstart,
					Blockers:         []WorkBlocker{*acqBlocker},
					AcquisitionSummary: &AcquisitionSummary{
						SourceSnapshotAcquired: snapshotAcquired,
						SourceSnapshotID:       snapshotID,
					},
				}
				return resp, nil
			}
		} else {
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
	}

	requirePacket := contextPacketRequired

	var packetID string
	var packetStatus string
	var coverageReportPath string
	var packetFound bool
	var packetUsable bool
	var packetCreated bool
	var packetUnusableReason string
	var acquisitionFailureReport *AcquisitionFailureReport

	if requirePacket {
		if svc.sourcesSvc != nil && svc.contextPacketsSvc != nil && svc.store != nil {
			var acqBlocker *WorkBlocker
			var report *AcquisitionFailureReport
			packetID, packetStatus, coverageReportPath, packetFound, packetUsable, packetCreated, acqBlocker, report = svc.acquireContextPacket(
				ctx, project, plan, pass, &ssReqs, ctxPlan, &ctxBudget, repoAliases, snapshotID)
			if acqBlocker != nil {
				acquisitionFailureReport = report
				jumpstart := buildPlannerJumpstart(selectedPass, project, plan.PlanID, &ssReqs, ctxPlan, &ctxBudget, repoAliases, snapshotID, snapshotStatus, packetID, packetStatus, requireSnapshot, requirePacket, snapshotFound, packetFound, packetUsable, "")
				if acquisitionFailureReport != nil {
					jumpstart.ReadinessState = acquisitionFailureReport.ReadinessState
					jumpstart.SuggestedContextAcquisitionActions = nil
					jumpstart.HandoffPreflightChecklist = append([]string{
						"Context acquisition failed after backend retry attempts.",
						"Use acquisition_failure_report for bounded diagnostics.",
					}, jumpstart.HandoffPreflightChecklist...)
				} else {
					jumpstart.ReadinessState = "needs_context_packet"
				}
				resp := NextPassWorkResponse{
					OK:   false,
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
					PlannerJumpstart: jumpstart,
					Blockers:         []WorkBlocker{*acqBlocker},
					AcquisitionSummary: &AcquisitionSummary{
						SourceSnapshotAcquired: snapshotAcquired,
						SourceSnapshotID:       snapshotID,
						ContextPacketCreated:   packetCreated,
						ContextPacketID:        packetID,
						ContextPacketStatus:    packetStatus,
					},
					AcquisitionFailureReport: acquisitionFailureReport,
				}
				return resp, nil
			}
		} else {
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
	}

	contextReady := (!sourceSnapshotNeeded || snapshotFound) && (!requirePacket || packetUsable)

	// Build the shared Planner jumpstart payload.
	jumpstart := buildPlannerJumpstart(selectedPass, project, plan.PlanID, &ssReqs, ctxPlan, &ctxBudget, repoAliases, snapshotID, snapshotStatus, packetID, packetStatus, requireSnapshot, requirePacket, snapshotFound, packetFound, packetUsable, packetUnusableReason)

	// Determine readiness state and optional blocker.
	var readinessState string
	var blocker *WorkBlocker
	switch {
	case sourceSnapshotNeeded && !snapshotFound:
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
		handoffWork := buildHandoffAuthoringPacket(project, plan, selectedPass, ctxPlan, &ctxBudget, ssReqs, criteria, snapshotID, snapshotStatus, packetID, packetStatus, coverageReportPath, contextReady, sourceSnapshotNeeded, requirePacket)
		resp.HandoffWork = handoffWork
		resp.HandoffAuthoringPacket = handoffWork
	}

	if svc.sourcesSvc != nil && svc.contextPacketsSvc != nil {
		resp.AcquisitionSummary = &AcquisitionSummary{
			SourceSnapshotAcquired: snapshotAcquired,
			SourceSnapshotID:       snapshotID,
			ContextPacketCreated:   packetCreated,
			ContextPacketID:        packetID,
			ContextPacketStatus:    packetStatus,
		}
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
	sourceSnapshotNeeded, requirePacket bool,
) *HandoffAuthoringPacket {
	checks := []HandoffAuthoringReadinessCheck{
		{Name: "dependencies_satisfied", Ready: true, Detail: "Selected pass dependencies are satisfied."},
		{Name: "active_runs_absent", Ready: true, Detail: "No active run is associated with the selected pass."},
		{Name: "plan_active", Ready: true, Detail: "Managed plan status is active."},
	}
	if sourceSnapshotNeeded {
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
		contextLines := contextBudgetInt(ctxBudget, "max_context_lines", 2, 10)
		item := map[string]interface{}{
			"pattern":       query,
			"reason":        reason,
			"required":      boolValue(seed.Required),
			"max_results":   maxResults,
			"context_lines": contextLines,
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
	case "max_context_lines":
		value = ctxBudget.MaxContextLines
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

func contextPlanRequiresPacket(cp ContextPlan) bool {
	for _, f := range cp.SeedFilesToRead {
		if boolValue(f.Required) {
			return true
		}
	}
	for _, s := range cp.SeedSearchTerms {
		if boolValue(s.Required) {
			return true
		}
	}
	for _, item := range cp.BlockedIfMissing {
		if strings.TrimSpace(item) != "" {
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
	if packet.Truncated != 0 {
		return false, "packet is truncated"
	}
	if strings.TrimSpace(packet.PacketJsonPath) == "" || strings.TrimSpace(packet.CoverageReportPath) == "" {
		return false, "packet is missing artifact paths"
	}
	if selectedSourceSnapshotID != "" && packet.SourceSnapshotID != selectedSourceSnapshotID {
		return false, fmt.Sprintf("packet source snapshot ID %q does not match selected source snapshot ID %q", packet.SourceSnapshotID, selectedSourceSnapshotID)
	}
	return true, ""
}

func (svc *OrchestratorWorkService) acquireSourceSnapshot(
	ctx context.Context,
	project *store.Project,
	plan *store.Plan,
	pass *store.PlanPass,
	requireSnapshot bool,
	ssReqs *SourceSnapshotRequirements,
	ctxPlan ContextPlan,
	repoAliases map[string]string,
) (snapshotID string, snapshotStatus string, found bool, acquired bool, blocker *WorkBlocker) {
	// Look up latest existing snapshot.
	latest, err := svc.store.GetLatestSourceSnapshotForProject(project.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", false, false, &WorkBlocker{
			Code:        BlockerSourceSnapshotAcquisitionFailed,
			Message:     fmt.Sprintf("failed to look up source snapshot for project %q: %v", project.ProjectID, err),
			Recoverable: true,
		}
	}

	snapshotUsable := false
	if latest != nil {
		snapshotUsable, _ = sourceSnapshotUsableForHandoff(latest, ctxPlan, repoAliases)
	}
	if snapshotUsable {
		return latest.SourceSnapshotID, latest.Status, true, false, nil
	}

	// If no snapshot is required and we have a usable one, great.
	// Otherwise, we need to create one.
	repoIDs := normalizeContextRepoIDs(ctxPlan.RequiredRepositories, repoAliases)
	// Filter to only repos that are actually registered for this project.
	projectRepos, repoErr := svc.store.ListProjectRepositories(project.ID)
	if repoErr != nil {
		return "", "", false, false, &WorkBlocker{
			Code:        BlockerSourceSnapshotAcquisitionFailed,
			Message:     fmt.Sprintf("failed to list project repositories: %v", repoErr),
			Recoverable: true,
		}
	}
	registeredSet := make(map[string]bool, len(projectRepos))
	for _, r := range projectRepos {
		registeredSet[r.RepoID] = true
	}
	var effectiveRepoIDs []string
	for _, r := range repoIDs {
		if registeredSet[r] {
			effectiveRepoIDs = append(effectiveRepoIDs, r)
		}
	}
	repoIDs = effectiveRepoIDs

	if len(repoIDs) == 0 && requireSnapshot {
		return "", "", false, false, &WorkBlocker{
			Code:        BlockerSourceSnapshotRequiredSeedMissing,
			Message:     fmt.Sprintf("pass %q requires a source snapshot but no required repositories could be resolved for project %q", pass.PassID, project.ProjectID),
			Recoverable: true,
		}
	}
	if len(repoIDs) == 0 {
		// No repos to snapshot; if a snapshot isn't strictly required, continue without one.
		if latest != nil {
			return latest.SourceSnapshotID, latest.Status, true, false, nil
		}
		return "", "", false, false, nil
	}

	// Create a new source snapshot.
	snapshotIDResult, snapshotStatusResult, includedCount, err := svc.sourcesSvc.CreateSourceSnapshot(ctx, project.ProjectID, repoIDs, true)
	if err != nil {
		return "", "", false, false, &WorkBlocker{
			Code:        BlockerSourceSnapshotAcquisitionFailed,
			Message:     fmt.Sprintf("failed to create source snapshot for project %q: %v", project.ProjectID, err),
			Recoverable: true,
		}
	}

	if includedCount == 0 {
		return snapshotIDResult, snapshotStatusResult, true, true, &WorkBlocker{
			Code:        BlockerSourceSnapshotMetadataMissing,
			Message:     fmt.Sprintf("created source snapshot %q has no included file metadata", snapshotIDResult),
			Recoverable: true,
		}
	}

	return snapshotIDResult, snapshotStatusResult, true, true, nil
}

func sourceSnapshotUsableForHandoff(snapshot *store.SourceSnapshot, ctxPlan ContextPlan, repoAliases map[string]string) (bool, string) {
	status := strings.TrimSpace(snapshot.Status)
	if status != "created" && status != "partial" {
		return false, fmt.Sprintf("snapshot status is %q, expected created or partial", status)
	}
	// Check metadata: a metadata-empty snapshot is never usable.
	if strings.TrimSpace(snapshot.SummaryJson) == "" || snapshot.SummaryJson == "{}" {
		// Accept if not {} but minimal
	}
	// Must have at least some file metadata rows.
	if snapshot.SummaryJson == "{}" {
		return false, "snapshot has no metadata (summary is empty)"
	}
	return true, ""
}

func (svc *OrchestratorWorkService) acquireContextPacket(
	ctx context.Context,
	project *store.Project,
	plan *store.Plan,
	pass *store.PlanPass,
	ssReqs *SourceSnapshotRequirements,
	ctxPlan ContextPlan,
	ctxBudget *ContextBudget,
	repoAliases map[string]string,
	snapshotID string,
) (packetID string, packetStatus string, coverageReportPath string, found bool, usable bool, created bool, blocker *WorkBlocker, report *AcquisitionFailureReport) {
	// Look up latest existing packet.
	latest, err := svc.store.GetLatestContextPacketForPass(project.ProjectID, plan.PlanID, pass.PassID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, false, false, &WorkBlocker{
			Code:        BlockerContextPacketAcquisitionFailed,
			Message:     fmt.Sprintf("failed to look up context packet for pass %q: %v", pass.PassID, err),
			Recoverable: true,
		}, nil
	}

	if latest != nil {
		packetUsable, _ := contextPacketUsableForHandoff(*latest, snapshotID)
		if packetUsable {
			return latest.ContextPacketID, latest.Status, latest.CoverageReportPath, true, true, false, nil, nil
		}
		// Stale or blocked packet -- create a replacement.
	}

	// Need to create a context packet. Must have a source snapshot ID.
	if strings.TrimSpace(snapshotID) == "" {
		return "", "", "", false, false, false, &WorkBlocker{
			Code:        BlockerRequiredContextPacketMissing,
			Message:     fmt.Sprintf("pass %q has required context inputs but no context packet exists and no source snapshot is available", pass.PassID),
			Recoverable: true,
		}, nil
	}

	attempts := buildContextAcquisitionAttempts(project.ProjectID, plan.PlanID, pass.PassID, ctxPlan, ctxBudget, repoAliases, snapshotID)
	var reports []AcquisitionAttemptReport
	var lastResult *CtxPacketResult
	var lastBlocker *WorkBlocker
	for _, attempt := range attempts {
		result, err := svc.contextPacketsSvc.CreateContextPacket(ctx, attempt.input)
		if err != nil {
			lastBlocker = &WorkBlocker{
				Code:        BlockerContextPacketAcquisitionFailed,
				Message:     fmt.Sprintf("failed to create context packet for pass %q using %s: %v", pass.PassID, attempt.strategy.Name, err),
				Recoverable: true,
			}
			reports = append(reports, AcquisitionAttemptReport{
				Strategy:       attempt.strategy,
				FailureCode:    lastBlocker.Code,
				TerminalReason: lastBlocker.Message,
			})
			continue
		}
		lastResult = result
		created = true
		packetID, packetStatus, coverageReportPath = result.ContextPacketID, result.Status, result.CoverageReportPath
		found = true
		packetUsable, unusableReason := contextPacketResultUsableForHandoff(result, snapshotID)
		attemptReport := AcquisitionAttemptReport{
			Strategy:            attempt.strategy,
			ContextPacketID:     result.ContextPacketID,
			ContextPacketStatus: result.Status,
			PacketSummary:       packetDiagnosticSummary(result),
			LimitHit:            result.LimitHit,
		}
		if packetUsable {
			reports = append(reports, attemptReport)
			return result.ContextPacketID, result.Status, result.CoverageReportPath, true, true, true, nil, nil
		}
		lastBlocker = blockerForContextPacketResult(result, unusableReason)
		attemptReport.FailureCode = lastBlocker.Code
		attemptReport.TerminalReason = lastBlocker.Message
		reports = append(reports, attemptReport)
	}
	if lastBlocker == nil {
		lastBlocker = &WorkBlocker{
			Code:        BlockerContextPacketAcquisitionFailed,
			Message:     fmt.Sprintf("context packet acquisition for pass %q did not produce a usable packet", pass.PassID),
			Recoverable: true,
		}
	}
	report = buildAcquisitionFailureReport(snapshotID, lastBlocker, reports, lastResult)
	return packetID, packetStatus, coverageReportPath, found, false, created, lastBlocker, report
}

type contextAcquisitionAttempt struct {
	strategy AcquisitionAttemptStrategy
	input    CtxPacketInput
}

func buildContextAcquisitionAttempts(projectID, planID, passID string, ctxPlan ContextPlan, ctxBudget *ContextBudget, repoAliases map[string]string, snapshotID string) []contextAcquisitionAttempt {
	plannedMaxSources := contextBudgetInt(ctxBudget, "max_files", defaultContextPacketMaxSources, maxContextPacketSources)
	plannedMaxBytes := contextBudgetInt(ctxBudget, "max_bytes", defaultContextPacketMaxTotalBytes, maxContextPacketTotalBytes)
	plannedMaxResults := contextBudgetInt(ctxBudget, "max_search_results", defaultSeedSearchMaxResults, maxSeedSearchResults)
	plannedContextLines := contextBudgetInt(ctxBudget, "max_context_lines", 2, 10)
	base := CtxPacketInput{
		ProjectID:        projectID,
		PlanID:           planID,
		PassID:           passID,
		TaskSlug:         safeTaskSlug("next-pass-work", planID, passID),
		SourceSnapshotID: snapshotID,
	}
	planned := base
	planned.IncludeInventory = defaultContextPacketIncludeInventory
	planned.MaxSources = plannedMaxSources
	planned.MaxTotalBytes = plannedMaxBytes
	planned.SeedFiles = buildCtxSeedFiles(ctxPlan, repoAliases, false)
	planned.SeedSearches = buildCtxSeedSearches(ctxPlan, repoAliases, plannedMaxResults, plannedContextLines, false, false)

	focused := base
	focused.TaskSlug = safeTaskSlug("next-pass-work-focused", planID, passID)
	focused.IncludeInventory = false
	focused.MaxSources = minInt(80, maxContextPacketSources)
	focused.MaxTotalBytes = minInt(600000, maxContextPacketTotalBytes)
	focused.SeedFiles = buildCtxSeedFiles(ctxPlan, repoAliases, false)
	focused.SeedSearches = buildCtxSeedSearches(ctxPlan, repoAliases, 10, 2, true, true)

	return []contextAcquisitionAttempt{
		{
			strategy: AcquisitionAttemptStrategy{
				Name:             "planned_context_budget",
				IncludeInventory: planned.IncludeInventory,
				MaxSources:       planned.MaxSources,
				MaxTotalBytes:    planned.MaxTotalBytes,
				MaxSearchResults: plannedMaxResults,
				ContextLines:     plannedContextLines,
			},
			input: planned,
		},
		{
			strategy: AcquisitionAttemptStrategy{
				Name:             "focused_required_context",
				IncludeInventory: focused.IncludeInventory,
				MaxSources:       focused.MaxSources,
				MaxTotalBytes:    focused.MaxTotalBytes,
				MaxSearchResults: 10,
				ContextLines:     2,
			},
			input: focused,
		},
	}
}

func buildCtxSeedFiles(ctxPlan ContextPlan, repoAliases map[string]string, requiredOnly bool) []CtxSeedFile {
	required := make([]CtxSeedFile, 0, len(ctxPlan.SeedFilesToRead))
	optional := make([]CtxSeedFile, 0, len(ctxPlan.SeedFilesToRead))
	for _, seed := range ctxPlan.SeedFilesToRead {
		repoID := normalizeContextRepoID(seed.RepoID, repoAliases)
		path := strings.TrimSpace(seed.Path)
		reason := strings.TrimSpace(seed.Purpose)
		if repoID == "" || path == "" || reason == "" || isLocalAbsolutePath(path) {
			continue
		}
		item := CtxSeedFile{
			RepoID:   repoID,
			Path:     path,
			Reason:   reason,
			Required: boolValue(seed.Required),
		}
		if item.Required {
			required = append(required, item)
		} else if !requiredOnly {
			optional = append(optional, item)
		}
	}
	return append(required, optional...)
}

func buildCtxSeedSearches(ctxPlan ContextPlan, repoAliases map[string]string, maxResults, contextLines int, requiredOnly bool, omitOptional bool) []CtxSeedSearch {
	required := make([]CtxSeedSearch, 0, len(ctxPlan.SeedSearchTerms))
	optional := make([]CtxSeedSearch, 0, len(ctxPlan.SeedSearchTerms))
	for _, seed := range ctxPlan.SeedSearchTerms {
		query := strings.TrimSpace(seed.Query)
		reason := strings.TrimSpace(seed.Purpose)
		if query == "" || reason == "" {
			continue
		}
		repoIDs := normalizeContextRepoIDs([]string{seed.RepoID}, repoAliases)
		item := CtxSeedSearch{
			RepoIDs:      repoIDs,
			Pattern:      query,
			Reason:       reason,
			Required:     boolValue(seed.Required),
			MaxResults:   maxResults,
			ContextLines: contextLines,
		}
		if item.Required {
			required = append(required, item)
		} else if !requiredOnly && !omitOptional {
			optional = append(optional, item)
		}
	}
	return append(required, optional...)
}

func contextPacketResultUsableForHandoff(result *CtxPacketResult, selectedSourceSnapshotID string) (bool, string) {
	if result == nil {
		return false, "context packet result is empty"
	}
	status := strings.TrimSpace(result.Status)
	if status != "created" {
		return false, fmt.Sprintf("packet status is %q, expected \"created\"", status)
	}
	if result.BlockedSeedCount > 0 {
		return false, fmt.Sprintf("packet has %d blocked seeds", result.BlockedSeedCount)
	}
	if result.MissingSeedCount > 0 {
		return false, fmt.Sprintf("packet has %d missing seeds", result.MissingSeedCount)
	}
	if result.Truncated {
		return false, "packet is truncated"
	}
	if strings.TrimSpace(result.CoverageReportPath) == "" {
		return false, "packet is missing coverage report path"
	}
	if selectedSourceSnapshotID != "" && result.SourceSnapshotID != "" && result.SourceSnapshotID != selectedSourceSnapshotID {
		return false, fmt.Sprintf("packet source snapshot ID %q does not match selected source snapshot ID %q", result.SourceSnapshotID, selectedSourceSnapshotID)
	}
	return true, ""
}

func blockerForContextPacketResult(result *CtxPacketResult, reason string) *WorkBlocker {
	if result != nil && result.Status == "blocked" {
		return &WorkBlocker{
			Code:        BlockerContextCoverageIncomplete,
			Message:     fmt.Sprintf("context packet %q was blocked: %d blocked seeds, %d missing seeds", result.ContextPacketID, result.BlockedSeedCount, result.MissingSeedCount),
			Recoverable: true,
		}
	}
	if result != nil && result.Truncated {
		return &WorkBlocker{
			Code:        BlockerContextPacketTruncated,
			Message:     fmt.Sprintf("context packet %q is truncated (source count=%d)", result.ContextPacketID, result.SourceCount),
			Recoverable: true,
		}
	}
	if reason == "" {
		reason = "context packet is unusable"
	}
	return &WorkBlocker{
		Code:        BlockerContextPacketUnusable,
		Message:     reason,
		Recoverable: true,
	}
}

func buildAcquisitionFailureReport(snapshotID string, blocker *WorkBlocker, attempts []AcquisitionAttemptReport, result *CtxPacketResult) *AcquisitionFailureReport {
	packetID, packetStatus := "", ""
	if result != nil {
		packetID = result.ContextPacketID
		packetStatus = result.Status
	}
	return &AcquisitionFailureReport{
		Stage:                     "context_packet_acquisition",
		FailureCode:               blocker.Code,
		ReadinessState:            "context_acquisition_failed",
		SourceSnapshotID:          snapshotID,
		ContextPacketID:           packetID,
		ContextPacketStatus:       packetStatus,
		TerminalReason:            blocker.Message,
		AttemptedStrategies:       attempts,
		PacketSummary:             packetDiagnosticSummary(result),
		CoverageSummary:           coverageDiagnosticSummary(result),
		RecommendedOperatorAction: "Inspect acquisition_failure_report coverage and packet_summary, adjust required seeds or budgets, then call get_next_pass_work again after inputs are corrected.",
	}
}

func packetDiagnosticSummary(result *CtxPacketResult) *ContextPacketDiagnosticSummary {
	if result == nil {
		return nil
	}
	limitHit := result.LimitHit
	if limitHit == "" {
		limitHit = "unknown"
	}
	summary := result.Summary
	if summary.SourceCount == 0 && result.SourceCount > 0 {
		summary.SourceCount = result.SourceCount
	}
	if summary.BlockedSeedCount == 0 && result.BlockedSeedCount > 0 {
		summary.BlockedSeedCount = result.BlockedSeedCount
	}
	if summary.MissingSeedCount == 0 && result.MissingSeedCount > 0 {
		summary.MissingSeedCount = result.MissingSeedCount
	}
	if !summary.Truncated && result.Truncated {
		summary.Truncated = result.Truncated
	}
	return &ContextPacketDiagnosticSummary{
		MaxSources:        summary.MaxSources,
		MaxTotalBytes:     summary.MaxTotalBytes,
		TotalSourceBytes:  summary.TotalSourceBytes,
		SourceCount:       summary.SourceCount,
		CoveredSeedCount:  summary.CoveredSeedCount,
		BlockedSeedCount:  summary.BlockedSeedCount,
		MissingSeedCount:  summary.MissingSeedCount,
		Truncated:         summary.Truncated,
		InventoryIncluded: summary.InventoryIncluded,
		LimitHit:          limitHit,
	}
}

func coverageDiagnosticSummary(result *CtxPacketResult) *ContextCoverageDiagnosticSummary {
	if result == nil {
		return nil
	}
	out := &ContextCoverageDiagnosticSummary{
		EntryCount: len(result.Coverage),
		Entries:    make([]ContextCoverageDiagnostic, 0, minInt(len(result.Coverage), 40)),
	}
	for i, entry := range result.Coverage {
		switch entry.Status {
		case "covered":
			out.CoveredCount++
		case "blocked":
			out.BlockedCount++
		case "missing":
			out.MissingCount++
		default:
			out.PartialCount++
		}
		if entry.Truncated {
			out.TruncatedCount++
		}
		if entry.Required {
			out.RequiredCount++
			if entry.Status == "covered" {
				out.RequiredCovered++
			}
		}
		if i >= 40 {
			continue
		}
		out.Entries = append(out.Entries, ContextCoverageDiagnostic{
			SeedID:       entry.SeedID,
			SeedType:     entry.SeedType,
			Required:     entry.Required,
			Path:         entry.Path,
			Pattern:      entry.Pattern,
			Reason:       entry.Reason,
			Status:       entry.Status,
			Truncated:    entry.Truncated,
			MissingCause: entry.MissingCause,
			Blockers:     append([]CtxSourceBlocker(nil), entry.Blockers...),
			SourceIDs:    append([]string(nil), entry.SourceIDs...),
		})
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
