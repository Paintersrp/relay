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
}
