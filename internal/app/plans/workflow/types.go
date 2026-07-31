package workflowplans

import workflowstore "relay/internal/store/workflow"

type GetPlanResult struct {
	Project   workflowstore.Project
	Plan      workflowstore.Plan
	Passes    []workflowstore.PlanPass
	Artifacts []workflowstore.Artifact
}
