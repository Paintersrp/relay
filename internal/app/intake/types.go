package intake

// IntakeInput holds the decoded planner-handoff intake request fields. Field
// names mirror the HTTP request DTO; the API handler maps from its DTO into this
// struct.
type IntakeInput struct {
	Repo        string
	Branch      string
	HandoffPath string
	PacketID    string
	Name        string

	PlannerHandoffMarkdown string
	RunID                  string
	RepoTarget             string
	BranchContext          string
	Source                 string
	ExecutorAdapter        string
	ExecutorAdapter2       string
	ExecutorModelProfile   string
	ExecutorModelProfile2  string
	RecommendedModel       string
	Model                  string
	PlanID                 string
	PlanIDSnake            string
	PassID                 string
	PassIDSnake            string
	ContextPacketID        string
	ContextPacketIDSnake   string
	SourceSnapshotID       string
	SourceSnapshotIDSnake  string
}

// IntakeResult carries the created/updated run identity for response assembly.
// The API handler reloads run details via the run app service to present the
// run status, lifecycle, artifacts, and validation summary.
type IntakeResult struct {
	RunID  int64
	PlanID string
	PassID string
}

// Error is a typed transport-mappable error produced by intake operations.
type Error struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *Error) Error() string { return e.Message }
