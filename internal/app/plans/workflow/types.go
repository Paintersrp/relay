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
	FeatureSlug      string
	CanonicalJSON    []byte
	RenderedMarkdown []byte
	Repositories     []RepositoryTargetInput
	Passes           []PassInput
}

type CreatePlanResult struct {
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}

type GetPlanResult struct {
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}
