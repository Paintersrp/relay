package drift

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appplans "relay/internal/app/plans"
	"relay/internal/store"
)

//go:embed testdata/valid_review.json
var validReviewTestData []byte

//go:embed testdata/invalid_review_missing_model_metadata.json
var invalidReviewMissingMetadataTestData []byte

//go:embed testdata/invalid_review_bad_confidence.json
var invalidReviewBadConfidenceTestData []byte

func TestValidateIntentDriftReviewJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "valid fixture passes",
			data:    validReviewTestData,
			wantErr: false,
		},
		{
			name:    "missing model_metadata fails",
			data:    invalidReviewMissingMetadataTestData,
			wantErr: true,
		},
		{
			name:    "bad confidence fails",
			data:    invalidReviewBadConfidenceTestData,
			wantErr: true,
		},
		{
			name:    "invalid JSON fails",
			data:    []byte("{invalid-json"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIntentDriftReviewJSON(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIntentDriftReviewJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateIntentDriftReviewJSONAcceptsNestedProvenanceSourceArtifactPath(t *testing.T) {
	doc := validReviewMap(t)
	provenance := doc["provenance"].(map[string]any)
	provenance["source_artifact_path"] = "handoffs/source.md"
	delete(doc, "source_artifact_path")

	if err := ValidateIntentDriftReviewJSON(mustJSON(t, doc)); err != nil {
		t.Fatalf("ValidateIntentDriftReviewJSON() error = %v", err)
	}
}

func TestValidateIntentDriftReviewJSONRejectsTopLevelSourceArtifactPath(t *testing.T) {
	doc := validReviewMap(t)
	doc["source_artifact_path"] = "handoffs/source.md"

	if err := ValidateIntentDriftReviewJSON(mustJSON(t, doc)); err == nil {
		t.Fatalf("ValidateIntentDriftReviewJSON() error = nil, want top-level source_artifact_path rejection")
	}
}

func TestValidateIntentDriftReviewJSONRejectsLegacyTopLevelReviewSource(t *testing.T) {
	doc := validReviewMap(t)
	delete(doc, "provenance")
	doc["review_source"] = "internal"
	doc["submitted_by"] = SubmittedByInternalReviewer

	if err := ValidateIntentDriftReviewJSON(mustJSON(t, doc)); err == nil {
		t.Fatalf("ValidateIntentDriftReviewJSON() error = nil, want legacy top-level provenance rejection")
	}
}

func TestNormalizeModelOutputNestsSourceArtifactPathInProvenance(t *testing.T) {
	packet := *defaultValidPacket()
	packet.ReviewedIntentPacket.SourceArtifactPath = "handoffs/source.md"
	out, err := ValidateModelOutput([]byte(defaultValidModelOutput))
	if err != nil {
		t.Fatalf("ValidateModelOutput: %v", err)
	}

	input, contractBytes, err := NormalizeModelOutput(
		packet,
		ReviewModelResponse{
			Provider:    "fake",
			Model:       "fake-standard",
			FinalTier:   appplans.ModelTierStandard,
			Temperature: 0,
		},
		out,
		SubmittedByInternalReviewer,
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		time.Date(2026, 6, 25, 23, 15, 53, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NormalizeModelOutput: %v", err)
	}
	if input.SourceArtifactPath != "handoffs/source.md" {
		t.Fatalf("expected persistence DTO source path to remain populated, got %q", input.SourceArtifactPath)
	}

	var top map[string]any
	if err := json.Unmarshal(contractBytes, &top); err != nil {
		t.Fatalf("json.Unmarshal contract: %v", err)
	}
	if _, ok := top["source_artifact_path"]; ok {
		t.Fatalf("top-level source_artifact_path must not be emitted")
	}
	provenance, ok := top["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance object, got %#v", top["provenance"])
	}
	if provenance["source_artifact_path"] != "handoffs/source.md" {
		t.Fatalf("expected nested source_artifact_path, got %#v", provenance["source_artifact_path"])
	}
}

func validReviewMap(t *testing.T) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(validReviewTestData, &doc); err != nil {
		t.Fatalf("json.Unmarshal valid review fixture: %v", err)
	}
	provenance, ok := doc["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("valid review fixture provenance is not an object")
	}
	doc["provenance"] = provenance
	return doc
}

func mustJSON(t *testing.T, doc map[string]any) []byte {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal review: %v", err)
	}
	return data
}

type fakePlanService struct {
	packetRes  *appplans.PlanAttemptResult
	packetErr  error
	submitRes  *appplans.PlanAttemptResult
	submitErr  error
	lastSubmit *appplans.SubmitIntentDriftReviewRequest
}

func (f *fakePlanService) GetPlanIntentReviewPacket(ctx context.Context, req appplans.GetPlanIntentReviewPacketRequest) (*appplans.PlanAttemptResult, error) {
	return f.packetRes, f.packetErr
}

func (f *fakePlanService) SubmitIntentDriftReview(ctx context.Context, req appplans.SubmitIntentDriftReviewRequest) (*appplans.PlanAttemptResult, error) {
	f.lastSubmit = &req
	return f.submitRes, f.submitErr
}

type fakeReviewer struct {
	calls  []ReviewModelRequest
	res    *ReviewModelResponse
	err    error
	resMap map[string]ReviewModelResponse
}

func (f *fakeReviewer) ReviewIntentDrift(ctx context.Context, req ReviewModelRequest) (ReviewModelResponse, error) {
	f.calls = append(f.calls, req)
	if f.resMap != nil {
		if r, ok := f.resMap[req.Tier]; ok {
			return r, nil
		}
	}
	if f.err != nil {
		return ReviewModelResponse{}, f.err
	}
	if f.res != nil {
		r := *f.res
		r.FinalTier = req.Tier
		r.Temperature = req.Temperature
		return r, nil
	}
	return ReviewModelResponse{}, fmt.Errorf("no response configured")
}

func defaultValidPacket() *appplans.PlanIntentReviewPacket {
	return &appplans.PlanIntentReviewPacket{
		PacketID:       "pkt-1",
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		IntentThreadID: "thread-1",
		RootIntentPacket: appplans.IntentPacketEvidence{
			IntentPacketID:     "intent-1",
			Kind:               "original",
			Summary:            "summary",
			LiteralUserRequest: "user request",
			Constraints:        "[]",
		},
		ReviewedIntentPacket: appplans.IntentPacketEvidence{
			IntentPacketID:     "intent-1",
			Kind:               "original",
			Summary:            "summary",
			LiteralUserRequest: "user request",
			Constraints:        "[]",
		},
		PlanAttempt: appplans.PlanAttemptEvidence{
			PlanAttemptID: "attempt-1",
			Status:        appplans.PlanAttemptStatusDraft,
		},
		RetrievalSemantics: appplans.RetrievalSemantics{
			RetrievalOnly:      true,
			ModelCallPerformed: false,
			StateMutated:       false,
		},
		PacketHash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

const defaultValidModelOutput = `{
  "overall_alignment": "aligned",
  "confidence": 0.95,
  "findings": [],
  "recommended_action": "approve",
  "approval_gate_status": "ready"
}`

func TestRunInternalReviewBlocksWhenModelCallNotAllowed(t *testing.T) {
	model := &fakeReviewer{}
	plans := &fakePlanService{}
	svc := NewService(plans, model)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: false,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected OK to be false")
	}
	if res.FailureCode != FailureModelCallNotAllowed {
		t.Errorf("expected FailureModelCallNotAllowed, got %v", res.FailureCode)
	}
	if len(model.calls) != 0 {
		t.Fatalf("expected provider call count 0, got %d", len(model.calls))
	}
	if plans.lastSubmit != nil {
		t.Fatalf("expected no persisted review")
	}
}

func TestRunInternalReviewBlocksWhenProviderMissing(t *testing.T) {
	plans := &fakePlanService{}
	svc := NewService(plans, nil)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected OK to be false")
	}
	if res.FailureCode != FailureModelProviderUnavailable {
		t.Errorf("expected FailureModelProviderUnavailable, got %v", res.FailureCode)
	}
	if plans.lastSubmit != nil {
		t.Fatalf("expected no persisted review")
	}
}

func TestRunInternalReviewUsesPacketOnlyInput(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(defaultValidModelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	// Stub persistence response
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true, got failed result: %v", res.Message)
	}

	if len(fakeModel.calls) != 1 {
		t.Fatalf("expected 1 model call, got %d", len(fakeModel.calls))
	}
	call := fakeModel.calls[0]
	// Verify that the prompt/payload does not contain environment/chat variables or arbitrary filesystem paths
	if strings.Contains(string(call.InputPayload), "user_env") || strings.Contains(string(call.InputPayload), "chat_history") {
		t.Errorf("unsafe inputs leaked in payload: %s", string(call.InputPayload))
	}
	if !strings.Contains(string(call.InputPayload), `"plan_attempt_id":"attempt-1"`) {
		t.Fatalf("expected packet fields in payload: %s", string(call.InputPayload))
	}
	if strings.Contains(string(call.InputPayload), "arbitrary chat string outside packet") {
		t.Fatalf("non-packet string leaked into payload")
	}
	if fakePlans.lastSubmit.DriftReview.InputHash != sha256Bytes(call.InputPayload) {
		t.Fatalf("expected persisted input hash to match provider input payload")
	}
}

func TestRunInternalReviewPersistsInternalReview(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(defaultValidModelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{IntentDriftReviewID: "final-review-id"},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true, got failed result: %v", res.Message)
	}

	if fakePlans.lastSubmit == nil {
		t.Fatalf("SubmitIntentDriftReview was not called")
	}
	sub := fakePlans.lastSubmit.DriftReview
	if sub.ReviewSource != "internal" {
		t.Errorf("expected review_source 'internal', got %q", sub.ReviewSource)
	}
	if sub.SubmittedBy != SubmittedByInternalReviewer {
		t.Errorf("expected submitted_by '%s', got %q", SubmittedByInternalReviewer, sub.SubmittedBy)
	}
	if sub.PlanAttemptID != "attempt-1" || sub.IntentThreadID != "thread-1" {
		t.Fatalf("unexpected lineage: %#v", sub)
	}
	if sub.RootIntentPacketID != "intent-1" || sub.ReviewedIntentPacketID != "intent-1" {
		t.Fatalf("unexpected intent lineage: %#v", sub)
	}
	if sub.ReviewPacketHash != defaultValidPacket().PacketHash {
		t.Fatalf("unexpected review packet hash %q", sub.ReviewPacketHash)
	}
	if sub.ApprovalGateStatus != appplans.ApprovalGateStatusReady {
		t.Fatalf("expected ready gate, got %q", sub.ApprovalGateStatus)
	}
	if !strings.HasPrefix(sub.InputHash, "sha256:") || !strings.HasPrefix(sub.OutputHash, "sha256:") {
		t.Fatalf("expected input/output hashes, got %q / %q", sub.InputHash, sub.OutputHash)
	}
}

func TestRunInternalReviewPersistsInternalReviewThroughAppPlansStore(t *testing.T) {
	planSvc, countManagedPlans := newRealPlanService(t)
	created := createRealPlanAttempt(t, planSvc, appplans.DriftReviewModeManual)
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(defaultValidModelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	svc := NewService(planSvc, fakeModel)

	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "relay",
		PlanAttemptID:  created.PlanAttempt.PlanAttemptID,
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK=true, got %#v", res)
	}
	if res.DriftReview == nil {
		t.Fatalf("expected persisted drift review")
	}
	if res.DriftReview.ReviewSource != appplans.ReviewSourceInternal {
		t.Fatalf("expected internal review_source, got %q", res.DriftReview.ReviewSource)
	}
	if res.DriftReview.ApprovalGateStatus != appplans.ApprovalGateStatusReady {
		t.Fatalf("expected ready approval gate, got %q", res.DriftReview.ApprovalGateStatus)
	}
	packetRes, err := planSvc.GetPlanIntentReviewPacket(context.Background(), appplans.GetPlanIntentReviewPacketRequest{
		ProjectID:     "relay",
		PlanAttemptID: created.PlanAttempt.PlanAttemptID,
	})
	if err != nil {
		t.Fatalf("GetPlanIntentReviewPacket: %v", err)
	}
	if !packetRes.OK {
		t.Fatalf("GetPlanIntentReviewPacket blocked: %#v", packetRes)
	}
	if packetRes.ReviewPacket.PlanAttempt.ReviewState != appplans.PlanAttemptReviewInternalGenerated {
		t.Fatalf("expected review_state internal_review_generated, got %q", packetRes.ReviewPacket.PlanAttempt.ReviewState)
	}
	if got := countManagedPlans(); got != 0 {
		t.Fatalf("expected no managed plan rows, got %d", got)
	}
}

func TestRunInternalReviewInvalidOutputDoesNotPersist(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	// Invalid model output: missing overall_alignment
	invalidModelOutput := `{
		"confidence": 0.95,
		"findings": [],
		"recommended_action": "approve",
		"approval_gate_status": "ready"
	}`
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(invalidModelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected validation to fail")
	}
	if res.FailureCode != FailureSchemaValidationFailed {
		t.Errorf("expected FailureSchemaValidationFailed, got %v", res.FailureCode)
	}
	if fakePlans.lastSubmit != nil {
		t.Errorf("expected no submission to be persisted on invalid schema")
	}
}

func TestRunInternalReviewBlocksSensitivePacketBeforeModelCall(t *testing.T) {
	pkt := defaultValidPacket()
	pkt.RedactionStatus = appplans.RedactionStatusBlockedSensitive

	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: pkt,
		},
	}
	fakeModel := &fakeReviewer{}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected review to be blocked")
	}
	if res.FailureCode != FailurePacketBlockedSensitive {
		t.Errorf("expected FailurePacketBlockedSensitive, got %v", res.FailureCode)
	}
	if len(fakeModel.calls) > 0 {
		t.Errorf("expected no model call to be made")
	}
}

func TestRunInternalReviewEscalatesLowConfidence(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}

	// 1st output: low confidence (0.5)
	outLowConf := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.50,
	  "findings": [],
	  "recommended_action": "approve",
	  "approval_gate_status": "ready"
	}`
	// 2nd output: high confidence (0.95)
	outHighConf := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.95,
	  "findings": [],
	  "recommended_action": "approve",
	  "approval_gate_status": "ready"
	}`

	fakeModel := &fakeReviewer{
		resMap: map[string]ReviewModelResponse{
			"standard": {
				RawJSON:  []byte(outLowConf),
				Provider: "fake",
				Model:    "fake-standard",
			},
			"high_assurance": {
				RawJSON:  []byte(outHighConf),
				Provider: "fake",
				Model:    "fake-high",
			},
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
		RequestedTier:  "auto_escalate",
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}
	if !res.Escalated {
		t.Errorf("expected escalated to be true")
	}
	if res.FinalTier != "high_assurance" {
		t.Errorf("expected final tier 'high_assurance', got %q", res.FinalTier)
	}
	if len(fakeModel.calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(fakeModel.calls))
	}
}

func TestRunInternalReviewAutoEscalatesOnInvalidFirstOutput(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}

	// 1st output: invalid schema (missing recommended_action)
	invalidOut := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.95,
	  "findings": [],
	  "approval_gate_status": "ready"
	}`
	// 2nd output: valid schema
	validOut := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.95,
	  "findings": [],
	  "recommended_action": "approve",
	  "approval_gate_status": "ready"
	}`

	fakeModel := &fakeReviewer{
		resMap: map[string]ReviewModelResponse{
			"standard": {
				RawJSON:  []byte(invalidOut),
				Provider: "fake",
				Model:    "fake-standard",
			},
			"high_assurance": {
				RawJSON:  []byte(validOut),
				Provider: "fake",
				Model:    "fake-high",
			},
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
		RequestedTier:  "auto_escalate",
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}
	if !res.Escalated {
		t.Errorf("expected escalated to be true")
	}
	if res.FinalTier != "high_assurance" {
		t.Errorf("expected final tier 'high_assurance', got %q", res.FinalTier)
	}
}

func TestRunInternalReviewRecordsInputAndOutputHashes(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	rawOut := []byte(defaultValidModelOutput)
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  rawOut,
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}

	sub := fakePlans.lastSubmit.DriftReview
	if !strings.HasPrefix(sub.InputHash, "sha256:") || len(sub.InputHash) != 71 {
		t.Errorf("invalid InputHash: %q", sub.InputHash)
	}
	expectedOutputHash := sha256Bytes(rawOut)
	if sub.OutputHash != expectedOutputHash {
		t.Errorf("expected OutputHash %q, got %q", expectedOutputHash, sub.OutputHash)
	}
}

func TestValidateModelOutputNormalizesGateStatus(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	// Model returns recommended_action=revise (which maps to revision_required),
	// but specifies approval_gate_status=ready (inconsistent/less strict).
	// The service must resolve to revision_required (the stricter of the two).
	inconsistentModelOutput := `{
	  "overall_alignment": "minor_drift",
	  "confidence": 0.95,
	  "findings": [
	    {
	      "finding_id": "finding-1",
	      "severity": "medium",
	      "summary": "minor code structure changes",
	      "evidence": ["plan section changed"],
	      "suggested_resolution": "revise the affected section"
	    }
	  ],
	  "recommended_action": "revise",
	  "approval_gate_status": "ready"
	}`

	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(inconsistentModelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}

	sub := fakePlans.lastSubmit.DriftReview
	if sub.ApprovalGateStatus != "revision_required" {
		t.Errorf("expected gate status normalized to 'revision_required', got %q", sub.ApprovalGateStatus)
	}
}

func TestRunInternalReviewUsesCanonicalGateInsteadOfModelGate(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	modelOutput := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.95,
	  "findings": [],
	  "recommended_action": "approve",
	  "approval_gate_status": "blocked"
	}`
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(modelOutput),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}

	sub := fakePlans.lastSubmit.DriftReview
	if sub.ApprovalGateStatus != "ready" {
		t.Errorf("expected canonical gate status 'ready', got %q", sub.ApprovalGateStatus)
	}
}

func TestRunInternalReviewDoesNotEscalateStandardTier(t *testing.T) {
	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: defaultValidPacket(),
		},
	}
	lowConfidence := `{
	  "overall_alignment": "aligned",
	  "confidence": 0.50,
	  "findings": [],
	  "recommended_action": "approve",
	  "approval_gate_status": "ready"
	}`
	fakeModel := &fakeReviewer{
		res: &ReviewModelResponse{
			RawJSON:  []byte(lowConfidence),
			Provider: "fake",
			Model:    "fake-standard",
		},
	}
	fakePlans.submitRes = &appplans.PlanAttemptResult{
		OK:          true,
		DriftReview: &store.IntentDriftReview{},
	}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
		RequestedTier:  appplans.ModelTierStandard,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK to be true: %v", res.Message)
	}
	if res.Escalated {
		t.Errorf("expected standard tier request not to auto-escalate")
	}
	if len(fakeModel.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(fakeModel.calls))
	}
	if res.FinalTier != appplans.ModelTierStandard {
		t.Errorf("expected final tier standard, got %q", res.FinalTier)
	}
}

func TestRunInternalReviewBlocksNonDraftAttempt(t *testing.T) {
	pkt := defaultValidPacket()
	pkt.PlanAttempt.Status = appplans.PlanAttemptStatusApproved

	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: pkt,
		},
	}
	fakeModel := &fakeReviewer{}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected review to be blocked")
	}
	if res.FailureCode != FailureAttemptNotDraft {
		t.Errorf("expected FailureAttemptNotDraft, got %v", res.FailureCode)
	}
}

func TestRunInternalReviewDetectsSecret(t *testing.T) {
	pkt := defaultValidPacket()
	pkt.RootIntentPacket.LiteralUserRequest = "Please use this api key: api_key='abcd1234efgh5678'"

	fakePlans := &fakePlanService{
		packetRes: &appplans.PlanAttemptResult{
			OK:           true,
			ReviewPacket: pkt,
		},
	}
	fakeModel := &fakeReviewer{}

	svc := NewService(fakePlans, fakeModel)
	res, err := svc.RunInternalReview(context.Background(), InternalReviewRequest{
		ProjectID:      "proj-1",
		PlanAttemptID:  "attempt-1",
		AllowModelCall: true,
	})
	if err != nil {
		t.Fatalf("RunInternalReview error: %v", err)
	}
	if res.OK {
		t.Errorf("expected review to be blocked")
	}
	if res.FailureCode != FailureSecretDetectedInPacket {
		t.Errorf("expected FailureSecretDetectedInPacket, got %v", res.FailureCode)
	}
}

func newRealPlanService(t *testing.T) (*appplans.Service, func() int) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "relay.sqlite"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	if _, err := st.CreateProject("relay", "Relay", "Drift test project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return appplans.NewService(st), func() int {
		plans, err := st.ListPlans(100)
		if err != nil {
			t.Fatalf("ListPlans: %v", err)
		}
		return len(plans)
	}
}

func createRealPlanAttempt(t *testing.T, svc *appplans.Service, mode string) *appplans.PlanAttemptResult {
	t.Helper()

	raw := validPlanRaw(t, "plan-drift-"+mode)
	hash := canonicalJSONSHA256(t, raw)
	result, err := svc.CreatePlanAttemptWithIntent(context.Background(), appplans.CreatePlanAttemptWithIntentRequest{
		ProjectID:      "relay",
		PlanAttemptID:  "attempt-drift-" + mode,
		IntentPacketID: "intent-drift-" + mode,
		IntentThreadID: "thread-drift-" + mode,
		PlanArtifactRef: appplans.PlanArtifactRef{
			Path:         "handoffs/planner/" + mode + ".planner-pass-plan.json",
			SHA256:       hash,
			ArtifactKind: "planner-pass-plan-json",
		},
		RawPlanJSON:     raw,
		DriftReviewMode: mode,
		ModelTier:       appplans.ModelTierStandard,
		IntentPacket: appplans.IntentPacketInput{
			Summary:            "Original request",
			LiteralUserRequest: "Create a reviewed draft plan attempt.",
			Constraints:        []string{"No route changes."},
			Source: appplans.IntentSource{
				CapturedFrom:       appplans.CapturedFromPlannerChat,
				CapturedBy:         "tester",
				SourceArtifactPath: "handoffs/source.md",
			},
			RedactionStatus: appplans.RedactionStatusVerifiedNoSecrets,
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanAttemptWithIntent error: %v", err)
	}
	if !result.OK {
		t.Fatalf("CreatePlanAttemptWithIntent blocked: %#v", result)
	}
	return result
}

func validPlanRaw(t *testing.T, planID string) json.RawMessage {
	t.Helper()

	required := true
	maxFiles := int64(12)
	plan := appplans.PlannerPassPlan{
		PlanMeta: appplans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T16:10:00Z",
			Title:         "Relay drift review test plan",
			Goal:          "Store validated internal drift review evidence",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectContext: &appplans.ProjectContext{
				PrimaryProject:       "relay",
				PrimaryRepository:    "relay",
				GitHubRole:           "repo_host_and_origin_only",
				LocalFirstAssumption: "Relay remains the local source of runtime context.",
			},
		},
		SourceIntent: appplans.SourceIntent{
			Summary: "Add a backend service for validated drift review persistence.",
		},
		Passes: []appplans.PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Validate drift review",
				Goal:                   "Validate internal review orchestration.",
				IntendedExecutionScope: []string{"internal/app/drift/service.go"},
				NonGoals:               []string{"No route changes"},
				Dependencies:           []string{},
				Status:                 appplans.StatusPassPlanned,
				PassType:               "backend_vertical_slice",
				ContextPlan: appplans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []appplans.ContextSearchTerm{
						{
							RepoID:   "relay",
							Query:    "RunInternalReview",
							Purpose:  "Find drift review orchestration.",
							Required: &required,
						},
					},
					SeedFilesToRead: []appplans.ContextFileRead{
						{
							RepoID:   "relay",
							Path:     "internal/app/drift/service.go",
							Purpose:  "Update orchestration.",
							Required: &required,
						},
					},
					ContextCoverageExpectations: []string{"Drift review evidence is persisted."},
					BlockedIfMissing:            []string{"Review packet cannot be retrieved."},
				},
				SourceSnapshotRequirements: appplans.SourceSnapshotRequirements{
					RequireGitStatus: &required,
				},
				HandoffReadinessCriteria: []string{"Internal review can be produced."},
				RiskLevel:                "low",
				ContextBudget: &appplans.ContextBudget{
					MaxFiles: &maxFiles,
				},
			},
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal plan: %v", err)
	}
	return raw
}

func canonicalJSONSHA256(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal plan: %v", err)
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal canonical plan: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}
