package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- Existing RenderAgentCommandTemplate tests ---

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

// --- New adapter tests ---

func TestOpenCodeModelUsesProviderModelDirectly(t *testing.T) {
	model, err := ResolveOpenCodeModel("anthropic/claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("expected anthropic/claude-sonnet-4-5, got %q", model)
	}
}

func TestOpenCodeModelSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"DeepSeek V4 Flash", "DEEPSEEK_V4_FLASH"},
		{"Qwen3 Coder Next", "QWEN3_CODER_NEXT"},
		{"Claude 3.5 Sonnet", "CLAUDE_3_5_SONNET"},
		{"GPT-4o", "GPT_4O"},
		{"gpt-4o-mini", "GPT_4O_MINI"},
	}
	for _, tt := range tests {
		got := OpenCodeModelEnvSlug(tt.input)
		if got != tt.want {
			t.Errorf("OpenCodeModelEnvSlug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOpenCodeModelMissingMappingErrors(t *testing.T) {
	envKey := "RELAY_OPENCODE_MODEL_QWEN3_CODER_NEXT"
	original := os.Getenv(envKey)
	os.Unsetenv(envKey)
	defer os.Setenv(envKey, original)

	_, err := ResolveOpenCodeModel("Qwen3 Coder Next")
	if err == nil {
		t.Fatal("expected error for missing mapping, got nil")
	}
	if !strings.Contains(err.Error(), "model mapping required") {
		t.Fatalf("expected mapping error, got: %v", err)
	}
}

func TestBuildOpenCodeRunInvocation(t *testing.T) {
	cfg := OpenCodeRunConfig{
		Binary: "opencode",
		Agent:  "build",
	}
	input := OpenCodeRunInput{
		RepoPath:        "/home/user/project",
		SelectedModel:   "anthropic/claude-sonnet-4-5",
		AgentPromptPath: "/tmp/agent_prompt.txt",
		AgentPromptText: "Do the thing",
		PacketPath:      "/tmp/packet.json",
	}

	inv, err := BuildOpenCodeRunInvocation(cfg, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inv.Args[0] != "run" {
		t.Fatalf("expected first arg 'run', got %q", inv.Args[0])
	}

	hasFormatJSON := false
	hasDir := false
	hasAgent := false
	hasModel := false
	hasThinking := false
	hasInteractive := false
	for _, arg := range inv.Args {
		if arg == "--format" {
			hasFormatJSON = true
		}
		if arg == "--dir" {
			hasDir = true
		}
		if arg == "--agent" {
			hasAgent = true
		}
		if arg == "--model" {
			hasModel = true
		}
		if arg == "--thinking" {
			hasThinking = true
		}
		if arg == "--interactive" {
			hasInteractive = true
		}
	}
	if !hasFormatJSON {
		t.Fatal("expected --format json in args")
	}
	if !hasDir {
		t.Fatal("expected --dir in args")
	}
	if !hasAgent {
		t.Fatal("expected --agent in args")
	}
	if !hasModel {
		t.Fatal("expected --model in args")
	}
	if !hasThinking {
		t.Fatal("expected --thinking in args")
	}
	if hasInteractive {
		t.Fatal("did not expect --interactive in args")
	}

	if inv.Stdin != "Do the thing" {
		t.Fatalf("expected stdin to be prompt text, got %q", inv.Stdin)
	}

	if !strings.Contains(inv.Preview, "/tmp/agent_prompt.txt") {
		t.Fatalf("preview should contain prompt path, got: %s", inv.Preview)
	}
}

func TestBuildOpenCodeRunInvocationWithVariant(t *testing.T) {
	cfg := OpenCodeRunConfig{
		Binary:  "opencode",
		Agent:   "build",
		Variant: "high",
	}
	input := OpenCodeRunInput{
		RepoPath:        "/home/user/project",
		SelectedModel:   "anthropic/claude-sonnet-4-5",
		AgentPromptPath: "/tmp/agent_prompt.txt",
		AgentPromptText: "Do the thing",
		PacketPath:      "/tmp/packet.json",
	}

	inv, err := BuildOpenCodeRunInvocation(cfg, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasVariant := false
	hasThinking := false
	for _, arg := range inv.Args {
		if arg == "--variant" {
			hasVariant = true
		}
		if arg == "--thinking" {
			hasThinking = true
		}
	}
	if !hasVariant {
		t.Fatal("expected --variant in args")
	}
	if !hasThinking {
		t.Fatal("expected --thinking in args")
	}
}

func TestExtractOpenCodeAssistantTextJSONL(t *testing.T) {
	stdout := `{"type":"text","part":{"type":"text","text":"DONE"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Test status: PASS"}}
`
	result := ExtractOpenCodeAssistantText(stdout)
	if !strings.Contains(result, "DONE") {
		t.Fatalf("expected DONE in result, got: %s", result)
	}
	if !strings.Contains(result, "Build status: PASS") {
		t.Fatalf("expected Build status: PASS in result, got: %s", result)
	}
}

func TestExtractOpenCodeAssistantTextIgnoresNonTextEvents(t *testing.T) {
	stdout := `{"type":"tool","part":{"type":"tool","name":"read_file"}}
{"type":"reasoning","part":{"type":"reasoning","text":"thinking..."}}
{"type":"error","part":{"type":"error","message":"something broke"}}
`
	result := ExtractOpenCodeAssistantText(stdout)
	if result != stdout {
		t.Fatalf("expected raw stdout fallback for non-text events, got: %s", result)
	}
}

func TestExtractOpenCodeAssistantTextFallsBackToRawStdout(t *testing.T) {
	stdout := "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 42\n"
	result := ExtractOpenCodeAssistantText(stdout)
	if result != stdout {
		t.Fatalf("expected raw stdout fallback, got: %s", result)
	}
}

func TestRunLocalAgentCommandTimeoutUsesTimeoutContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec.CommandContext does not reliably kill child processes on Windows; skipping timeout test")
	}
	result := RunLocalAgentCommand(context.Background(), ".", "sleep 60", 100*time.Millisecond)
	if !result.TimedOut {
		t.Fatalf("expected timeout, but TimedOut is false (exit code=%d, stderr=%q)", result.ExitCode, result.Stderr)
	}
	if result.ExitCode != -2 {
		t.Fatalf("expected exit code -2 for timeout, got %d", result.ExitCode)
	}
}

func TestOpenCodeModelUsesEnvMapping(t *testing.T) {
	envKey := "RELAY_OPENCODE_MODEL_DEEPSEEK_V4_FLASH"
	original := os.Getenv(envKey)
	os.Setenv(envKey, "deepseek/deepseek-chat")
	defer os.Setenv(envKey, original)

	model, err := ResolveOpenCodeModel("DeepSeek V4 Flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "deepseek/deepseek-chat" {
		t.Fatalf("expected deepseek/deepseek-chat, got %q", model)
	}
}

// TestShellPreview checks the display-only preview helper
func TestShellPreview(t *testing.T) {
	preview := ShellPreview("opencode", []string{"run", "--format", "json", "--dir", "/path with spaces"})
	if !strings.Contains(preview, "opencode") {
		t.Fatalf("expected opencode in preview: %s", preview)
	}
}

// TestQuotePreview checks shell argument quoting
func TestQuotePreview(t *testing.T) {
	if got := quotePreview("simple"); got != "simple" {
		t.Fatalf("expected 'simple', got %q", got)
	}
	if got := quotePreview("/path/with spaces"); got != `"/path/with spaces"` {
		t.Fatalf("expected quoted path, got %q", got)
	}
	if got := quotePreview(""); got != `""` {
		t.Fatalf("expected empty quotes, got %q", got)
	}
}

// TestParseAgentResultFromExtractedText verifies that agent result parsing
// works on extracted assistant text with a DONE marker
func TestParseAgentResultFromExtractedText(t *testing.T) {
	text := "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 42\n"
	result := ParseAgentResult(text)
	if result.Status != AgentResultDone {
		t.Fatalf("expected DONE, got %s", result.Status)
	}
	if result.BuildStatus != "PASS" {
		t.Fatalf("expected PASS build, got %s", result.BuildStatus)
	}
	if result.TestStatus != "PASS" {
		t.Fatalf("expected PASS test, got %s", result.TestStatus)
	}
	if result.LOCChanged != "42" {
		t.Fatalf("expected 42 LOC, got %s", result.LOCChanged)
	}
}

// TestDryRunJSONRoundTrip ensures the dry run preview struct marshals/unmarshals correctly
func TestDryRunJSONRoundTrip(t *testing.T) {
	preview := struct {
		Binary          string   `json:"binary"`
		Args            []string `json:"args"`
		WorkDir         string   `json:"work_dir"`
		StdinSource     string   `json:"stdin_source"`
		StdinBytes      int      `json:"stdin_bytes"`
		AgentPromptPath string   `json:"agent_prompt_path"`
		PacketPath      string   `json:"packet_path"`
		Model           string   `json:"model"`
		Agent           string   `json:"agent"`
		Variant         string   `json:"variant,omitempty"`
		Preview         string   `json:"preview"`
	}{
		Binary:          "opencode",
		Args:            []string{"run", "--format", "json", "--thinking", "max"},
		WorkDir:         "/repo",
		StdinSource:     "/tmp/prompt.txt",
		StdinBytes:      100,
		AgentPromptPath: "/tmp/prompt.txt",
		PacketPath:      "/tmp/packet.json",
		Model:           "anthropic/claude-sonnet-4-5",
		Agent:           "build",
		Preview:         "opencode run --format json --dir /repo --agent build --model anthropic/claude-sonnet-4-5 --thinking max",
	}

	data, err := json.MarshalIndent(preview, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded struct {
		Binary string `json:"binary"`
		Model  string `json:"model"`
		Agent  string `json:"agent"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Binary != "opencode" {
		t.Fatalf("expected opencode, got %q", decoded.Binary)
	}
}

func TestBuildOpenCodeRunInvocationPreviewDoesNotIncludePromptBody(t *testing.T) {
	secret := "super-secret-prompt-body-do-not-leak"
	cfg := OpenCodeRunConfig{
		Binary: "opencode",
		Agent:  "build",
	}
	input := OpenCodeRunInput{
		RepoPath:        "/home/user/project",
		SelectedModel:   "anthropic/claude-sonnet-4-5",
		AgentPromptPath: "/tmp/agent_prompt.txt",
		AgentPromptText: secret,
		PacketPath:      "/tmp/packet.json",
	}

	inv, err := BuildOpenCodeRunInvocation(cfg, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(inv.Preview, secret) {
		t.Fatal("preview must not contain prompt body")
	}
	if !strings.Contains(inv.Preview, "/tmp/agent_prompt.txt") {
		t.Fatal("preview must contain the prompt path")
	}
}

func TestBuildOpenCodeRunInvocationIncludesExpectedArgsValues(t *testing.T) {
	cfg := OpenCodeRunConfig{
		Binary: "opencode",
		Agent:  "build",
	}
	input := OpenCodeRunInput{
		RepoPath:        "/home/user/project",
		SelectedModel:   "anthropic/claude-sonnet-4-5",
		AgentPromptPath: "/tmp/agent_prompt.txt",
		AgentPromptText: "Do the thing",
		PacketPath:      "/tmp/packet.json",
	}

	inv, err := BuildOpenCodeRunInvocation(cfg, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check positional args after flag names
	for i, arg := range inv.Args {
		switch arg {
		case "--format":
			if i+1 >= len(inv.Args) || inv.Args[i+1] != "json" {
				t.Fatalf("expected --format json, got --format %v", map[bool]string{true: inv.Args[i+1]}[i+1 < len(inv.Args)])
			}
		case "--dir":
			if i+1 >= len(inv.Args) || inv.Args[i+1] != "/home/user/project" {
				t.Fatal("expected --dir to be followed by repo path")
			}
		case "--agent":
			if i+1 >= len(inv.Args) || inv.Args[i+1] != "build" {
				t.Fatal("expected --agent to be followed by build")
			}
		case "--model":
			if i+1 >= len(inv.Args) || inv.Args[i+1] != "anthropic/claude-sonnet-4-5" {
				t.Fatal("expected --model to be followed by resolved model")
			}
		case "--thinking":
			if i+1 >= len(inv.Args) || inv.Args[i+1] != "max" {
				t.Fatal("expected --thinking to be followed by max")
			}
		}
	}
}

func TestResolveOpenCodeModelMissingMappingIncludesEnvKey(t *testing.T) {
	envKey := "RELAY_OPENCODE_MODEL_QWEN3_CODER_NEXT"
	original := os.Getenv(envKey)
	os.Unsetenv(envKey)
	defer os.Setenv(envKey, original)

	_, err := ResolveOpenCodeModel("Qwen3 Coder Next")
	if err == nil {
		t.Fatal("expected error for missing mapping, got nil")
	}
	if !strings.Contains(err.Error(), envKey) {
		t.Fatalf("expected error to include env key %q, got: %v", envKey, err)
	}
}
