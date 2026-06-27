package projects

const (
	PlanSeedStatusCaptured = "captured"
	PlanSeedStatusPlanned  = "planned"
	PlanSeedStatusDeferred = "deferred"
	PlanSeedStatusRejected = "rejected"

	PlanSeedSourceManual = "manual"
	PlanSeedSourceChat   = "chat"
	PlanSeedSourceMCP    = "mcp"

	DefaultPlanSeedPriority  = "normal"
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
