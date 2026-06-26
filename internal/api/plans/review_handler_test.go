package plans

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appplans "relay/internal/app/plans"
)

func TestPlanReviewSettingsRoutes(t *testing.T) {
	_, _, router := newAttemptAPITestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-review-settings", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var getResp PlanReviewSettingsAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	if !getResp.Success || getResp.Settings == nil {
		t.Fatalf("expected settings success, got %+v", getResp)
	}
	if getResp.Settings.DriftReviewMode != appplans.DriftReviewModeManual || getResp.Settings.ModelTier != appplans.ModelTierStandard {
		t.Fatalf("expected manual/standard defaults, got %+v", getResp.Settings)
	}

	body := mustJSON(t, UpdatePlanReviewSettingsAPIRequest{
		DriftReviewMode: "surprise",
		ModelTier:       appplans.ModelTierStandard,
	})
	req = httptest.NewRequest(http.MethodPut, "/api/projects/relay/plan-review-settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var putResp PlanReviewSettingsAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode invalid settings response: %v", err)
	}
	if putResp.BlockerCode != string(appplans.BlockerDriftReviewBlocked) {
		t.Fatalf("expected drift_review_blocked, got %+v", putResp)
	}
}

func TestPlanAttemptReviewGateRouteManualNoReview(t *testing.T) {
	_, _, router := newAttemptAPITestServer(t)
	createAttemptViaAPI(t, router, "attempt-manual-gate", appplans.DriftReviewModeManual)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/relay/plan-attempts/attempt-manual-gate/review-gate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PlanAttemptReviewGateAPIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode gate response: %v", err)
	}
	if !resp.Success || resp.ReviewGate == nil {
		t.Fatalf("expected review gate success, got %+v", resp)
	}
	if resp.ReviewGate.WorkflowState != appplans.WorkflowManualReviewAvailable {
		t.Fatalf("expected manual review workflow, got %+v", resp.ReviewGate)
	}
	if !stringSliceContains(resp.ReviewGate.AllowedActions, "run_drift_review") {
		t.Fatalf("expected run_drift_review action, got %+v", resp.ReviewGate.AllowedActions)
	}
}

func TestRunPlanAttemptDriftReviewRouteBlocksWithoutModelCall(t *testing.T) {
	_, _, router := newAttemptAPITestServer(t)

	tests := []struct {
		name        string
		attemptID   string
		mode        string
		body        RunPlanAttemptDriftReviewAPIRequest
		wantStatus  int
		wantBlocker string
		wantFailure string
	}{
		{name: "manual no consent", attemptID: "attempt-manual-no-consent", mode: appplans.DriftReviewModeManual, body: RunPlanAttemptDriftReviewAPIRequest{}, wantStatus: http.StatusUnprocessableEntity, wantBlocker: string(appplans.BlockerDriftReviewBlocked), wantFailure: "model_call_not_allowed"},
		{name: "disabled", attemptID: "attempt-disabled-review", mode: appplans.DriftReviewModeDisabled, body: RunPlanAttemptDriftReviewAPIRequest{AllowModelCall: true}, wantStatus: http.StatusUnprocessableEntity, wantBlocker: string(appplans.BlockerDriftReviewBlocked)},
		{name: "external", attemptID: "attempt-external-review", mode: appplans.DriftReviewModeExternal, body: RunPlanAttemptDriftReviewAPIRequest{AllowModelCall: true}, wantStatus: http.StatusUnprocessableEntity, wantBlocker: string(appplans.BlockerDriftReviewRequired)},
		{name: "nil drift service after preparation", attemptID: "attempt-manual-consent", mode: appplans.DriftReviewModeManual, body: RunPlanAttemptDriftReviewAPIRequest{AllowModelCall: true}, wantStatus: http.StatusUnprocessableEntity, wantBlocker: string(appplans.BlockerDriftReviewBlocked)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			createAttemptViaAPI(t, router, tc.attemptID, tc.mode)
			resp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts/"+tc.attemptID+"/run-drift-review", mustJSON(t, tc.body), tc.wantStatus)
			if resp.BlockerCode != tc.wantBlocker {
				t.Fatalf("expected blocker %q, got %+v", tc.wantBlocker, resp)
			}
			if resp.ReviewGate == nil {
				t.Fatalf("expected review gate on blocked response, got %+v", resp)
			}
			if tc.wantFailure != "" {
				if resp.ReviewAction == nil || resp.ReviewAction.FailureCode != tc.wantFailure {
					t.Fatalf("expected failure %q, got %+v", tc.wantFailure, resp.ReviewAction)
				}
			}
			if tc.mode == appplans.DriftReviewModeExternal && resp.ReviewGate.ExternalReviewInstructions == nil {
				t.Fatalf("expected external review instructions, got %+v", resp.ReviewGate)
			}
		})
	}
}

func createAttemptViaAPI(t *testing.T, router http.Handler, attemptID, mode string) {
	t.Helper()
	rawPlan, planHash := attemptTestRawPlan(t, "plan-"+attemptID)
	body := mustJSON(t, CreatePlanAttemptWithIntentAPIRequest{
		PlanAttemptID:  attemptID,
		IntentPacketID: "intent-" + attemptID,
		IntentThreadID: "thread-" + attemptID,
		PlanArtifactRef: PlanArtifactRefAPI{
			Path:         "handoffs/plans/" + attemptID + ".json",
			SHA256:       planHash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     RawPlanJSONAPI{Content: rawPlan, ContentHash: planHash},
		DriftReviewMode: mode,
		IntentPacket: IntentPacketInputAPI{
			Summary:            "Review route test.",
			LiteralUserRequest: "Exercise review routes.",
			Constraints:        []string{},
			Source:             IntentSourceAPI{CapturedFrom: appplans.CapturedFromPlannerChat, CapturedBy: "api-test", SourceArtifactPath: "source.md"},
			RedactionStatus:    appplans.RedactionStatusVerifiedNoSecrets,
		},
	})
	resp := doAttemptRequest(t, router, http.MethodPost, "/api/projects/relay/plan-attempts", body, http.StatusCreated)
	if !resp.Success || resp.PlanAttempt == nil {
		t.Fatalf("expected created attempt, got %+v", resp)
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
