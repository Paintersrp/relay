package drift

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"testing"

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
		PacketID:      "pkt-1",
		ProjectID:     "proj-1",
		PlanAttemptID: "attempt-1",
		IntentThreadID: "thread-1",
		RootIntentPacket: appplans.IntentPacketEvidence{
			IntentPacketID: "intent-1",
			Kind:           "original",
			Summary:        "summary",
			LiteralUserRequest: "user request",
			Constraints:    "[]",
		},
		ReviewedIntentPacket: appplans.IntentPacketEvidence{
			IntentPacketID: "intent-1",
			Kind:           "original",
			Summary:        "summary",
			LiteralUserRequest: "user request",
			Constraints:    "[]",
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

func TestRunInternalReviewBlocksWithoutExplicitModelCallAllowance(t *testing.T) {
	svc := NewService(&fakePlanService{}, &fakeReviewer{})
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
}

func TestRunInternalReviewBlocksWithoutProvider(t *testing.T) {
	svc := NewService(&fakePlanService{}, nil)
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
}

func TestRunInternalReviewUsesReviewPacketOnlyAsModelInput(t *testing.T) {
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
}

func TestRunInternalReviewPersistsInternalReviewThroughPlanService(t *testing.T) {
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
}

func TestRunInternalReviewValidatesSchemaBeforePersistence(t *testing.T) {
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

func TestRunInternalReviewBlocksSensitivePacketContent(t *testing.T) {
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

func TestRunInternalReviewAutoEscalatesOnLowConfidence(t *testing.T) {
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

func TestRunInternalReviewNormalizesGateConsistency(t *testing.T) {
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
	      "finding_type": "drift",
	      "severity": "medium",
	      "description": "minor code structure changes"
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
