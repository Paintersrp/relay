package pipeline

import "encoding/json"

type OpenCodeHandoffPacket struct {
	RunID              int64                    `json:"run_id"`
	RepoPath           string                   `json:"repo_path"`
	BranchName         string                   `json:"branch_name"`
	SelectedModel      string                   `json:"selected_model"`
	RecommendedModel   string                   `json:"recommended_model,omitempty"`
	PromptArtifactKind string                   `json:"prompt_artifact_kind"`
	PromptArtifactPath string                   `json:"prompt_artifact_path"`
	ArtifactDir        string                   `json:"artifact_dir"`
	Execution          OpenCodeExecutionPreview `json:"execution"`
	Artifacts          HandoffArtifactManifest  `json:"artifacts"`
}

type OpenCodeExecutionPreview struct {
	Status string `json:"status"`
}

type HandoffArtifactManifest struct {
	Dir      string                `json:"dir"`
	Required []HandoffArtifactItem `json:"required"`
	Optional []HandoffArtifactItem `json:"optional"`
}

type HandoffArtifactItem struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Filename    string `json:"filename,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	Description string `json:"description,omitempty"`
}

func NewOpenCodeHandoffPacket(
	runID int64,
	repoPath string,
	branchName string,
	selectedModel string,
	recommendedModel string,
	promptArtifactPath string,
	artifactDir string,
) OpenCodeHandoffPacket {
	return OpenCodeHandoffPacket{
		RunID:              runID,
		RepoPath:           repoPath,
		BranchName:         branchName,
		SelectedModel:      selectedModel,
		RecommendedModel:   recommendedModel,
		PromptArtifactKind: "agent_prompt",
		PromptArtifactPath: promptArtifactPath,
		ArtifactDir:        artifactDir,
		Execution: OpenCodeExecutionPreview{
			Status: "configured",
		},
		Artifacts: HandoffArtifactManifest{
			Dir:      artifactDir,
			Required: nil,
			Optional: nil,
		},
	}
}

func MarshalOpenCodeHandoffPacket(packet OpenCodeHandoffPacket) ([]byte, error) {
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
