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

func (svc *RunLifecycleService) MarkAssociatedPassInProgress(run *store.Run) error {
	if run == nil || !run.PlanPassRowID.Valid {
		return nil
	}

	pass, err := svc.store.GetPlanPass(run.PlanPassRowID.Int64)
	if err != nil {
		return fmt.Errorf("get associated plan pass: %w", err)
	}

	switch pass.Status {
	case "planned", "ready_for_planner", "handoff_ready", "run_created", "revision_required":
		_, err := svc.store.UpdatePlanPassStatus(pass.ID, "in_progress")
		if err != nil {
			return fmt.Errorf("update plan pass to in_progress: %w", err)
		}
	default:
		// "in_progress", "audit_ready", "completed", "blocked", "skipped", etc. remain unchanged
		return nil
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
		if pass.Status != "completed" && pass.Status != "skipped" {
			return false, nil
		}
	}

	return true, nil
}

func planPassStatusForAuditDecision(currentStatus string, decision string) (string, error) {
	if currentStatus == "completed" || currentStatus == "skipped" {
		return "", nil
	}

	switch decision {
	case "accepted", "accepted_with_warnings":
		return "completed", nil
	case "revision_required":
		return "revision_required", nil
	case "blocked", "manual_review_required", "rejected":
		return "blocked", nil
	default:
		return "", fmt.Errorf("unsupported audit decision %q", decision)
	}
}
