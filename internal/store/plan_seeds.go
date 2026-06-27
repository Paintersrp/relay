package store

import (
	"context"

	"relay/internal/store/generated"
)

type CreatePlanSeedParams struct {
	SeedID          string
	ProjectRowID    int64
	ProjectID       string
	Title           string
	QuickContext    string
	ConstraintsJSON string
	NonGoalsJSON    string
	TagsJSON        string
	Priority        string
	Status          string
	SourceType      string
	SourceLabel     string
	SourceRefID     string
	PlanAttemptID   string
	ManagedPlanID   string
	PlannedAt       string
	DeferReason     string
	RejectReason    string
}

type ListPlanSeedsByProjectParams struct {
	ProjectRowID int64
	Limit        int64
}

type ListPlanSeedsByProjectAndStatusParams struct {
	ProjectRowID int64
	Status       string
	Limit        int64
}

type UpdatePlanSeedCaptureFieldsParams struct {
	ProjectRowID    int64
	SeedID          string
	Title           string
	QuickContext    string
	ConstraintsJSON string
	NonGoalsJSON    string
	TagsJSON        string
	Priority        string
}

type UpdatePlanSeedStatusMetadataParams struct {
	ProjectRowID int64
	SeedID       string
	Status       string
	DeferReason  string
	RejectReason string
	PlannedAt    string
}

type MarkPlanSeedPlannedParams struct {
	ProjectRowID  int64
	SeedID        string
	PlanAttemptID string
}

type LinkPlanSeedManagedPlanParams struct {
	ProjectRowID  int64
	SeedID        string
	ManagedPlanID string
}

func (s *Store) CreatePlanSeed(params CreatePlanSeedParams) (*PlanSeed, error) {
	seed, err := s.queries.CreatePlanSeed(context.Background(), generated.CreatePlanSeedParams{
		SeedID:          params.SeedID,
		ProjectRowID:    params.ProjectRowID,
		ProjectID:       params.ProjectID,
		Title:           params.Title,
		QuickContext:    params.QuickContext,
		ConstraintsJson: params.ConstraintsJSON,
		NonGoalsJson:    params.NonGoalsJSON,
		TagsJson:        params.TagsJSON,
		Priority:        params.Priority,
		Status:          params.Status,
		SourceType:      params.SourceType,
		SourceLabel:     params.SourceLabel,
		SourceRefID:     params.SourceRefID,
		PlanAttemptID:   params.PlanAttemptID,
		ManagedPlanID:   params.ManagedPlanID,
		PlannedAt:       params.PlannedAt,
		DeferReason:     params.DeferReason,
		RejectReason:    params.RejectReason,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) GetPlanSeedBySeedID(projectRowID int64, seedID string) (*PlanSeed, error) {
	seed, err := s.queries.GetPlanSeedBySeedID(context.Background(), generated.GetPlanSeedBySeedIDParams{
		ProjectRowID: projectRowID,
		SeedID:       seedID,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) ListPlanSeedsByProject(params ListPlanSeedsByProjectParams) ([]PlanSeed, error) {
	return s.queries.ListPlanSeedsByProject(context.Background(), generated.ListPlanSeedsByProjectParams{
		ProjectRowID: params.ProjectRowID,
		Limit:        params.Limit,
	})
}

func (s *Store) ListPlanSeedsByProjectAndStatus(params ListPlanSeedsByProjectAndStatusParams) ([]PlanSeed, error) {
	return s.queries.ListPlanSeedsByProjectAndStatus(context.Background(), generated.ListPlanSeedsByProjectAndStatusParams{
		ProjectRowID: params.ProjectRowID,
		Status:       params.Status,
		Limit:        params.Limit,
	})
}

func (s *Store) UpdatePlanSeedCaptureFields(params UpdatePlanSeedCaptureFieldsParams) (*PlanSeed, error) {
	seed, err := s.queries.UpdatePlanSeedCaptureFields(context.Background(), generated.UpdatePlanSeedCaptureFieldsParams{
		Title:           params.Title,
		QuickContext:    params.QuickContext,
		ConstraintsJson: params.ConstraintsJSON,
		NonGoalsJson:    params.NonGoalsJSON,
		TagsJson:        params.TagsJSON,
		Priority:        params.Priority,
		ProjectRowID:    params.ProjectRowID,
		SeedID:          params.SeedID,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) UpdatePlanSeedStatusMetadata(params UpdatePlanSeedStatusMetadataParams) (*PlanSeed, error) {
	seed, err := s.queries.UpdatePlanSeedStatusMetadata(context.Background(), generated.UpdatePlanSeedStatusMetadataParams{
		Status:       params.Status,
		DeferReason:  params.DeferReason,
		RejectReason: params.RejectReason,
		PlannedAt:    params.PlannedAt,
		ProjectRowID: params.ProjectRowID,
		SeedID:       params.SeedID,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) MarkPlanSeedPlanned(params MarkPlanSeedPlannedParams) (*PlanSeed, error) {
	seed, err := s.queries.MarkPlanSeedPlanned(context.Background(), generated.MarkPlanSeedPlannedParams{
		PlanAttemptID: params.PlanAttemptID,
		ProjectRowID:  params.ProjectRowID,
		SeedID:        params.SeedID,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}

func (s *Store) LinkPlanSeedManagedPlan(params LinkPlanSeedManagedPlanParams) (*PlanSeed, error) {
	seed, err := s.queries.LinkPlanSeedManagedPlan(context.Background(), generated.LinkPlanSeedManagedPlanParams{
		ManagedPlanID: params.ManagedPlanID,
		ProjectRowID:  params.ProjectRowID,
		SeedID:        params.SeedID,
	})
	if err != nil {
		return nil, err
	}
	return &seed, nil
}
