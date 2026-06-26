package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/store"
	"relay/internal/store/generated"
)

const defaultManualModelCallWarning = "Running an internal drift review may call a configured model provider. Confirm before continuing."

type PlanReviewSettings struct {
	ProjectID              string
	DriftReviewMode        string
	ModelTier              string
	ManualModelCallWarning string
	CreatedAt              string
	UpdatedAt              string
}

type UpdatePlanReviewSettingsRequest struct {
	ProjectID       string
	DriftReviewMode string
	ModelTier       string
}

type EffectivePlanReviewPolicy struct {
	ProjectID              string
	DriftReviewMode        string
	ModelTier              string
	ManualModelCallWarning string
	Source                 string
}

func (svc *Service) GetPlanReviewSettings(ctx context.Context, projectID string) (*PlanReviewSettings, *PlanAttemptResult, error) {
	project, err := svc.lookupAttemptProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		blocked, err := blockAttempt(BlockerUnknownProject, "project is unknown")
		return nil, blocked, err
	}
	settings, source, err := svc.loadPlanReviewSettings(ctx, *project)
	if err != nil {
		return nil, nil, err
	}
	if source == "default" {
		return settings, nil, nil
	}
	return settings, nil, nil
}

func (svc *Service) UpdatePlanReviewSettings(ctx context.Context, req UpdatePlanReviewSettingsRequest) (*PlanReviewSettings, *PlanAttemptResult, error) {
	project, err := svc.lookupAttemptProject(req.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		blocked, err := blockAttempt(BlockerUnknownProject, "project is unknown")
		return nil, blocked, err
	}
	mode, ok := normalizeDriftReviewMode(req.DriftReviewMode)
	if !ok {
		blocked, err := blockAttempt(BlockerDriftReviewBlocked, "invalid drift_review_mode")
		return nil, blocked, err
	}
	tier, ok := normalizeModelTier(req.ModelTier)
	if !ok {
		blocked, err := blockAttempt(BlockerDriftReviewBlocked, "invalid model_tier")
		return nil, blocked, err
	}
	row, err := generated.New(svc.store.DB()).UpsertPlanReviewSettings(ctx, generated.UpsertPlanReviewSettingsParams{
		ProjectRowID:    project.ID,
		ProjectID:       project.ProjectID,
		DriftReviewMode: mode,
		ModelTier:       tier,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("upsert plan review settings: %w", err)
	}
	settings := mapPlanReviewSettings(row)
	return &settings, nil, nil
}

func (svc *Service) ResolvePlanReviewPolicy(ctx context.Context, projectID string, requestedMode string, requestedTier string) (*EffectivePlanReviewPolicy, *PlanAttemptResult, error) {
	project, err := svc.lookupAttemptProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		blocked, err := blockAttempt(BlockerUnknownProject, "project is unknown")
		return nil, blocked, err
	}
	settings, source, err := svc.loadPlanReviewSettings(ctx, *project)
	if err != nil {
		return nil, nil, err
	}
	mode := settings.DriftReviewMode
	tier := settings.ModelTier
	if strings.TrimSpace(requestedMode) != "" {
		var ok bool
		mode, ok = normalizeDriftReviewMode(requestedMode)
		if !ok {
			blocked, err := blockAttempt(BlockerDriftReviewBlocked, "invalid drift_review_mode")
			return nil, blocked, err
		}
		source = "request"
	}
	if strings.TrimSpace(requestedTier) != "" {
		var ok bool
		tier, ok = normalizeModelTier(requestedTier)
		if !ok {
			blocked, err := blockAttempt(BlockerDriftReviewBlocked, "invalid model_tier")
			return nil, blocked, err
		}
		source = "request"
	}
	return &EffectivePlanReviewPolicy{
		ProjectID:              project.ProjectID,
		DriftReviewMode:        mode,
		ModelTier:              tier,
		ManualModelCallWarning: settings.ManualModelCallWarning,
		Source:                 source,
	}, nil, nil
}

func (svc *Service) loadPlanReviewSettings(ctx context.Context, project store.Project) (*PlanReviewSettings, string, error) {
	row, err := generated.New(svc.store.DB()).GetPlanReviewSettingsByProject(ctx, project.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultPlanReviewSettings(project.ProjectID), "default", nil
		}
		return nil, "", fmt.Errorf("get plan review settings: %w", err)
	}
	settings := mapPlanReviewSettings(row)
	return &settings, "settings", nil
}

func defaultPlanReviewSettings(projectID string) *PlanReviewSettings {
	return &PlanReviewSettings{
		ProjectID:              projectID,
		DriftReviewMode:        DriftReviewModeManual,
		ModelTier:              ModelTierStandard,
		ManualModelCallWarning: defaultManualModelCallWarning,
	}
}

func mapPlanReviewSettings(row store.PlanReviewSetting) PlanReviewSettings {
	return PlanReviewSettings{
		ProjectID:              row.ProjectID,
		DriftReviewMode:        row.DriftReviewMode,
		ModelTier:              row.ModelTier,
		ManualModelCallWarning: row.ManualModelCallWarning,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

func normalizeDriftReviewMode(mode string) (string, bool) {
	switch strings.TrimSpace(mode) {
	case DriftReviewModeDisabled:
		return DriftReviewModeDisabled, true
	case DriftReviewModeManual:
		return DriftReviewModeManual, true
	case DriftReviewModeAutomatic:
		return DriftReviewModeAutomatic, true
	case DriftReviewModeExternal:
		return DriftReviewModeExternal, true
	default:
		return "", false
	}
}

func normalizeModelTier(tier string) (string, bool) {
	switch strings.TrimSpace(tier) {
	case ModelTierEconomy:
		return ModelTierEconomy, true
	case ModelTierStandard:
		return ModelTierStandard, true
	case ModelTierHighAssurance:
		return ModelTierHighAssurance, true
	case ModelTierAutoEscalate:
		return ModelTierAutoEscalate, true
	default:
		return "", false
	}
}
