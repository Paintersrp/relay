package projects

import appprojects "relay/internal/app/projects"

type PlanSeedAPIRequest struct {
	Title        string   `json:"title"`
	QuickContext string   `json:"quick_context"`
	Priority     string   `json:"priority"`
	Constraints  []string `json:"constraints"`
	NonGoals     []string `json:"non_goals"`
	Tags         []string `json:"tags"`
	SourceLabel  string   `json:"source_label"`
}

type PlanSeedLifecycleAPIRequest struct {
	DeferReason  string `json:"defer_reason"`
	RejectReason string `json:"reject_reason"`
}

type ProjectAPIPlanSeed struct {
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

func mapPlanSeedToAPI(seed appprojects.PlanSeedResult) ProjectAPIPlanSeed {
	return ProjectAPIPlanSeed{
		SeedID:        seed.SeedID,
		ProjectID:     seed.ProjectID,
		Title:         seed.Title,
		QuickContext:  seed.QuickContext,
		Constraints:   seed.Constraints,
		NonGoals:      seed.NonGoals,
		Tags:          seed.Tags,
		Priority:      seed.Priority,
		Status:        seed.Status,
		SourceType:    seed.SourceType,
		SourceLabel:   seed.SourceLabel,
		SourceRefID:   seed.SourceRefID,
		PlanAttemptID: seed.PlanAttemptID,
		ManagedPlanID: seed.ManagedPlanID,
		PlannedAt:     seed.PlannedAt,
		DeferReason:   seed.DeferReason,
		RejectReason:  seed.RejectReason,
		CreatedAt:     seed.CreatedAt,
		UpdatedAt:     seed.UpdatedAt,
	}
}

func mapPlanSeedsToAPI(seeds []appprojects.PlanSeedResult) []ProjectAPIPlanSeed {
	if seeds == nil {
		return []ProjectAPIPlanSeed{}
	}
	res := make([]ProjectAPIPlanSeed, len(seeds))
	for i, s := range seeds {
		res[i] = mapPlanSeedToAPI(s)
	}
	return res
}

func planSeedAPIPtr(seed ProjectAPIPlanSeed) *ProjectAPIPlanSeed {
	return &seed
}
