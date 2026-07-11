package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	appaudits "relay/internal/app/audits"
	workflowprojects "relay/internal/app/projects/workflow"
	workflowruns "relay/internal/app/runs/workflow"
	"relay/internal/mcp"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type memoryFetcher struct {
	files map[string]mcp.FileParameterContent
}

func (f memoryFetcher) FetchArtifact(_ context.Context, ref mcp.ChatGPTFileReference) (mcp.FileParameterContent, *mcp.FileParameterError) {
	value, ok := f.files[ref.FileID]
	if !ok {
		return mcp.FileParameterContent{}, &mcp.FileParameterError{
			Code:    "file_download_failed",
			Message: "unknown smoke file",
		}
	}
	value.DisplayName = ref.FileName
	return value, nil
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func call(server *mcp.Server, id int, method string, params any) (map[string]any, error) {
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := server.Serve(strings.NewReader(string(request)+"\n"), &output); err != nil {
		return nil, err
	}
	var response rpcResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("rpc %d: %s", response.Error.Code, response.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func tool(server *mcp.Server, id int, name string, arguments map[string]any) (map[string]any, error) {
	result, err := call(server, id, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return nil, err
	}
	if failed, _ := result["isError"].(bool); failed {
		return nil, fmt.Errorf("tool %s returned error: %v", name, result)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if structured == nil {
		return nil, fmt.Errorf("tool %s omitted structuredContent", name)
	}
	return structured, nil
}

func fileRef(id, name string) map[string]any {
	return map[string]any{
		"download_url": "https://files.example.invalid/" + name,
		"file_id":      id,
		"file_name":    name,
		"mime_type":    "application/json",
	}
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func runGit(repo string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func initializeSmokeRepository(repo string) (string, error) {
	if _, err := runGit(repo, "init"); err != nil {
		return "", err
	}
	for _, command := range [][]string{
		{"config", "user.email", "relay-smoke@example.invalid"},
		{"config", "user.name", "Relay Smoke"},
		{"checkout", "-b", "main"},
	} {
		if _, err := runGit(repo, command...); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "workflow.txt"), []byte("base\n"), 0o600); err != nil {
		return "", err
	}
	if _, err := runGit(repo, "add", "workflow.txt"); err != nil {
		return "", err
	}
	if _, err := runGit(repo, "commit", "-m", "smoke base"); err != nil {
		return "", err
	}
	return runGit(repo, "rev-parse", "HEAD")
}

func commitSmokeChange(repo string) (string, error) {
	if err := os.WriteFile(filepath.Join(repo, "workflow.txt"), []byte("base\nimplemented\n"), 0o600); err != nil {
		return "", err
	}
	if _, err := runGit(repo, "add", "workflow.txt"); err != nil {
		return "", err
	}
	if _, err := runGit(repo, "commit", "-m", "smoke implementation"); err != nil {
		return "", err
	}
	return runGit(repo, "rev-parse", "HEAD")
}

func rewriteExecutionSpec(spec []byte, branch, baseCommit string) ([]byte, error) {
	branchPattern := regexp.MustCompile(`"branch":\s*"[^"]+"`)
	basePattern := regexp.MustCompile(`"base_commit":\s*"[0-9a-f]{40}"`)
	rewritten := branchPattern.ReplaceAll(spec, []byte(fmt.Sprintf(`"branch": %q`, branch)))
	rewritten = basePattern.ReplaceAll(rewritten, []byte(fmt.Sprintf(`"base_commit": %q`, baseCommit)))
	if bytes.Equal(rewritten, spec) ||
		!bytes.Contains(rewritten, []byte(fmt.Sprintf(`"branch": %q`, branch))) ||
		!bytes.Contains(rewritten, []byte(fmt.Sprintf(`"base_commit": %q`, baseCommit))) {
		return nil, fmt.Errorf("execution spec fixture branch/base rewrite failed")
	}
	return rewritten, nil
}

func stageSmokeEvidence(ctx context.Context, store *workflowstore.Store, attempt workflowstore.ExecutionAttempt) error {
	batch, err := store.ArtifactStore().Begin("attempt-smoke/" + attempt.AttemptID)
	if err != nil {
		return err
	}
	staged, err := batch.Stage(
		"execution_evidence",
		"execution-evidence.json",
		"application/json",
		[]byte(`{"validated":true,"source":"mcp-smoke"}`),
	)
	if err != nil {
		_ = batch.Rollback()
		return err
	}
	return store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:            workflowstore.NewArtifactID(),
			OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
			ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true},
			Kind:                  staged.Kind,
			RelativePath:          staged.RelativePath,
			MediaType:             staged.MediaType,
			SHA256:                staged.SHA256,
			SizeBytes:             staged.SizeBytes,
		})
		return err
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "MCP smoke failed:", err)
		os.Exit(1)
	}
	fmt.Println("MCP canonical smoke passed")
}

func run() error {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "relay-canonical-mcp-smoke-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	store, err := workflowstore.Open(
		filepath.Join(root, "workflow.sqlite"),
		filepath.Join(root, "artifacts"),
	)
	if err != nil {
		return err
	}
	defer store.Close()

	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return err
	}
	baseCommit, err := initializeSmokeRepository(repoDir)
	if err != nil {
		return err
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		return err
	}
	if _, err := registry.Register(ctx, "relay", repoDir); err != nil {
		return err
	}
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		return err
	}
	project, err := projects.CreateProject(ctx, workflowprojects.CreateProjectInput{
		Name:        "Smoke Project",
		Description: "Canonical MCP smoke",
	})
	if err != nil {
		return err
	}

	planBytes, err := os.ReadFile("internal/speccompiler/testdata/valid.plan.json")
	if err != nil {
		return err
	}
	specFixture, err := os.ReadFile("internal/speccompiler/testdata/valid.execution-spec.json")
	if err != nil {
		return err
	}
	specBytes, err := rewriteExecutionSpec(specFixture, "main", baseCommit)
	if err != nil {
		return err
	}

	fetcher := memoryFetcher{files: map[string]mcp.FileParameterContent{
		"plan": {Bytes: planBytes},
		"spec": {Bytes: specBytes},
	}}
	server := mcp.NewServer(slog.Default(), &mcp.MCPDeps{
		WorkflowStore:       store,
		ToolProfile:         mcp.ToolProfileLocalOperator,
		ArtifactFileFetcher: fetcher,
	})

	listed, err := call(server, 1, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	tools, _ := listed["tools"].([]any)
	if len(tools) != 8 {
		return fmt.Errorf("tools/list returned %d tools, want 8", len(tools))
	}

	projectList, err := tool(server, 2, "list_projects", map[string]any{
		"status": "active",
		"limit":  10,
	})
	if err != nil {
		return err
	}
	if count, _ := projectList["count"].(float64); count != 1 {
		return fmt.Errorf("list_projects count = %v, want 1", projectList["count"])
	}
	if _, err := tool(server, 3, "validate_artifact", map[string]any{
		"artifact_file": fileRef("plan", "compiler-plan-fixture.plan.json"),
	}); err != nil {
		return err
	}

	planResult, err := tool(server, 4, "submit_plan", map[string]any{
		"project_id":      project.ProjectID,
		"artifact_file":   fileRef("plan", "compiler-plan-fixture.plan.json"),
		"expected_sha256": digest(planBytes),
	})
	if err != nil {
		return err
	}
	planData, _ := planResult["plan"].(map[string]any)
	planID, _ := planData["plan_id"].(string)
	if planID == "" {
		return fmt.Errorf("submit_plan omitted plan_id")
	}
	if _, err := tool(server, 5, "get_plan", map[string]any{"plan_id": planID}); err != nil {
		return err
	}

	managedResult, err := tool(server, 6, "create_run", map[string]any{
		"artifact_file":   fileRef("spec", "compiler-fixture.pass-1.execution-spec.json"),
		"expected_sha256": digest(specBytes),
		"plan_id":         planID,
		"pass_number":     1,
	})
	if err != nil {
		return err
	}
	managedData, _ := managedResult["run"].(map[string]any)
	managedRunID, _ := managedData["run_id"].(string)
	if managedRunID == "" {
		return fmt.Errorf("managed create_run omitted run_id")
	}
	if _, err := tool(server, 7, "create_run", map[string]any{
		"artifact_file":   fileRef("spec", "compiler-fixture.execution-spec.json"),
		"expected_sha256": digest(specBytes),
	}); err != nil {
		return err
	}

	runs, err := workflowruns.NewService(store)
	if err != nil {
		return err
	}
	begun, err := runs.BeginExecutionAttempt(ctx, workflowruns.BeginExecutionAttemptInput{
		RunID:   managedRunID,
		Adapter: "codex",
		Model:   "smoke-model",
	})
	if err != nil {
		return err
	}
	if _, err := runs.MarkExecutionAttemptRunning(ctx, begun.Attempt.AttemptID, `{"ok":true}`); err != nil {
		return err
	}
	finished, err := runs.FinishExecutionAttempt(ctx, workflowruns.FinishExecutionAttemptInput{
		AttemptID:  begun.Attempt.AttemptID,
		Status:     workflowstore.AttemptStatusSucceeded,
		ResultJSON: `{"normalized_status":"done","completion_summary":"smoke complete"}`,
	})
	if err != nil {
		return err
	}
	if err := stageSmokeEvidence(ctx, store, finished.Attempt); err != nil {
		return err
	}
	auditedCommit, err := commitSmokeChange(repoDir)
	if err != nil {
		return err
	}
	audits, err := appaudits.NewWorkflowAuditService(store)
	if err != nil {
		return err
	}
	prepared, err := audits.Prepare(ctx, appaudits.PrepareWorkflowAuditInput{
		RunID:         managedRunID,
		AuditedCommit: auditedCommit,
	})
	if err != nil {
		return err
	}

	packetResult, err := tool(server, 8, "get_audit_packet", map[string]any{
		"run_id": managedRunID,
	})
	if err != nil {
		return err
	}
	packet, _ := packetResult["packet"].(map[string]any)
	artifacts, _ := packet["artifacts"].([]any)
	artifactReference := ""
	for _, rawArtifact := range artifacts {
		artifact, _ := rawArtifact.(map[string]any)
		if artifact["artifact_type"] != "execution_evidence" {
			continue
		}
		if artifactReference != "" {
			return fmt.Errorf("audit packet returned multiple execution evidence artifacts")
		}
		artifactReference, _ = artifact["artifact_reference"].(string)
	}
	if artifactReference == "" {
		return fmt.Errorf("audit packet omitted execution evidence artifact")
	}

	artifactResult, err := tool(server, 9, "get_run_artifact", map[string]any{
		"run_id":             managedRunID,
		"artifact_reference": artifactReference,
		"max_bytes":          4096,
	})
	if err != nil {
		return err
	}
	if artifactResult["artifact_reference"] != artifactReference ||
		!strings.Contains(fmt.Sprint(artifactResult["content"]), `"validated":true`) {
		return fmt.Errorf("get_run_artifact returned unexpected evidence: %v", artifactResult)
	}

	decisionResult, err := tool(server, 10, "record_audit_decision", map[string]any{
		"run_id":             managedRunID,
		"audit_packet_id":    prepared.Packet.AuditPacketID,
		"packet_sha256":      prepared.Packet.PacketSHA256,
		"audited_commit":     auditedCommit,
		"decision":           workflowstore.AuditDecisionAccepted,
		"rationale":          "canonical MCP smoke accepted",
		"operator_confirmed": true,
	})
	if err != nil {
		return err
	}
	if decisionResult["run_status"] != workflowstore.RunStatusCompleted {
		return fmt.Errorf("record_audit_decision run_status = %v", decisionResult["run_status"])
	}
	return nil
}
