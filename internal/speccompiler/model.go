package speccompiler

import "encoding/json"

type scopeModel struct {
	InScope    []string `json:"in_scope"`
	OutOfScope []string `json:"out_of_scope"`
}

type executionSpecModel struct {
	SchemaVersion json.RawMessage `json:"schema_version"`
	FeatureSlug   string          `json:"feature_slug"`
	RepoTarget    string          `json:"repo_target"`
	Branch        string          `json:"branch"`
	BaseCommit    string          `json:"base_commit"`
	Goal          string          `json:"goal"`
	Context       string          `json:"context"`
	Scope         scopeModel      `json:"scope"`
	Steps         []stepModel     `json:"steps"`
	Validation    validationModel `json:"validation"`
	Completion    []string        `json:"completion_criteria"`
}

type stepModel struct {
	Number     int            `json:"number"`
	Goal       string         `json:"goal"`
	Substeps   []substepModel `json:"substeps"`
	Completion []string       `json:"completion_criteria"`
}

type substepModel struct {
	Number      int         `json:"number"`
	Instruction string      `json:"instruction"`
	Files       []fileModel `json:"files"`
	Completion  []string    `json:"completion_criteria"`
}

type fileModel struct {
	Path            string          `json:"path"`
	DestinationPath string          `json:"destination_path"`
	Operation       string          `json:"operation"`
	Purpose         string          `json:"purpose"`
	Implementation  json.RawMessage `json:"implementation"`
}

type modifyImplementationModel struct {
	Changes []modifyChangeModel `json:"changes"`
}

type modifyChangeModel struct {
	Kind                string `json:"kind"`
	OldText             string `json:"old_text"`
	NewText             string `json:"new_text"`
	Anchor              string `json:"anchor"`
	Content             string `json:"content"`
	ExpectedOccurrences int    `json:"expected_occurrences"`
}

type createImplementationModel struct {
	Content string `json:"content"`
}

type deleteImplementationModel struct {
	DeleteFile bool `json:"delete_file"`
}

type renameImplementationModel struct {
	PreserveContent bool   `json:"preserve_content"`
	Content         string `json:"content"`
}

type validationModel struct {
	Commands       []validationCommandModel `json:"commands"`
	ExecutorChecks []string                 `json:"executor_checks"`
}

type validationCommandModel struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory"`
	Expected         string `json:"expected"`
}

type planModel struct {
	SchemaVersion json.RawMessage         `json:"schema_version"`
	FeatureSlug   string                  `json:"feature_slug"`
	Goal          string                  `json:"goal"`
	Context       string                  `json:"context"`
	Scope         scopeModel              `json:"scope"`
	RepoTargets   []repositoryTargetModel `json:"repo_targets"`
	Passes        []passModel             `json:"passes"`
	Completion    []string                `json:"completion_criteria"`
}

type repositoryTargetModel struct {
	RepoTarget         string `json:"repo_target"`
	Branch             string `json:"branch"`
	PlanningBaseCommit string `json:"planning_base_commit"`
}

type passModel struct {
	Number           int                 `json:"number"`
	Name             string              `json:"name"`
	RepoTarget       string              `json:"repo_target"`
	Goal             string              `json:"goal"`
	Context          string              `json:"context"`
	Scope            scopeModel          `json:"scope"`
	DependsOn        []int               `json:"depends_on"`
	Outcomes         []string            `json:"outcomes"`
	SourceTargets    []sourceTargetModel `json:"source_targets"`
	ValidationIntent []string            `json:"validation_intent"`
	Completion       []string            `json:"completion_criteria"`
}

type sourceTargetModel struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}
