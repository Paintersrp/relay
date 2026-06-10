package handlers

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
)

func TestDefaultActiveRunStep_StartsAtIntakeAfterAutoSetup(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
	}
	checks := []store.Check{
		{Kind: "validation", Status: "pass"},
	}

	got := defaultActiveRunStep(artifacts, checks)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForFreshRun(t *testing.T) {
	got := defaultActiveRunStep(nil, nil)
	if got != "intake" {
		t.Fatalf("expected intake for fresh run, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeForArtifactsOnly(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
	}
	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterAgentResult(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "agent_result_raw"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterValidationRun(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "handoff_validation_json"},
		{Kind: "agent_prompt"},
		{Kind: "opencode_handoff_packet"},
		{Kind: "validation_run_json"},
	}

	got := defaultActiveRunStep(artifacts, nil)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestDefaultActiveRunStep_StartsAtIntakeAfterValidationRunCheck(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}

	got := defaultActiveRunStep(nil, checks)
	if got != "intake" {
		t.Fatalf("expected intake, got %q", got)
	}
}

func TestParseValidationRunPreview_Pass(t *testing.T) {
	jsonData := `{
		"status": "pass",
		"repo_path": "D:/Code/relay",
		"commands": [
			{"label": "go fmt", "command": "go fmt ./...", "source": "handoff", "exit_code": 0, "stdout": "", "stderr": "", "timed_out": false, "duration_ms": 1500},
			{"label": "go test", "command": "go test ./...", "source": "handoff", "exit_code": 0, "stdout": "ok", "stderr": "", "timed_out": false, "duration_ms": 5000}
		]
	}`

	preview := parseValidationRunPreview(jsonData)
	if preview.Status != "pass" {
		t.Errorf("expected pass, got %s", preview.Status)
	}
	if preview.CommandCount != 2 {
		t.Errorf("expected 2 commands, got %d", preview.CommandCount)
	}
	if preview.PassedCount != 2 {
		t.Errorf("expected 2 passed, got %d", preview.PassedCount)
	}
	if preview.FailedCount != 0 {
		t.Errorf("expected 0 failed, got %d", preview.FailedCount)
	}
	if len(preview.Commands) != 2 {
		t.Errorf("expected 2 command previews, got %d", len(preview.Commands))
	}
	if preview.Commands[0].Status != "pass" {
		t.Errorf("expected pass status for first command, got %s", preview.Commands[0].Status)
	}
	if !preview.Commands[1].HasStdout {
		t.Error("expected hasStdout for second command")
	}
	if preview.TotalDurationMs != 6500 {
		t.Errorf("expected total 6500ms, got %d", preview.TotalDurationMs)
	}
}

func TestParseValidationRunPreview_FailAndTimeout(t *testing.T) {
	jsonData := `{
		"status": "fail",
		"repo_path": "D:/Code/test",
		"commands": [
			{"label": "passing", "command": "passing", "source": "handoff", "exit_code": 0, "stdout": "", "stderr": "", "timed_out": false, "duration_ms": 100},
			{"label": "failing", "command": "failing", "source": "handoff", "exit_code": 1, "stdout": "", "stderr": "error", "timed_out": false, "duration_ms": 200},
			{"label": "timedout", "command": "timedout", "source": "handoff", "exit_code": -2, "stdout": "", "stderr": "", "timed_out": true, "duration_ms": 300}
		]
	}`

	preview := parseValidationRunPreview(jsonData)
	if preview.Status != "fail" {
		t.Errorf("expected fail, got %s", preview.Status)
	}
	if preview.CommandCount != 3 {
		t.Errorf("expected 3 commands, got %d", preview.CommandCount)
	}
	if preview.PassedCount != 1 {
		t.Errorf("expected 1 passed, got %d", preview.PassedCount)
	}
	if preview.FailedCount != 1 {
		t.Errorf("expected 1 failed, got %d", preview.FailedCount)
	}
	if preview.TimedOutCount != 1 {
		t.Errorf("expected 1 timed out, got %d", preview.TimedOutCount)
	}
	if preview.Commands[2].Status != "timed_out" {
		t.Errorf("expected timed_out status, got %s", preview.Commands[2].Status)
	}
	if !preview.Commands[1].HasStderr {
		t.Error("expected HasStderr for failing command")
	}
}

func TestParseValidationRunPreview_EmptyJSON(t *testing.T) {
	preview := parseValidationRunPreview("")
	if preview.CommandCount != 0 {
		t.Errorf("expected 0 commands for empty, got %d", preview.CommandCount)
	}
}

func TestParseValidationRunPreview_InvalidJSON(t *testing.T) {
	preview := parseValidationRunPreview("not json")
	if preview.CommandCount != 0 {
		t.Errorf("expected 0 commands for invalid json, got %d", preview.CommandCount)
	}
}

func TestNormalizeRunStepAcceptsKnownSteps(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"intake", "intake"},
		{"prompt", "prompt"},
		{"packet", "packet"},
		{"handoff", "handoff"},
		{"run", "run"},
		{"validation", "validation"},
		{"audit", "audit"},
		{"commit", "commit"},
	}
	for _, tt := range tests {
		got := normalizeRunStep(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeRunStep(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeRunStepRejectsInvalidStep(t *testing.T) {
	tests := []string{
		"",
		"nonsense",
		"invalid",
		"step",
		"  ",
		"intake ",
	}
	for _, input := range tests {
		got := normalizeRunStep(input)
		if got != "intake" {
			t.Errorf("normalizeRunStep(%q) = %q, want %q", input, got, "intake")
		}
	}
}

func TestNormalizeRunStepRunIsRealStep(t *testing.T) {
	got := normalizeRunStep("run")
	if got != "run" {
		t.Fatalf("normalizeRunStep(%q) = %q, want %q", "run", got, "run")
	}
}

func TestHasArtifactKind_ReturnsTrueWhenFound(t *testing.T) {
	artifacts := []store.Artifact{
		{Kind: "agent_prompt"},
	}
	if !hasArtifactKind(artifacts, "agent_prompt") {
		t.Error("expected true for existing artifact kind")
	}
}

func TestHasArtifactKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasArtifactKind(nil, "agent_prompt") {
		t.Error("expected false for nil slice")
	}
}

func TestHasCheckKind_ReturnsTrueWhenFound(t *testing.T) {
	checks := []store.Check{
		{Kind: "validation_run", Status: "pass"},
	}
	if !hasCheckKind(checks, "validation_run") {
		t.Error("expected true for existing check kind")
	}
}

func TestHasCheckKind_ReturnsFalseWhenNotFound(t *testing.T) {
	if hasCheckKind(nil, "validation_run") {
		t.Error("expected false for nil slice")
	}
}

func TestHasValidationCommandsForPreviewFromHandoff(t *testing.T) {
	// Verify that an unwrapped command in a bash fence under "## Tests / validation" is detected
	handoff := "# Test\n\n## Tests / validation\n\n" + "```bash\n" + "go test ./...\n" + "```\n"
	if !hasValidationCommandsForPreview(handoff, "") {
		t.Fatal("expected validation commands from handoff")
	}
}

func TestHasValidationCommandsForPreviewFromRepoDefaults(t *testing.T) {
	// When handoff has no commands, repo defaults should be used
	if !hasValidationCommandsForPreview("# Test", "[\"go test ./...\"]") {
		t.Fatal("expected validation commands from repo defaults")
	}
}

func TestHasValidationCommandsForPreviewMissing(t *testing.T) {
	if hasValidationCommandsForPreview("# Test", "") {
		t.Fatal("expected no validation commands")
	}
}

func TestHasValidationCommandsForPreviewFallsBackToRepoDefaults(t *testing.T) {
	// Full integration-style test that handoff metadata parsing falls back to defaults
	handoff := "# Test\n\nNo validation section here.\n"
	repoDefaults := "[\"npm run build\"]"
	commands := pipeline.ExtractValidationCommands(handoff, repoDefaults)
	if len(commands) != 1 {
		t.Fatalf("expected 1 command from repo defaults, got %d", len(commands))
	}
	if commands[0].Source != "repo_default" {
		t.Fatalf("expected source 'repo_default', got %q", commands[0].Source)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
}

func gitAddCommit(t *testing.T, dir string, msg string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "add", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", dir, "commit", "-m", msg)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestInspectDiffWritesArtifacts(t *testing.T) {
	s := setupTestStore(t)
	handoffText := "# Test\n\n## Goal\nDo something.\n"
	runID := newTestHandoff(t, s, handoffText)

	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}

	initGitRepo(t, repo.Path)

	if err := os.WriteFile(filepath.Join(repo.Path, "README.md"), []byte("# Repo\n"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	gitAddCommit(t, repo.Path, "initial commit")

	if err := os.WriteFile(filepath.Join(repo.Path, "README.md"), []byte("# Modified\n\nChanged.\n"), 0644); err != nil {
		t.Fatalf("modify readme: %v", err)
	}

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.inspectDiff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=audit") {
		t.Fatalf("expected redirect to step=audit, got %s", loc)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}

	hasStatus := false
	hasDiffStat := false
	hasDiffNumstat := false
	hasPatch := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "git_status_text":
			hasStatus = true
		case "git_diff_stat":
			hasDiffStat = true
		case "git_diff_numstat":
			hasDiffNumstat = true
		case "git_diff_patch":
			hasPatch = true
		}
	}
	if !hasStatus {
		t.Error("expected git_status_text artifact")
	}
	if !hasDiffStat {
		t.Error("expected git_diff_stat artifact")
	}
	if !hasDiffNumstat {
		t.Error("expected git_diff_numstat artifact")
	}
	if !hasPatch {
		t.Error("expected git_diff_patch artifact")
	}

	// Verify artifact files exist on disk
	if !artifacts.Exists(runID, "git_status_text", pipeline.ArtifactFilename("git_status_text")) {
		t.Error("expected git_status_text file on disk")
	}
	if !artifacts.Exists(runID, "git_diff_patch", pipeline.ArtifactFilename("git_diff_patch")) {
		t.Error("expected git_diff_patch file on disk")
	}
}

func TestGenerateAuditHandoffReplacesExistingRow(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, "# Test\n\n## Goal\nDo something.\n")

	// Create initial audit handoff to simulate prior generation
	initialPath, err := artifacts.Write(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"), []byte("initial"))
	if err != nil {
		t.Fatalf("write initial audit handoff: %v", err)
	}
	s.CreateArtifact(runID, "audit_handoff", initialPath, "text/markdown")

	// Confirm exactly one row exists before regeneration
	artifactsBefore, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts before: %v", err)
	}
	beforeCount := 0
	for _, a := range artifactsBefore {
		if a.Kind == "audit_handoff" {
			beforeCount++
		}
	}
	if beforeCount != 1 {
		t.Fatalf("expected 1 audit_handoff before, got %d", beforeCount)
	}

	// Regenerate
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateAuditHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=audit") {
		t.Fatalf("expected redirect to step=audit, got %s", loc)
	}

	// Confirm exactly one row remains after regeneration
	artifactsAfter, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts after: %v", err)
	}
	afterCount := 0
	for _, a := range artifactsAfter {
		if a.Kind == "audit_handoff" {
			afterCount++
		}
	}
	if afterCount != 1 {
		t.Fatalf("expected exactly 1 audit_handoff after regeneration, got %d", afterCount)
	}

	// Confirm the artifact content changed (new content written)
	for _, a := range artifactsAfter {
		if a.Kind == "audit_handoff" {
			data, err := artifacts.Read(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"))
			if err != nil {
				t.Fatalf("read regenerated audit handoff: %v", err)
			}
			if string(data) == "initial" {
				t.Error("expected regenerated content to differ from initial")
			}
			break
		}
	}
}
