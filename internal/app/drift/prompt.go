package drift

import (
	"encoding/json"
	"fmt"
	"strings"

	appplans "relay/internal/app/plans"
)

// promptInstructions is the static system prompt text instructing the model on its role and constraints.
// It is prepended to the structured input evidence in BuildPromptInput.
const promptInstructions = `You are a Relay intent drift reviewer. Your task is to assess whether the reviewed intent packet
and the submitted plan artifact are aligned with the root intent.

Your response MUST be valid JSON conforming to the intent_drift_review schema.

CRITICAL CONSTRAINTS — you MUST NOT:
- Approve, submit, revise, void, create runs, or dispatch executors.
- Mutate git state, plan attempts, or managed plan records directly.
- Reference live chat history, environment variables, file system paths, or secrets not present in the input evidence.
- Claim semantic alignment as a deterministic proof — your output is evidence only.

Your output fields:
- overall_alignment: one of aligned | minor_drift | major_drift | unclear
- confidence: float in [0.0, 1.0]
- findings: JSON array of finding objects (may be empty ONLY when overall_alignment=aligned AND recommended_action=approve)
- recommended_action: one of approve | approve_with_acknowledgement | revise | void | manual_review
- approval_gate_status: one of not_required | ready | acknowledgement_required | revision_required | blocked
- notes: optional string with your reasoning

The approval_gate_status MUST be consistent with recommended_action:
  approve                      → ready
  approve_with_acknowledgement → acknowledgement_required
  revise                       → revision_required
  void                         → blocked
  manual_review                → blocked`

// inputPayload is the structure serialized as model input evidence.
// All fields are derived solely from the PlanIntentReviewPacket.
// No live chat history, filesystem reads, GitHub state, or secrets are included.
type inputPayload struct {
	PacketID      string `json:"packet_id"`
	PacketHash    string `json:"packet_hash"`
	GeneratedAt   string `json:"generated_at"`
	ProjectID     string `json:"project_id"`
	PlanAttemptID string `json:"plan_attempt_id"`

	RootIntentPacket     intentEvidencePayload `json:"root_intent_packet"`
	ReviewedIntentPacket intentEvidencePayload `json:"reviewed_intent_packet"`

	PlanAttempt   planAttemptPayload   `json:"plan_attempt"`
	PlanArtifacts planArtifactsPayload `json:"plan_artifacts"`
	RawPlanJSON   json.RawMessage      `json:"raw_plan_json,omitempty"`

	PriorAttemptSummaries []priorAttemptPayload `json:"prior_attempt_summaries"`
	PriorReviewSummaries  []priorReviewPayload  `json:"prior_drift_review_summaries"`
}

type intentEvidencePayload struct {
	IntentPacketID     string `json:"intent_packet_id"`
	Kind               string `json:"kind"`
	Summary            string `json:"summary"`
	LiteralUserRequest string `json:"literal_user_request"`
	Constraints        string `json:"constraints"`
	ContentHash        string `json:"content_hash"`
	RedactionStatus    string `json:"redaction_status"`
	SourceArtifactPath string `json:"source_artifact_path"`
	CreatedAt          string `json:"created_at"`
}

type planAttemptPayload struct {
	PlanAttemptID   string `json:"plan_attempt_id"`
	Status          string `json:"status"`
	ReviewState     string `json:"review_state"`
	DriftReviewMode string `json:"drift_review_mode"`
	ModelTier       string `json:"model_tier"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type planArtifactsPayload struct {
	JSONArtifactPath   string `json:"json_artifact_path"`
	JSONArtifactSHA256 string `json:"json_artifact_sha256"`
	RawPlanJSONHash    string `json:"raw_plan_json_hash"`
}

type priorAttemptPayload struct {
	PlanAttemptID string `json:"plan_attempt_id"`
	Status        string `json:"status"`
	ReviewState   string `json:"review_state"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type priorReviewPayload struct {
	IntentDriftReviewID string `json:"intent_drift_review_id"`
	ReviewSource        string `json:"review_source"`
	OverallAlignment    string `json:"overall_alignment"`
	RecommendedAction   string `json:"recommended_action"`
	ApprovalGateStatus  string `json:"approval_gate_status"`
	CreatedAt           string `json:"created_at"`
}

// maxRawPlanJSONPromptSize is the maximum raw plan JSON size included in the model input.
// Plans larger than this are excluded to bound the prompt size.
const maxRawPlanJSONPromptSize = 64 * 1024 // 64 KiB

// BuildPromptInput builds the prompt text and serialized input payload from a PlanIntentReviewPacket.
//
// The returned promptText is the static instruction text.
// The returned inputPayloadBytes is a serialized JSON object derived only from the packet.
//
// This function accepts no chat history, filesystem paths, GitHub state, or secrets.
// Its sole input is the bounded review packet produced by the plan attempt service.
func BuildPromptInput(packet appplans.PlanIntentReviewPacket) (promptText string, inputPayloadBytes []byte, err error) {
	priorAttempts := make([]priorAttemptPayload, 0, len(packet.PriorAttemptSummaries))
	for _, a := range packet.PriorAttemptSummaries {
		priorAttempts = append(priorAttempts, priorAttemptPayload{
			PlanAttemptID: a.PlanAttemptID,
			Status:        a.Status,
			ReviewState:   a.ReviewState,
			CreatedAt:     a.CreatedAt,
			UpdatedAt:     a.UpdatedAt,
		})
	}

	priorReviews := make([]priorReviewPayload, 0, len(packet.PriorReviewSummaries))
	for _, r := range packet.PriorReviewSummaries {
		priorReviews = append(priorReviews, priorReviewPayload{
			IntentDriftReviewID: r.IntentDriftReviewID,
			ReviewSource:        r.ReviewSource,
			OverallAlignment:    r.OverallAlignment,
			RecommendedAction:   r.RecommendedAction,
			ApprovalGateStatus:  r.ApprovalGateStatus,
			CreatedAt:           r.CreatedAt,
		})
	}

	// Include raw plan JSON only when within the prompt size limit.
	var rawPlanJSON json.RawMessage
	if len(packet.RawPlanJSON) > 0 && len(packet.RawPlanJSON) <= maxRawPlanJSONPromptSize {
		rawPlanJSON = packet.RawPlanJSON
	}

	payload := inputPayload{
		PacketID:      packet.PacketID,
		PacketHash:    packet.PacketHash,
		GeneratedAt:   packet.GeneratedAt,
		ProjectID:     packet.ProjectID,
		PlanAttemptID: packet.PlanAttemptID,

		RootIntentPacket: intentEvidencePayload{
			IntentPacketID:     packet.RootIntentPacket.IntentPacketID,
			Kind:               packet.RootIntentPacket.Kind,
			Summary:            packet.RootIntentPacket.Summary,
			LiteralUserRequest: packet.RootIntentPacket.LiteralUserRequest,
			Constraints:        packet.RootIntentPacket.Constraints,
			ContentHash:        packet.RootIntentPacket.ContentHash,
			RedactionStatus:    packet.RootIntentPacket.RedactionStatus,
			SourceArtifactPath: packet.RootIntentPacket.SourceArtifactPath,
			CreatedAt:          packet.RootIntentPacket.CreatedAt,
		},
		ReviewedIntentPacket: intentEvidencePayload{
			IntentPacketID:     packet.ReviewedIntentPacket.IntentPacketID,
			Kind:               packet.ReviewedIntentPacket.Kind,
			Summary:            packet.ReviewedIntentPacket.Summary,
			LiteralUserRequest: packet.ReviewedIntentPacket.LiteralUserRequest,
			Constraints:        packet.ReviewedIntentPacket.Constraints,
			ContentHash:        packet.ReviewedIntentPacket.ContentHash,
			RedactionStatus:    packet.ReviewedIntentPacket.RedactionStatus,
			SourceArtifactPath: packet.ReviewedIntentPacket.SourceArtifactPath,
			CreatedAt:          packet.ReviewedIntentPacket.CreatedAt,
		},

		PlanAttempt: planAttemptPayload{
			PlanAttemptID:   packet.PlanAttempt.PlanAttemptID,
			Status:          packet.PlanAttempt.Status,
			ReviewState:     packet.PlanAttempt.ReviewState,
			DriftReviewMode: packet.PlanAttempt.DriftReviewMode,
			ModelTier:       packet.PlanAttempt.ModelTier,
			CreatedAt:       packet.PlanAttempt.CreatedAt,
			UpdatedAt:       packet.PlanAttempt.UpdatedAt,
		},
		PlanArtifacts: planArtifactsPayload{
			JSONArtifactPath:   packet.PlanArtifacts.JSONArtifactPath,
			JSONArtifactSHA256: packet.PlanArtifacts.JSONArtifactSHA256,
			RawPlanJSONHash:    packet.PlanArtifacts.RawPlanJSONHash,
		},
		RawPlanJSON: rawPlanJSON,

		PriorAttemptSummaries: priorAttempts,
		PriorReviewSummaries:  priorReviews,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal prompt input payload: %w", err)
	}

	// Build the full prompt text. The prompt is instruction text only;
	// the structured input evidence is sent separately as InputPayload.
	var sb strings.Builder
	sb.WriteString(promptInstructions)
	sb.WriteString("\n\n")
	sb.WriteString("## Review Evidence (bounded to this packet only)\n\n")
	sb.WriteString("```json\n")
	sb.Write(payloadBytes)
	sb.WriteString("\n```\n\n")
	sb.WriteString("Return your response as a valid JSON object matching the intent_drift_review schema.")

	return sb.String(), payloadBytes, nil
}
