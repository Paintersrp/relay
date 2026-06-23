// Package projectmemory stores durable, long-form project context for chat agents
// and operators. It is not for temporary pass/run state, routine progress updates,
// or current task status.
package projectmemory

const (
	KindDecision              = "decision"
	KindConstraint            = "constraint"
	KindArchitectureRationale = "architecture_rationale"
	KindOperatorPreference    = "operator_preference"
	KindProjectPrinciple      = "project_principle"
	KindRisk                  = "risk"
	KindTerminology           = "terminology"
	KindSupersession          = "supersession"
	KindOpenQuestion          = "open_question"

	StatusActive     = "active"
	StatusSuperseded = "superseded"
	StatusArchived   = "archived"

	ImportanceLow      = "low"
	ImportanceNormal   = "normal"
	ImportanceHigh     = "high"
	ImportanceCritical = "critical"

	SourceChat              = "chat"
	SourceOperatorStatement = "operator_statement"
	SourceHandoff           = "handoff"
	SourceAudit             = "audit"
	SourceSourceDoc         = "source_doc"
	SourceManual            = "manual"

	CreatedByChatAgent = "chat_agent"
	CreatedByOperator  = "operator"
	CreatedBySystem    = "system"

	DefaultLimit = 20
	MaxLimit     = 50

	MaxTitleRunes   = 180
	MaxBodyRunes    = 32768
	MaxExcerptRunes = 320
	MaxTags         = 20
	MaxTagRunes     = 40
	MaxDedupeRunes  = 500
)

type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Record struct {
	ContextRecordID      string   `json:"context_record_id"`
	ProjectID            string   `json:"project_id"`
	Kind                 string   `json:"kind"`
	Title                string   `json:"title"`
	Body                 string   `json:"body,omitempty"`
	BodyExcerpt          string   `json:"body_excerpt,omitempty"`
	BodyHash             string   `json:"body_hash"`
	Status               string   `json:"status"`
	Importance           string   `json:"importance"`
	Tags                 []string `json:"tags"`
	Source               string   `json:"source"`
	CreatedBy            string   `json:"created_by"`
	DedupeReason         string   `json:"dedupe_reason"`
	RedactionStatus      string   `json:"redaction_status"`
	SupersedesRecordID   string   `json:"supersedes_record_id"`
	SupersededByRecordID string   `json:"superseded_by_record_id"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

type SearchInput struct {
	ProjectID   string
	Query       string
	Kinds       []string
	Statuses    []string
	Importance  []string
	Tags        []string
	Limit       int
	IncludeBody bool
}

type SearchResult struct {
	ProjectID   string   `json:"project_id"`
	Records     []Record `json:"records"`
	Limit       int      `json:"limit"`
	Truncated   bool     `json:"truncated"`
	IncludeBody bool     `json:"include_body"`
}

type ListInput struct {
	ProjectID  string
	Kinds      []string
	Statuses   []string
	Importance []string
	Tags       []string
	Limit      int
}

type ListResult struct {
	ProjectID string   `json:"project_id"`
	Records   []Record `json:"records"`
	Limit     int      `json:"limit"`
	Truncated bool     `json:"truncated"`
}

type GetInput struct {
	ProjectID string
	RecordID  string
}

type CreateInput struct {
	ProjectID     string
	Kind          string
	Title         string
	Body          string
	Importance    string
	Tags          []string
	Source        string
	CreatedBy     string
	DedupeReason  string
	SupersedesID  string
}

type SupersedeInput struct {
	ProjectID    string
	RecordID     string
	Kind         string
	Title        string
	Body         string
	Importance   string
	Tags         []string
	Source       string
	CreatedBy    string
	DedupeReason string
}

type SupersedeResult struct {
	OldRecord Record `json:"old_record"`
	NewRecord Record `json:"new_record"`
}
