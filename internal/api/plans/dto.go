package plans

import (
	"encoding/json"

	appplans "relay/internal/app/plans"
)

// PlanAPIRequest is the request body for plan submission and validation.
type PlanAPIRequest struct {
	Plan                  json.RawMessage `json:"plan"`
	SourceArtifactPath    string          `json:"sourceArtifactPath,omitempty"`
	ProjectID             string          `json:"projectId,omitempty"`
	UnmanagedAcknowledged bool            `json:"unmanagedAcknowledged,omitempty"`
}

// PlanAPIResponse is the response for plan submission and validation.
type PlanAPIResponse struct {
	Success    bool                          `json:"success"`
	Plan       *PlanAPIPlan                  `json:"plan,omitempty"`
	Passes     []PlanAPIPass                 `json:"passes,omitempty"`
	Validation appplans.PlanValidationReport `json:"validation"`
}

// PlanAPIPlan is the plan summary DTO for API responses.
type PlanAPIPlan struct {
	ID                  string `json:"id"`
	PlanID              string `json:"planId"`
	SchemaVersion       string `json:"schemaVersion"`
	Title               string `json:"title"`
	Goal                string `json:"goal"`
	RepoTarget          string `json:"repoTarget"`
	BranchContext       string `json:"branchContext"`
	Status              string `json:"status"`
	SourceIntentSummary string `json:"sourceIntentSummary"`
	SourceArtifactPath  string `json:"sourceArtifactPath,omitempty"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	ProjectRowID        string `json:"projectRowId,omitempty"`
	ProjectID           string `json:"projectId,omitempty"`
}

// PlanAPIPass is the pass detail DTO for API responses.
type PlanAPIPass struct {
	ID                         string                            `json:"id"`
	PlanRowID                  string                            `json:"planRowId"`
	PassID                     string                            `json:"passId"`
	Sequence                   int64                             `json:"sequence"`
	Name                       string                            `json:"name"`
	Goal                       string                            `json:"goal"`
	IntendedExecutionScope     []string                          `json:"intendedExecutionScope"`
	NonGoals                   []string                          `json:"nonGoals"`
	Dependencies               []string                          `json:"dependencies"`
	Status                     string                            `json:"status"`
	AssociatedRunIDs           []string                          `json:"associatedRunIds"`
	AssociatedRuns             []PlanAPIRunSummary               `json:"associatedRuns"`
	CreatedAt                  string                            `json:"createdAt"`
	UpdatedAt                  string                            `json:"updatedAt"`
	PassType                   string                            `json:"passType,omitempty"`
	ContextPlan                PlanAPIContextPlan                `json:"contextPlan"`
	SourceSnapshotRequirements PlanAPISourceSnapshotRequirements `json:"sourceSnapshotRequirements"`
	HandoffReadinessCriteria   []string                          `json:"handoffReadinessCriteria"`
	RiskLevel                  string                            `json:"riskLevel,omitempty"`
	ContextBudget              PlanAPIContextBudget              `json:"contextBudget"`
	ContextParseWarnings       []string                          `json:"contextParseWarnings,omitempty"`
}

// PlanAPIRunSummary is the run summary embedded in pass responses.
type PlanAPIRunSummary struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	LifecycleState string `json:"lifecycleState"`
	ActiveStep     string `json:"activeStep"`
	WorkbenchPath  string `json:"workbenchPath"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

// PlanAPIReadPlan is the plan detail DTO for read API responses.
type PlanAPIReadPlan struct {
	PlanAPIPlan
	PassCount           int    `json:"passCount"`
	CompletionReady     bool   `json:"completionReady"`
	CompletedPassCount  int    `json:"completedPassCount"`
	InProgressPassCount int    `json:"inProgressPassCount"`
	PlannedPassCount    int    `json:"plannedPassCount"`
	SkippedPassCount    int    `json:"skippedPassCount"`
	CurrentPassID       string `json:"currentPassId,omitempty"`
	CurrentPassName     string `json:"currentPassName,omitempty"`
	CurrentPassGoal     string `json:"currentPassGoal,omitempty"`
	NextPassID          string `json:"nextPassId,omitempty"`
	NextPassName        string `json:"nextPassName,omitempty"`
	NextPassGoal        string `json:"nextPassGoal,omitempty"`
}

// PlanReadAPIResponse is the envelope for plan read responses.
type PlanReadAPIResponse struct {
	Success         bool              `json:"success"`
	Count           int               `json:"count,omitempty"`
	Plans           []PlanAPIReadPlan `json:"plans,omitempty"`
	Plan            *PlanAPIReadPlan  `json:"plan,omitempty"`
	Passes          []PlanAPIPass     `json:"passes,omitempty"`
	Pass            *PlanAPIPass      `json:"pass,omitempty"`
	CompletionReady bool              `json:"completionReady"`
}

// PlanAPIContextPlan is the context plan DTO embedded in pass responses.
type PlanAPIContextPlan struct {
	RequiredRepositories        []string                   `json:"requiredRepositories"`
	SeedSearchTerms             []PlanAPIContextSearchTerm `json:"seedSearchTerms"`
	SeedFilesToRead             []PlanAPIContextFileRead   `json:"seedFilesToRead"`
	ContextCoverageExpectations []string                   `json:"contextCoverageExpectations"`
	BlockedIfMissing            []string                   `json:"blockedIfMissing"`
}

// PlanAPIContextSearchTerm is a seed search term for context plan responses.
type PlanAPIContextSearchTerm struct {
	RepoID   string `json:"repoId"`
	Query    string `json:"query"`
	Purpose  string `json:"purpose"`
	Required *bool  `json:"required,omitempty"`
}

// PlanAPIContextFileRead is a seed file read for context plan responses.
type PlanAPIContextFileRead struct {
	RepoID   string `json:"repoId"`
	Path     string `json:"path"`
	Purpose  string `json:"purpose"`
	Required *bool  `json:"required,omitempty"`
}

// PlanAPISourceSnapshotRequirements is the source snapshot requirements DTO.
type PlanAPISourceSnapshotRequirements struct {
	RequireGitStatus   *bool `json:"requireGitStatus,omitempty"`
	RequireCommitSHA   *bool `json:"requireCommitSha,omitempty"`
	AllowDirtyWorktree *bool `json:"allowDirtyWorktree,omitempty"`
}

// PlanAPIContextBudget is the context budget DTO.
type PlanAPIContextBudget struct {
	MaxFiles         *int64 `json:"maxFiles,omitempty"`
	MaxBytes         *int64 `json:"maxBytes,omitempty"`
	MaxSearchResults *int64 `json:"maxSearchResults,omitempty"`
	MaxContextLines  *int64 `json:"maxContextLines,omitempty"`
}
