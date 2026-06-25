package drift

import (
	"encoding/json"

	"relay/internal/store"
)

// ReviewFailureCode describes why an internal drift review was blocked or failed.
type ReviewFailureCode string

const (
	// FailureModelCallNotAllowed is returned when AllowModelCall=false in the request.
	FailureModelCallNotAllowed ReviewFailureCode = "model_call_not_allowed"
	// FailureModelProviderUnavailable is returned when no DriftReviewer is configured.
	FailureModelProviderUnavailable ReviewFailureCode = "model_provider_unavailable"
	// FailurePacketRetrievalFailed is returned when the review packet cannot be retrieved.
	FailurePacketRetrievalFailed ReviewFailureCode = "packet_retrieval_failed"
	// FailurePacketBlockedSensitive is returned when the packet redaction_status is blocked_sensitive_content.
	FailurePacketBlockedSensitive ReviewFailureCode = "packet_blocked_sensitive"
	// FailureSecretDetectedInPacket is returned when secret-like content is detected in the packet.
	FailureSecretDetectedInPacket ReviewFailureCode = "secret_detected_in_packet"
	// FailureSchemaValidationFailed is returned when the model output fails schema validation after all retries.
	FailureSchemaValidationFailed ReviewFailureCode = "schema_validation_failed"
	// FailureReviewGenerationFailed is returned when the provider call or normalization fails after escalation.
	FailureReviewGenerationFailed ReviewFailureCode = "review_generation_failed"
	// FailureAttemptNotDraft is returned when the plan attempt is not in draft status.
	FailureAttemptNotDraft ReviewFailureCode = "attempt_not_draft"
	// FailureUnsafeRetrievalSemantics is returned when the retrieval semantics indicate a non-retrieval call.
	FailureUnsafeRetrievalSemantics ReviewFailureCode = "unsafe_retrieval_semantics"
)

// SubmittedByInternalReviewer is the stable submitted_by value used for all internally-generated reviews.
// It is not a user identity, secret, e-mail, or API key.
const SubmittedByInternalReviewer = "relay-internal-drift-reviewer"

// InternalReviewRequest is the entry-point request for RunInternalReview.
type InternalReviewRequest struct {
	// ProjectID is the project the plan attempt belongs to.
	ProjectID string
	// PlanAttemptID is the plan attempt to review.
	PlanAttemptID string
	// RequestedTier is the model tier to use: economy, standard, high_assurance, or auto_escalate.
	// When auto_escalate, the service starts at standard and escalates to high_assurance if needed.
	RequestedTier string
	// AllowModelCall must be explicitly true for a model call to occur.
	// When false, the service returns FailureModelCallNotAllowed immediately.
	AllowModelCall bool
	// ForceHighAssurance overrides RequestedTier and starts at high_assurance.
	ForceHighAssurance bool
	// SubmittedBy is the identity recorded in the drift review. Defaults to SubmittedByInternalReviewer.
	SubmittedBy string
}

// InternalReviewResult is the result of RunInternalReview.
type InternalReviewResult struct {
	// OK is true when a valid review was persisted.
	OK bool
	// FailureCode is set when OK=false.
	FailureCode ReviewFailureCode
	// Message provides a human-readable explanation when OK=false.
	Message string
	// Escalated is true when the service retried at a higher tier.
	Escalated bool
	// EscalationReason describes why escalation occurred.
	EscalationReason string
	// FinalTier is the tier used for the accepted provider response.
	FinalTier string
	// DriftReview is the persisted review record when OK=true.
	DriftReview *store.IntentDriftReview
}

// ReviewModelRequest is the bounded input sent to a DriftReviewer implementation.
// It contains only derived-from-packet content; no live chat history or secrets.
type ReviewModelRequest struct {
	// Tier is the requested model tier for this call (economy, standard, or high_assurance).
	Tier string
	// PromptText is the structured text prompt instructing the model.
	PromptText string
	// InputPayload is the serialized JSON review packet evidence sent as model input.
	// Its sha256 hash is recorded as input_hash in the drift review.
	InputPayload []byte
	// SchemaHint is the intent_drift_review schema JSON provided as structured output context.
	SchemaHint []byte
	// Temperature is the requested sampling temperature (0 for deterministic).
	Temperature float64
}

// ReviewModelResponse is the raw response from a DriftReviewer implementation.
type ReviewModelResponse struct {
	// RawJSON is the raw JSON bytes returned by the model. Must be valid JSON.
	RawJSON []byte
	// Provider is the provider identifier (e.g. "fake", "openai").
	Provider string
	// Model is the model identifier (e.g. "fake-standard", "gpt-4o").
	Model string
	// FinalTier is the tier that was actually used for this call.
	FinalTier string
	// Temperature is the temperature used for this call.
	Temperature float64
}

// ModelOutput is the normalized model output struct parsed from ReviewModelResponse.RawJSON.
// The model is expected to return JSON conforming to this shape.
type ModelOutput struct {
	// OverallAlignment is one of: aligned, minor_drift, major_drift, unclear.
	OverallAlignment string `json:"overall_alignment"`
	// Confidence is a float in [0,1] expressing the model's confidence in its assessment.
	Confidence float64 `json:"confidence"`
	// Findings is a JSON array of finding objects. May be empty when aligned+approve.
	Findings json.RawMessage `json:"findings"`
	// RecommendedAction is one of: approve, approve_with_acknowledgement, revise, void, manual_review.
	RecommendedAction string `json:"recommended_action"`
	// ApprovalGateStatus is one of: not_required, ready, acknowledgement_required, revision_required, blocked.
	ApprovalGateStatus string `json:"approval_gate_status"`
	// Notes is an optional free-text field for reviewer reasoning.
	Notes string `json:"notes,omitempty"`
}

// normalizedGateStatus maps a RecommendedAction to its canonical ApprovalGateStatus.
// This enforces consistency between the model-output recommended_action and approval_gate_status.
// The mapped gate is always the stricter of the two.
func normalizedGateStatus(recommendedAction string) string {
	switch recommendedAction {
	case "approve":
		return "ready"
	case "approve_with_acknowledgement":
		return "acknowledgement_required"
	case "revise":
		return "revision_required"
	case "void", "manual_review":
		return "blocked"
	default:
		return "blocked"
	}
}

// escalationRequired returns true when the model output requires a retry at a higher tier.
func escalationRequired(out ModelOutput, schemaErr error, forceHigh bool) bool {
	if schemaErr != nil {
		return true
	}
	if forceHigh {
		return true
	}
	if out.Confidence < 0.70 {
		return true
	}
	if out.OverallAlignment == "unclear" || out.OverallAlignment == "major_drift" {
		return true
	}
	if out.RecommendedAction == "manual_review" {
		return true
	}
	return false
}

// escalationReason returns a human-readable reason string for why escalation occurred.
func escalationReason(out ModelOutput, schemaErr error, forceHigh bool) string {
	if schemaErr != nil {
		return "model output failed schema validation"
	}
	if forceHigh {
		return "force_high_assurance requested"
	}
	if out.Confidence < 0.70 {
		return "confidence below 0.70 threshold"
	}
	if out.OverallAlignment == "unclear" {
		return "overall_alignment is unclear"
	}
	if out.OverallAlignment == "major_drift" {
		return "overall_alignment is major_drift"
	}
	if out.RecommendedAction == "manual_review" {
		return "recommended_action is manual_review"
	}
	return ""
}
