package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"relay/internal/store/generated"
)

func TestPlanAttemptReviewGateStates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		mode         string
		reviewSource string
		gateStatus   string
		wantWorkflow string
		wantBlocker  PlanAttemptBlockerCode
	}{
		{name: "disabled", mode: DriftReviewModeDisabled, wantWorkflow: WorkflowReviewNotRequired},
		{name: "manual no review", mode: DriftReviewModeManual, wantWorkflow: WorkflowManualReviewAvailable},
		{name: "automatic missing", mode: DriftReviewModeAutomatic, wantWorkflow: WorkflowAutomaticReviewPending, wantBlocker: BlockerDriftReviewRequired},
		{name: "external missing", mode: DriftReviewModeExternal, wantWorkflow: WorkflowExternalReviewRequired, wantBlocker: BlockerDriftReviewRequired},
		{name: "ready review", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gateStatus: ApprovalGateStatusReady, wantWorkflow: WorkflowApprovalReady},
		{name: "ack review", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gateStatus: ApprovalGateStatusAckRequired, wantWorkflow: WorkflowDriftAcknowledgementNeeded, wantBlocker: BlockerDriftAcknowledgementReq},
		{name: "revision review", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gateStatus: ApprovalGateStatusRevisionRequired, wantWorkflow: WorkflowRevisionRequired, wantBlocker: BlockerDriftRevisionRequired},
		{name: "blocked review", mode: DriftReviewModeExternal, reviewSource: ReviewSourceExternal, gateStatus: ApprovalGateStatusBlocked, wantWorkflow: WorkflowDriftReviewBlocked, wantBlocker: BlockerDriftReviewBlocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestService(t)
			created := createTestPlanAttempt(t, svc, tc.mode, "")
			if tc.reviewSource != "" {
				submitTestReview(t, svc, *created.PlanAttempt, tc.reviewSource, tc.gateStatus)
			}
			gate, blocked, err := svc.GetPlanAttemptReviewGate(context.Background(), PlanAttemptReviewGateRequest{
				ProjectID:     "relay",
				PlanAttemptID: created.PlanAttempt.PlanAttemptID,
			})
			if err != nil || blocked != nil {
				t.Fatalf("GetPlanAttemptReviewGate blocked=%#v err=%v", blocked, err)
			}
			if gate.WorkflowState != tc.wantWorkflow {
				t.Fatalf("expected workflow %q, got %#v", tc.wantWorkflow, gate)
			}
			if tc.wantBlocker != "" {
				if len(gate.Blockers) == 0 || gate.Blockers[0].Code != tc.wantBlocker {
					t.Fatalf("expected blocker %q, got %#v", tc.wantBlocker, gate.Blockers)
				}
			}
		})
	}
}

func TestPlanAttemptReviewGateStrengthensFindingSeverity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		severity     string
		wantWorkflow string
		wantBlocker  PlanAttemptBlockerCode
	}{
		{name: "medium", severity: "medium", wantWorkflow: WorkflowDriftAcknowledgementNeeded, wantBlocker: BlockerDriftAcknowledgementReq},
		{name: "high", severity: "high", wantWorkflow: WorkflowRevisionRequired, wantBlocker: BlockerDriftRevisionRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestService(t)
			created := createTestPlanAttempt(t, svc, DriftReviewModeExternal, "")
			review := submitTestReview(t, svc, *created.PlanAttempt, ReviewSourceExternal, ApprovalGateStatusReady)
			findings, err := json.Marshal([]map[string]string{{"severity": tc.severity}})
			if err != nil {
				t.Fatalf("marshal findings: %v", err)
			}
			_, err = svc.store.DB().ExecContext(context.Background(), "UPDATE intent_drift_reviews SET findings_json = ? WHERE id = ?", string(findings), review.DriftReview.ID)
			if err != nil {
				t.Fatalf("update findings: %v", err)
			}
			gate, blocked, err := svc.GetPlanAttemptReviewGate(context.Background(), PlanAttemptReviewGateRequest{
				ProjectID:     "relay",
				PlanAttemptID: created.PlanAttempt.PlanAttemptID,
			})
			if err != nil || blocked != nil {
				t.Fatalf("GetPlanAttemptReviewGate blocked=%#v err=%v", blocked, err)
			}
			if gate.WorkflowState != tc.wantWorkflow {
				t.Fatalf("expected workflow %q, got %#v", tc.wantWorkflow, gate)
			}
			if len(gate.Blockers) == 0 || gate.Blockers[0].Code != tc.wantBlocker {
				t.Fatalf("expected blocker %q, got %#v", tc.wantBlocker, gate.Blockers)
			}
		})
	}
}

func TestApprovePlanAttemptUsesSeverityStrengthenedGate(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	created := createTestPlanAttempt(t, svc, DriftReviewModeExternal, "")
	review := submitTestReview(t, svc, *created.PlanAttempt, ReviewSourceExternal, ApprovalGateStatusReady)
	_, err := svc.store.DB().ExecContext(context.Background(), "UPDATE intent_drift_reviews SET findings_json = ? WHERE id = ?", `[{"severity":"high"}]`, review.DriftReview.ID)
	if err != nil {
		t.Fatalf("update findings: %v", err)
	}
	result, err := svc.ApprovePlanAttempt(context.Background(), ApprovePlanAttemptRequest{
		ProjectID:             "relay",
		PlanAttemptID:         created.PlanAttempt.PlanAttemptID,
		Approved:              true,
		AcceptedDriftReviewID: review.DriftReview.IntentDriftReviewID,
	})
	if err != nil {
		t.Fatalf("ApprovePlanAttempt error: %v", err)
	}
	if result.OK || result.BlockerCode != BlockerDriftRevisionRequired {
		t.Fatalf("expected revision blocker, got %#v", result)
	}
}

func TestCreatePlanAttemptUsesProjectReviewSettings(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	project, err := svc.store.GetProjectByProjectID("relay")
	if err != nil {
		t.Fatalf("GetProjectByProjectID: %v", err)
	}
	_, err = generated.New(svc.store.DB()).UpsertPlanReviewSettings(context.Background(), generated.UpsertPlanReviewSettingsParams{
		ProjectRowID:    project.ID,
		ProjectID:       project.ProjectID,
		DriftReviewMode: DriftReviewModeExternal,
		ModelTier:       ModelTierHighAssurance,
	})
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("UpsertPlanReviewSettings: %v", err)
	}
	raw := planRawWithID(t, "")
	_, hash, err := canonicalRawPlanJSON(raw)
	if err != nil {
		t.Fatalf("canonicalRawPlanJSON: %v", err)
	}
	created, err := svc.CreatePlanAttemptWithIntent(context.Background(), CreatePlanAttemptWithIntentRequest{
		ProjectID:     "relay",
		PlanAttemptID: "attempt-settings",
		PlanArtifactRef: PlanArtifactRef{
			Path:         "handoffs/planner/settings.planner-pass-plan.json",
			SHA256:       hash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON: raw,
		IntentPacket: IntentPacketInput{
			Summary:            "Original request",
			LiteralUserRequest: "Create a settings-backed draft plan attempt.",
			Source:             IntentSource{CapturedFrom: CapturedFromPlannerChat, CapturedBy: "tester"},
			RedactionStatus:    RedactionStatusVerifiedNoSecrets,
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanAttemptWithIntent: %v", err)
	}
	if !created.OK {
		t.Fatalf("CreatePlanAttemptWithIntent blocked: %#v", created)
	}
	if created.PlanAttempt.DriftReviewMode != DriftReviewModeExternal || created.PlanAttempt.ModelTier != ModelTierHighAssurance {
		t.Fatalf("expected settings-backed mode/tier, got %#v", created.PlanAttempt)
	}
	if created.ReviewPolicy == nil || created.ReviewPolicy.DriftReviewMode != DriftReviewModeExternal || created.ReviewPolicy.ModelTier != ModelTierHighAssurance {
		t.Fatalf("expected result review policy to mirror settings, got %#v", created.ReviewPolicy)
	}
	if created.ReviewAction == nil || created.ReviewAction.Action != "external_review_required" {
		t.Fatalf("expected external initial review action, got %#v", created.ReviewAction)
	}
}

func TestPreparePlanAttemptDriftReviewPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mode          string
		allow         bool
		requestedTier string
		wantPrepared  bool
		wantAllow     bool
		wantTier      string
		wantBlocker   PlanAttemptBlockerCode
		wantFailure   string
	}{
		{name: "disabled blocks", mode: DriftReviewModeDisabled, wantBlocker: BlockerDriftReviewBlocked},
		{name: "external blocks", mode: DriftReviewModeExternal, wantBlocker: BlockerDriftReviewRequired},
		{name: "manual without consent blocks", mode: DriftReviewModeManual, wantBlocker: BlockerDriftReviewBlocked, wantFailure: "model_call_not_allowed"},
		{name: "manual with consent prepares", mode: DriftReviewModeManual, allow: true, wantPrepared: true, wantAllow: true, wantTier: ModelTierStandard},
		{name: "automatic prepares", mode: DriftReviewModeAutomatic, wantPrepared: true, wantAllow: true, wantTier: ModelTierStandard},
		{name: "invalid requested tier blocks", mode: DriftReviewModeManual, allow: true, requestedTier: "surprise", wantBlocker: BlockerDriftReviewBlocked},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, _ := newTestService(t)
			created := createTestPlanAttempt(t, svc, tc.mode, "")

			prepared, blocked, err := svc.PreparePlanAttemptDriftReview(context.Background(), RunPlanAttemptDriftReviewRequest{
				ProjectID:      "relay",
				PlanAttemptID:  created.PlanAttempt.PlanAttemptID,
				AllowModelCall: tc.allow,
				RequestedTier:  tc.requestedTier,
			})
			if err != nil {
				t.Fatalf("PreparePlanAttemptDriftReview error: %v", err)
			}
			if tc.wantPrepared {
				if blocked != nil {
					t.Fatalf("expected prepared request, got blocker %#v", blocked)
				}
				if prepared == nil {
					t.Fatal("expected prepared request")
				}
				if prepared.AllowModelCall != tc.wantAllow {
					t.Fatalf("expected AllowModelCall=%v, got %v", tc.wantAllow, prepared.AllowModelCall)
				}
				if prepared.RequestedTier != tc.wantTier {
					t.Fatalf("expected tier %q, got %q", tc.wantTier, prepared.RequestedTier)
				}
				if prepared.ReviewGate == nil {
					t.Fatal("expected review gate on prepared request")
				}
				return
			}
			if prepared != nil {
				t.Fatalf("expected no prepared request, got %#v", prepared)
			}
			if blocked == nil || blocked.BlockerCode != tc.wantBlocker {
				t.Fatalf("expected blocker %q, got %#v", tc.wantBlocker, blocked)
			}
			if blocked.ReviewGate == nil {
				t.Fatal("expected review gate on blocked result")
			}
			if tc.wantFailure != "" {
				if blocked.ReviewAction == nil || blocked.ReviewAction.FailureCode != tc.wantFailure {
					t.Fatalf("expected failure %q, got %#v", tc.wantFailure, blocked.ReviewAction)
				}
			}
		})
	}
}
