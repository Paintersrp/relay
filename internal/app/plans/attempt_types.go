package plans

import (
	"encoding/json"

	"relay/internal/store"
)

type IntentPacket = store.IntentPacket
type PlanAttempt = store.PlanAttempt
type IntentDriftReview = store.IntentDriftReview

// Plan attempt status constants
const (
	PlanAttemptStatusDraft      = "draft"
	PlanAttemptStatusApproved   = "approved"
	PlanAttemptStatusSubmitted  = "submitted"
	PlanAttemptStatusVoided     = "voided"
	PlanAttemptStatusSuperseded = "superseded"
)

// Plan attempt review state constants
const (
	PlanAttemptReviewNotRequested      = "not_requested"
	PlanAttemptReviewPacketReady       = "review_packet_ready"
	PlanAttemptReviewExternalSubmitted = "external_review_submitted"
	PlanAttemptReviewInternalGenerated = "internal_review_generated"
	PlanAttemptReviewApprovalReady     = "approval_ready"
	PlanAttemptReviewRevisionRequested = "revision_requested"
	PlanAttemptReviewBlocked           = "blocked"
)

// Drift review mode constants
const (
	DriftReviewModeDisabled  = "disabled"
	DriftReviewModeManual    = "manual"
	DriftReviewModeAutomatic = "automatic"
	DriftReviewModeExternal  = "external"
)

// Model tier constants
const (
	ModelTierEconomy       = "economy"
	ModelTierStandard      = "standard"
	ModelTierHighAssurance = "high_assurance"
	ModelTierAutoEscalate  = "auto_escalate"
)

// Intent packet kind constants
const (
	IntentKindOriginal = "original"
	IntentKindRevision = "revision"
)

// Captured from constants
const (
	CapturedFromPlannerChat   = "planner_chat"
	CapturedFromRevisionNotes = "revision_notes"
	CapturedFromImportedReq   = "imported_request"
)

// Redaction status constants
const (
	RedactionStatusNotRequired       = "not_required"
	RedactionStatusRedacted          = "redacted"
	RedactionStatusVerifiedNoSecrets = "verified_no_secrets"
	RedactionStatusBlockedSensitive  = "blocked_sensitive_content"
)

// Overall alignment constants
const (
	OverallAlignmentAligned    = "aligned"
	OverallAlignmentMinorDrift = "minor_drift"
	OverallAlignmentMajorDrift = "major_drift"
	OverallAlignmentUnclear    = "unclear"
)

// Recommended action constants
const (
	RecommendedActionApprove        = "approve"
	RecommendedActionApproveWithAck = "approve_with_acknowledgement"
	RecommendedActionRevise         = "revise"
	RecommendedActionVoid           = "void"
	RecommendedActionManualReview   = "manual_review"
)

// Approval gate status constants
const (
	ApprovalGateStatusNotRequired      = "not_required"
	ApprovalGateStatusReady            = "ready"
	ApprovalGateStatusAckRequired      = "acknowledgement_required"
	ApprovalGateStatusRevisionRequired = "revision_required"
	ApprovalGateStatusBlocked          = "blocked"
)

// Review source constants
const (
	ReviewSourceExternal = "external"
	ReviewSourceInternal = "internal"
)

// Blocker codes for plan attempt operations
type PlanAttemptBlockerCode string

const (
	BlockerUnknownProject          PlanAttemptBlockerCode = "unknown_project"
	BlockerUnknownAttempt          PlanAttemptBlockerCode = "unknown_attempt"
	BlockerAttemptNotReviewable    PlanAttemptBlockerCode = "attempt_not_reviewable"
	BlockerStaleAttempt            PlanAttemptBlockerCode = "stale_attempt"
	BlockerMissingIntentPacket     PlanAttemptBlockerCode = "missing_intent_packet"
	BlockerMissingPlanArtifact     PlanAttemptBlockerCode = "missing_plan_artifact"
	BlockerArtifactHashMismatch    PlanAttemptBlockerCode = "artifact_hash_mismatch"
	BlockerUnsafeRetrieval         PlanAttemptBlockerCode = "unsafe_retrieval"
	BlockerApprovalRequired        PlanAttemptBlockerCode = "approval_required"
	BlockerDriftAcknowledgementReq PlanAttemptBlockerCode = "drift_acknowledgement_required"
	BlockerDriftReviewRequired     PlanAttemptBlockerCode = "drift_review_required"
	BlockerDriftRevisionRequired   PlanAttemptBlockerCode = "drift_revision_required"
	BlockerDriftReviewBlocked      PlanAttemptBlockerCode = "drift_review_blocked"
)

// PlanArtifactRef represents a reference to a plan artifact file
type PlanArtifactRef struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	ArtifactKind string `json:"artifact_kind"` // "planner-pass-plan-json" or "planner-pass-plan-markdown"
}

// IntentSource captures the origin of the intent
type IntentSource struct {
	CapturedFrom       string `json:"captured_from"`
	CapturedBy         string `json:"captured_by"`
	SourceArtifactPath string `json:"source_artifact_path"`
}

// IntentPacketInput represents the input for creating an intent packet
type IntentPacketInput struct {
	Summary            string       `json:"summary"`
	LiteralUserRequest string       `json:"literal_user_request"`
	Constraints        []string     `json:"constraints"`
	Source             IntentSource `json:"source"`
	RedactionStatus    string       `json:"redaction_status"`
	ContentHash        string       `json:"content_hash"`
}

// RawPlanJSONRef holds the raw plan JSON content and its hash
type RawPlanJSONRef struct {
	Content     json.RawMessage `json:"content"`
	ContentHash string          `json:"content_hash"`
}

// RetrievalSemantics indicates the retrieval behavior of a review packet
type RetrievalSemantics struct {
	RetrievalOnly      bool `json:"retrieval_only"`
	ModelCallPerformed bool `json:"model_call_performed"`
	StateMutated       bool `json:"state_mutated"`
}

// IntentPacketEvidence represents evidence about an intent packet in a review packet
type IntentPacketEvidence struct {
	IntentPacketID     string `json:"intent_packet_id"`
	Kind               string `json:"kind"`
	Summary            string `json:"summary"`
	LiteralUserRequest string `json:"literal_user_request"`
	Constraints        string `json:"constraints"` // JSON array
	ContentHash        string `json:"content_hash"`
	RedactionStatus    string `json:"redaction_status"`
	SourceArtifactPath string `json:"source_artifact_path"`
	CreatedAt          string `json:"created_at"`
}

// PlanAttemptEvidence represents evidence about a plan attempt in a review packet
type PlanAttemptEvidence struct {
	PlanAttemptID            string `json:"plan_attempt_id"`
	Status                   string `json:"status"`
	ReviewState              string `json:"review_state"`
	DriftReviewMode          string `json:"drift_review_mode"`
	ModelTier                string `json:"model_tier"`
	CurrentIntentPacketID    string `json:"current_intent_packet_id"`
	RootIntentPacketID       string `json:"root_intent_packet_id"`
	SupersedesPlanAttemptID  string `json:"supersedes_plan_attempt_id"`
	ReplacementPlanAttemptID string `json:"replacement_plan_attempt_id"`
	AcceptedDriftReviewID    string `json:"accepted_drift_review_id"`
	SubmittedPlanID          string `json:"submitted_plan_id"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

// PlanArtifactsEvidence represents artifact references in a review packet
type PlanArtifactsEvidence struct {
	JSONArtifactPath       string `json:"json_artifact_path"`
	JSONArtifactSHA256     string `json:"json_artifact_sha256"`
	MarkdownArtifactPath   string `json:"markdown_artifact_path,omitempty"`
	MarkdownArtifactSHA256 string `json:"markdown_artifact_sha256,omitempty"`
	RawPlanJSONHash        string `json:"raw_plan_json_hash"`
}

// PlanIntentReviewPacket represents a bounded review packet for intent review
type PlanIntentReviewPacket struct {
	PacketID              string                `json:"packet_id"`
	ProjectID             string                `json:"project_id"`
	PlanAttemptID         string                `json:"plan_attempt_id"`
	IntentThreadID        string                `json:"intent_thread_id"`
	RootIntentPacket      IntentPacketEvidence  `json:"root_intent_packet"`
	ReviewedIntentPacket  IntentPacketEvidence  `json:"reviewed_intent_packet"`
	PlanAttempt           PlanAttemptEvidence   `json:"plan_attempt"`
	RawPlanJSON           json.RawMessage       `json:"raw_plan_json,omitempty"`
	PlanArtifacts         PlanArtifactsEvidence `json:"plan_artifacts"`
	PriorAttemptSummaries []PriorAttemptInfo    `json:"prior_attempt_summaries"`
	PriorReviewSummaries  []PriorReviewInfo     `json:"prior_drift_review_summaries"`
	RedactionStatus       string                `json:"redaction_status"`
	RetrievalSemantics    RetrievalSemantics    `json:"retrieval_semantics"`
	PacketHash            string                `json:"packet_hash"`
	GeneratedAt           string                `json:"generated_at"`
}

// PriorAttemptInfo provides info about prior attempts in the thread
type PriorAttemptInfo struct {
	PlanAttemptID         string `json:"plan_attempt_id"`
	Status                string `json:"status"`
	ReviewState           string `json:"review_state"`
	CurrentIntentPacketID string `json:"current_intent_packet_id"`
	SupersedesID          string `json:"supersedes_plan_attempt_id,omitempty"`
	ReplacementID         string `json:"replacement_plan_attempt_id,omitempty"`
	SubmittedPlanID       string `json:"submitted_plan_id,omitempty"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

// PriorReviewInfo provides info about prior drift reviews
type PriorReviewInfo struct {
	IntentDriftReviewID string `json:"intent_drift_review_id"`
	ReviewSource        string `json:"review_source"`
	OverallAlignment    string `json:"overall_alignment"`
	RecommendedAction   string `json:"recommended_action"`
	ApprovalGateStatus  string `json:"approval_gate_status"`
	ReviewPacketHash    string `json:"review_packet_hash"`
	CreatedAt           string `json:"created_at"`
}

// DriftReviewInput represents input for submitting an intent drift review
type DriftReviewInput struct {
	IntentDriftReviewID    string          `json:"intent_drift_review_id"`
	PlanAttemptID          string          `json:"plan_attempt_id"`
	IntentThreadID         string          `json:"intent_thread_id"`
	RootIntentPacketID     string          `json:"root_intent_packet_id"`
	ReviewedIntentPacketID string          `json:"reviewed_intent_packet_id"`
	ReviewPacketHash       string          `json:"review_packet_hash"`
	ReviewSource           string          `json:"review_source"`
	SubmittedBy            string          `json:"submitted_by"`
	SourceArtifactPath     string          `json:"source_artifact_path"`
	OverallAlignment       string          `json:"overall_alignment"`
	Confidence             float64         `json:"confidence"`
	FindingsJSON           json.RawMessage `json:"findings_json"`
	RecommendedAction      string          `json:"recommended_action"`
	ApprovalGateStatus     string          `json:"approval_gate_status"`
	ModelMetadataJSON      json.RawMessage `json:"model_metadata_json,omitempty"`
	InputHash              string          `json:"input_hash"`
	OutputHash             string          `json:"output_hash"`
}

// CreatePlanAttemptWithIntentRequest is the request to create a draft plan attempt with intent
type CreatePlanAttemptWithIntentRequest struct {
	ProjectID           string            `json:"project_id"`
	PlanAttemptID       string            `json:"plan_attempt_id,omitempty"` // optional, generated if empty
	IntentPacketID      string            `json:"intent_packet_id,omitempty"`
	IntentThreadID      string            `json:"intent_thread_id,omitempty"`
	PlanArtifactRef     PlanArtifactRef   `json:"plan_artifact_ref"`
	OptionalMarkdownRef *PlanArtifactRef  `json:"optional_markdown_ref,omitempty"`
	RawPlanJSON         json.RawMessage   `json:"raw_plan_json"`
	DriftReviewMode     string            `json:"drift_review_mode"`
	ModelTier           string            `json:"model_tier"`
	IntentPacket        IntentPacketInput `json:"intent_packet"`
}

// GetPlanIntentReviewPacketRequest is the request to get a review packet
type GetPlanIntentReviewPacketRequest struct {
	ProjectID     string `json:"project_id"`
	PlanAttemptID string `json:"plan_attempt_id"`
}

// SubmitIntentDriftReviewRequest is the request to submit an external drift review
type SubmitIntentDriftReviewRequest struct {
	ProjectID     string           `json:"project_id"`
	PlanAttemptID string           `json:"plan_attempt_id"`
	DriftReview   DriftReviewInput `json:"drift_review"`
}

// RevisePlanAttemptRequest is the request to revise a plan attempt
type RevisePlanAttemptRequest struct {
	ProjectID           string            `json:"project_id"`
	PlanAttemptID       string            `json:"plan_attempt_id"`
	NewPlanAttemptID    string            `json:"new_plan_attempt_id,omitempty"`
	NewIntentPacketID   string            `json:"new_intent_packet_id,omitempty"`
	PlanArtifactRef     PlanArtifactRef   `json:"plan_artifact_ref"`
	OptionalMarkdownRef *PlanArtifactRef  `json:"optional_markdown_ref,omitempty"`
	RawPlanJSON         json.RawMessage   `json:"raw_plan_json"`
	NewIntentPacket     IntentPacketInput `json:"new_intent_packet"`
}

// VoidPlanAttemptRequest is the request to void a plan attempt
type VoidPlanAttemptRequest struct {
	ProjectID     string `json:"project_id"`
	PlanAttemptID string `json:"plan_attempt_id"`
}

// ApprovePlanAttemptRequest is the request to approve a plan attempt
type ApprovePlanAttemptRequest struct {
	ProjectID                 string `json:"project_id"`
	PlanAttemptID             string `json:"plan_attempt_id"`
	Approved                  bool   `json:"approved"`
	AcceptedDriftReviewID     string `json:"accepted_drift_review_id,omitempty"`
	DriftAcknowledged         bool   `json:"drift_acknowledged"`
	NoDriftReviewAcknowledged bool   `json:"no_drift_review_acknowledged"`
}

// SubmitPlanAttemptRequest is the request to submit a plan attempt as a managed plan
type SubmitPlanAttemptRequest struct {
	ProjectID     string `json:"project_id"`
	PlanAttemptID string `json:"plan_attempt_id"`
}

// PlanAttemptResult is the result of a plan attempt operation
type PlanAttemptResult struct {
	OK           bool                     `json:"ok"`
	BlockerCode  PlanAttemptBlockerCode   `json:"blocker_code,omitempty"`
	Message      string                   `json:"message,omitempty"`
	IntentPacket *store.IntentPacket      `json:"intent_packet,omitempty"`
	PlanAttempt  *store.PlanAttempt       `json:"plan_attempt,omitempty"`
	DriftReview  *store.IntentDriftReview `json:"drift_review,omitempty"`
	Plan         *store.Plan              `json:"plan,omitempty"`
	Passes       []store.PlanPass         `json:"passes,omitempty"`
	ReviewPacket *PlanIntentReviewPacket  `json:"review_packet,omitempty"`
}
