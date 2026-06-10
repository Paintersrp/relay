package pipeline

import (
	"strings"
	"testing"
)

func TestNewOpenCodeHandoffPacket(t *testing.T) {
	packet := NewOpenCodeHandoffPacket(
		123,
		"D:/Code/relay",
		"feat/example",
		"DeepSeek V4 Flash",
		"DeepSeek V4 Pro",
		"data/artifacts/runs/123/agent_prompt.txt",
		"data/artifacts/runs/123",
	)

	if packet.RunID != 123 {
		t.Fatalf("expected RunID 123, got %d", packet.RunID)
	}
	if packet.RepoPath != "D:/Code/relay" {
		t.Fatalf("expected RepoPath 'D:/Code/relay', got %q", packet.RepoPath)
	}
	if packet.BranchName != "feat/example" {
		t.Fatalf("expected BranchName 'feat/example', got %q", packet.BranchName)
	}
	if packet.SelectedModel != "DeepSeek V4 Flash" {
		t.Fatalf("expected SelectedModel 'DeepSeek V4 Flash', got %q", packet.SelectedModel)
	}
	if packet.RecommendedModel != "DeepSeek V4 Pro" {
		t.Fatalf("expected RecommendedModel 'DeepSeek V4 Pro', got %q", packet.RecommendedModel)
	}
	if packet.PromptArtifactKind != "agent_prompt" {
		t.Fatalf("expected PromptArtifactKind 'agent_prompt', got %q", packet.PromptArtifactKind)
	}
	if packet.PromptArtifactPath != "data/artifacts/runs/123/agent_prompt.txt" {
		t.Fatalf("expected PromptArtifactPath 'data/artifacts/runs/123/agent_prompt.txt', got %q", packet.PromptArtifactPath)
	}
	if packet.ArtifactDir != "data/artifacts/runs/123" {
		t.Fatalf("expected ArtifactDir 'data/artifacts/runs/123', got %q", packet.ArtifactDir)
	}
	if packet.Execution.Status != "configured" {
		t.Fatalf("expected Execution.Status 'configured', got %q", packet.Execution.Status)
	}
}

func TestMarshalOpenCodeHandoffPacket(t *testing.T) {
	packet := NewOpenCodeHandoffPacket(
		123,
		"D:/Code/relay",
		"feat/example",
		"DeepSeek V4 Flash",
		"DeepSeek V4 Pro",
		"data/artifacts/runs/123/agent_prompt.txt",
		"data/artifacts/runs/123",
	)

	data, err := MarshalOpenCodeHandoffPacket(packet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(data), `"run_id": 123`) {
		t.Fatal("expected JSON to contain run_id: 123")
	}
	if !strings.Contains(string(data), `"repo_path": "D:/Code/relay"`) {
		t.Fatal("expected JSON to contain repo_path")
	}
	if !strings.Contains(string(data), `"selected_model": "DeepSeek V4 Flash"`) {
		t.Fatal("expected JSON to contain selected_model")
	}
	if !strings.Contains(string(data), `"prompt_artifact_kind": "agent_prompt"`) {
		t.Fatal("expected JSON to contain prompt_artifact_kind: agent_prompt")
	}
	if !strings.Contains(string(data), `"status": "configured"`) {
		t.Fatal("expected JSON to contain execution status")
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("expected JSON to end with newline")
	}
}

func TestNewOpenCodeHandoffPacketEmptyOptionalFields(t *testing.T) {
	packet := NewOpenCodeHandoffPacket(
		456,
		"/home/user/project",
		"",
		"DeepSeek V4 Flash",
		"",
		"data/artifacts/runs/456/agent_prompt.txt",
		"data/artifacts/runs/456",
	)

	if packet.RecommendedModel != "" {
		t.Fatalf("expected empty RecommendedModel, got %q", packet.RecommendedModel)
	}
	if packet.BranchName != "" {
		t.Fatalf("expected empty BranchName, got %q", packet.BranchName)
	}
}
