package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
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
	if out.ArtifactIdentity == nil || out.ArtifactIdentity.DisplayName == "" || out.ArtifactIdentity.DisplayName == path || strings.Contains(out.ArtifactIdentity.DisplayName, `\`) || strings.Contains(out.ArtifactIdentity.DisplayName, `/`) {
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
	if !strings.Contains(evidence, "submitted_sha256") || !strings.Contains(evidence, "expected_sha256") || !strings.Contains(evidence, "artifact_name") {
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

func TestPlannerHandoffFileFailuresAreSanitized(t *testing.T) {
	for _, tool := range []string{"create", "validate"} {
		t.Run(tool, func(t *testing.T) {
			for _, tc := range plannerHandoffFileFailureCases(t) {
				t.Run(tc.name, func(t *testing.T) {
					artifactDir := setupTestArtifactDir(t)
					deps := &MCPDeps{Store: setupTestStore(t), Log: discardLogger()}
					srv := NewServer(discardLogger(), deps)
					path := tc.path(t)
					args := mustMarshal(t, map[string]any{
						"planner_handoff_file": path,
						"repo_target":          "test-repo",
					})

					var result ToolCallResult
					if tool == "create" {
						result = srv.HandleCreateRunFromPlannerHandoffFile(args)
					} else {
						result = srv.HandleValidatePlannerHandoffForCompile(args)
					}

					assertPlannerHandoffFileFailureSanitized(t, result, path, filepath.Dir(path))
					for _, table := range []string{"runs", "artifacts", "run_submission_provenance", "events"} {
						if got := countTableRows(t, deps.Store.DB(), table); got != 0 {
							t.Fatalf("expected no %s rows, got %d", table, got)
						}
					}
					if got := countArtifactFiles(t, artifactDir); got != 0 {
						t.Fatalf("expected no artifact files, got %d", got)
					}
				})
			}
		})
	}
}

type plannerHandoffFileFailureCase struct {
	name string
	path func(t *testing.T) string
}

func plannerHandoffFileFailureCases(t *testing.T) []plannerHandoffFileFailureCase {
	t.Helper()
	return []plannerHandoffFileFailureCase{
		{
			name: "nonexistent file",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "reviewed.md")
			},
		},
		{
			name: "unreadable file",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "reviewed.md")
				if err := os.WriteFile(path, []byte(validMCPHandoffMarkdown("Unreadable", "test-repo")), 0600); err != nil {
					t.Fatalf("write unreadable fixture: %v", err)
				}
				if err := os.Chmod(path, 0000); err != nil {
					t.Skipf("chmod unreadable fixture unsupported: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
				if _, err := os.ReadFile(path); err == nil {
					t.Skip("unreadable fixture is readable on this host")
				}
				return path
			},
		},
		{
			name: "directory supplied",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "reviewed.md")
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatalf("mkdir handoff dir: %v", err)
				}
				return path
			},
		},
		{
			name: "invalid extension",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "reviewed.txt")
				if err := os.WriteFile(path, []byte(validMCPHandoffMarkdown("Bad Extension", "test-repo")), 0644); err != nil {
					t.Fatalf("write invalid extension fixture: %v", err)
				}
				return path
			},
		},
		{
			name: "empty file",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "reviewed.md")
				if err := os.WriteFile(path, nil, 0644); err != nil {
					t.Fatalf("write empty fixture: %v", err)
				}
				return path
			},
		},
		{
			name: "oversized file",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "reviewed.md")
				data := []byte(strings.Repeat("a", maxPlannerHandoffFileBytes+1))
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatalf("write oversized fixture: %v", err)
				}
				return path
			},
		},
	}
}

func assertPlannerHandoffFileFailureSanitized(t *testing.T, result ToolCallResult, fullPath, dir string) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected blocked result, got %+v", result)
	}
	var blocked MCPBlockedResponse
	serialized := string(mustMarshal(t, result.StructuredContent))
	if err := json.Unmarshal([]byte(serialized), &blocked); err != nil {
		t.Fatalf("decode blocked response: %v", err)
	}
	if blocked.Status != "blocked" || len(blocked.Blockers) != 1 || blocked.Blockers[0].Code != MCPBlockerBlockedPath {
		t.Fatalf("unexpected blocked response: %+v", blocked)
	}
	if !blocked.Blockers[0].Recoverable {
		t.Fatal("expected file failure blocker to be recoverable")
	}
	for _, content := range result.Content {
		if strings.Contains(content.Text, fullPath) || strings.Contains(content.Text, dir) {
			t.Fatalf("text content leaked path %q or dir %q: %s", fullPath, dir, content.Text)
		}
	}
	if strings.Contains(serialized, fullPath) || strings.Contains(serialized, dir) {
		t.Fatalf("structuredContent leaked path %q or dir %q: %s", fullPath, dir, serialized)
	}
}

func countArtifactFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk artifact dir: %v", err)
	}
	return count
}
