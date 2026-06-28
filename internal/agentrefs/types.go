package agentrefs

type FactLabel string

const (
	FactLabelProven     FactLabel = "proven"
	FactLabelDerived    FactLabel = "derived"
	FactLabelConvention FactLabel = "convention"
	FactLabelUnresolved FactLabel = "unresolved"
	FactLabelConflict   FactLabel = "conflict"
)

var FactLabelOrder = []FactLabel{
	FactLabelProven,
	FactLabelDerived,
	FactLabelConvention,
	FactLabelUnresolved,
	FactLabelConflict,
}

type RepoIdentity struct {
	ProjectID string `json:"project_id"`
	RepoID    string `json:"repo_id"`
	Branch    string `json:"branch"`
}

type GeneratorIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RenderingContract struct {
	JSONPrimary       bool `json:"json_primary"`
	MarkdownFromJSON  bool `json:"markdown_from_json"`
	DeterministicSort bool `json:"deterministic_sort"`
	NoTimestamps      bool `json:"no_timestamps"`
	RelativePathsOnly bool `json:"relative_paths_only"`
}

type SourceInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}

type Evidence struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Fact struct {
	ID        string     `json:"id"`
	Label     FactLabel  `json:"label"`
	Statement string     `json:"statement"`
	Evidence  []Evidence `json:"evidence,omitempty"`
}

type ReferenceEntry struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type ReferenceDocument struct {
	SchemaVersion string            `json:"schema_version"`
	ReferenceID   string            `json:"reference_id"`
	Repo          RepoIdentity      `json:"repo"`
	GeneratedBy   GeneratorIdentity `json:"generated_by"`
	Rendering     RenderingContract `json:"rendering"`
	SourceInputs  []SourceInput     `json:"source_inputs"`
	FactLabels    []FactLabel       `json:"fact_labels"`
	Facts         []Fact            `json:"facts"`
	References    []ReferenceEntry  `json:"references"`
}
