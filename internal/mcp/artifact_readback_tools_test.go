package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/store"
)

func seedTestRun(t *testing.T, s *store.Store) int64 {
	t.Helper()
	repoName := fmt.Sprintf("test-repo-%d", timeNowUnixNano())
	repoPath := fmt.Sprintf("/test/path/%d", timeNowUnixNano())
	repo, err := s.CreateRepo(repoName, repoPath)
	if err != nil {
		t.Fatalf("create test repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "test-run", "validated", "gpt-4", "gpt-4", "main")
	if err != nil {
		t.Fatalf("create test run: %v", err)
	}
	return run.ID
}

var testCounter int64

func timeNowUnixNano() int64 {
	testCounter++
	return testCounter
}

func seedTestArtifact(t *testing.T, s *store.Store, runID int64, kind, content, mimeType string) {
	t.Helper()
	filename := kind + ".txt"
	if strings.HasSuffix(kind, "_json") || strings.HasSuffix(kind, "_report") {
		filename = kind + ".json"
	}
	if strings.HasPrefix(mimeType, "text/markdown") {
		filename = kind + ".md"
	}
	path, err := artifacts.Write(runID, kind, filename, []byte(content))
	if err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, err := s.CreateArtifact(runID, kind, path, mimeType); err != nil {
		t.Fatalf("create artifact row: %v", err)
	}
}

func setupReadbackDeps(t *testing.T) *MCPDeps {
	t.Helper()
	setupTestArtifactDir(t)
	s := setupTestStore(t)
	return &MCPDeps{Store: s, Log: discardLogger(), ToolProfile: ToolProfileLocalOperator}
}

func unmarshalArtifactOutput(t *testing.T, result ToolCallResult) getRunArtifactOutput {
	t.Helper()
	if result.IsError {
		var blocker getRunArtifactBlockers
		if err := json.Unmarshal([]byte(result.Content[0].Text), &blocker); err == nil {
			if len(blocker.Blockers) > 0 {
				t.Fatalf("expected success, got blocker: %s: %s", blocker.Blockers[0].Code, blocker.Blockers[0].Message)
			}
		}
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	var out getRunArtifactOutput
	if err := json.Unmarshal([]byte(result.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal output: %v\nraw: %s", err, result.Content[0].Text)
	}
	return out
}

func containsArtifactBlocker(t *testing.T, result ToolCallResult, code string) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected tool error with blocker %q, got success", code)
	}
	var blocker getRunArtifactBlockers
	if err := json.Unmarshal([]byte(result.Content[0].Text), &blocker); err != nil {
		t.Fatalf("unmarshal blocker: %v\nraw: %s", err, result.Content[0].Text)
	}
	for _, b := range blocker.Blockers {
		if b.Code == code {
			return
		}
	}
	t.Fatalf("expected blocker %q, got blockers: %+v\nraw: %s", code, blocker.Blockers, result.Content[0].Text)
}

func TestServerToolsListIncludesGetRunArtifactLocalOperator(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileLocalOperator})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	found := false
	for _, tool := range list.Tools {
		if tool.Name == "get_run_artifact" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("get_run_artifact not found in tools/list under local-operator profile")
	}
}

func TestServerToolsListOmitsGetRunArtifactRestricted(t *testing.T) {
	srv := NewServer(discardLogger(), &MCPDeps{ToolProfile: ToolProfileRestricted})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	var list ToolsListResult
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("unmarshal tools list: %v", err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "get_run_artifact" {
			t.Fatal("get_run_artifact should not appear under restricted profile")
		}
	}
}

func TestGetRunArtifactStrictSchemaRejectsExtraArgs(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runIDInt := seedTestRun(t, deps.Store)
	runID := fmt.Sprintf("%d", runIDInt)
	seedTestArtifact(t, deps.Store, runIDInt, "canonical_packet", `{"key":"value"}`, "application/json")

	args, _ := json.Marshal(map[string]any{
		"run_id":        runID,
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
		"extra_field":   "should_be_rejected",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerUnsafeRequest)
}

func TestGetRunArtifactRejectsInvalidRunID(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)

	cases := []struct {
		name  string
		runID string
	}{
		{"empty", ""},
		{"negative", "-1"},
		{"zero", "0"},
		{"non_numeric", "abc"},
		{"float", "1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{
				"run_id":        tc.runID,
				"artifact_kind": "canonical_packet",
				"view_mode":     "metadata_only",
			})
			result := srv.HandleGetRunArtifact(args)
			if !result.IsError {
				t.Fatal("expected error for invalid run_id")
			}
		})
	}
}

func TestGetRunArtifactUnknownRun(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]any{
		"run_id":        "99999",
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerUnknownRun)
}

func TestGetRunArtifactMissingKind(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := fmt.Sprintf("%d", seedTestRun(t, deps.Store))
	args, _ := json.Marshal(map[string]any{
		"run_id":        runID,
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerArtifactNotFound)
}

func TestGetRunArtifactKindNotAllowed(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := fmt.Sprintf("%d", seedTestRun(t, deps.Store))
	args, _ := json.Marshal(map[string]any{
		"run_id":        runID,
		"artifact_kind": "unsafe_shell_artifact",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerArtifactKindNotAllowed)
}

func TestGetRunArtifactUsedByRunIDCheck(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	run1 := seedTestRun(t, deps.Store)
	run2 := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, run1, "canonical_packet", `{"key":"value"}`, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", run2),
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerArtifactNotFound)
}

func TestGetRunArtifactMetadataOnly(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	content := `{"status":"success","data":{"key":"value"}}`
	seedTestArtifact(t, deps.Store, runID, "validation_run_json", content, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "validation_run_json",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content != nil {
		t.Fatal("metadata_only should return no content")
	}
	if out.Artifact == nil {
		t.Fatal("metadata_only should return artifact metadata")
	}
	if out.Artifact.Kind != "validation_run_json" {
		t.Fatalf("expected kind validation_run_json, got %q", out.Artifact.Kind)
	}
	if out.Artifact.MimeType != "application/json" {
		t.Fatalf("expected mime application/json, got %q", out.Artifact.MimeType)
	}
	if out.Artifact.SizeBytes <= 0 {
		t.Fatal("expected positive size_bytes")
	}
	if out.Artifact.ArtifactRef == "" {
		t.Fatal("expected non-empty artifact_ref")
	}
	if out.Artifact.ArtifactID <= 0 {
		t.Fatal("expected positive artifact_id")
	}
}

func TestGetRunArtifactBoundedExcerpt(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	longContent := strings.Repeat("line of text\n", 500)
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", longContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
		"max_bytes":     200,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if !out.Truncated {
		t.Fatal("bounded_excerpt should report truncated=true for large content")
	}
	if out.ReturnedBytes > 200 {
		t.Fatalf("returned_bytes %d exceeds max_bytes 200", out.ReturnedBytes)
	}
	if out.ViewMode != ViewModeBoundedExcerpt {
		t.Fatalf("expected view_mode %q, got %q", ViewModeBoundedExcerpt, out.ViewMode)
	}
	if out.Artifact.ContentHash == "" {
		t.Fatal("expected content hash for content mode")
	}
}

func TestGetRunArtifactBoundedExcerptNoLocalPath(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", `{"status":"ok"}`, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "canonical_packet",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, artifacts.BaseDir) {
		t.Fatal("response must not contain local absolute artifact paths")
	}
	if strings.Contains(rawStr, "data/artifacts") {
		t.Fatal("response must not contain local artifact paths")
	}
	if out.Artifact.ArtifactRef == "" {
		t.Fatal("expected non-empty artifact_ref")
	}
	if strings.Contains(out.Artifact.ArtifactRef, string(filepath.Separator)) {
		t.Fatal("artifact_ref must not contain filesystem separators")
	}
}

func TestGetRunArtifactSummaryJSON(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	jsonContent := `{"status":"completed","pass_count":3,"fail_count":1,"blockers":[{"code":"E001"}],"results":[1,2,3]}`
	seedTestArtifact(t, deps.Store, runID, "validation_run_json", jsonContent, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "validation_run_json",
		"view_mode":     "summary",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content == nil {
		t.Fatal("summary mode should return content")
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, `"blockers":[{"code":"E001"}]`) {
		t.Fatal("summary should not dump full JSON payload including nested arrays")
	}
}

func TestGetRunArtifactSummaryText(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	mdContent := "# Status Report\n\n## Results\n\nAll tests passed."
	seedTestArtifact(t, deps.Store, runID, "audit_input_summary", mdContent, "text/markdown")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "audit_input_summary",
		"view_mode":     "summary",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content == nil {
		t.Fatal("summary mode should return content for text artifacts")
	}
}

func TestGetRunArtifactErrorsJSON(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	jsonContent := `{"status":"failed","errors":[{"code":"E001","message":"timeout"},{"code":"E002","message":"denied"}],"warnings":[{"msg":"deprecated"}]}`
	seedTestArtifact(t, deps.Store, runID, "packet_validation_report", jsonContent, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "packet_validation_report",
		"view_mode":     "errors",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content == nil {
		t.Fatal("errors mode should return content")
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if !strings.Contains(rawStr, "error") && !strings.Contains(rawStr, "errors") {
		t.Fatalf("errors output should contain error data, got: %s", rawStr)
	}
}

func TestGetRunArtifactErrorsText(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	textContent := "INFO: starting build\nERROR: compilation failed\nWARN: deprecated API\nPANIC: unrecoverable"
	seedTestArtifact(t, deps.Store, runID, "executor_stderr", textContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stderr",
		"view_mode":     "errors",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content == nil {
		t.Fatal("errors mode should return content for text artifacts")
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if !strings.Contains(rawStr, "ERROR") && !strings.Contains(rawStr, "error") {
		t.Fatalf("errors output should contain error data, got: %s", rawStr)
	}
}

func TestGetRunArtifactErrorsNoErrrors(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	jsonContent := `{"status":"success","results":[],"message":"all good"}`
	seedTestArtifact(t, deps.Store, runID, "validation_run_json", jsonContent, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "validation_run_json",
		"view_mode":     "errors",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if !strings.Contains(rawStr, "no_errors_found") {
		t.Fatalf("no-error artifact should return status:no_errors_found, got: %s", rawStr)
	}
}

func TestGetRunArtifactBinaryRejected(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	binaryContent := make([]byte, 50)
	binaryContent[10] = 0
	binaryContent[20] = 0xFF
	binaryContent[30] = 0xFE
	filename := "binary_test.bin"
	path, err := artifacts.Write(runID, "executor_stdout", filename, binaryContent)
	if err != nil {
		t.Fatalf("write binary artifact: %v", err)
	}
	if _, err := deps.Store.CreateArtifact(runID, "executor_stdout", path, "application/octet-stream"); err != nil {
		t.Fatalf("create artifact row: %v", err)
	}
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerArtifactBinary)
}

func TestGetRunArtifactRedactionApplied(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	sensitiveContent := "{\"token\": \"sk-abcdefghijklmnopqrstuvwxyz1234567890\", \"key\": \"value\"}"
	seedTestArtifact(t, deps.Store, runID, "executor_result", sensitiveContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_result",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.RedactionStatus == RedactionNotRequired {
		t.Fatal("expected redaction_status to indicate redaction for sensitive content")
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, "sk-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Fatal("sensitive content was not redacted in bounded_excerpt")
	}
}

func TestGetRunArtifactRedactionPEMRedacted(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	pemContent := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7\n-----END PRIVATE KEY-----"
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", pemContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.RedactionStatus != RedactionRedacted {
		t.Fatalf("expected redaction_status=%q for PEM content, got %q", RedactionRedacted, out.RedactionStatus)
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, "PRIVATE KEY") {
		t.Fatal("PEM content was not redacted")
	}
}

func TestGetRunArtifactMaxBytesHardcap(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	longContent := strings.Repeat("x", 70000)
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", longContent, "text/plain")
	exceedArg := 100000
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
		"max_bytes":     exceedArg,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.MaxBytes != hardMaxBytes {
		t.Fatalf("max_bytes should be clamped to hard cap %d, got %d", hardMaxBytes, out.MaxBytes)
	}
}

func TestGetRunArtifactToolRegistration(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]any{
		"run_id":        "1",
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	params, _ := json.Marshal(ToolCallParams{
		Name:      "get_run_artifact",
		Arguments: args,
	})
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`42`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.handleLine(mustMarshal(t, req))
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error from dispatcher: %v", resp.Error)
	}
}

func TestGetRunArtifactContentHashStatus(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	content := `{"status":"ok"}`
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", content, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":               fmt.Sprintf("%d", runID),
		"artifact_kind":        "canonical_packet",
		"view_mode":            "bounded_excerpt",
		"include_content_hash": true,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Artifact.ContentHash == "" {
		t.Fatal("expected content hash when include_content_hash is true")
	}
	if out.Artifact.ContentHashStatus != HashStatusComputedFull {
		t.Fatalf("expected hash status %q, got %q", HashStatusComputedFull, out.Artifact.ContentHashStatus)
	}
}

func TestGetRunArtifactIncludeHashFalse(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", `{"status":"ok"}`, "application/json")
	includeHash := false
	args, _ := json.Marshal(map[string]any{
		"run_id":               fmt.Sprintf("%d", runID),
		"artifact_kind":        "canonical_packet",
		"view_mode":            "bounded_excerpt",
		"include_content_hash": includeHash,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Artifact.ContentHash != "" {
		t.Fatal("expected no content hash when include_content_hash is false")
	}
}

func TestGetRunArtifactAllBlockers(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)

	expectedBlockers := []string{
		BlockerUnsafeRequest,
		BlockerUnknownRun,
		BlockerArtifactKindNotAllowed,
		BlockerArtifactNotFound,
		BlockerUnsafeArtifactPath,
		BlockerArtifactOversized,
		BlockerArtifactBinary,
		BlockerArtifactReadFailed,
		BlockerRedactionBlocked,
	}
	for _, code := range expectedBlockers {
		t.Run("blocker_"+code, func(t *testing.T) {
			blockerText := mustMarshal(t, getRunArtifactBlockers{
				OK:    false,
				Tool:  "get_run_artifact",
				RunID: "",
				Blockers: []artifactBlocker{
					newArtifactBlocker(code, "test", false, nil, nil),
				},
			})
			if !strings.Contains(string(blockerText), code) {
				t.Fatalf("blocker code %q not found in serialized output: %s", code, blockerText)
			}
		})
	}

	_ = srv
}

func TestGetRunArtifactErrorsRedactionApplied(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	jsonContent := `{"errors":[{"code":"E001","message":"token: sk-abcdefghijk1234567890"}],"status":"failed"}`
	seedTestArtifact(t, deps.Store, runID, "packet_validation_report", jsonContent, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "packet_validation_report",
		"view_mode":     "errors",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, "sk-abcdefghijk1234567890") {
		t.Fatal("errors mode should apply redaction before return")
	}
}

func TestGetRunArtifactOversizedContentHashStatus(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	hugeContent := strings.Repeat("{\"x\":1}\n", 1000)
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", hugeContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Artifact.ContentHashStatus == "" {
		t.Fatal("expected content_hash_status to be set")
	}
}

func TestGetRunArtifactResponseHasNoLocalPath(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", `{"status":"ok"}`, "application/json")
	for _, mode := range []string{"metadata_only", "bounded_excerpt", "summary", "errors"} {
		t.Run(mode, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{
				"run_id":        fmt.Sprintf("%d", runID),
				"artifact_kind": "canonical_packet",
				"view_mode":     mode,
			})
			result := srv.HandleGetRunArtifact(args)
			if result.IsError {
				t.Fatalf("unexpected error for mode %s: %s", mode, result.Content[0].Text)
			}
			raw := result.Content[0].Text
			if strings.Contains(strings.ToLower(raw), "data/artifacts") {
				t.Fatalf("mode %s leaks local path in response: %s", mode, raw)
			}
		})
	}
}

func TestGetRunArtifactDefaultMaxBytes(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", `{"status":"ok"}`, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "canonical_packet",
		"view_mode":     "bounded_excerpt",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.MaxBytes != defaultMaxBytes {
		t.Fatalf("expected default max_bytes %d, got %d", defaultMaxBytes, out.MaxBytes)
	}
}

func TestGetRunArtifactInvalidViewMode(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]any{
		"run_id":        "1",
		"artifact_kind": "canonical_packet",
		"view_mode":     "full_dump",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerUnsafeRequest)
}

func TestGetRunArtifactRedactionStatusExplicit(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	cleanContent := `{"status":"ok","message":"no secrets here"}`
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", cleanContent, "application/json")
	for _, mode := range []string{"metadata_only", "bounded_excerpt", "summary", "errors"} {
		t.Run(mode, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{
				"run_id":        fmt.Sprintf("%d", runID),
				"artifact_kind": "canonical_packet",
				"view_mode":     mode,
			})
			result := srv.HandleGetRunArtifact(args)
			if result.IsError {
				t.Fatalf("unexpected error for mode %s: %s", mode, result.Content[0].Text)
			}
			raw := result.Content[0].Text
			if !strings.Contains(raw, "redaction_status") {
				t.Fatalf("mode %s: expected redaction_status in response, got: %s", mode, raw)
			}
		})
	}
}

func TestGetRunArtifactMaxBytesMinMaxBounds(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", strings.Repeat("a", 100), "text/plain")

	zero := 0
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
		"max_bytes":     zero,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.MaxBytes != 1 {
		t.Fatalf("zero max_bytes should clamp to 1, got %d", out.MaxBytes)
	}

	overCap := hardMaxBytes + 100000
	args2, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
		"max_bytes":     overCap,
	})
	result2 := srv.HandleGetRunArtifact(args2)
	out2 := unmarshalArtifactOutput(t, result2)
	if out2.MaxBytes != hardMaxBytes {
		t.Fatalf("over-cap max_bytes should clamp to %d, got %d", hardMaxBytes, out2.MaxBytes)
	}
}

func TestGetRunArtifactErrorsEmptyResult(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	cleanLog := "INFO: task started\nDEBUG: processing\nINFO: task completed"
	seedTestArtifact(t, deps.Store, runID, "command_log", cleanLog, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "command_log",
		"view_mode":     "errors",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if !strings.Contains(rawStr, "no_errors_found") {
		t.Fatalf("expected no_errors_found for clean log, got: %s", rawStr)
	}
}

func TestGetRunArtifactKindStartsWithUnsafe(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "../escape_attempt",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	containsArtifactBlocker(t, result, BlockerArtifactKindNotAllowed)
}

func TestGetRunArtifactSummaryMarkdown(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	md := "# Title\n\n## Section One\n\nContent here.\n\n## Section Two\n\nMore content."
	seedTestArtifact(t, deps.Store, runID, "planner_handoff", md, "text/markdown")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "planner_handoff",
		"view_mode":     "summary",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Content == nil {
		t.Fatal("summary mode should return content for markdown")
	}
	raw, _ := json.Marshal(out)
	rawStr := string(raw)
	if strings.Contains(rawStr, "Content here") {
		t.Fatal("summary should not expose full body text of markdown")
	}
}

func TestGetRunArtifactContextPacketReadback(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "context_packet_json", `{"contexts":["a","b"]}`, "application/json")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "context_packet_json",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if out.Artifact == nil {
		t.Fatal("expected artifact metadata for context_packet_json")
	}
}

func TestGetRunArtifactTruncationMetadata(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	longContent := strings.Repeat("line\n", 500)
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", longContent, "text/plain")
	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "executor_stdout",
		"view_mode":     "bounded_excerpt",
		"max_bytes":     300,
	})
	result := srv.HandleGetRunArtifact(args)
	out := unmarshalArtifactOutput(t, result)
	if !out.Truncated {
		t.Fatal("expected truncated=true for large content")
	}
	if out.ReturnedBytes <= 0 {
		t.Fatal("expected returned_bytes > 0")
	}
}

func assertBlockerEnvelope(t *testing.T, text string) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("failed to unmarshal raw blocker json: %v", err)
	}
	// check blockers array
	blockersVal, exists := raw["blockers"]
	if !exists {
		t.Fatal("expected blockers field in envelope")
	}
	blockers, ok := blockersVal.([]any)
	if !ok {
		t.Fatalf("blockers must be an array, got %T", blockersVal)
	}
	if len(blockers) == 0 {
		t.Fatal("expected at least one blocker in envelope")
	}
	for _, blkAny := range blockers {
		blk, ok := blkAny.(map[string]any)
		if !ok {
			t.Fatalf("blocker item must be map, got %T", blkAny)
		}
		// assert fields
		for _, field := range []string{"code", "message", "recoverable", "evidence", "next_actions"} {
			if _, exists := blk[field]; !exists {
				t.Fatalf("blocker missing field %q", field)
			}
		}
		// check recoverable is bool
		if _, ok := blk["recoverable"].(bool); !ok {
			t.Fatalf("recoverable must be bool, got %T", blk["recoverable"])
		}
		// check evidence is list
		evList, ok := blk["evidence"].([]any)
		if !ok {
			t.Fatalf("evidence must be list, got %T", blk["evidence"])
		}
		for _, evAny := range evList {
			ev, ok := evAny.(map[string]any)
			if !ok {
				t.Fatalf("evidence item must be map, got %T", evAny)
			}
			// kind is required
			if _, exists := ev["kind"]; !exists {
				t.Fatal("evidence item missing kind")
			}
			// assert no local paths in evidence
			for k, v := range ev {
				s, ok := v.(string)
				if ok && (strings.Contains(s, "data/artifacts") || strings.Contains(s, artifacts.BaseDir) || strings.Contains(s, "/test/path")) {
					t.Fatalf("leak detected in evidence field %s: %s", k, s)
				}
			}
		}
		// check next actions are safe
		naList, ok := blk["next_actions"].([]any)
		if !ok {
			t.Fatalf("next_actions must be list, got %T", blk["next_actions"])
		}
		for _, naAny := range naList {
			na, ok := naAny.(map[string]any)
			if !ok {
				t.Fatalf("next_action item must be map, got %T", naAny)
			}
			if _, exists := na["action"]; !exists {
				t.Fatal("next_action item missing action")
			}
			if _, exists := na["description"]; !exists {
				t.Fatal("next_action item missing description")
			}
			// assert no shell/git/file browsing named in actions
			actionStr := strings.ToLower(na["action"].(string))
			descStr := strings.ToLower(na["description"].(string))
			for _, keyword := range []string{"shell", "git", "bash", "cmd", "exec", "browse", "file manager"} {
				if strings.Contains(actionStr, keyword) || strings.Contains(descStr, keyword) {
					t.Fatalf("unsafe next_action detected: action=%q, desc=%q", actionStr, descStr)
				}
			}
		}
	}
}

func TestGetRunArtifactRawJSONSuccessOKIsBoolean(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	content := `{"status":"ok"}`
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", content, "application/json")

	// metadata_only
	args1, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result1 := srv.HandleGetRunArtifact(args1)
	if result1.IsError {
		t.Fatalf("unexpected error: %s", result1.Content[0].Text)
	}

	var raw1 map[string]any
	if err := json.Unmarshal([]byte(result1.Content[0].Text), &raw1); err != nil {
		t.Fatalf("failed to unmarshal raw JSON: %v", err)
	}
	val1, exists1 := raw1["ok"]
	if !exists1 {
		t.Fatal("ok field not found in response")
	}
	bVal1, isBool1 := val1.(bool)
	if !isBool1 {
		t.Fatalf("expected ok to be boolean, got %T", val1)
	}
	if !bVal1 {
		t.Fatal("expected ok to be true")
	}

	// content mode (bounded_excerpt)
	args2, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "canonical_packet",
		"view_mode":     "bounded_excerpt",
	})
	result2 := srv.HandleGetRunArtifact(args2)
	if result2.IsError {
		t.Fatalf("unexpected error: %s", result2.Content[0].Text)
	}

	var raw2 map[string]any
	if err := json.Unmarshal([]byte(result2.Content[0].Text), &raw2); err != nil {
		t.Fatalf("failed to unmarshal raw JSON: %v", err)
	}
	val2, exists2 := raw2["ok"]
	if !exists2 {
		t.Fatal("ok field not found in response")
	}
	bVal2, isBool2 := val2.(bool)
	if !isBool2 {
		t.Fatalf("expected ok to be boolean, got %T", val2)
	}
	if !bVal2 {
		t.Fatal("expected ok to be true")
	}
}

func TestGetRunArtifactUnknownRunEnvelope(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	args, _ := json.Marshal(map[string]any{
		"run_id":        "99999",
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	if !result.IsError {
		t.Fatal("expected error")
	}
	assertBlockerEnvelope(t, result.Content[0].Text)

	var raw map[string]any
	json.Unmarshal([]byte(result.Content[0].Text), &raw)
	if raw["ok"] != false {
		t.Fatalf("expected ok false, got %v", raw["ok"])
	}

	var blockersEnvelope getRunArtifactBlockers
	json.Unmarshal([]byte(result.Content[0].Text), &blockersEnvelope)
	blk := blockersEnvelope.Blockers[0]
	if blk.Code != BlockerUnknownRun {
		t.Fatalf("expected unknown_run, got %s", blk.Code)
	}
	if !blk.Recoverable {
		t.Fatal("expected recoverable to be true")
	}
	foundRunID := false
	for _, ev := range blk.Evidence {
		if ev.Kind == "run_id" && ev.ID == "99999" {
			foundRunID = true
		}
	}
	if !foundRunID {
		t.Fatal("expected run_id 99999 in evidence")
	}
	foundNextAction := false
	for _, na := range blk.NextActions {
		if na.Action == "verify_run_id" && strings.Contains(strings.ToLower(na.Description), "registered run id") {
			foundNextAction = true
		}
	}
	if !foundNextAction {
		t.Fatal("expected next action verify_run_id instructing verification of registered run ID")
	}
}

func TestGetRunArtifactUnsafeArtifactPathEnvelope(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	unsafePath := "../escaping_secret.txt"
	if _, err := deps.Store.CreateArtifact(runID, "canonical_packet", unsafePath, "application/json"); err != nil {
		t.Fatalf("failed to seed unsafe artifact: %v", err)
	}

	args, _ := json.Marshal(map[string]any{
		"run_id":        fmt.Sprintf("%d", runID),
		"artifact_kind": "canonical_packet",
		"view_mode":     "metadata_only",
	})
	result := srv.HandleGetRunArtifact(args)
	if !result.IsError {
		t.Fatal("expected error")
	}
	assertBlockerEnvelope(t, result.Content[0].Text)

	var blockersEnvelope getRunArtifactBlockers
	json.Unmarshal([]byte(result.Content[0].Text), &blockersEnvelope)
	blk := blockersEnvelope.Blockers[0]
	if blk.Code != BlockerUnsafeArtifactPath {
		t.Fatalf("expected unsafe_artifact_path, got %s", blk.Code)
	}
	if blk.Recoverable {
		t.Fatal("expected recoverable to be false")
	}
	rawText := result.Content[0].Text
	if strings.Contains(rawText, "escaping_secret") {
		t.Fatal("unsafe path leaked in response!")
	}
	for _, ev := range blk.Evidence {
		if ev.Kind != "run_id" && ev.Kind != "artifact_kind" {
			t.Fatalf("unexpected evidence kind: %s", ev.Kind)
		}
	}
}

func TestGetRunArtifactHashStatusNeverEmpty(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)
	seedTestArtifact(t, deps.Store, runID, "canonical_packet", `{"status":"ok"}`, "application/json")

	for _, mode := range []string{"metadata_only", "bounded_excerpt", "summary", "errors"} {
		t.Run(mode, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{
				"run_id":               fmt.Sprintf("%d", runID),
				"artifact_kind":        "canonical_packet",
				"view_mode":            mode,
				"include_content_hash": true,
			})
			result := srv.HandleGetRunArtifact(args)
			if result.IsError {
				t.Fatalf("expected success, got: %s", result.Content[0].Text)
			}
			out := unmarshalArtifactOutput(t, result)
			if out.Artifact.ContentHashStatus == "" {
				t.Fatal("content_hash_status must not be empty")
			}
		})
	}
}

func TestGetRunArtifactOptionAHashSemantics(t *testing.T) {
	deps := setupReadbackDeps(t)
	srv := NewServer(discardLogger(), deps)
	runID := seedTestRun(t, deps.Store)

	largeContent := strings.Repeat("abcd", 4000) // 16,000 bytes > defaultMaxBytes
	seedTestArtifact(t, deps.Store, runID, "executor_stdout", largeContent, "text/plain")

	args1, _ := json.Marshal(map[string]any{
		"run_id":               fmt.Sprintf("%d", runID),
		"artifact_kind":        "executor_stdout",
		"view_mode":            "bounded_excerpt",
		"max_bytes":            1000,
		"include_content_hash": true,
	})
	result1 := srv.HandleGetRunArtifact(args1)
	out1 := unmarshalArtifactOutput(t, result1)

	h := sha256.Sum256([]byte(largeContent))
	expectedFullHash := hex.EncodeToString(h[:])

	if out1.Artifact.ContentHash != expectedFullHash {
		t.Fatalf("expected hash of full artifact %q, got %q", expectedFullHash, out1.Artifact.ContentHash)
	}
	if out1.Artifact.ContentHashStatus != HashStatusComputedFull {
		t.Fatalf("expected status %q, got %q", HashStatusComputedFull, out1.Artifact.ContentHashStatus)
	}

	// 17MB file
	oversizedBytes := make([]byte, 17*1024*1024)
	filename := "executor_stderr.txt"
	path, err := artifacts.Write(runID, "executor_stderr", filename, oversizedBytes)
	if err != nil {
		t.Fatalf("failed to write oversized file: %v", err)
	}
	if _, err := deps.Store.CreateArtifact(runID, "executor_stderr", path, "text/plain"); err != nil {
		t.Fatalf("failed to seed oversized artifact row: %v", err)
	}

	args2, _ := json.Marshal(map[string]any{
		"run_id":               fmt.Sprintf("%d", runID),
		"artifact_kind":        "executor_stderr",
		"view_mode":            "metadata_only",
		"include_content_hash": true,
	})
	result2 := srv.HandleGetRunArtifact(args2)
	out2 := unmarshalArtifactOutput(t, result2)
	if out2.Artifact.ContentHash != "" {
		t.Fatalf("expected empty hash for oversized file, got %q", out2.Artifact.ContentHash)
	}
	if out2.Artifact.ContentHashStatus != HashStatusOmittedOversized {
		t.Fatalf("expected status %q, got %q", HashStatusOmittedOversized, out2.Artifact.ContentHashStatus)
	}

	args3, _ := json.Marshal(map[string]any{
		"run_id":               fmt.Sprintf("%d", runID),
		"artifact_kind":        "executor_stdout",
		"view_mode":            "bounded_excerpt",
		"include_content_hash": false,
	})
	result3 := srv.HandleGetRunArtifact(args3)
	out3 := unmarshalArtifactOutput(t, result3)
	if out3.Artifact.ContentHash != "" {
		t.Fatalf("expected empty hash, got %q", out3.Artifact.ContentHash)
	}
	if out3.Artifact.ContentHashStatus != HashStatusOmittedByRequest {
		t.Fatalf("expected status %q, got %q", HashStatusOmittedByRequest, out3.Artifact.ContentHashStatus)
	}
}
