package pipeline

import (
	"testing"
)

func TestRenderAgentCommandTemplate_RendersRepoPath(t *testing.T) {
	result, err := RenderAgentCommandTemplate("cd {{repo_path}} && echo done", AgentCommandContext{
		RepoPath: "/home/user/project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "cd /home/user/project && echo done"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersBranchName(t *testing.T) {
	result, err := RenderAgentCommandTemplate("git checkout {{branch_name}}", AgentCommandContext{
		BranchName: "feat/test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "git checkout feat/test"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersSelectedModel(t *testing.T) {
	result, err := RenderAgentCommandTemplate("--model \"{{selected_model}}\"", AgentCommandContext{
		SelectedModel: "DeepSeek V4 Pro",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "--model \"DeepSeek V4 Pro\""
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersRecommendedModel(t *testing.T) {
	result, err := RenderAgentCommandTemplate("{{recommended_model}}", AgentCommandContext{
		RecommendedModel: "DeepSeek V4 Flash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "DeepSeek V4 Flash"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersAgentPromptPath(t *testing.T) {
	result, err := RenderAgentCommandTemplate("--prompt-file \"{{agent_prompt_path}}\"", AgentCommandContext{
		AgentPromptPath: "data/artifacts/1/agent_prompt.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "--prompt-file \"data/artifacts/1/agent_prompt.txt\""
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersPacketPath(t *testing.T) {
	result, err := RenderAgentCommandTemplate("--packet {{packet_path}}", AgentCommandContext{
		PacketPath: "data/artifacts/1/opencode_handoff_packet.json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "--packet data/artifacts/1/opencode_handoff_packet.json"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_RendersArtifactDir(t *testing.T) {
	result, err := RenderAgentCommandTemplate("--artifact-dir {{artifact_dir}}", AgentCommandContext{
		ArtifactDir: "data/artifacts/1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "--artifact-dir data/artifacts/1"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestRenderAgentCommandTemplate_ErrorsOnUnknownPlaceholder(t *testing.T) {
	_, err := RenderAgentCommandTemplate("--unknown {{unknown_placeholder}}", AgentCommandContext{
		RepoPath: "/repo",
	})
	if err == nil {
		t.Fatal("expected error for unknown placeholder, got nil")
	}
}

func TestRenderAgentCommandTemplate_ErrorsWhenMissingRequiredValue(t *testing.T) {
	_, err := RenderAgentCommandTemplate("cd {{repo_path}} && run", AgentCommandContext{})
	if err == nil {
		t.Fatal("expected error for missing required value, got nil")
	}
}

func TestRenderAgentCommandTemplate_ErrorsOnEmptyTemplate(t *testing.T) {
	_, err := RenderAgentCommandTemplate("", AgentCommandContext{
		RepoPath: "/repo",
	})
	if err == nil {
		t.Fatal("expected error for empty template, got nil")
	}
}

func TestRenderAgentCommandTemplate_ErrorsOnWhitespaceOnlyTemplate(t *testing.T) {
	_, err := RenderAgentCommandTemplate("   ", AgentCommandContext{
		RepoPath: "/repo",
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only template, got nil")
	}
}

func TestRenderAgentCommandTemplate_MultiplePlaceholders(t *testing.T) {
	result, err := RenderAgentCommandTemplate(
		"opencode-go --model \"{{selected_model}}\" --prompt-file \"{{agent_prompt_path}}\" --repo {{repo_path}}",
		AgentCommandContext{
			RepoPath:        "/home/user/project",
			SelectedModel:   "DeepSeek V4 Pro",
			AgentPromptPath: "data/artifacts/1/agent_prompt.txt",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "opencode-go --model \"DeepSeek V4 Pro\" --prompt-file \"data/artifacts/1/agent_prompt.txt\" --repo /home/user/project"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}