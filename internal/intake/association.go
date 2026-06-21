package intake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/store"
)

const (
	ErrCodeValidation = "VALIDATION_ERROR"
	ErrCodeNotFound   = "NOT_FOUND"
)

type InputError struct {
	Code    string
	Message string
}

func (e *InputError) Error() string {
	return e.Message
}

func (e *InputError) Is(target error) bool {
	var other *InputError
	if !errors.As(target, &other) {
		return false
	}
	return e.Code == other.Code
}

type RunPlanAssociation struct {
	PlanID        string
	PassID        string
	PlanRowID     sql.NullInt64
	PlanPassRowID sql.NullInt64
}

func ResolveRunPlanAssociation(ctx context.Context, s *store.Store, planID, passID string) (RunPlanAssociation, error) {
	association := RunPlanAssociation{
		PlanID: strings.TrimSpace(planID),
		PassID: strings.TrimSpace(passID),
	}

	if association.PlanID == "" && association.PassID == "" {
		return association, nil
	}
	if association.PassID != "" && association.PlanID == "" {
		return RunPlanAssociation{}, &InputError{
			Code:    ErrCodeValidation,
			Message: "pass_id requires plan_id",
		}
	}

	plan, err := s.GetPlanByPlanID(association.PlanID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunPlanAssociation{}, &InputError{
				Code:    ErrCodeNotFound,
				Message: fmt.Sprintf("plan_id %q was not found", association.PlanID),
			}
		}
		return RunPlanAssociation{}, fmt.Errorf("lookup plan %q: %w", association.PlanID, err)
	}
	association.PlanRowID = sql.NullInt64{Int64: plan.ID, Valid: true}

	if association.PassID == "" {
		return association, nil
	}

	pass, err := s.GetPlanPassByPassID(plan.ID, association.PassID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunPlanAssociation{}, &InputError{
				Code:    ErrCodeNotFound,
				Message: fmt.Sprintf("pass_id %q was not found under plan_id %q", association.PassID, association.PlanID),
			}
		}
		return RunPlanAssociation{}, fmt.Errorf("lookup pass %q under plan %q: %w", association.PassID, association.PlanID, err)
	}
	association.PlanPassRowID = sql.NullInt64{Int64: pass.ID, Valid: true}

	_ = ctx
	return association, nil
}
