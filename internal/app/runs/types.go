package runs

import "relay/internal/store"

// ArtifactView is a presentation-neutral artifact projection. The app layer
// performs all filesystem reads (size hint, preview) so API packages never read
// files or import the store package.
type ArtifactView struct {
	ID        int64
	Kind      string
	Path      string
	CreatedAt string
	SizeHint  string
	Preview   string
}

// RunDetails carries a fully loaded run and its associated data for
// presentation. All store/file access happens in the app layer.
type RunDetails struct {
	Run             store.Run
	RepoName        string
	ArtifactViews   []ArtifactView
	Checks          []store.Check
	Events          []store.Event
	LatestExecution *store.AgentExecution
	Worktree        string
	Plan            *store.Plan
	Pass            *store.PlanPass
	PassPlan        *store.Plan
	Provenance      *store.RunSubmissionProvenance
	ContextPacket   *store.ContextPacket
}

// ArtifactContent carries raw artifact bytes for content endpoints.
type ArtifactContent struct {
	Data []byte
}

// ApproveIntakeOverrides mirrors the intake approval override fields.
type ApproveIntakeOverrides struct {
	Model              string `json:"model"`
	Repo               string `json:"repo"`
	Branch             string `json:"branch"`
	Worktree           string `json:"worktree"`
	ValidationCommands string `json:"validationCommands"`
	ExecutorAdapter    string `json:"executorAdapter"`
}

// ApproveIntakeRequest is the decoded intake approval request body.
type ApproveIntakeRequest struct {
	Action    string                 `json:"action"`
	Notes     string                 `json:"notes"`
	Overrides ApproveIntakeOverrides `json:"overrides"`
}

// RunError is a typed transport-mappable error produced by run lifecycle
// operations. When Body is non-nil it is written verbatim with HTTPStatus;
// otherwise a standard error envelope is written from Code/Message.
type RunError struct {
	HTTPStatus int
	Code       string
	Message    string
	Body       map[string]interface{}
}

func (e *RunError) Error() string { return e.Message }

// ApproveIntakeResult carries the updated run for intake approval responses.
type ApproveIntakeResult struct {
	Run      store.Run
	RepoName string
}

// PrepareResult carries the outcome of a prepare/compile operation.
type PrepareResult struct {
	Run              store.Run
	Success          bool
	PacketID         string
	ValidationReport interface{}
	Issues           interface{}
}

// BriefResult carries the outcome of render-brief and approve-brief operations.
type BriefResult struct {
	Run       store.Run
	RunLoaded bool
	Success   bool
	Issues    interface{}
}

// ExecuteResult carries the outcome of an execute start operation.
type ExecuteResult struct {
	Run store.Run
}

// ValidateResult carries the outcome of a validate operation.
type ValidateResult struct {
	ValidationStatus string
	RunStatus        string
	Commands         interface{}
	Stdout           string
	Stderr           string
	Progress         string
}

// RepairResult carries the rendered repair response body and HTTP status.
type RepairResult struct {
	HTTPStatus int
	Body       map[string]interface{}
}
