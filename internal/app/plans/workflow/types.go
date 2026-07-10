package workflowplans

import workflowstore "relay/internal/store/workflow"

type RepositoryTargetInput struct {
	RepoTarget         string
	Branch             string
	PlanningBaseCommit string
}

type PassInput struct {
	Number     int64
	Name       string
	RepoTarget string
	DependsOn  []int64
}

type CreatePlanInput struct {
	ProjectID        string
	FeatureSlug      string
	CanonicalJSON    []byte
	RenderedMarkdown []byte
	Repositories     []RepositoryTargetInput
	Passes           []PassInput
}

type CreatePlanResult struct {
	Project   workflowstore.Project
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}

type GetPlanResult struct {
	Project   workflowstore.Project
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}

type MovePlanInput struct {
	PlanID    string
	ProjectID string
}

type MovePlanResult struct {
	Project workflowstore.Project
	Plan    workflowstore.Plan
}
