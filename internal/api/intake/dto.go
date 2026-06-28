package intake

import runsapi "relay/internal/api/runs"

// PlannerHandoffIntakeRequest is the intake request contract.
type PlannerHandoffIntakeRequest struct {
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	HandoffPath string `json:"handoffPath"`
	PacketID    string `json:"packetId,omitempty"`
	Name        string `json:"name,omitempty"`

	PlannerHandoffMarkdown string `json:"planner_handoff_markdown"`
	RunID                  string `json:"run_id,omitempty"`
	RepoTarget             string `json:"repo_target,omitempty"`
	BranchContext          string `json:"branch_context,omitempty"`
	Source                 string `json:"source,omitempty"`
	ExecutorAdapter        string `json:"executorAdapter,omitempty"`
	ExecutorAdapter2       string `json:"executor_adapter,omitempty"`
	ExecutorModelProfile   string `json:"executorModelProfile,omitempty"`
	ExecutorModelProfile2  string `json:"executor_model_profile,omitempty"`
	RecommendedModel       string `json:"recommended_model,omitempty"`
	Model                  string `json:"model,omitempty"`
	PlanID                 string `json:"planId,omitempty"`
	PlanIDSnake            string `json:"plan_id,omitempty"`
	PassID                 string `json:"passId,omitempty"`
	PassIDSnake            string `json:"pass_id,omitempty"`
	ContextPacketID        string `json:"contextPacketId,omitempty"`
	ContextPacketIDSnake   string `json:"context_packet_id,omitempty"`
	SourceSnapshotID       string `json:"sourceSnapshotId,omitempty"`
	SourceSnapshotIDSnake  string `json:"source_snapshot_id,omitempty"`
}

// PlannerHandoffIntakeResponse is the intake response contract.
type PlannerHandoffIntakeResponse struct {
	Success        bool                          `json:"success"`
	RunID          string                        `json:"runId"`
	RunIDSnake     string                        `json:"run_id"`
	Status         string                        `json:"status"`
	LifecycleState string                        `json:"lifecycleState,omitempty"`
	CreatedAt      string                        `json:"createdAt,omitempty"`
	ReviewURL      string                        `json:"review_url"`
	PlanID         string                        `json:"planId,omitempty"`
	PassID         string                        `json:"passId,omitempty"`
	Artifacts      []runsapi.RelayArtifact       `json:"artifacts,omitempty"`
	Validation     runsapi.RelayValidationResult `json:"validation"`
}
