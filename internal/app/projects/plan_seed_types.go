package projects

import (
	"encoding/json"

	appplans "relay/internal/app/plans"
)

const (
	PlanSeedStatusCaptured = "captured"
	PlanSeedStatusPlanned  = "planned"
	PlanSeedStatusDeferred = "deferred"
	PlanSeedStatusRejected = "rejected"

	PlanSeedSourceManual = "manual"
	PlanSeedSourceChat   = "chat"
	PlanSeedSourceMCP    = "mcp"

	DefaultPlanSeedPriority   = "normal"
	DefaultListPlanSeedsLimit = int64(50)
	MaxListPlanSeedsLimit     = int64(100)
)

const (
	PlanSeedIssueRequired          = "required"
	PlanSeedIssueInvalidStatus     = "invalid_status"
	PlanSeedIssueInvalidSourceType = "invalid_source_type"
	PlanSeedIssueInvalidTransition = "invalid_transition"
	PlanSeedIssueTerminalStatus    = "terminal_status"
	PlanSeedIssueSecretLikeValue   = "secret_like_value"
	PlanSeedIssueDuplicateLinkage  = "duplicate_linkage"
)

const (
	PlanSeedBlockerDraftAttemptsUnavailable = "DRAFT_PLAN_ATTEMPTS_UNAVAILABLE"
	PlanSeedBlockerSeedAlreadyPlanned       = "SEED_ALREADY_PLANNED"
	PlanSeedBlockerSeedNotExpandable        = "SEED_NOT_EXPANDABLE"
	PlanSeedBlockerMissingPlanArtifact      = "MISSING_PLAN_ARTIFACT"
	PlanSeedBlockerUnsafeSeedContext        = "UNSAFE_SEED_CONTEXT"
)

type PlanSeedInput struct {
	SeedID       string   `json:"seed_id"`
	Title        string   `json:"title"`
	QuickContext string   `json:"quick_context"`
	Constraints  []string `json:"constraints"`
	NonGoals     []string `json:"non_goals"`
	Tags         []string `json:"tags"`
	Priority     string   `json:"priority"`
	SourceType   string   `json:"source_type"`
	SourceLabel  string   `json:"source_label"`
	SourceRefID  string   `json:"source_ref_id"`
}

type NormalizedPlanSeedInput struct {
	SeedID          string
	Title           string
	QuickContext    string
	Constraints     []string
	NonGoals        []string
	Tags            []string
	ConstraintsJSON string
	NonGoalsJSON    string
	TagsJSON        string
	Priority        string
	SourceType      string
	SourceLabel     string
	SourceRefID     string
}

type PlanSeedValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PlanSeedLifecycleInput struct {
	DeferReason  string `json:"defer_reason"`
	RejectReason string `json:"reject_reason"`
}

type PlanSeedAttemptLinkInput struct {
	PlanAttemptID string `json:"plan_attempt_id"`
}

type PlanSeedManagedPlanLinkInput struct {
	ManagedPlanID string `json:"managed_plan_id"`
}

type PlanSeedResult struct {
	SeedID        string   `json:"seedId"`
	ProjectID     string   `json:"projectId"`
	Title         string   `json:"title"`
	QuickContext  string   `json:"quickContext"`
	Constraints   []string `json:"constraints"`
	NonGoals      []string `json:"nonGoals"`
	Tags          []string `json:"tags"`
	Priority      string   `json:"priority"`
	Status        string   `json:"status"`
	SourceType    string   `json:"sourceType"`
	SourceLabel   string   `json:"sourceLabel,omitempty"`
	SourceRefID   string   `json:"sourceRefId,omitempty"`
	PlanAttemptID string   `json:"planAttemptId,omitempty"`
	ManagedPlanID string   `json:"managedPlanId,omitempty"`
	PlannedAt     string   `json:"plannedAt,omitempty"`
	DeferReason   string   `json:"deferReason,omitempty"`
	RejectReason  string   `json:"rejectReason,omitempty"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

type PlanSeedPlanningProject struct {
	ProjectID           string `json:"projectId"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Status              string `json:"status"`
	DefaultRepositoryID string `json:"defaultRepositoryId"`
}

type PlanSeedExistingLinks struct {
	PlanAttemptID string `json:"planAttemptId"`
	ManagedPlanID string `json:"managedPlanId"`
}

type PlanSeedRetrievalSemantics struct {
	RetrievalOnly        bool `json:"retrievalOnly"`
	StateMutated         bool `json:"stateMutated"`
	IntentPacketCreated  bool `json:"intentPacketCreated"`
	PlanAttemptCreated   bool `json:"planAttemptCreated"`
	ManagedPlanSubmitted bool `json:"managedPlanSubmitted"`
	RunCreated           bool `json:"runCreated"`
	ModelCallPerformed   bool `json:"modelCallPerformed"`
}

type PlanSeedPlanningContext struct {
	Project             PlanSeedPlanningProject    `json:"project"`
	Seed                PlanSeedResult             `json:"seed"`
	ExistingLinks       PlanSeedExistingLinks      `json:"existingLinks"`
	PlannerInstructions []string                   `json:"plannerInstructions"`
	RetrievalSemantics  PlanSeedRetrievalSemantics `json:"retrievalSemantics"`
}

type CreatePlanAttemptFromSeedInput struct {
	PlannerPassPlanJSON json.RawMessage `json:"planner_pass_plan_json"`
	SourceArtifactPath  string          `json:"source_artifact_path"`
	DriftReviewMode     string          `json:"drift_review_mode,omitempty"`
	ModelTier           string          `json:"model_tier,omitempty"`
}

type CreatePlanAttemptFromSeedResult struct {
	OK           bool                                `json:"ok"`
	BlockerCode  string                              `json:"blocker_code,omitempty"`
	Message      string                              `json:"message,omitempty"`
	Seed         *PlanSeedResult                     `json:"seed,omitempty"`
	PlanAttempt  *appplans.PlanAttempt               `json:"plan_attempt,omitempty"`
	IntentPacket *appplans.IntentPacket              `json:"intent_packet,omitempty"`
	ReviewPolicy *appplans.EffectivePlanReviewPolicy `json:"review_policy,omitempty"`
	ReviewAction *appplans.PlanAttemptReviewAction   `json:"review_action,omitempty"`
	ReviewGate   *appplans.PlanAttemptReviewGate     `json:"review_gate,omitempty"`
}
