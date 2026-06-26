package plans

import (
	"context"
	"testing"
)

func TestPlanReviewSettingsDefaultPersistOverride(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	settings, blocked, err := svc.GetPlanReviewSettings(context.Background(), "relay")
	if err != nil || blocked != nil {
		t.Fatalf("GetPlanReviewSettings blocked=%#v err=%v", blocked, err)
	}
	if settings.DriftReviewMode != DriftReviewModeManual || settings.ModelTier != ModelTierStandard {
		t.Fatalf("expected manual/standard defaults, got %#v", settings)
	}

	updated, blocked, err := svc.UpdatePlanReviewSettings(context.Background(), UpdatePlanReviewSettingsRequest{
		ProjectID:       "relay",
		DriftReviewMode: DriftReviewModeExternal,
		ModelTier:       ModelTierHighAssurance,
	})
	if err != nil || blocked != nil {
		t.Fatalf("UpdatePlanReviewSettings blocked=%#v err=%v", blocked, err)
	}
	if updated.DriftReviewMode != DriftReviewModeExternal || updated.ModelTier != ModelTierHighAssurance {
		t.Fatalf("expected persisted external/high assurance, got %#v", updated)
	}

	policy, blocked, err := svc.ResolvePlanReviewPolicy(context.Background(), "relay", DriftReviewModeDisabled, ModelTierEconomy)
	if err != nil || blocked != nil {
		t.Fatalf("ResolvePlanReviewPolicy blocked=%#v err=%v", blocked, err)
	}
	if policy.DriftReviewMode != DriftReviewModeDisabled || policy.ModelTier != ModelTierEconomy || policy.Source != "request" {
		t.Fatalf("expected request override, got %#v", policy)
	}
}

func TestPlanReviewSettingsRejectsInvalidEnum(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	_, blocked, err := svc.UpdatePlanReviewSettings(context.Background(), UpdatePlanReviewSettingsRequest{
		ProjectID:       "relay",
		DriftReviewMode: "surprise",
		ModelTier:       ModelTierStandard,
	})
	if err != nil {
		t.Fatalf("UpdatePlanReviewSettings error: %v", err)
	}
	if blocked == nil || blocked.BlockerCode != BlockerDriftReviewBlocked {
		t.Fatalf("expected drift_review_blocked, got %#v", blocked)
	}
}
