package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArtifactManifestRequiredAgentPrompt(t *testing.T) {
	kindPaths := map[string]string{
		"agent_prompt":     "/tmp/runs/1/agent_prompt.txt",
		"original_handoff": "/tmp/runs/1/original_handoff.txt",
	}
	manifest := BuildArtifactManifest("/tmp/runs/1", kindPaths)

	if len(manifest.Required) != 1 {
		t.Fatalf("expected 1 required artifact, got %d", len(manifest.Required))
	}
	if manifest.Required[0].Kind != "agent_prompt" {
		t.Fatalf("expected required kind 'agent_prompt', got %q", manifest.Required[0].Kind)
	}
	if manifest.Required[0].Path != "/tmp/runs/1/agent_prompt.txt" {
		t.Fatalf("expected required path '/tmp/runs/1/agent_prompt.txt', got %q", manifest.Required[0].Path)
	}
}

func TestBuildArtifactManifestOptionalArtifacts(t *testing.T) {
	kindPaths := map[string]string{
		"agent_prompt":            "/tmp/runs/1/agent_prompt.txt",
		"original_handoff":        "/tmp/runs/1/original_handoff.txt",
		"handoff_validation_json": "/tmp/runs/1/handoff_validation.json",
	}
	manifest := BuildArtifactManifest("/tmp/runs/1", kindPaths)

	if len(manifest.Optional) != 2 {
		t.Fatalf("expected 2 optional artifacts, got %d", len(manifest.Optional))
	}
	optionalKinds := make(map[string]bool)
	for _, item := range manifest.Optional {
		optionalKinds[item.Kind] = true
	}
	if !optionalKinds["original_handoff"] {
		t.Error("expected optional artifact 'original_handoff'")
	}
	if !optionalKinds["handoff_validation_json"] {
		t.Error("expected optional artifact 'handoff_validation_json'")
	}
}

func TestBuildArtifactManifestNoOptionalDoesNotBlock(t *testing.T) {
	kindPaths := map[string]string{
		"agent_prompt": "/tmp/runs/1/agent_prompt.txt",
	}
	manifest := BuildArtifactManifest("/tmp/runs/1", kindPaths)

	if len(manifest.Required) != 1 {
		t.Fatalf("expected 1 required artifact, got %d", len(manifest.Required))
	}
	if len(manifest.Optional) != 0 {
		t.Fatalf("expected 0 optional artifacts, got %d", len(manifest.Optional))
	}
}

func TestBuildArtifactManifestMissingAgentPrompt(t *testing.T) {
	kindPaths := map[string]string{
		"original_handoff": "/tmp/runs/1/original_handoff.txt",
	}
	manifest := BuildArtifactManifest("/tmp/runs/1", kindPaths)

	if len(manifest.Required) != 0 {
		t.Fatalf("expected 0 required artifacts when agent_prompt is missing, got %d", len(manifest.Required))
	}
}

func TestBuildArtifactManifestEmptyMap(t *testing.T) {
	manifest := BuildArtifactManifest("/tmp/runs/1", nil)
	if len(manifest.Required) != 0 {
		t.Error("expected 0 required artifacts for nil map")
	}
	if len(manifest.Optional) != 0 {
		t.Error("expected 0 optional artifacts for nil map")
	}
	if manifest.Dir != "/tmp/runs/1" {
		t.Errorf("expected dir '/tmp/runs/1', got %q", manifest.Dir)
	}
}

func TestBuildHandoffPreflightReady(t *testing.T) {
	dir := t.TempDir()
	// Create .git directory so git check passes
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(dir, "opencode_handoff_packet.json")
	if err := os.WriteFile(packetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	requiredPaths := map[string]string{
		"agent_prompt": promptPath,
	}

	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", promptPath, packetPath, requiredPaths)

	if preflight.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", preflight.Status)
	}
}

func TestBuildHandoffPreflightBlockedMissingRepo(t *testing.T) {
	preflight := BuildHandoffPreflight("", "", "", "", "", nil)

	if preflight.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", preflight.Status)
	}
	hasBlock := false
	for _, c := range preflight.Checks {
		if c.Key == "repo_path" && c.Status == "block" {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Error("expected repo_path block check")
	}
}

func TestBuildHandoffPreflightBlockedRepoNotExist(t *testing.T) {
	preflight := BuildHandoffPreflight("/nonexistent/path", "main", "DeepSeek V4 Flash", "", "", nil)

	if preflight.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", preflight.Status)
	}
}

func TestBuildHandoffPreflightBlockedMissingModel(t *testing.T) {
	dir := t.TempDir()
	preflight := BuildHandoffPreflight(dir, "main", "", "", "", nil)

	hasBlock := false
	for _, c := range preflight.Checks {
		if c.Key == "selected_model" && c.Status == "block" {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Error("expected selected_model block check")
	}
	if preflight.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %q", preflight.Status)
	}
}

func TestBuildHandoffPreflightBlockedMissingAgentPrompt(t *testing.T) {
	dir := t.TempDir()
	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", "", "", nil)

	hasBlock := false
	for _, c := range preflight.Checks {
		if c.Key == "agent_prompt" && c.Status == "block" {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Error("expected agent_prompt block check")
	}
}

func TestBuildHandoffPreflightWarningEmptyBranch(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	requiredPaths := map[string]string{
		"agent_prompt": promptPath,
	}

	preflight := BuildHandoffPreflight(dir, "", "DeepSeek V4 Flash", promptPath, "", requiredPaths)

	hasWarn := false
	for _, c := range preflight.Checks {
		if c.Key == "branch" && c.Status == "warn" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected branch warn check when branch is empty")
	}
}

func TestBuildHandoffPreflightPassesGitFileWorktree(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /tmp/worktree-git-dir\n"), 0644); err != nil {
		t.Fatal(err)
	}

	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	packetPath := filepath.Join(dir, "opencode_handoff_packet.json")
	if err := os.WriteFile(packetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	preflight := BuildHandoffPreflight(
		dir,
		"feature/example",
		"DeepSeek V4 Flash",
		promptPath,
		packetPath,
		map[string]string{"agent_prompt": promptPath},
	)

	var found bool
	for _, c := range preflight.Checks {
		if c.Key == "repo_git" {
			found = true
			if c.Status != "pass" {
				t.Fatalf("repo_git status = %q, want pass: %s", c.Status, c.Summary)
			}
		}
	}
	if !found {
		t.Fatal("repo_git check not found")
	}
}

func TestBuildHandoffPreflightBlockedMissingGit(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	requiredPaths := map[string]string{
		"agent_prompt": promptPath,
	}

	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", promptPath, "", requiredPaths)

	hasBlock := false
	for _, c := range preflight.Checks {
		if c.Key == "repo_git" && c.Status == "block" {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Error("expected repo_git block check when .git is missing")
	}
}

func TestBuildHandoffPreflightWarningMissingPacket(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	requiredPaths := map[string]string{
		"agent_prompt": promptPath,
	}

	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", promptPath, "", requiredPaths)

	hasWarn := false
	for _, c := range preflight.Checks {
		if c.Key == "opencode_packet" && c.Status == "warn" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected opencode_packet warn check when packet is missing")
	}
}

func TestBuildHandoffPreflightRequiredPathsCheck(t *testing.T) {
	dir := t.TempDir()
	// Create .git directory so git check passes
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "agent_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(dir, "opencode_handoff_packet.json")
	if err := os.WriteFile(packetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	requiredPaths := map[string]string{
		"agent_prompt": promptPath,
	}

	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", promptPath, packetPath, requiredPaths)

	if preflight.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", preflight.Status)
	}

	hasManifestPass := false
	for _, c := range preflight.Checks {
		if c.Key == "artifact_manifest" && c.Status == "pass" {
			hasManifestPass = true
		}
	}
	if !hasManifestPass {
		t.Error("expected artifact_manifest pass check when all required artifacts exist")
	}
}

func TestBuildHandoffPreflightBlockedWhenRequiredMissing(t *testing.T) {
	dir := t.TempDir()
	requiredPaths := map[string]string{
		"agent_prompt": filepath.Join(dir, "missing.txt"),
	}

	preflight := BuildHandoffPreflight(dir, "main", "DeepSeek V4 Flash", "", "", requiredPaths)

	hasBlock := false
	for _, c := range preflight.Checks {
		if c.Key == "artifact_manifest" && c.Status == "block" {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Error("expected artifact_manifest block when required artifact is missing from disk")
	}
}

func TestBuildCompactAgentPromptRemovesExecutionModel(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash
Reason: It is fast.

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Tests / validation

` + "```bash" + `
go test ./...
` + "```" + `

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildCompactAgentPrompt(handoff)

	if strings.Contains(prompt, "Use: DeepSeek") {
		t.Error("prompt should not contain execution model details")
	}
	if strings.Contains(prompt, "## Execution model") {
		t.Error("prompt should not contain Execution model heading")
	}
}

func TestBuildCompactAgentPromptRemovesRelayValidationCommands(t *testing.T) {
	handoff := `## Relay validation commands

` + "```bash" + `
go fmt ./...
go test ./...
` + "```" + `
`
	prompt := BuildCompactAgentPrompt(handoff)

	if strings.Contains(prompt, "go fmt ./...") {
		t.Error("prompt should not contain go fmt command")
	}
	if strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should not contain go test command")
	}
	if strings.Contains(prompt, "## Relay validation commands") {
		t.Error("prompt should not contain Relay validation commands heading")
	}
}

func TestBuildCompactAgentPromptPreservesCoreSections(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Direct files likely changed

- internal/handlers/foo.go

## Current implementation facts to preserve

- Relay stores original handoff.

## Tests to add or update

- Add test A

## Surgical implementation details

Update the handler.
`
	prompt := BuildCompactAgentPrompt(handoff)

	requiredSections := []string{
		"Do a thing",
		"- foo.go",
		"- Nothing",
		"- [ ] Do it",
		"internal/handlers/foo.go",
		"Relay stores original handoff",
		"Add test A",
		"Update the handler",
	}

	for _, s := range requiredSections {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt should contain %q", s)
		}
	}
}

func TestBuildCompactAgentPromptRemovesRTKPreference(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Tests / validation

If RTK is available in the environment, Relay or the user may prefer rtk.exe first, then rtk, then the raw command.

Do not list RTK-wrapped commands as separate validation commands.
`
	prompt := BuildCompactAgentPrompt(handoff)

	if strings.Contains(prompt, "rtk") && (strings.Contains(prompt, "prefer") || strings.Contains(prompt, "available")) {
		t.Error("prompt should not contain RTK preference text")
	}
	if strings.Contains(prompt, "RTK-wrapped") {
		t.Error("prompt should not contain RTK-wrapped instructions")
	}
}

func TestBuildCompactAgentPromptFinalOutputContract(t *testing.T) {
	handoff := `# Example

## Goal

Do a thing.

## Agent final output requirement

Return DONE or BLOCKED.
`
	prompt := BuildCompactAgentPrompt(handoff)

	count := strings.Count(prompt, "## Agent final output requirement")
	if count != 1 {
		t.Errorf("expected exactly 1 '## Agent final output requirement', got %d", count)
	}
	if !strings.Contains(prompt, "DONE or BLOCKED") {
		t.Error("prompt should contain DONE or BLOCKED")
	}
}

func TestBuildCompactAgentPromptIsShorterThanFull(t *testing.T) {
	handoff := `# Example Surgical Implementation

## Execution model

Use: DeepSeek V4 Flash
Reason: It is fast.

## Goal

Do a thing.

## Scope

- foo.go

## Do not change

- Nothing.

## Task checklist

- [ ] Do it

## Direct files likely changed

- internal/handlers/foo.go

## Tests / validation

` + "```bash" + `
go test ./...
go vet ./...
` + "```" + `

RTK preference:

If RTK is available in the environment, Relay or the user may prefer rtk.exe first, then rtk, then the raw command.

## Agent final output requirement

Return DONE or BLOCKED.
`
	full := BuildAgentPrompt(handoff)
	compact := BuildCompactAgentPrompt(handoff)

	if len(compact) >= len(full) {
		t.Errorf("compact prompt (%d bytes) should be shorter than full prompt (%d bytes)", len(compact), len(full))
	}
}

func TestEstimateTokens(t *testing.T) {
	est := EstimateTokens("hello")
	if est.Bytes != 5 {
		t.Errorf("expected 5 bytes, got %d", est.Bytes)
	}
	// 5 runes / 4 = 1.25, ceiled = 2
	if est.ApproxTokens != 2 {
		t.Errorf("expected ~2 tokens, got %d", est.ApproxTokens)
	}

	est2 := EstimateTokens("")
	if est2.Bytes != 0 {
		t.Errorf("expected 0 bytes for empty string, got %d", est2.Bytes)
	}
	if est2.ApproxTokens != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", est2.ApproxTokens)
	}

	est3 := EstimateTokens("hello world")
	// 11 runes (no multi-byte), bytes = 11
	// 11 / 4 = 2.75, ceiled = 3
	if est3.ApproxTokens != 3 {
		t.Errorf("expected ~3 tokens, got %d", est3.ApproxTokens)
	}
}
