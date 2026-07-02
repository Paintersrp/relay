package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRunFromPlannerHandoffInlineReturnsExactProvenance(t *testing.T) {
	setupTestArtifactDir(t)
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	markdown := validMCPHandoffMarkdown("Inline Provenance", "test-repo")
	args := mustMarshal(t, map[string]any{
		"planner_handoff_markdown": markdown,
		"repo_target":              "test-repo",
	})
	result := srv.HandleCreateRunFromPlannerHandoff(args)
	if result.IsError {
		t.Fatalf("expected success, got %s", result.Content[0].Text)
	}
	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	sum := sha256.Sum256([]byte(markdown))
	if out.SubmittedHandoffSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected submitted hash %q", out.SubmittedHandoffSHA256)
	}
	if out.SHAMatchStatus != "not_supplied" || out.SourceMode != "inline" {
		t.Fatalf("unexpected provenance mode/status: %+v", out)
	}
	if out.ArtifactIdentity == nil || out.ArtifactIdentity.DisplayName != "inline-planner-handoff" || out.ArtifactIdentity.ByteCount != int64(len([]byte(markdown))) {
		t.Fatalf("unexpected artifact identity: %+v", out.ArtifactIdentity)
	}
}

func TestCreateRunFromPlannerHandoffFileReturnsSanitizedIdentity(t *testing.T) {
	setupTestArtifactDir(t)
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	dir := t.TempDir()
	markdown := []byte(validMCPHandoffMarkdown("File Provenance", "test-repo"))
	path := filepath.Join(dir, "reviewed.md")
	if err := os.WriteFile(path, markdown, 0644); err != nil {
		t.Fatalf("write handoff: %v", err)
	}
	sum := sha256.Sum256(markdown)
	expected := hex.EncodeToString(sum[:])
	args := mustMarshal(t, map[string]any{
		"planner_handoff_file": path,
		"expected_sha256":      expected,
		"repo_target":          "test-repo",
	})
	result := srv.HandleCreateRunFromPlannerHandoffFile(args)
	if result.IsError {
		t.Fatalf("expected success, got %s", result.Content[0].Text)
	}
	var out createRunOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.SubmittedHandoffSHA256 != expected || out.ExpectedSHA256 != expected || out.SHAMatchStatus != "matched" {
		t.Fatalf("unexpected hash provenance: %+v", out)
	}
	if out.ArtifactIdentity == nil || out.ArtifactIdentity.DisplayName != "reviewed.md" || out.ArtifactIdentity.DisplayName == path {
		t.Fatalf("unsafe artifact identity: %+v", out.ArtifactIdentity)
	}
}

func TestCreateRunFromPlannerHandoffFileHashMismatchStructuredBlocker(t *testing.T) {
	setupTestArtifactDir(t)
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	dir := t.TempDir()
	path := filepath.Join(dir, "reviewed.md")
	if err := os.WriteFile(path, []byte(validMCPHandoffMarkdown("Mismatch", "test-repo")), 0644); err != nil {
		t.Fatalf("write handoff: %v", err)
	}
	args := mustMarshal(t, map[string]any{
		"planner_handoff_file": path,
		"expected_sha256":      "0000000000000000000000000000000000000000000000000000000000000000",
		"repo_target":          "test-repo",
	})
	result := srv.HandleCreateRunFromPlannerHandoffFile(args)
	if !result.IsError {
		t.Fatal("expected blocked hash mismatch")
	}
	var blocked MCPBlockedResponse
	data, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(data, &blocked); err != nil {
		t.Fatalf("unmarshal structuredContent: %v", err)
	}
	if blocked.Status != "blocked" || len(blocked.Blockers) != 1 || blocked.Blockers[0].Code != MCPBlockerExpectedHashMismatch {
		t.Fatalf("unexpected blocked response: %+v", blocked)
	}
	if !blocked.Blockers[0].Recoverable {
		t.Fatal("expected hash mismatch to be recoverable")
	}
	evidence := string(mustMarshal(t, blocked.Blockers[0].Evidence))
	if !strings.Contains(evidence, "submitted_sha256") || !strings.Contains(evidence, "expected_sha256") || !strings.Contains(evidence, "reviewed.md") {
		t.Fatalf("expected safe hash and artifact-name evidence, got %s", evidence)
	}
	if strings.Contains(evidence, dir) || strings.Contains(evidence, path) {
		t.Fatalf("evidence leaked local path: %s", evidence)
	}
	if got := countTableRows(t, deps.Store.DB(), "runs"); got != 0 {
		t.Fatalf("expected no run rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "artifacts"); got != 0 {
		t.Fatalf("expected no artifact rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "run_submission_provenance"); got != 0 {
		t.Fatalf("expected no provenance rows, got %d", got)
	}
	if got := countTableRows(t, deps.Store.DB(), "events"); got != 0 {
		t.Fatalf("expected no event rows, got %d", got)
	}
}
