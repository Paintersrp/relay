package projects

import (
	"encoding/json"
	"fmt"

	appplans "relay/internal/app/plans"
	appprojects "relay/internal/app/projects"
)

type PlanSeedAPIRequest struct {
	Title        string   `json:"title"`
	QuickContext string   `json:"quick_context"`
	Priority     string   `json:"priority"`
	Constraints  []string `json:"constraints"`
	NonGoals     []string `json:"non_goals"`
	Tags         []string `json:"tags"`
	SourceLabel  string   `json:"source_label"`
}

type PlanSeedUpdateAPIRequest struct {
	Title        *string   `json:"title"`
	QuickContext *string   `json:"quick_context"`
	Priority     *string   `json:"priority"`
	Constraints  *[]string `json:"constraints"`
	NonGoals     *[]string `json:"non_goals"`
	Tags         *[]string `json:"tags"`
}

type PlanSeedLifecycleAPIRequest struct {
	DeferReason  string `json:"defer_reason"`
	RejectReason string `json:"reject_reason"`
}

type CreatePlanAttemptFromSeedAPIRequest struct {
	PlannerPassPlanJSON json.RawMessage `json:"planner_pass_plan_json"`
	SourceArtifactPath  string          `json:"source_artifact_path"`
	DriftReviewMode     string          `json:"drift_review_mode,omitempty"`
	ModelTier           string          `json:"model_tier,omitempty"`
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

type PlanSeedPlanningContextAPI struct {
	Project             appprojects.PlanSeedPlanningProject    `json:"project"`
	Seed                ProjectAPIPlanSeed                     `json:"seed"`
	ExistingLinks       appprojects.PlanSeedExistingLinks      `json:"existingLinks"`
	PlannerInstructions []string                               `json:"plannerInstructions"`
	RetrievalSemantics  appprojects.PlanSeedRetrievalSemantics `json:"retrievalSemantics"`
}

type PlanSeedPlanningContextAPIResponse struct {
	Success         bool                       `json:"success"`
	PlanningContext PlanSeedPlanningContextAPI `json:"planningContext"`
}

type PlanSeedAttemptAPIResponse struct {
	Success      bool                    `json:"success"`
	BlockerCode  string                  `json:"blockerCode,omitempty"`
	Message      string                  `json:"message,omitempty"`
	Seed         *ProjectAPIPlanSeed     `json:"seed,omitempty"`
	PlanAttempt  *ProjectAPIPlanAttempt  `json:"planAttempt,omitempty"`
	IntentPacket *ProjectAPIIntentPacket `json:"intentPacket,omitempty"`
	ReviewGate   any                     `json:"reviewGate,omitempty"`
}

type ProjectAPIIntentPacket struct {
	ID                      string `json:"id"`
	IntentPacketID          string `json:"intentPacketId"`
	ProjectID               string `json:"projectId"`
	IntentThreadID          string `json:"intentThreadId"`
	RootIntentPacketID      string `json:"rootIntentPacketId"`
	ParentIntentPacketID    string `json:"parentIntentPacketId,omitempty"`
	RevisionOfPlanAttemptID string `json:"revisionOfPlanAttemptId,omitempty"`
	Kind                    string `json:"kind"`
	CapturedFrom            string `json:"capturedFrom"`
	CapturedBy              string `json:"capturedBy"`
	SourceArtifactPath      string `json:"sourceArtifactPath"`
	Summary                 string `json:"summary"`
	LiteralUserRequest      string `json:"literalUserRequest"`
	ConstraintsJSON         string `json:"constraintsJson"`
	RedactionStatus         string `json:"redactionStatus"`
	ContentHash             string `json:"contentHash"`
	CreatedAt               string `json:"createdAt"`
}

type ProjectAPIPlanAttempt struct {
	ID                         string `json:"id"`
	PlanAttemptID              string `json:"planAttemptId"`
	ProjectID                  string `json:"projectId"`
	IntentThreadID             string `json:"intentThreadId"`
	RootIntentPacketID         string `json:"rootIntentPacketId"`
	CurrentIntentPacketID      string `json:"currentIntentPacketId"`
	Status                     string `json:"status"`
	ReviewState                string `json:"reviewState"`
	DriftReviewMode            string `json:"driftReviewMode"`
	ModelTier                  string `json:"modelTier"`
	PlanJsonArtifactPath       string `json:"planJsonArtifactPath"`
	PlanJsonArtifactSHA256     string `json:"planJsonArtifactSha256"`
	RawPlanJsonHash            string `json:"rawPlanJsonHash"`
	PlanMarkdownArtifactPath   string `json:"planMarkdownArtifactPath,omitempty"`
	PlanMarkdownArtifactSHA256 string `json:"planMarkdownArtifactSha256,omitempty"`
	CreatedAt                  string `json:"createdAt"`
	UpdatedAt                  string `json:"updatedAt"`
}

func mapPlanSeedPlanningContextToAPI(ctx appprojects.PlanSeedPlanningContext) PlanSeedPlanningContextAPI {
	return PlanSeedPlanningContextAPI{
		Project:             ctx.Project,
		Seed:                mapPlanSeedToAPI(ctx.Seed),
		ExistingLinks:       ctx.ExistingLinks,
		PlannerInstructions: ctx.PlannerInstructions,
		RetrievalSemantics:  ctx.RetrievalSemantics,
	}
}

func mapPlanSeedAttemptResultToAPI(result *appprojects.CreatePlanAttemptFromSeedResult) PlanSeedAttemptAPIResponse {
	if result == nil {
		return PlanSeedAttemptAPIResponse{Success: false, BlockerCode: appprojects.PlanSeedBlockerDraftAttemptsUnavailable, Message: "seed attempt bridge returned no result"}
	}
	resp := PlanSeedAttemptAPIResponse{
		Success:     result.OK,
		BlockerCode: result.BlockerCode,
		Message:     result.Message,
		ReviewGate:  result.ReviewGate,
	}
	if result.Seed != nil {
		seed := mapPlanSeedToAPI(*result.Seed)
		resp.Seed = &seed
	}
	if result.PlanAttempt != nil {
		attempt := mapProjectPlanAttemptToAPI(*result.PlanAttempt)
		resp.PlanAttempt = &attempt
	}
	if result.IntentPacket != nil {
		intent := mapProjectIntentPacketToAPI(*result.IntentPacket)
		resp.IntentPacket = &intent
	}
	return resp
}

func mapProjectIntentPacketToAPI(packet appplans.IntentPacket) ProjectAPIIntentPacket {
	return ProjectAPIIntentPacket{
		ID:                      fmt.Sprintf("%d", packet.ID),
		IntentPacketID:          packet.IntentPacketID,
		ProjectID:               packet.ProjectID,
		IntentThreadID:          packet.IntentThreadID,
		RootIntentPacketID:      packet.RootIntentPacketID,
		ParentIntentPacketID:    packet.ParentIntentPacketID.String,
		RevisionOfPlanAttemptID: packet.RevisionOfPlanAttemptID.String,
		Kind:                    packet.Kind,
		CapturedFrom:            packet.CapturedFrom,
		CapturedBy:              packet.CapturedBy,
		SourceArtifactPath:      packet.SourceArtifactPath,
		Summary:                 packet.Summary,
		LiteralUserRequest:      packet.LiteralUserRequest,
		ConstraintsJSON:         packet.ConstraintsJson,
		RedactionStatus:         packet.RedactionStatus,
		ContentHash:             packet.ContentHash,
		CreatedAt:               packet.CreatedAt,
	}
}

func mapProjectPlanAttemptToAPI(attempt appplans.PlanAttempt) ProjectAPIPlanAttempt {
	return ProjectAPIPlanAttempt{
		ID:                         fmt.Sprintf("%d", attempt.ID),
		PlanAttemptID:              attempt.PlanAttemptID,
		ProjectID:                  attempt.ProjectID,
		IntentThreadID:             attempt.IntentThreadID,
		RootIntentPacketID:         attempt.RootIntentPacketID,
		CurrentIntentPacketID:      attempt.CurrentIntentPacketID,
		Status:                     attempt.Status,
		ReviewState:                attempt.ReviewState,
		DriftReviewMode:            attempt.DriftReviewMode,
		ModelTier:                  attempt.ModelTier,
		PlanJsonArtifactPath:       attempt.PlanJsonArtifactPath,
		PlanJsonArtifactSHA256:     attempt.PlanJsonArtifactSha256,
		RawPlanJsonHash:            attempt.RawPlanJsonHash,
		PlanMarkdownArtifactPath:   attempt.PlanMarkdownArtifactPath.String,
		PlanMarkdownArtifactSHA256: attempt.PlanMarkdownArtifactSha256.String,
		CreatedAt:                  attempt.CreatedAt,
		UpdatedAt:                  attempt.UpdatedAt,
	}
}
