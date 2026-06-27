package projects

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appplans "relay/internal/app/plans"
	"relay/internal/store"
	"relay/internal/store/generated"

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

func (s *Service) GetPlanSeedPlanningContext(ctx context.Context, projectID string, seedID string) (*PlanSeedPlanningContext, error) {
	_ = ctx
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, err
	}
	mapped := mapPlanSeed(seed)
	return &PlanSeedPlanningContext{
		Project: PlanSeedPlanningProject{
			ProjectID:           project.ProjectID,
			Name:                project.Name,
			Description:         project.Description,
			Status:              project.Status,
			DefaultRepositoryID: project.DefaultRepositoryID,
		},
		Seed: mapped,
		ExistingLinks: PlanSeedExistingLinks{
			PlanAttemptID: mapped.PlanAttemptID,
			ManagedPlanID: mapped.ManagedPlanID,
		},
		PlannerInstructions: []string{
			"Use this context to draft a reviewed Plan of Passes JSON.",
			"Do not submit a managed plan from this action.",
			"Do not create runs or dispatch executors from this action.",
			"Use the seed quickContext as the literal source request when registering a draft attempt.",
		},
		RetrievalSemantics: PlanSeedRetrievalSemantics{
			RetrievalOnly:        true,
			StateMutated:         false,
			IntentPacketCreated:  false,
			PlanAttemptCreated:   false,
			ManagedPlanSubmitted: false,
			RunCreated:           false,
			ModelCallPerformed:   false,
		},
	}, nil
}

func (s *Service) CreatePlanAttemptFromSeed(ctx context.Context, projectID, seedID string, input CreatePlanAttemptFromSeedInput) (*CreatePlanAttemptFromSeedResult, error) {
	project, seed, err := s.loadPlanSeed(projectID, seedID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(seed.PlanAttemptID) != "" || seed.Status == PlanSeedStatusPlanned {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerSeedAlreadyPlanned, Message: "plan seed already has a linked plan attempt"}, nil
	}
	if seed.Status != PlanSeedStatusCaptured {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerSeedNotExpandable, Message: fmt.Sprintf("cannot create a draft plan attempt from seed in %s status", seed.Status)}, nil
	}

	sourceArtifactPath := strings.TrimSpace(input.SourceArtifactPath)
	if sourceArtifactPath == "" {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerMissingPlanArtifact, Message: "source_artifact_path is required"}, nil
	}
	if issues := scanPlanSeedSecretLikeStrings("source_artifact_path", sourceArtifactPath); len(issues) > 0 {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerUnsafeSeedContext, Message: issues[0].Message}, nil
	}
	if issues := scanPlanSeedSecretLikeStrings("seed", seed.Title, seed.QuickContext, seed.ConstraintsJson, seed.NonGoalsJson); len(issues) > 0 {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerUnsafeSeedContext, Message: issues[0].Message}, nil
	}

	canonicalPlan, planHash, blockedMessage, err := canonicalPlanSeedPlanJSON(input.PlannerPassPlanJSON)
	if err != nil {
		return nil, err
	}
	if blockedMessage != "" {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerMissingPlanArtifact, Message: blockedMessage}, nil
	}

	attemptReq := appplans.CreatePlanAttemptWithIntentRequest{
		ProjectID: project.ProjectID,
		PlanArtifactRef: appplans.PlanArtifactRef{
			Path:         sourceArtifactPath,
			SHA256:       planHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     canonicalPlan,
		DriftReviewMode: input.DriftReviewMode,
		ModelTier:       input.ModelTier,
		IntentPacket: appplans.IntentPacketInput{
			Summary:            seed.Title,
			LiteralUserRequest: seed.QuickContext,
			Constraints:        parseJSONStringSlice(seed.ConstraintsJson),
			Source: appplans.IntentSource{
				CapturedFrom:       appplans.CapturedFromImportedReq,
				CapturedBy:         "plan_seed_bridge",
				SourceArtifactPath: sourceArtifactPath,
			},
			RedactionStatus: appplans.RedactionStatusVerifiedNoSecrets,
		},
	}
	attemptSvc := appplans.NewService(s.store)
	policy, attemptBlocked, err := attemptSvc.ResolvePlanReviewPolicy(ctx, project.ProjectID, input.DriftReviewMode, input.ModelTier)
	if err != nil {
		return nil, err
	}
	if attemptBlocked != nil {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: mapPlanAttemptBlockerForSeed(attemptBlocked.BlockerCode), Message: attemptBlocked.Message}, nil
	}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create plan attempt from seed transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	attemptResult, err := attemptSvc.CreatePlanAttemptWithIntentInTxWithPolicy(ctx, tx, *project, attemptReq, policy)
	if err != nil {
		return nil, err
	}
	if attemptResult == nil {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerDraftAttemptsUnavailable, Message: "draft plan attempt infrastructure returned no result"}, nil
	}
	if !attemptResult.OK {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: mapPlanAttemptBlockerForSeed(attemptResult.BlockerCode), Message: attemptResult.Message}, nil
	}
	if attemptResult.PlanAttempt == nil || attemptResult.IntentPacket == nil {
		return &CreatePlanAttemptFromSeedResult{OK: false, BlockerCode: PlanSeedBlockerDraftAttemptsUnavailable, Message: "draft plan attempt infrastructure did not create required rows"}, nil
	}

	updatedSeed, err := generated.New(tx).MarkPlanSeedPlanned(ctx, generated.MarkPlanSeedPlannedParams{
		ProjectRowID:  project.ID,
		SeedID:        seed.SeedID,
		PlanAttemptID: attemptResult.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		return nil, fmt.Errorf("link plan seed to created attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create plan attempt from seed: %w", err)
	}
	committed = true
	mappedSeed := mapPlanSeed(&updatedSeed)
	return &CreatePlanAttemptFromSeedResult{
		OK:           true,
		Seed:         &mappedSeed,
		PlanAttempt:  attemptResult.PlanAttempt,
		IntentPacket: attemptResult.IntentPacket,
		ReviewPolicy: attemptResult.ReviewPolicy,
		ReviewAction: attemptResult.ReviewAction,
		ReviewGate:   attemptResult.ReviewGate,
	}, nil
}

func canonicalPlanSeedPlanJSON(raw json.RawMessage) (json.RawMessage, string, string, error) {
	if len(raw) == 0 {
		return nil, "", "planner_pass_plan_json is required", nil
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", "planner_pass_plan_json must be valid JSON", nil
	}
	if _, ok := doc.(map[string]any); !ok {
		return nil, "", "planner_pass_plan_json must be a JSON object", nil
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, "", "", fmt.Errorf("canonicalize planner_pass_plan_json: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return json.RawMessage(canonical), "sha256:" + hex.EncodeToString(sum[:]), "", nil
}

func mapPlanAttemptBlockerForSeed(code appplans.PlanAttemptBlockerCode) string {
	switch code {
	case appplans.BlockerMissingPlanArtifact, appplans.BlockerArtifactHashMismatch:
		return PlanSeedBlockerMissingPlanArtifact
	case appplans.BlockerUnsafeRetrieval:
		return PlanSeedBlockerUnsafeSeedContext
	default:
		return PlanSeedBlockerDraftAttemptsUnavailable
	}
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
