package drift

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	appplans "relay/internal/app/plans"
	"relay/internal/validation"
)

// PlanAttemptService interface matches the plans.Service seams we consume.
type PlanAttemptService interface {
	GetPlanIntentReviewPacket(ctx context.Context, req appplans.GetPlanIntentReviewPacketRequest) (*appplans.PlanAttemptResult, error)
	SubmitIntentDriftReview(ctx context.Context, req appplans.SubmitIntentDriftReviewRequest) (*appplans.PlanAttemptResult, error)
}

// Service orchestrates Relay-internal LLM intent drift reviews.
type Service struct {
	plans    PlanAttemptService
	reviewer DriftReviewer
	now      func() time.Time
}

// NewService creates a new drift review service.
func NewService(plans PlanAttemptService, reviewer DriftReviewer) *Service {
	return &Service{
		plans:    plans,
		reviewer: reviewer,
		now:      time.Now,
	}
}

// RunInternalReview runs the internal drift review workflow.
func (svc *Service) RunInternalReview(ctx context.Context, req InternalReviewRequest) (*InternalReviewResult, error) {
	// 1. Guard: AllowModelCall=false
	if !req.AllowModelCall {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureModelCallNotAllowed,
			Message:     "model call is not explicitly allowed in the request",
		}, nil
	}

	// 2. Guard: reviewer is nil
	if svc.reviewer == nil {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureModelProviderUnavailable,
			Message:     "no model reviewer provider is configured",
		}, nil
	}

	// 3. Retrieve review packet
	res, err := svc.plans.GetPlanIntentReviewPacket(ctx, appplans.GetPlanIntentReviewPacketRequest{
		ProjectID:     req.ProjectID,
		PlanAttemptID: req.PlanAttemptID,
	})
	if err != nil {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailurePacketRetrievalFailed,
			Message:     fmt.Sprintf("failed to retrieve review packet: %v", err),
		}, nil
	}
	if res == nil || !res.OK || res.ReviewPacket == nil {
		msg := "packet retrieval failed"
		if res != nil && res.Message != "" {
			msg = res.Message
		}
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailurePacketRetrievalFailed,
			Message:     msg,
		}, nil
	}

	packet := res.ReviewPacket

	// 4. Validate retrieval semantics
	if !packet.RetrievalSemantics.RetrievalOnly || packet.RetrievalSemantics.ModelCallPerformed || packet.RetrievalSemantics.StateMutated {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureUnsafeRetrievalSemantics,
			Message:     "review packet retrieval semantics are unsafe or incorrect",
		}, nil
	}

	// 5. Guard: attempt status (Status != "draft")
	if packet.PlanAttempt.Status != appplans.PlanAttemptStatusDraft {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureAttemptNotDraft,
			Message:     fmt.Sprintf("plan attempt is not in draft status (current: %s)", packet.PlanAttempt.Status),
		}, nil
	}

	// 6. Guard: redaction
	if packet.RedactionStatus == appplans.RedactionStatusBlockedSensitive ||
		packet.RootIntentPacket.RedactionStatus == appplans.RedactionStatusBlockedSensitive ||
		packet.ReviewedIntentPacket.RedactionStatus == appplans.RedactionStatusBlockedSensitive {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailurePacketBlockedSensitive,
			Message:     "packet contains blocked sensitive content and cannot be sent to the model",
		}, nil
	}

	// 7. Guard: secret scan
	if hasSecretInPacket(*packet) {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureSecretDetectedInPacket,
			Message:     "secret-like content detected in review packet",
		}, nil
	}

	// 8. Tier selection
	currentTier := req.RequestedTier
	if currentTier == appplans.ModelTierAutoEscalate || currentTier == "" {
		currentTier = appplans.ModelTierStandard
	}
	if req.ForceHighAssurance {
		currentTier = appplans.ModelTierHighAssurance
	}

	// 9. Build prompt/input
	promptText, inputPayload, err := BuildPromptInput(*packet)
	if err != nil {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureReviewGenerationFailed,
			Message:     fmt.Sprintf("failed to build model prompt input: %v", err),
		}, nil
	}
	inputHash := sha256Bytes(inputPayload)

	// 10. Call provider and validate
	var providerRes ReviewModelResponse
	var modelOut ModelOutput
	var isSchemaErr bool

	makeCall := func(tier string) error {
		isSchemaErr = false
		var err error
		providerRes, err = svc.reviewer.ReviewIntentDrift(ctx, ReviewModelRequest{
			Tier:         tier,
			PromptText:   promptText,
			InputPayload: inputPayload,
			SchemaHint:   intentDriftReviewSchemaBytes,
			Temperature:  0.0,
		})
		if err != nil {
			return err
		}

		// Normalize & Parse
		if err := json.Unmarshal(providerRes.RawJSON, &modelOut); err != nil {
			isSchemaErr = true
			return fmt.Errorf("unmarshal model raw JSON: %w", err)
		}

		// Normalize gate status consistency
		mappedGate := normalizedGateStatus(modelOut.RecommendedAction)
		modelOut.ApprovalGateStatus = stricterGateStatus(modelOut.ApprovalGateStatus, mappedGate)

		// Validate against schema
		meta := ModelMetadata{
			Provider:    providerRes.Provider,
			Model:       providerRes.Model,
			ModelTier:   tier,
			Temperature: providerRes.Temperature,
		}
		metaBytes, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal model metadata: %w", err)
		}

		findingsBytes := modelOut.Findings
		if len(findingsBytes) == 0 {
			findingsBytes = json.RawMessage("[]")
		}

		submittedBy := req.SubmittedBy
		if submittedBy == "" {
			submittedBy = SubmittedByInternalReviewer
		}

		validationObj := schemaValidationStruct{
			IntentDriftReviewID:    generateReviewID(svc.now()),
			PlanAttemptID:          packet.PlanAttemptID,
			IntentThreadID:         packet.IntentThreadID,
			RootIntentPacketID:     packet.RootIntentPacket.IntentPacketID,
			ReviewedIntentPacketID: packet.ReviewedIntentPacket.IntentPacketID,
			ReviewPacketHash:       packet.PacketHash,
			ReviewSource:           appplans.ReviewSourceInternal,
			SubmittedBy:            submittedBy,
			SourceArtifactPath:     packet.ReviewedIntentPacket.SourceArtifactPath,
			OverallAlignment:       modelOut.OverallAlignment,
			Confidence:             modelOut.Confidence,
			Findings:               findingsBytes,
			RecommendedAction:      modelOut.RecommendedAction,
			ApprovalGateStatus:     modelOut.ApprovalGateStatus,
			ModelMetadata:          metaBytes,
			InputHash:              inputHash,
			OutputHash:             sha256Bytes(providerRes.RawJSON),
			Notes:                  modelOut.Notes,
		}

		valBytes, err := json.Marshal(validationObj)
		if err != nil {
			return fmt.Errorf("marshal validation object: %w", err)
		}

		if err := ValidateIntentDriftReviewJSON(valBytes); err != nil {
			isSchemaErr = true
			return fmt.Errorf("validate schema: %w", err)
		}

		return nil
	}

	callErr := makeCall(currentTier)

	escalated := false
	escReason := ""

	// Check if auto-escalation check triggers a single retry at high_assurance
	if currentTier != appplans.ModelTierHighAssurance && (callErr != nil || escalationRequired(modelOut, callErr, req.ForceHighAssurance)) {
		escalated = true
		escReason = escalationReason(modelOut, callErr, req.ForceHighAssurance)
		if escReason == "" {
			if callErr != nil {
				escReason = fmt.Sprintf("initial call failed: %v", callErr)
			} else {
				escReason = "escalation policy triggered"
			}
		}

		currentTier = appplans.ModelTierHighAssurance
		callErr = makeCall(currentTier)
	}

	if callErr != nil {
		code := FailureReviewGenerationFailed
		if isSchemaErr {
			code = FailureSchemaValidationFailed
		}
		return &InternalReviewResult{
			OK:               false,
			FailureCode:      code,
			Message:          fmt.Sprintf("review generation failed: %v", callErr),
			Escalated:        escalated,
			EscalationReason: escReason,
			FinalTier:        currentTier,
		}, nil
	}

	// 14. Build DriftReviewInput
	findingsJSON := modelOut.Findings
	if len(findingsJSON) == 0 {
		findingsJSON = json.RawMessage("[]")
	}

	meta := ModelMetadata{
		Provider:    providerRes.Provider,
		Model:       providerRes.Model,
		ModelTier:   currentTier,
		Temperature: providerRes.Temperature,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal final model metadata: %w", err)
	}

	submittedBy := req.SubmittedBy
	if submittedBy == "" {
		submittedBy = SubmittedByInternalReviewer
	}

	reviewID := generateReviewID(svc.now())

	driftInput := appplans.DriftReviewInput{
		IntentDriftReviewID:    reviewID,
		PlanAttemptID:          packet.PlanAttemptID,
		IntentThreadID:         packet.IntentThreadID,
		RootIntentPacketID:     packet.RootIntentPacket.IntentPacketID,
		ReviewedIntentPacketID: packet.ReviewedIntentPacket.IntentPacketID,
		ReviewPacketHash:       packet.PacketHash,
		ReviewSource:           appplans.ReviewSourceInternal,
		SubmittedBy:            submittedBy,
		SourceArtifactPath:     packet.ReviewedIntentPacket.SourceArtifactPath,
		OverallAlignment:       modelOut.OverallAlignment,
		Confidence:             modelOut.Confidence,
		FindingsJSON:           findingsJSON,
		RecommendedAction:      modelOut.RecommendedAction,
		ApprovalGateStatus:     modelOut.ApprovalGateStatus,
		ModelMetadataJSON:      metaBytes,
		InputHash:              inputHash,
		OutputHash:             sha256Bytes(providerRes.RawJSON),
	}

	// 15. Persist via SubmitIntentDriftReview
	submitRes, err := svc.plans.SubmitIntentDriftReview(ctx, appplans.SubmitIntentDriftReviewRequest{
		ProjectID:     req.ProjectID,
		PlanAttemptID: req.PlanAttemptID,
		DriftReview:   driftInput,
	})
	if err != nil {
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureReviewGenerationFailed,
			Message:     fmt.Sprintf("failed to submit drift review: %v", err),
			Escalated:   escalated,
			EscalationReason: escReason,
			FinalTier:   currentTier,
		}, nil
	}
	if submitRes == nil || !submitRes.OK {
		msg := "drift review submission rejected by plan service"
		if submitRes != nil && submitRes.Message != "" {
			msg = submitRes.Message
		}
		return &InternalReviewResult{
			OK:          false,
			FailureCode: FailureReviewGenerationFailed,
			Message:     msg,
			Escalated:   escalated,
			EscalationReason: escReason,
			FinalTier:   currentTier,
		}, nil
	}

	// 16. Return
	return &InternalReviewResult{
		OK:               true,
		Escalated:        escalated,
		EscalationReason: escReason,
		FinalTier:        currentTier,
		DriftReview:      submitRes.DriftReview,
	}, nil
}

type schemaValidationStruct struct {
	IntentDriftReviewID    string          `json:"intent_drift_review_id"`
	PlanAttemptID          string          `json:"plan_attempt_id"`
	IntentThreadID         string          `json:"intent_thread_id"`
	RootIntentPacketID     string          `json:"root_intent_packet_id"`
	ReviewedIntentPacketID string          `json:"reviewed_intent_packet_id"`
	ReviewPacketHash       string          `json:"review_packet_hash"`
	ReviewSource           string          `json:"review_source"`
	SubmittedBy            string          `json:"submitted_by"`
	SourceArtifactPath     string          `json:"source_artifact_path,omitempty"`
	OverallAlignment       string          `json:"overall_alignment"`
	Confidence             float64         `json:"confidence"`
	Findings               json.RawMessage `json:"findings"`
	RecommendedAction      string          `json:"recommended_action"`
	ApprovalGateStatus     string          `json:"approval_gate_status"`
	ModelMetadata          json.RawMessage `json:"model_metadata,omitempty"`
	InputHash              string          `json:"input_hash"`
	OutputHash             string          `json:"output_hash"`
	Notes                  string          `json:"notes,omitempty"`
}

type ModelMetadata struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	ModelTier   string  `json:"model_tier"`
	Temperature float64 `json:"temperature"`
}

func sha256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func generateReviewID(t time.Time) string {
	return fmt.Sprintf("intent-drift-review-%s-%d", t.Format("2006-01-02"), t.UnixNano())
}

func hasSecretInPacket(packet appplans.PlanIntentReviewPacket) bool {
	fields := []string{
		packet.RootIntentPacket.LiteralUserRequest,
		packet.RootIntentPacket.Summary,
		packet.RootIntentPacket.Constraints,
		packet.ReviewedIntentPacket.LiteralUserRequest,
		packet.ReviewedIntentPacket.Summary,
		packet.ReviewedIntentPacket.Constraints,
		string(packet.RawPlanJSON),
	}
	for _, f := range fields {
		if f != "" && validation.HasSecret(f) {
			return true
		}
	}
	return false
}

func stricterGateStatus(statusA, statusB string) string {
	levels := map[string]int{
		"not_required":             0,
		"ready":                    1,
		"acknowledgement_required": 2,
		"revision_required":        3,
		"blocked":                  4,
	}
	valA, okA := levels[statusA]
	if !okA {
		valA = 4
	}
	valB, okB := levels[statusB]
	if !okB {
		valB = 4
	}
	if valA >= valB {
		return statusA
	}
	return statusB
}
