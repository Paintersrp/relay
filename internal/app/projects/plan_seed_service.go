package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"relay/internal/store"

	"github.com/google/uuid"
)

func newPlanSeedID() string {
	return "seed-" + uuid.NewString()
}

func parseJSONStringSlice(jsonStr string) []string {
	var res []string
	if jsonStr == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return []string{}
	}
	if res == nil {
		return []string{}
	}
	return res
}

func mapPlanSeed(row *store.PlanSeed) PlanSeedResult {
	return PlanSeedResult{
		SeedID:        row.SeedID,
		ProjectID:     row.ProjectID,
		Title:         row.Title,
		QuickContext:  row.QuickContext,
		Constraints:   parseJSONStringSlice(row.ConstraintsJson),
		NonGoals:      parseJSONStringSlice(row.NonGoalsJson),
		Tags:          parseJSONStringSlice(row.TagsJson),
		Priority:      row.Priority,
		Status:        row.Status,
		SourceType:    row.SourceType,
		SourceLabel:   row.SourceLabel,
		SourceRefID:   row.SourceRefID,
		PlanAttemptID: row.PlanAttemptID,
		ManagedPlanID: row.ManagedPlanID,
		PlannedAt:     row.PlannedAt,
		DeferReason:   row.DeferReason,
		RejectReason:  row.RejectReason,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func planSeedTransitionIssue(status, action string) PlanSeedValidationIssue {
	return planSeedValidationIssue("status", PlanSeedIssueInvalidTransition, fmt.Sprintf("cannot %s seed in %s status", action, status))
}

func (s *Service) resolvePlanSeedProject(projectID string) (*store.Project, error) {
	return s.store.GetProjectByProjectID(projectID)
}

func (s *Service) loadPlanSeed(projectID, seedID string) (*store.Project, *store.PlanSeed, error) {
	project, err := s.resolvePlanSeedProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	seed, err := s.store.GetPlanSeedBySeedID(project.ID, seedID)
	if err != nil {
		return nil, nil, err
	}
	return project, seed, nil
}

func (s *Service) CreatePlanSeed(ctx context.Context, projectID string, input PlanSeedInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, err := s.resolvePlanSeedProject(projectID)
	if err != nil {
		return nil, nil, err
	}

	if strings.TrimSpace(input.SeedID) == "" {
		input.SeedID = newPlanSeedID()
	}

	normalized, issues := NormalizePlanSeedInput(input, true)
	if len(issues) > 0 {
		return nil, issues, nil
	}

	existing, err := s.store.GetPlanSeedBySeedID(project.ID, normalized.SeedID)
	if err == nil && existing != nil {
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("seed_id", "duplicate", "seed_id already exists")}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	seed, err := s.store.CreatePlanSeed(store.CreatePlanSeedParams{
		SeedID:          normalized.SeedID,
		ProjectRowID:    project.ID,
		ProjectID:       project.ProjectID,
		Title:           normalized.Title,
		QuickContext:    normalized.QuickContext,
		ConstraintsJSON: normalized.ConstraintsJSON,
		NonGoalsJSON:    normalized.NonGoalsJSON,
		TagsJSON:        normalized.TagsJSON,
		Priority:        normalized.Priority,
		Status:          PlanSeedStatusCaptured,
		SourceType:      normalized.SourceType,
		SourceLabel:     normalized.SourceLabel,
		SourceRefID:     normalized.SourceRefID,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(seed)
	return &res, nil, nil
}

func (s *Service) GetPlanSeed(ctx context.Context, projectID, seedID string) (*PlanSeedResult, error) {
	_ = ctx
	_, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, err
	}
	res := mapPlanSeed(seed)
	return &res, nil
}

func (s *Service) ListPlanSeeds(ctx context.Context, projectID, status string, limit int64) ([]PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, err := s.resolvePlanSeedProject(projectID)
	if err != nil {
		return nil, nil, err
	}

	limit = normalizePlanSeedListLimit(limit)

	var seeds []store.PlanSeed
	if status != "" {
		status = strings.TrimSpace(status)
		if !isValidPlanSeedStatus(status) {
			return nil, []PlanSeedValidationIssue{planSeedValidationIssue("status", PlanSeedIssueInvalidStatus, "status is invalid")}, nil
		}
		seeds, err = s.store.ListPlanSeedsByProjectAndStatus(store.ListPlanSeedsByProjectAndStatusParams{
			ProjectRowID: project.ID,
			Status:       status,
			Limit:        limit,
		})
	} else {
		seeds, err = s.store.ListPlanSeedsByProject(store.ListPlanSeedsByProjectParams{
			ProjectRowID: project.ID,
			Limit:        limit,
		})
	}
	if err != nil {
		return nil, nil, err
	}

	results := make([]PlanSeedResult, len(seeds))
	for i := range seeds {
		results[i] = mapPlanSeed(&seeds[i])
	}
	return results, nil, nil
}

func (s *Service) UpdatePlanSeed(ctx context.Context, projectID, seedID string, input PlanSeedInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	if seed.Status != PlanSeedStatusCaptured && seed.Status != PlanSeedStatusDeferred {
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("status", PlanSeedIssueTerminalStatus, fmt.Sprintf("cannot update seed in %s status", seed.Status))}, nil
	}

	input.SeedID = seedID
	normalized, issues := NormalizePlanSeedInput(input, true)
	if len(issues) > 0 {
		return nil, issues, nil
	}

	updated, err := s.store.UpdatePlanSeedCaptureFields(store.UpdatePlanSeedCaptureFieldsParams{
		ProjectRowID:    project.ID,
		SeedID:          seed.SeedID,
		Title:           normalized.Title,
		QuickContext:    normalized.QuickContext,
		ConstraintsJSON: normalized.ConstraintsJSON,
		NonGoalsJSON:    normalized.NonGoalsJSON,
		TagsJSON:        normalized.TagsJSON,
		Priority:        normalized.Priority,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}

func (s *Service) DeferPlanSeed(ctx context.Context, projectID, seedID string, input PlanSeedLifecycleInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	if seed.Status != PlanSeedStatusCaptured {
		return nil, []PlanSeedValidationIssue{planSeedTransitionIssue(seed.Status, "defer")}, nil
	}

	deferReason := strings.TrimSpace(input.DeferReason)
	var issues []PlanSeedValidationIssue
	if deferReason != "" {
		issues = append(issues, scanPlanSeedSecretLikeStrings("defer_reason", deferReason)...)
	}
	if len(issues) > 0 {
		return nil, issues, nil
	}

	updated, err := s.store.UpdatePlanSeedStatusMetadata(store.UpdatePlanSeedStatusMetadataParams{
		ProjectRowID: project.ID,
		SeedID:       seed.SeedID,
		Status:       PlanSeedStatusDeferred,
		DeferReason:  deferReason,
		RejectReason: seed.RejectReason,
		PlannedAt:    seed.PlannedAt,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}

func (s *Service) RelaunchDeferredPlanSeed(ctx context.Context, projectID, seedID string) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	if seed.Status != PlanSeedStatusDeferred {
		return nil, []PlanSeedValidationIssue{planSeedTransitionIssue(seed.Status, "relaunch")}, nil
	}

	updated, err := s.store.UpdatePlanSeedStatusMetadata(store.UpdatePlanSeedStatusMetadataParams{
		ProjectRowID: project.ID,
		SeedID:       seed.SeedID,
		Status:       PlanSeedStatusCaptured,
		DeferReason:  "",
		RejectReason: "",
		PlannedAt:    seed.PlannedAt,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}

func (s *Service) RejectPlanSeed(ctx context.Context, projectID, seedID string, input PlanSeedLifecycleInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	if seed.Status != PlanSeedStatusCaptured && seed.Status != PlanSeedStatusDeferred {
		return nil, []PlanSeedValidationIssue{planSeedTransitionIssue(seed.Status, "reject")}, nil
	}

	rejectReason := strings.TrimSpace(input.RejectReason)
	var issues []PlanSeedValidationIssue
	if rejectReason != "" {
		issues = append(issues, scanPlanSeedSecretLikeStrings("reject_reason", rejectReason)...)
	}
	if len(issues) > 0 {
		return nil, issues, nil
	}

	updated, err := s.store.UpdatePlanSeedStatusMetadata(store.UpdatePlanSeedStatusMetadataParams{
		ProjectRowID: project.ID,
		SeedID:       seed.SeedID,
		Status:       PlanSeedStatusRejected,
		DeferReason:  seed.DeferReason,
		RejectReason: rejectReason,
		PlannedAt:    seed.PlannedAt,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}

func (s *Service) LinkPlanSeedAttempt(ctx context.Context, projectID, seedID string, input PlanSeedAttemptLinkInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	attemptID := strings.TrimSpace(input.PlanAttemptID)
	if attemptID == "" {
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("plan_attempt_id", PlanSeedIssueRequired, "plan_attempt_id is required")}, nil
	}
	if issues := scanPlanSeedSecretLikeStrings("plan_attempt_id", attemptID); len(issues) > 0 {
		return nil, issues, nil
	}

	if seed.Status == PlanSeedStatusPlanned {
		if seed.PlanAttemptID == attemptID {
			res := mapPlanSeed(seed)
			return &res, nil, nil
		}
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("plan_attempt_id", PlanSeedIssueDuplicateLinkage, "cannot replace plan_attempt_id with a different one")}, nil
	}

	if seed.Status != PlanSeedStatusCaptured {
		return nil, []PlanSeedValidationIssue{planSeedTransitionIssue(seed.Status, "link attempt")}, nil
	}

	updated, err := s.store.MarkPlanSeedPlanned(store.MarkPlanSeedPlannedParams{
		ProjectRowID:  project.ID,
		SeedID:        seed.SeedID,
		PlanAttemptID: attemptID,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}

func (s *Service) LinkPlanSeedManagedPlan(ctx context.Context, projectID, seedID string, input PlanSeedManagedPlanLinkInput) (*PlanSeedResult, []PlanSeedValidationIssue, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, nil, err
	}

	managedPlanID := strings.TrimSpace(input.ManagedPlanID)
	if managedPlanID == "" {
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("managed_plan_id", PlanSeedIssueRequired, "managed_plan_id is required")}, nil
	}
	if issues := scanPlanSeedSecretLikeStrings("managed_plan_id", managedPlanID); len(issues) > 0 {
		return nil, issues, nil
	}

	if seed.Status != PlanSeedStatusPlanned {
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("status", PlanSeedIssueInvalidTransition, "only planned seeds can link managed plans")}, nil
	}

	if seed.ManagedPlanID != "" {
		if seed.ManagedPlanID == managedPlanID {
			res := mapPlanSeed(seed)
			return &res, nil, nil
		}
		return nil, []PlanSeedValidationIssue{planSeedValidationIssue("managed_plan_id", PlanSeedIssueDuplicateLinkage, "cannot replace managed_plan_id with a different one")}, nil
	}

	updated, err := s.store.LinkPlanSeedManagedPlan(store.LinkPlanSeedManagedPlanParams{
		ProjectRowID:  project.ID,
		SeedID:        seed.SeedID,
		ManagedPlanID: managedPlanID,
	})
	if err != nil {
		return nil, nil, err
	}

	res := mapPlanSeed(updated)
	return &res, nil, nil
}
