package workflowplans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrProjectNotFound = errors.New("Project not found")
	ErrPlanNotFound    = errors.New("Plan not found")
)

// Service exposes read-only presentation of historical Plans and Passes.
// Legacy Plan and Pass write admission is retired; no operation in this
// package creates or moves Plans.
type Service struct {
	store *workflowstore.Store
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) GetPlan(ctx context.Context, planID string) (GetPlanResult, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return GetPlanResult{}, fmt.Errorf("%w: Plan ID is required", ErrPlanNotFound)
	}
	plan, err := s.store.GetPlanByPlanID(ctx, planID)
	if errors.Is(err, sql.ErrNoRows) {
		return GetPlanResult{}, fmt.Errorf("%w: %s", ErrPlanNotFound, planID)
	}
	if err != nil {
		return GetPlanResult{}, err
	}
	project, err := s.store.GetProjectByRowID(ctx, plan.ProjectRowID)
	if err != nil {
		return GetPlanResult{}, err
	}
	passes, err := s.store.ListPlanPasses(ctx, plan.ID)
	if err != nil {
		return GetPlanResult{}, fmt.Errorf("list Plan passes: %w", err)
	}
	artifacts, err := s.store.ListArtifactsByPlan(ctx, plan.ID)
	if err != nil {
		return GetPlanResult{}, fmt.Errorf("list Plan artifacts: %w", err)
	}
	return GetPlanResult{Project: project, Plan: plan, Passes: passes, Artifacts: artifacts}, nil
}
