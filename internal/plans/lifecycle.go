package plans

import (
	"fmt"
	"time"

	"relay/internal/store"

	"github.com/google/uuid"
)

type RunLifecycleService struct {
	store *store.Store
}

func NewRunLifecycleService(s *store.Store) *RunLifecycleService {
	return &RunLifecycleService{store: s}
}

// isTerminalPassStatus reports whether a pass status is terminal and must never
// be downgraded by run-status synchronization.
func isTerminalPassStatus(status string) bool {
	return status == StatusPassCompleted || status == StatusPassSkipped
}

// MarkAssociatedPassRunCreated marks a managed pass as run_created when a run is
// created from a reviewed Planner handoff. It is a no-op for standalone runs,
// plan-only runs, and terminal passes. Intake approval (not run creation) is the
// first transition to in_progress.
func (svc *RunLifecycleService) MarkAssociatedPassRunCreated(run *store.Run) error {
	if run == nil || !run.PlanPassRowID.Valid {
		return nil
	}

	pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
	if err != nil {
		return fmt.Errorf("get associated plan pass: %w", err)
	}

	if isTerminalPassStatus(pass.Status) {
		return nil
	}

	switch pass.Status {
	case StatusPassPlanned, StatusPassReadyForPlanner, StatusPassHandoffReady, StatusPassRevisionRequired:
		if _, err := svc.store.UpdatePlanPassStatus(pass.ID, StatusPassRunCreated); err != nil {
			return fmt.Errorf("update plan pass to run_created: %w", err)
		}
	default:
		// run_created, in_progress, audit_ready, blocked remain unchanged.
		return nil
	}

	return nil
}

func (svc *RunLifecycleService) MarkAssociatedPassInProgress(run *store.Run) error {
	if run == nil || !run.PlanPassRowID.Valid {
		return nil
	}

	pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
	if err != nil {
		return fmt.Errorf("get associated plan pass: %w", err)
	}

	if isTerminalPassStatus(pass.Status) {
		return nil
	}

	switch pass.Status {
	case StatusPassPlanned, StatusPassReadyForPlanner, StatusPassHandoffReady, StatusPassRunCreated, StatusPassRevisionRequired:
		if _, err := svc.store.UpdatePlanPassStatus(pass.ID, StatusPassInProgress); err != nil {
			return fmt.Errorf("update plan pass to in_progress: %w", err)
		}
	default:
		// in_progress, audit_ready, blocked remain unchanged.
		return nil
	}

	return nil
}

// SyncAssociatedPassForRunStatus deterministically synchronizes the associated
// managed pass status from a run's current status. It is a no-op for standalone
// runs, plan-only runs, terminal passes, and unmapped run statuses.
func (svc *RunLifecycleService) SyncAssociatedPassForRunStatus(run *store.Run) error {
	if run == nil || !run.PlanPassRowID.Valid {
		return nil
	}

	pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
	if err != nil {
		return fmt.Errorf("get associated plan pass: %w", err)
	}

	targetStatus, err := planPassStatusForRunStatus(pass.Status, run.Status)
	if err != nil {
		return err
	}
	if targetStatus == "" || targetStatus == pass.Status {
		return nil
	}

	if _, err := svc.store.UpdatePlanPassStatus(pass.ID, targetStatus); err != nil {
		return fmt.Errorf("update plan pass to %s: %w", targetStatus, err)
	}

	return nil
}

func (svc *RunLifecycleService) ApplyAuditDecision(run *store.Run, decision string) error {
	if run == nil || !run.PlanPassRowID.Valid {
		return nil
	}

	pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
	if err != nil {
		return fmt.Errorf("get associated plan pass: %w", err)
	}

	targetStatus, err := planPassStatusForAuditDecision(pass.Status, decision)
	if err != nil {
		return err
	}

	// Preflight the scheduled refactor candidate mapping BEFORE mutating the
	// managed pass status. For audit decisions that map a candidate status, a
	// stale/missing/mismatched/malformed scheduling reference must fail closed
	// here so neither the pass status nor the candidate status is partially
	// mutated. This runs only at this audit/lifecycle boundary -- never during
	// read-only work-packet retrieval.
	prepared, err := svc.prepareRefactorCandidateAuditMapping(pass, decision)
	if err != nil {
		return err
	}

	if targetStatus != "" && targetStatus != pass.Status {
		if _, err := svc.store.UpdatePlanPassStatus(pass.ID, targetStatus); err != nil {
			return fmt.Errorf("update plan pass to %s: %w", targetStatus, err)
		}
	}

	if prepared != nil && !prepared.noop {
		if err := svc.applyPreparedRefactorCandidateTransition(prepared); err != nil {
			return err
		}
	}

	return nil
}

// refactorCandidateStatusForAuditDecision maps an explicit audit decision to a
// scheduled refactor candidate target status. An empty status means "no candidate
// mutation": blocked, manual_review_required, and rejected outcomes must never
// auto-reject, defer, or reset a candidate; those require an explicit user
// decision through the candidate lifecycle.
func refactorCandidateStatusForAuditDecision(decision string) (status string, reason string) {
	switch decision {
	case "accepted":
		return refactorCandidateStatusCompleted, "scheduled refactor pass audit accepted"
	case "accepted_with_warnings":
		return refactorCandidateStatusCompletedWithWarnings, "scheduled refactor pass audit accepted with warnings"
	case "revision_required":
		return refactorCandidateStatusScheduledRevisionRequired, "scheduled refactor pass audit revision required"
	default:
		return "", ""
	}
}

// preparedRefactorCandidateTransition carries the resolved, validated state for
// a scheduled refactor candidate transition. It is produced by a read-only
// preflight and consumed by the mutation step, so validation can happen before
// any managed pass status update. A prepared transition with noop set true means
// the preflight resolved a refactor candidate that requires no mutation (terminal
// candidate, non-active candidate, or already at the target status).
type preparedRefactorCandidateTransition struct {
	project      *store.Project
	plan         *store.Plan
	pass         *store.PlanPass
	candidate    *store.RefactorCandidate
	targetStatus string
	reason       string
	noop         bool
}

// prepareRefactorCandidateAuditMapping maps an explicit audit decision to a
// scheduled refactor candidate target status and runs the read-only preflight.
// It returns (nil, nil) for decisions that must not mutate a candidate and for
// non-refactor passes. It returns an error when the pass is a scheduled refactor
// pass whose scheduling reference is missing, mismatched, stale, or malformed.
func (svc *RunLifecycleService) prepareRefactorCandidateAuditMapping(pass *store.PlanPass, decision string) (*preparedRefactorCandidateTransition, error) {
	targetStatus, reason := refactorCandidateStatusForAuditDecision(decision)
	if targetStatus == "" {
		return nil, nil
	}
	return svc.prepareScheduledRefactorCandidateTransition(pass, targetStatus, reason)
}

// ApplyRefactorCandidateForSkippedPass maps a skipped scheduled refactor pass to
// a deferred candidate. It is a service-only lifecycle helper: this pass does not
// introduce a public skip route, so this helper exists for an existing/future
// skip flow to call. It is a no-op for non-refactor passes and for candidates
// that are not in an active scheduled state.
func (svc *RunLifecycleService) ApplyRefactorCandidateForSkippedPass(pass *store.PlanPass) error {
	return svc.applyScheduledRefactorCandidateTransition(pass, refactorCandidateStatusDeferred, "scheduled refactor pass skipped")
}

// applyScheduledRefactorCandidateTransition resolves the scheduled refactor
// candidate behind a pass and applies a target status. It fails closed on a
// stale/mismatched schedule reference, never downgrades terminal candidates, and
// is idempotent (re-applying the same status is a no-op).
func (svc *RunLifecycleService) applyScheduledRefactorCandidateTransition(pass *store.PlanPass, targetStatus, reason string) error {
	prepared, err := svc.prepareScheduledRefactorCandidateTransition(pass, targetStatus, reason)
	if err != nil {
		return err
	}
	if prepared == nil || prepared.noop {
		return nil
	}
	return svc.applyPreparedRefactorCandidateTransition(prepared)
}

// prepareScheduledRefactorCandidateTransition is the read-only preflight for a
// scheduled refactor candidate transition. It resolves refactor metadata, plan,
// project, and candidate, and validates the active schedule reference WITHOUT
// mutating any state. It fails closed (returns an error) on a stale/mismatched/
// malformed scheduling reference, returns (nil, nil) for non-refactor passes, and
// returns a noop prepared transition for terminal/non-active candidates and for
// candidates already at the target status.
func (svc *RunLifecycleService) prepareScheduledRefactorCandidateTransition(pass *store.PlanPass, targetStatus, reason string) (*preparedRefactorCandidateTransition, error) {
	meta, err := refactorMetadataFromPass(pass)
	if err != nil {
		return nil, fmt.Errorf("resolve refactor metadata for pass %q: %w", pass.PassID, err)
	}
	if meta == nil {
		return nil, nil
	}

	plan, err := svc.store.GetPlan(pass.PlanRowID)
	if err != nil {
		return nil, fmt.Errorf("get plan for refactor candidate mapping: %w", err)
	}
	project, err := svc.store.GetProject(plan.ProjectRowID)
	if err != nil {
		return nil, fmt.Errorf("get project for refactor candidate mapping: %w", err)
	}

	candidate, err := svc.store.GetRefactorCandidateByCandidateID(project.ID, meta.CandidateID)
	if err != nil {
		return nil, fmt.Errorf("lookup refactor candidate %q: %w", meta.CandidateID, err)
	}

	// Only actively scheduled candidates are mapped. Terminal candidates
	// (completed/completed_with_warnings) and user-decided states are never
	// downgraded or overwritten by lifecycle synchronization.
	switch candidate.Status {
	case refactorCandidateStatusScheduled, refactorCandidateStatusScheduledRevisionRequired:
		// proceed
	default:
		return &preparedRefactorCandidateTransition{noop: true}, nil
	}
	if candidate.Status == targetStatus {
		// Idempotent: avoid duplicate status events on re-application.
		return &preparedRefactorCandidateTransition{noop: true}, nil
	}

	// Fail closed if the scheduling reference no longer matches this plan/pass.
	if _, blocker, err := validateRefactorSchedule(svc.store, project, plan, pass); err != nil {
		return nil, err
	} else if blocker != nil {
		return nil, fmt.Errorf("cannot map scheduled refactor pass outcome to candidate: %s", blocker.Message)
	}

	return &preparedRefactorCandidateTransition{
		project:      project,
		plan:         plan,
		pass:         pass,
		candidate:    candidate,
		targetStatus: targetStatus,
		reason:       reason,
	}, nil
}

// applyPreparedRefactorCandidateTransition mutates the candidate status and
// records a status event for a validated, non-noop prepared transition. It must
// only be called after a successful preflight.
func (svc *RunLifecycleService) applyPreparedRefactorCandidateTransition(prepared *preparedRefactorCandidateTransition) error {
	project := prepared.project
	candidate := prepared.candidate
	targetStatus := prepared.targetStatus
	reason := prepared.reason

	params := store.UpdateRefactorCandidateStatusMetadataParams{
		ProjectRowID: project.ID,
		CandidateID:  candidate.CandidateID,
		Status:       targetStatus,
	}
	switch targetStatus {
	case refactorCandidateStatusCompleted, refactorCandidateStatusCompletedWithWarnings:
		params.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	case refactorCandidateStatusDeferred:
		params.DeferReason = reason
	}

	if _, err := svc.store.UpdateRefactorCandidateStatusMetadata(params); err != nil {
		return fmt.Errorf("update refactor candidate %q to %s: %w", candidate.CandidateID, targetStatus, err)
	}

	_, err := svc.store.CreateRefactorCandidateStatusEvent(store.CreateRefactorCandidateStatusEventParams{
		EventID:        "revent-" + uuid.NewString(),
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: candidate.ID,
		EventType:      targetStatus,
		FromStatus:     candidate.Status,
		ToStatus:       targetStatus,
		Reason:         reason,
	})
	if err != nil {
		return fmt.Errorf("record refactor candidate status event: %w", err)
	}

	return nil
}

func (svc *RunLifecycleService) CompletionReady(planRowID int64) (bool, error) {
	passes, err := svc.store.ListPlanPassesByPlan(planRowID)
	if err != nil {
		return false, fmt.Errorf("list plan passes: %w", err)
	}
	if len(passes) == 0 {
		return false, nil
	}

	for _, pass := range passes {
		if pass.Status != StatusPassCompleted && pass.Status != StatusPassSkipped {
			return false, nil
		}
	}

	return true, nil
}

// planPassStatusForRunStatus maps a backend run status to the target managed pass
// status. An empty target means "no change". Terminal passes are never downgraded.
func planPassStatusForRunStatus(currentPassStatus string, runStatus string) (string, error) {
	if isTerminalPassStatus(currentPassStatus) {
		return "", nil
	}

	switch runStatus {
	case "intake_received", "intake_needs_review":
		return StatusPassRunCreated, nil
	case "approved_for_prepare",
		"packet_validated",
		"repair_validated",
		"brief_ready_for_review",
		"approved_for_executor",
		"executor_dispatched",
		"executor_done",
		"local_validation_running",
		"validation_passed",
		"validation_failed_accepted":
		return StatusPassInProgress, nil
	case "packet_validation_failed",
		"executor_blocked",
		"agent_blocked",
		"validation_failed",
		"blocked":
		return StatusPassBlocked, nil
	case "audit_ready", "audit_ready_for_review":
		return StatusPassAuditReady, nil
	case "revision_required":
		return StatusPassRevisionRequired, nil
	case "accepted", "accepted_with_warnings", "completed":
		return StatusPassCompleted, nil
	default:
		return "", nil
	}
}

func planPassStatusForAuditDecision(currentStatus string, decision string) (string, error) {
	if isTerminalPassStatus(currentStatus) {
		return "", nil
	}

	switch decision {
	case "accepted", "accepted_with_warnings":
		return StatusPassCompleted, nil
	case "revision_required":
		return StatusPassRevisionRequired, nil
	case "blocked", "manual_review_required", "rejected":
		return StatusPassBlocked, nil
	default:
		return "", fmt.Errorf("unsupported audit decision %q", decision)
	}
}
