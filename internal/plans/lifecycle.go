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
	case "planned":
		_, err := svc.store.UpdatePlanPassStatus(pass.ID, "in_progress")
		if err != nil {
			return fmt.Errorf("update plan pass to in_progress: %w", err)
		}
	case "in_progress", "completed", "skipped":
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
	switch decision {
	case "accepted", "accepted_with_warnings":
		if currentStatus == "skipped" || currentStatus == "completed" {
			return "", nil
		}
		return "completed", nil
	case "revision_required":
		if currentStatus == "skipped" || currentStatus == "completed" {
			return "", nil
		}
		return "in_progress", nil
	default:
		return "", fmt.Errorf("unsupported audit decision %q", decision)
	}
}
