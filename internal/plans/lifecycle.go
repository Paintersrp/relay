package plans

import (
	"fmt"

	"relay/internal/store"
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
	if targetStatus == "" || targetStatus == pass.Status {
		return nil
	}

	_, err = svc.store.UpdatePlanPassStatus(pass.ID, targetStatus)
	if err != nil {
		return fmt.Errorf("update plan pass to %s: %w", targetStatus, err)
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
