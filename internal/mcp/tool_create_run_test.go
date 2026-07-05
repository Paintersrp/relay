package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

type fakeFileParameterFetcher struct {
	content map[string]FileParameterContent
	err     *FileParameterError
	calls   []ChatGPTFileReference
}

func (f *fakeFileParameterFetcher) FetchPlannerHandoff(ctx context.Context, ref ChatGPTFileReference) (FileParameterContent, *FileParameterError) {
	f.calls = append(f.calls, ref)
	if f.err != nil {
		return FileParameterContent{}, f.err
	}
	if ref.DownloadURL == "" || ref.FileID == "" {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file requires download_url and file_id")
	}
	if f.content != nil {
		if out, ok := f.content[ref.FileID]; ok {
			return out, nil
		}
	}
	return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file could not be downloaded")
}

func fileReference(fileID, fileName string) map[string]any {
	return map[string]any{
		"download_url": "https://files.example.test/download/" + fileID + "?signature=secret",
		"file_id":      fileID,
		"mime_type":    "text/markdown",
		"file_name":    fileName,
	}
}

func injectHandoffFetch(t *testing.T, deps *MCPDeps, fileID, fileName string, markdown []byte) {
	t.Helper()
	deps.FileFetcher = &fakeFileParameterFetcher{content: map[string]FileParameterContent{
		fileID: {Bytes: markdown, DisplayName: safeArtifactDisplayName(fileName, "planner-handoff.md")},
	}}
}

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

func TestLegacyPlannerHandoffFileToolDefinitionsStillCompile(t *testing.T) {
	tools := []ToolDefinition{ToolCreateRunFromPlannerHandoffFile, ToolValidatePlannerHandoffForCompile}
	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			params, ok := tool.Meta["openai/fileParams"].([]string)
			if !ok || len(params) != 1 || params[0] != "planner_handoff_file" {
				t.Fatalf("unexpected file params: %#v", tool.Meta["openai/fileParams"])
			}
			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			props := schema["properties"].(map[string]any)
			fileProp := props["planner_handoff_file"].(map[string]any)
			if fileProp["type"] != "object" || fileProp["additionalProperties"] != false {
				t.Fatalf("unexpected file schema: %+v", fileProp)
			}
		})
	}
}

func TestCreateRunFromPlannerHandoffFileReturnsSanitizedIdentity(t *testing.T) {
	setupTestArtifactDir(t)
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	markdown := []byte(validMCPHandoffMarkdown("File Provenance", "test-repo"))
	injectHandoffFetch(t, deps, "file-provenance", "reviewed.md", markdown)
	sum := sha256.Sum256(markdown)
	expected := hex.EncodeToString(sum[:])
	args := mustMarshal(t, map[string]any{
		"planner_handoff_file": fileReference("file-provenance", "reviewed.md"),
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
	if out.ArtifactIdentity == nil || out.ArtifactIdentity.DisplayName != "reviewed.md" || strings.Contains(out.ArtifactIdentity.DisplayName, `\`) || strings.Contains(out.ArtifactIdentity.DisplayName, `/`) {
		t.Fatalf("unsafe artifact identity: %+v", out.ArtifactIdentity)
	}
}

func TestCreateRunFromPlannerHandoffFileHashMismatchStructuredBlocker(t *testing.T) {
	setupTestArtifactDir(t)
	deps := setupTestDeps(t)
	srv := NewServer(discardLogger(), deps)
	injectHandoffFetch(t, deps, "mismatch", "reviewed.md", []byte(validMCPHandoffMarkdown("Mismatch", "test-repo")))
	args := mustMarshal(t, map[string]any{
		"planner_handoff_file": fileReference("mismatch", "reviewed.md"),
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
	if strings.Contains(evidence, "signature=secret") || strings.Contains(string(mustMarshal(t, result.StructuredContent)), "signature=secret") {
		t.Fatalf("evidence leaked signed URL details: %s", evidence)
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

func TestPlannerHandoffFileFetcherFailuresAreSanitized(t *testing.T) {
	for _, tool := range []string{"create", "validate"} {
		t.Run(tool, func(t *testing.T) {
			for _, tc := range plannerHandoffFileFailureCases() {
				t.Run(tc.name, func(t *testing.T) {
					artifactDir := setupTestArtifactDir(t)
					deps := &MCPDeps{Store: setupTestStore(t), Log: discardLogger()}
					deps.FileFetcher = &fakeFileParameterFetcher{err: tc.err}
					srv := NewServer(discardLogger(), deps)
					args := mustMarshal(t, map[string]any{
						"planner_handoff_file": fileReference("blocked", "reviewed.md"),
						"repo_target":          "test-repo",
					})

					var result ToolCallResult
					if tool == "create" {
						result = srv.HandleCreateRunFromPlannerHandoffFile(args)
					} else {
						result = srv.HandleValidatePlannerHandoffForCompile(args)
					}

					assertPlannerHandoffFileFailureSanitized(t, result)
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
	err  *FileParameterError
}

func plannerHandoffFileFailureCases() []plannerHandoffFileFailureCase {
	return []plannerHandoffFileFailureCase{
		{name: "invalid reference", err: fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.download_url is required")},
		{name: "unsafe target", err: fileParamErr(MCPBlockerUnsafeDownloadTarget, "planner_handoff_file.download_url target is not public routable")},
		{name: "download failure", err: fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file could not be downloaded")},
		{name: "non success", err: fileParamErr(MCPBlockerFileDownloadStatus, "planner_handoff_file download returned HTTP 403")},
		{name: "empty", err: fileParamErr(MCPBlockerFileDownloadEmpty, "planner_handoff_file response was empty")},
		{name: "oversized", err: fileParamErr(MCPBlockerFileDownloadTooLarge, "planner_handoff_file exceeds the 1 MiB limit")},
	}
}

func assertPlannerHandoffFileFailureSanitized(t *testing.T, result ToolCallResult) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected blocked result, got %+v", result)
	}
	var blocked MCPBlockedResponse
	serialized := string(mustMarshal(t, result.StructuredContent))
	if err := json.Unmarshal([]byte(serialized), &blocked); err != nil {
		t.Fatalf("decode blocked response: %v", err)
	}
	if blocked.Status != "blocked" || len(blocked.Blockers) != 1 {
		t.Fatalf("unexpected blocked response: %+v", blocked)
	}
	for _, content := range result.Content {
		if strings.Contains(content.Text, "signature=secret") || strings.Contains(content.Text, "https://files.example.test") {
			t.Fatalf("text content leaked URL details: %s", content.Text)
		}
	}
	if strings.Contains(serialized, "signature=secret") || strings.Contains(serialized, "https://files.example.test") {
		t.Fatalf("structuredContent leaked URL details: %s", serialized)
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
