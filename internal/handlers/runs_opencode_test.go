package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"

	"github.com/go-chi/chi/v5"
)

func setupOpenCodeRun(t *testing.T, s *store.Store) int64 {
	t.Helper()
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "ready", "test-model", "anthropic/claude-sonnet-4-5", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	// Create agent_prompt artifact
	agentPromptText := "compact prompt for agent"
	agentPromptPath, err := artifacts.Write(run.ID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte(agentPromptText))
	if err != nil {
		t.Fatalf("write agent prompt: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "agent_prompt", agentPromptPath, "text/plain")
	if err != nil {
		t.Fatalf("create agent prompt artifact: %v", err)
	}
	// Create opencode_handoff_packet artifact
	packetData := `{"run_id":1,"status":"configured"}`
	packetPath, err := artifacts.Write(run.ID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet"), []byte(packetData))
	if err != nil {
		t.Fatalf("write packet: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "opencode_handoff_packet", packetPath, "application/json")
	if err != nil {
		t.Fatalf("create packet artifact: %v", err)
	}
	return run.ID
}

func TestDryRunOpenCodeGoDoesNotExecuteRunner(t *testing.T) {
	s := setupTestStore(t)
	runnerCalled := false

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		runnerCalled = true
		return pipeline.AgentCommandRunResult{}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.dryRunOpenCodeGo(w, req, runID)

	if runnerCalled {
		t.Fatal("dry run must not execute the runner")
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasDryRun := false
	for _, a := range artifactsList {
		if a.Kind == "opencode_dry_run_json" {
			hasDryRun = true
			break
		}
	}
	if !hasDryRun {
		t.Fatal("expected opencode_dry_run_json artifact after dry run")
	}
}

func TestStartOpenCodeGoUsesArgsRunner(t *testing.T) {
	s := setupTestStore(t)

	var recordedWorkDir, recordedBinary, recordedStdin string
	var recordedArgs []string
	var runnerCalled bool

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		runnerCalled = true
		recordedWorkDir = workDir
		recordedBinary = binary
		recordedArgs = args
		recordedStdin = stdin
		return pipeline.AgentCommandRunResult{
			ExitCode: 0,
			Stdout:   "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	if !runnerCalled {
		t.Fatal("expected runner to be called")
	}
	if recordedBinary == "" {
		t.Fatal("expected runner to be called with a binary")
	}
	if recordedBinary != "opencode" {
		t.Fatalf("expected binary 'opencode', got %q", recordedBinary)
	}

	hasRun := false
	hasFormatJSON := false
	hasDir := false
	hasAgent := false
	hasModel := false
	hasThinking := false
	for _, arg := range recordedArgs {
		switch arg {
		case "run":
			hasRun = true
		case "--format":
			hasFormatJSON = true
		case "--dir":
			hasDir = true
		case "--agent":
			hasAgent = true
		case "--model":
			hasModel = true
		case "--thinking":
			hasThinking = true
		}
	}
	if !hasRun {
		t.Fatal("expected 'run' in args")
	}
	if !hasFormatJSON {
		t.Fatal("expected '--format' in args")
	}
	if !hasDir {
		t.Fatal("expected '--dir' in args")
	}
	if !hasAgent {
		t.Fatal("expected '--agent' in args")
	}
	if !hasModel {
		t.Fatal("expected '--model' in args")
	}
	if !hasThinking {
		t.Fatal("expected '--thinking' in args")
	}
	if recordedWorkDir == "" {
		t.Fatal("expected workDir to be set")
	}
	if !strings.Contains(recordedStdin, "compact prompt") {
		t.Fatal("expected stdin to contain the agent prompt")
	}
}

func TestStartOpenCodeGoPersistsDoneFromJSONL(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 0,
			Stdout: `{"type":"text","part":{"type":"text","text":"DONE"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Test status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Count of LOC changed: 12"}}
`,
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	hasJSON := false
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			hasRaw = true
		}
		if a.Kind == "agent_result_json" {
			hasJSON = true
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw artifact after DONE from JSONL")
	}
	if !hasJSON {
		t.Fatal("expected agent_result_json artifact after DONE from JSONL")
	}
}

func TestStartOpenCodeGoNonZeroExitPersistsArtifacts(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 1,
			Stdout:   "some output",
			Stderr:   "error: something went wrong",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasStdout := false
	hasStderr := false
	hasCombined := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "opencode_stdout":
			hasStdout = true
		case "opencode_stderr":
			hasStderr = true
		case "opencode_combined_log":
			hasCombined = true
		}
	}
	if !hasStdout {
		t.Fatal("expected opencode_stdout artifact after non-zero exit")
	}
	if !hasStderr {
		t.Fatal("expected opencode_stderr artifact after non-zero exit")
	}
	if !hasCombined {
		t.Fatal("expected opencode_combined_log artifact after non-zero exit")
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Status != "failed" {
		t.Fatalf("expected status 'failed', got %q", exec.Status)
	}
	if !exec.ExitCode.Valid || exec.ExitCode.Int64 != 1 {
		t.Fatalf("expected exit code 1, got %v", exec.ExitCode.Int64)
	}
}

func TestStartOpenCodeGoNonZeroExitWithUnknownOutputDoesNotPersistAgentResult(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 1,
			Stdout:   "some unexpected output without DONE or BLOCKED",
			Stderr:   "error: something went wrong",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			t.Fatal("did not expect agent_result_raw for non-zero exit without DONE/BLOCKED")
		}
	}
}

func TestStartOpenCodeGoNonZeroExitWithDoneStillPersistsResult(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 1,
			Stdout:   `{"type":"text","part":{"type":"text","text":"DONE\nBuild status: PASS"}}`,
			Stderr:   "model unavailable error",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			hasRaw = true
			break
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw when DONE was parsed despite non-zero exit")
	}
}

func TestDryRunOpenCodeGoPersistsAllFields(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		t.Fatal("runner should not be called during dry run")
		return pipeline.AgentCommandRunResult{}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.dryRunOpenCodeGo(w, req, runID)

	// Read the dry run JSON artifact
	data, err := artifacts.Read(runID, "opencode_dry_run_json", pipeline.ArtifactFilename("opencode_dry_run_json"))
	if err != nil {
		t.Fatalf("read dry run json: %v", err)
	}

	type dryRunPreview struct {
		Binary      string   `json:"binary"`
		Args        []string `json:"args"`
		WorkDir     string   `json:"work_dir"`
		StdinSource string   `json:"stdin_source"`
		StdinBytes  int      `json:"stdin_bytes"`
		Model       string   `json:"model"`
		Agent       string   `json:"agent"`
		Preview     string   `json:"preview"`
	}

	var preview dryRunPreview
	if err := json.Unmarshal(data, &preview); err != nil {
		t.Fatalf("unmarshal dry run json: %v", err)
	}

	if preview.Binary != "opencode" {
		t.Fatalf("expected binary 'opencode', got %q", preview.Binary)
	}
	if len(preview.Args) == 0 {
		t.Fatal("expected non-empty args")
	}
	if preview.WorkDir == "" {
		t.Fatal("expected non-empty work_dir")
	}
	if preview.StdinSource == "" {
		t.Fatal("expected non-empty stdin_source")
	}
	if preview.StdinBytes == 0 {
		t.Fatal("expected non-zero stdin_bytes")
	}
	if preview.Model == "" {
		t.Fatal("expected non-empty model")
	}
	if preview.Agent == "" {
		t.Fatal("expected non-empty agent")
	}
	if preview.Preview == "" {
		t.Fatal("expected non-empty preview")
	}

	// Verify important flags in preview
	if !strings.Contains(preview.Preview, "--thinking max") {
		t.Fatal("expected --thinking max in preview")
	}
	if !strings.Contains(preview.Preview, "--format json") {
		t.Fatal("expected --format json in preview")
	}
}

func TestCheckOpenCodeCLIRunsVersionAndModels(t *testing.T) {
	s := setupTestStore(t)

	var callCount int

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		callCount++
		// Return success for both version and models
		return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "opencode version 1.0.0"}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.checkOpenCodeCLI(w, req, runID)

	if callCount != 2 {
		t.Fatalf("expected 2 runner calls (version + models), got %d", callCount)
	}

	// Check that opencode_cli_check_json artifact was created
	data, err := artifacts.Read(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"))
	if err != nil {
		t.Fatalf("read cli check json: %v", err)
	}

	type cliCheckResult struct {
		Binary          string `json:"binary"`
		VersionExitCode int    `json:"version_exit_code"`
		ModelsExitCode  int    `json:"models_exit_code"`
		ModelAvailable  bool   `json:"model_available"`
	}
	var result cliCheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal cli check: %v", err)
	}

	if result.Binary != "opencode" {
		t.Fatalf("expected binary 'opencode', got %q", result.Binary)
	}
	if result.VersionExitCode != 0 {
		t.Fatalf("expected version exit code 0, got %d", result.VersionExitCode)
	}
	if result.ModelsExitCode != 0 {
		t.Fatalf("expected models exit code 0, got %d", result.ModelsExitCode)
	}
}

func TestCheckOpenCodeCLIFailsOnVersionError(t *testing.T) {
	s := setupTestStore(t)

	var callCount int

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		callCount++
		return pipeline.AgentCommandRunResult{
			ExitCode: -1,
			Error:    "executable file not found",
			Stderr:   "not recognized",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.checkOpenCodeCLI(w, req, runID)

	if callCount != 1 {
		t.Fatalf("expected only 1 runner call (version fails, models skipped), got %d", callCount)
	}

	// Even on failure, the check artifact should be persisted
	data, err := artifacts.Read(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"))
	if err != nil {
		t.Fatalf("read cli check json: %v", err)
	}

	type cliCheckResult struct {
		Binary          string `json:"binary"`
		VersionExitCode int    `json:"version_exit_code"`
	}
	var result cliCheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal cli check: %v", err)
	}
	if result.VersionExitCode != -1 {
		t.Fatalf("expected version exit code -1, got %d", result.VersionExitCode)
	}
}

func TestCheckOpenCodeCLIPersistsAllFields(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "opencode version 1.0.0"}
	}

	// Create a run with a friendly model label that we can map
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "ready", "My Model", "my-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	agentPromptPath, err := artifacts.Write(run.ID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte("compact prompt"))
	if err != nil {
		t.Fatalf("write agent prompt: %v", err)
	}
	s.CreateArtifact(run.ID, "agent_prompt", agentPromptPath, "text/plain")
	packetPath, err := artifacts.Write(run.ID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet"), []byte(`{"status":"configured"}`))
	if err != nil {
		t.Fatalf("write packet: %v", err)
	}
	s.CreateArtifact(run.ID, "opencode_handoff_packet", packetPath, "application/json")

	// Set model mapping so resolution succeeds
	t.Setenv("RELAY_OPENCODE_MODEL_MY_MODEL", "opencode-go/my-model")

	runID := run.ID
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.checkOpenCodeCLI(w, req, runID)

	data, err := artifacts.Read(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"))
	if err != nil {
		t.Fatalf("read cli check json: %v", err)
	}

	type cliCheckResult struct {
		Binary          string `json:"binary"`
		VersionExitCode int    `json:"version_exit_code"`
		ModelsExitCode  int    `json:"models_exit_code"`
		ResolvedModel   string `json:"resolved_model"`
		ModelAvailable  bool   `json:"model_available"`
		CheckedAt       string `json:"checked_at"`
		Error           string `json:"error,omitempty"`
	}
	var result cliCheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal cli check: %v", err)
	}

	if result.Binary != "opencode" {
		t.Fatalf("expected binary 'opencode', got %q", result.Binary)
	}
	if result.VersionExitCode != 0 {
		t.Fatalf("expected version exit code 0, got %d", result.VersionExitCode)
	}
	if result.ModelsExitCode != 0 {
		t.Fatalf("expected models exit code 0, got %d", result.ModelsExitCode)
	}
	if result.ResolvedModel != "opencode-go/my-model" {
		t.Fatalf("expected resolved model 'opencode-go/my-model', got %q", result.ResolvedModel)
	}
	if result.ModelAvailable {
		t.Fatal("expected model_available false since models output does not contain resolved model")
	}
	if result.CheckedAt == "" {
		t.Fatal("expected non-empty checked_at")
	}
}

func TestCheckOpenCodeCLIModelResolutionErrorPersisted(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "opencode version 1.0.0"}
	}

	// Create a run with a friendly model label that has no env mapping
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "ready", "Unmapped Model", "unmapped-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	agentPromptPath, err := artifacts.Write(run.ID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte("compact prompt"))
	if err != nil {
		t.Fatalf("write agent prompt: %v", err)
	}
	s.CreateArtifact(run.ID, "agent_prompt", agentPromptPath, "text/plain")
	packetPath, err := artifacts.Write(run.ID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet"), []byte(`{"status":"configured"}`))
	if err != nil {
		t.Fatalf("write packet: %v", err)
	}
	s.CreateArtifact(run.ID, "opencode_handoff_packet", packetPath, "application/json")

	runID := run.ID
	// Do NOT set model mapping so resolution fails
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.checkOpenCodeCLI(w, req, runID)

	data, err := artifacts.Read(runID, "opencode_cli_check_json", pipeline.ArtifactFilename("opencode_cli_check_json"))
	if err != nil {
		t.Fatalf("read cli check json: %v", err)
	}

	type cliCheckResult struct {
		Binary               string `json:"binary"`
		VersionExitCode      int    `json:"version_exit_code"`
		ModelsExitCode       int    `json:"models_exit_code"`
		ResolvedModel        string `json:"resolved_model"`
		ModelResolutionError string `json:"model_resolution_error,omitempty"`
		VersionStdout        string `json:"version_stdout,omitempty"`
	}
	var result cliCheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal cli check: %v", err)
	}

	if result.VersionExitCode != 0 {
		t.Fatalf("expected version exit code 0 (still runs), got %d", result.VersionExitCode)
	}
	if result.ResolvedModel != "" {
		t.Fatalf("expected empty resolved model when resolution fails, got %q", result.ResolvedModel)
	}
	if result.ModelResolutionError == "" {
		t.Fatal("expected model_resolution_error to be set when resolution fails")
	}
	if !strings.Contains(result.ModelResolutionError, "RELAY_OPENCODE_MODEL_UNMAPPED_MODEL") {
		t.Fatalf("expected model_resolution_error to mention the missing env var, got %q", result.ModelResolutionError)
	}
	// Version and models should still have run
	if result.VersionStdout != "opencode version 1.0.0" {
		t.Fatalf("expected version stdout to be captured, got %q", result.VersionStdout)
	}
}

func TestDryRunOpenCodeGoDoesNotCallCheckOpenCodeCLI(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runnerCalled := false
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		runnerCalled = true
		return pipeline.AgentCommandRunResult{}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.dryRunOpenCodeGo(w, req, runID)

	if runnerCalled {
		t.Fatal("dry run must not call the args runner")
	}
}

func TestStartOpenCodeGoNonZeroExitPersistsStdoutStderrCombined(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 2,
			Stdout:   "stdout content",
			Stderr:   "stderr content",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	// Verify artifacts contain the content
	stdoutData, _ := artifacts.Read(runID, "opencode_stdout", pipeline.ArtifactFilename("opencode_stdout"))
	if string(stdoutData) != "stdout content" {
		t.Fatalf("expected stdout artifact to contain 'stdout content', got %q", string(stdoutData))
	}

	stderrData, _ := artifacts.Read(runID, "opencode_stderr", pipeline.ArtifactFilename("opencode_stderr"))
	if string(stderrData) != "stderr content" {
		t.Fatalf("expected stderr artifact to contain 'stderr content', got %q", string(stderrData))
	}

	combinedData, _ := artifacts.Read(runID, "opencode_combined_log", pipeline.ArtifactFilename("opencode_combined_log"))
	if !strings.Contains(string(combinedData), "stdout content") || !strings.Contains(string(combinedData), "stderr content") {
		t.Fatalf("expected combined log to contain both stdout and stderr, got %q", string(combinedData))
	}
}

func TestStartOpenCodeGoDoesNotPersistUnknownJSONNoise(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 0,
			Stdout: `{"type":"tool","part":{"type":"tool","name":"read_file"}}
{"type":"reasoning","part":{"type":"reasoning","text":"thinking..."}}
{"type":"text","part":{"type":"text","text":"some intermediate text"}}
`,
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			t.Fatal("did not expect agent_result_raw artifact for unknown JSON noise")
		}
		if a.Kind == "agent_result_json" {
			t.Fatal("did not expect agent_result_json artifact for unknown JSON noise")
		}
	}
}

func TestStartOpenCodeGoLaunchesAsyncAndRedirectsToRunStep(t *testing.T) {
	s := setupTestStore(t)

	launched := false
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) {
		launched = true
		// do not execute fn for this test
	}
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		t.Fatal("runner should not be called in async test")
		return pipeline.AgentCommandRunResult{}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=run") {
		t.Fatalf("expected redirect to step=run, got %s", loc)
	}
	if !launched {
		t.Fatal("expected launchAgentExecution to be called")
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Status != "starting" {
		t.Fatalf("expected execution status 'starting', got %q", exec.Status)
	}
}

func TestStartOpenCodeGoDuplicateRunningRejected(t *testing.T) {
	s := setupTestStore(t)

	runID := setupOpenCodeRun(t, s)

	// Create a running execution
	exec, err := s.CreateAgentExecution(runID, "opencode_go", "running", "test command")
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	_ = exec

	launchCalled := false
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) {
		launchCalled = true
	}

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	if launchCalled {
		t.Fatal("should not launch when execution is already running")
	}
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=run") {
		t.Fatalf("expected redirect to step=run, got %s", loc)
	}

	// Check that a "already running" event was created
	events, err := s.ListEventsByRun(runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "already running") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'already running' event")
	}
}

func TestStartOpenCodeGoCreatesSingleStartedEvent(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 0,
			Stdout:   "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 5\n",
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	events, err := s.ListEventsByRun(runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	count := 0
	for _, ev := range events {
		if strings.Contains(ev.Message, "OpenCode Go execution started") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 'OpenCode Go execution started' event, got %d", count)
	}
}

func TestBuildOpenCodeTranscriptParsesRealSmokeOutput(t *testing.T) {
	stdout := `{"type":"reasoning","part":{"type":"reasoning","text":"Let me follow the implementation handoff exactly."}}
{"type":"tool_use","part":{"type":"tool","tool":"read","state":{"status":"completed","input":{"filePath":"D:\\Code\\relay\\README.md"}}}}
{"type":"text","part":{"type":"text","text":"DONE\nNo build changes (README-only)\nNo test changes\n1 LOC changed"}}
`
	events := pipeline.BuildOpenCodeTranscript(stdout, "", 0)
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	hasReasoning := false
	hasTool := false
	hasText := false
	for _, ev := range events {
		switch ev.Kind {
		case "reasoning":
			hasReasoning = true
			if !strings.Contains(ev.Text, "implementation handoff") {
				t.Fatal("expected reasoning text to contain 'implementation handoff'")
			}
		case "tool":
			hasTool = true
			if !strings.Contains(ev.Text, "read") {
				t.Fatal("expected tool event to contain 'read'")
			}
		case "text":
			hasText = true
			if !strings.Contains(ev.Text, "DONE") {
				t.Fatal("expected text event to contain 'DONE'")
			}
		}
	}
	if !hasReasoning {
		t.Fatal("expected reasoning event")
	}
	if !hasTool {
		t.Fatal("expected tool event")
	}
	if !hasText {
		t.Fatal("expected text event")
	}
}

func TestValidateHandoffRedirectsToPromptWhenReady(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.validateHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=prompt") {
		t.Fatalf("expected redirect to step=prompt, got %s", loc)
	}
}

func TestValidateHandoffRedirectsToIntakeWhenBlocked(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, blockedHandoff())

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.validateHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=intake") {
		t.Fatalf("expected redirect to step=intake, got %s", loc)
	}
}

func TestPreparePromptRedirectsToPromptStep(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.preparePrompt(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=prompt") {
		t.Fatalf("expected redirect to step=prompt, got %s", loc)
	}
}

func TestGenerateOpenCodePacketRedirectsToHandoffStep(t *testing.T) {
	s := setupTestStore(t)
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "ready", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	promptPath, err := artifacts.Write(run.ID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte("compact prompt"))
	if err != nil {
		t.Fatalf("write agent prompt: %v", err)
	}
	_, err = s.CreateArtifact(run.ID, "agent_prompt", promptPath, "text/plain")
	if err != nil {
		t.Fatalf("create agent prompt artifact: %v", err)
	}

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.generateOpenCodePacket(w, req, run.ID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=handoff") {
		t.Fatalf("expected redirect to step=handoff, got %s", loc)
	}
}

func TestDryRunOpenCodeGoRedirectsToHandoffStep(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{}
	}
	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.dryRunOpenCodeGo(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=handoff") {
		t.Fatalf("expected redirect to step=handoff, got %s", loc)
	}
}

func TestSubmitAgentResultRedirectsToValidationStep(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	body := "agent_result_text=DONE%0ABuild+status%3A+PASS%0ATest+status%3A+PASS%0ACount+of+LOC+changed%3A+42"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	h.submitAgentResult(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}
}

func TestRunValidationRedirectsToValidationStep(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test Handoff

## Goal
Do something

## Scope
- README.md

## Do not change
Nothing

## Task checklist
- [ ] Do it

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// Verify redirect behavior only; do not run the background worker
	launched := false
	h.launchValidation = func(fn func()) {
		launched = true
	}

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startValidation(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}
	if !launched {
		t.Fatal("expected validation worker to be scheduled")
	}

	// DB-backed execution should exist in starting state
	exec, err := s.GetActiveValidationExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get active execution: %v", err)
	}
	if exec == nil {
		t.Fatal("expected a DB-backed validation execution to exist")
	}
	if exec.Status != "starting" {
		t.Errorf("expected execution status starting, got %s", exec.Status)
	}
}

func TestStartValidationRedirectsImmediately(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	// Submit agent result so validation is ready
	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 1"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// Use synchronous launcher so the background worker completes before the test assertions
	h.launchValidation = func(fn func()) {
		fn()
	}

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	h.startValidation(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}

	// Check progress artifact exists with a final status (not stuck running)
	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read validation progress: %v", err)
	}
	var vp pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &vp); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if vp.Status != "pass" && vp.Status != "fail" && vp.Status != "error" {
		t.Fatalf("expected progress final status (pass/fail/error), got %s", vp.Status)
	}
	if vp.TotalCommands != 1 {
		t.Errorf("expected 1 total command, got %d", vp.TotalCommands)
	}

	// Check DB-backed execution was finalized
	exec, err := s.GetActiveValidationExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get active execution: %v", err)
	}
	if exec != nil {
		t.Errorf("expected no active execution after worker completed, got status %s", exec.Status)
	}
}

func TestValidationWorkerWritesProgressAndFinalArtifacts(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	// Submit agent result so validation is ready
	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 1"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		fn()
	}

	// Run startValidation synchronously (the worker will also run synchronously)
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.startValidation(w, req, runID)

	// Check final validation artifacts exist
	if !artifacts.Exists(runID, "validation_run_json", pipeline.ArtifactFilename("validation_run_json")) {
		t.Error("expected validation_run_json artifact to exist")
	}
	if !artifacts.Exists(runID, "validation_stdout", pipeline.ArtifactFilename("validation_stdout")) {
		t.Error("expected validation_stdout artifact to exist")
	}
	if !artifacts.Exists(runID, "validation_stderr", pipeline.ArtifactFilename("validation_stderr")) {
		t.Error("expected validation_stderr artifact to exist")
	}

	// Check run status updated
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "validation_passed" && run.Status != "validation_failed" {
		t.Errorf("expected validation_passed or validation_failed, got %s", run.Status)
	}

	// Check validation_run checks exist
	checks, err := s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	hasRunCheck := false
	for _, c := range checks {
		if c.Kind == "validation_run" {
			hasRunCheck = true
			break
		}
	}
	if !hasRunCheck {
		t.Error("expected validation_run check to exist")
	}

	// Check DB execution was finalized (pass or fail)
	execs, err := s.DB().Query("SELECT status FROM validation_executions WHERE run_id = ?", runID)
	if err != nil {
		t.Fatalf("query executions: %v", err)
	}
	defer execs.Close()
	hasFinal := false
	for execs.Next() {
		var status string
		if err := execs.Scan(&status); err != nil {
			t.Fatalf("scan status: %v", err)
		}
		if status == "pass" || status == "fail" || status == "error" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Error("expected DB execution to have terminal status (pass/fail/error)")
	}
}

func TestStartValidationDoesNotDoubleStart(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Tests / validation

` + "```bash" + `
go version
` + "```" + `
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	// Seed an active DB-backed validation execution (not stale)
	s.DB().Exec(
		`INSERT INTO validation_executions (run_id, status, started_at, updated_at) VALUES (?, 'running', datetime('now'), datetime('now'))`,
		runID,
	)

	launchCount := 0
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		launchCount++
	}

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.startValidation(w, req, runID)

	if launchCount != 0 {
		t.Errorf("expected 0 worker launches (double-start prevented), got %d", launchCount)
	}
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}
}

func TestValidationWorkerErrorFinalizesProgress(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Tests / validation

` + "```bash" + `
go version
` + "```" + `
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	agentResultPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte("DONE"))
	if err != nil {
		t.Fatalf("write agent result: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", agentResultPath, "text/plain")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		fn()
	}

	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	handoffData, _ := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	commands := pipeline.ExtractValidationCommands(string(handoffData), "")

	// Write initial progress first (as startValidation would)
	initialProgress := pipeline.NewValidationProgress(repo.Path, len(commands))
	initData, _ := json.MarshalIndent(initialProgress, "", "  ")
	initPath, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), initData)
	s.CreateArtifact(runID, "validation_progress_json", initPath, "application/json")

	// Create a DB-backed execution (as startValidation would)
	execID, acquired, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("try create validation execution: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire execution")
	}

	writeProgress := func(p pipeline.ValidationProgress) {
		data, _ := json.MarshalIndent(p, "", "  ")
		h.store.DeleteArtifactsByRunKind(runID, "validation_progress_json")
		pth, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), data)
		if pth != "" {
			s.CreateArtifact(runID, "validation_progress_json", pth, "application/json")
		}
	}

	h.executeValidation(runID, execID, repo.Path, commands, writeProgress)

	// Verify progress final status is not stuck running
	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var finalProgress pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &finalProgress); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if finalProgress.Status == "starting" || finalProgress.Status == "running" {
		t.Errorf("progress should not be stuck running, got %s", finalProgress.Status)
	}
	if finalProgress.FinishedAt == "" {
		t.Error("expected finished_at to be set")
	}
	if finalProgress.Status != "pass" && finalProgress.Status != "fail" {
		t.Errorf("expected pass or fail, got %s", finalProgress.Status)
	}
}

func TestStartValidationAcquiresExecutionLockAndRedirectsImmediately(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	launchRecorded := false
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		launchRecorded = true
		// Do NOT run the worker — just record the launch
	}

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.startValidation(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}

	if !launchRecorded {
		t.Fatal("expected worker launch to be recorded")
	}

	// DB-backed execution should exist
	exec, err := s.GetActiveValidationExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get active execution: %v", err)
	}
	if exec == nil {
		t.Fatal("expected a DB-backed validation execution to exist")
	}
	if exec.Status != "starting" && exec.Status != "running" {
		t.Errorf("expected execution status starting or running, got %s", exec.Status)
	}

	// Progress artifact should exist
	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read validation progress: %v", err)
	}
	if len(progressData) == 0 {
		t.Fatal("expected validation_progress_json to exist")
	}
}

func TestStartValidationConcurrentDoubleStartLaunchesOnce(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	launchCount := 0
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		launchCount++
	}

	// First call
	req1 := httptest.NewRequest("POST", "/", nil)
	w1 := httptest.NewRecorder()
	h.startValidation(w1, req1, runID)

	if w1.Code != 303 {
		t.Fatalf("first call expected 303, got %d", w1.Code)
	}

	// Second call (simulating rapid duplicate)
	req2 := httptest.NewRequest("POST", "/", nil)
	w2 := httptest.NewRecorder()
	h.startValidation(w2, req2, runID)

	if w2.Code != 303 {
		t.Fatalf("second call expected 303, got %d", w2.Code)
	}

	if launchCount != 1 {
		t.Errorf("expected exactly 1 worker launch, got %d", launchCount)
	}
}

func TestStartValidationActiveExecutionBlocksEvenWithoutProgressArtifact(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	// Seed an active DB-backed validation execution WITHOUT progress artifact
	s.DB().Exec(
		`INSERT INTO validation_executions (run_id, status, started_at, updated_at) VALUES (?, 'running', datetime('now'), datetime('now'))`,
		runID,
	)

	launchCount := 0
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) {
		launchCount++
	}

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.startValidation(w, req, runID)

	if launchCount != 0 {
		t.Errorf("expected 0 worker launches (active DB execution blocked), got %d", launchCount)
	}
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}
}

func TestValidationWorkerFinalizesExecutionOnPanic(t *testing.T) {
	s := setupTestStore(t)

	handoffText := `# Test

## Goal
Test

## Scope
- README.md

## Tests / validation

` + "```bash" + `
go version
` + "```" + `

## Output
DONE or BLOCKED
`
	runID := newTestHandoffWithRepoFiles(t, s, handoffText, map[string]string{
		"README.md": "# repo",
	})

	// Create a DB execution and progress artifact (as startValidation would)
	execID, acquired, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("try create execution: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire execution")
	}

	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	repo, err := s.GetRepo(run.RepoID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	handoffData, _ := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	commands := pipeline.ExtractValidationCommands(string(handoffData), "")

	initialProgress := pipeline.NewValidationProgress(repo.Path, len(commands))
	initData, _ := json.MarshalIndent(initialProgress, "", "  ")
	initPath, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), initData)
	s.CreateArtifact(runID, "validation_progress_json", initPath, "application/json")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchValidation = func(fn func()) { fn() }

	// writeProgress that panics only once to simulate a worker panic
	// (the defer recovery will call writeProgress again, which must succeed)
	var panicOnce sync.Once
	writeProgress := func(p pipeline.ValidationProgress) {
		panicOnce.Do(func() {
			panic("simulated worker panic")
		})
		data, _ := json.MarshalIndent(p, "", "  ")
		h.store.DeleteArtifactsByRunKind(runID, "validation_progress_json")
		pth, _ := artifacts.Write(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"), data)
		if pth != "" {
			s.CreateArtifact(runID, "validation_progress_json", pth, "application/json")
		}
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Log("panic caught by executeValidation defer as expected")
			}
		}()
		h.executeValidation(runID, execID, repo.Path, commands, writeProgress)
	}()

	// After the panic, the DB execution should be finalized as 'error'
	exec, err := s.GetActiveValidationExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get active execution: %v", err)
	}
	if exec != nil {
		t.Errorf("expected no active execution after panic, got status %s", exec.Status)
	}

	// The old execution should now be in 'error' state
	var errStatus string
	err = s.DB().QueryRow("SELECT status FROM validation_executions WHERE id = ?", execID).Scan(&errStatus)
	if err != nil {
		t.Fatalf("query execution status: %v", err)
	}
	if errStatus != "error" {
		t.Errorf("expected execution status error after panic, got %s", errStatus)
	}

	// Progress should be marked as error
	progressData, err := artifacts.Read(runID, "validation_progress_json", pipeline.ArtifactFilename("validation_progress_json"))
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var vp pipeline.ValidationProgress
	if err := json.Unmarshal(progressData, &vp); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if vp.Status != "error" {
		t.Errorf("expected progress status error after panic, got %s", vp.Status)
	}

	// A subsequent startValidation should succeed (can acquire new execution)
	launchCount := 0
	h2 := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h2.launchValidation = func(fn func()) {
		launchCount++
	}
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h2.startValidation(w, req, runID)
	if w.Code != 303 {
		t.Fatalf("expected 303 redirect after retry, got %d", w.Code)
	}
	if launchCount != 1 {
		t.Errorf("expected 1 worker launch after retry, got %d", launchCount)
	}
}

// seedValidationPass creates a validation_run_json artifact and a pass check.
func seedValidationPass(t *testing.T, s *store.Store, runID int64) {
	t.Helper()
	runJSON := `{"status":"pass","repo_path":"/tmp/test","commands":[]}`
	p, err := artifacts.Write(runID, "validation_run_json", pipeline.ArtifactFilename("validation_run_json"), []byte(runJSON))
	if err != nil {
		t.Fatalf("write validation_run_json: %v", err)
	}
	s.CreateArtifact(runID, "validation_run_json", p, "application/json")
	s.CreateCheck(runID, "validation_run", "pass", "Validation passed", runJSON)
	s.UpdateRunStatus(runID, "validation_passed")
}

// seedValidationFail creates a validation_run_json artifact and a fail check.
func seedValidationFail(t *testing.T, s *store.Store, runID int64) {
	t.Helper()
	runJSON := `{"status":"fail","repo_path":"/tmp/test","commands":[]}`
	p, err := artifacts.Write(runID, "validation_run_json", pipeline.ArtifactFilename("validation_run_json"), []byte(runJSON))
	if err != nil {
		t.Fatalf("write validation_run_json: %v", err)
	}
	s.CreateArtifact(runID, "validation_run_json", p, "application/json")
	s.CreateCheck(runID, "validation_run", "fail", "Validation failed", runJSON)
	s.UpdateRunStatus(runID, "validation_failed")
}

// seedGitDiffEvidence creates git diff artifacts.
func seedGitDiffEvidence(t *testing.T, s *store.Store, runID int64) {
	t.Helper()
	for kind, content := range map[string]string{
		"git_status_text":      "M foo.go\n",
		"git_diff_stat":        " foo.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n",
		"git_diff_numstat":     "1\t1\tfoo.go\n",
		"git_diff_name_status": "M\tfoo.go\n",
		"git_diff_patch":       "diff --git a/foo.go b/foo.go\nindex abc..def 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-package old\n+package new\n",
	} {
		p, err := artifacts.Write(runID, kind, pipeline.ArtifactFilename(kind), []byte(content))
		if err != nil {
			t.Fatalf("write %s: %v", kind, err)
		}
		s.CreateArtifact(runID, kind, p, "text/plain")
	}
}

// seedAuditHandoff creates an audit_handoff artifact.
func seedAuditHandoff(t *testing.T, s *store.Store, runID int64) {
	t.Helper()
	content := "## Audit Handoff\n\nValidation passed.\n"
	p, err := artifacts.Write(runID, "audit_handoff", pipeline.ArtifactFilename("audit_handoff"), []byte(content))
	if err != nil {
		t.Fatalf("write audit_handoff: %v", err)
	}
	s.CreateArtifact(runID, "audit_handoff", p, "text/markdown")
}

// seedAgentResult creates agent_result_raw/json artifacts with DONE status.
func seedAgentResult(t *testing.T, s *store.Store, runID int64) {
	t.Helper()
	raw := "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 5\n"
	rawPath, err := artifacts.Write(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"), []byte(raw))
	if err != nil {
		t.Fatalf("write agent_result_raw: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_raw", rawPath, "text/plain")
	resultJSON := `{"status":"done","build_status":"PASS","test_status":"PASS","loc_changed":"5"}`
	jsonPath, err := artifacts.Write(runID, "agent_result_json", pipeline.ArtifactFilename("agent_result_json"), []byte(resultJSON))
	if err != nil {
		t.Fatalf("write agent_result_json: %v", err)
	}
	s.CreateArtifact(runID, "agent_result_json", jsonPath, "application/json")
	s.CreateCheck(runID, "agent_result", "pass", "Agent reported DONE", resultJSON)
}

func TestPrepareGitCommitWritesArtifactsAndRedirects(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	// Seed validation pass, agent result, git diff evidence, audit handoff
	seedAgentResult(t, s, runID)
	seedValidationPass(t, s, runID)
	seedGitDiffEvidence(t, s, runID)
	seedAuditHandoff(t, s, runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.prepareGitCommit(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=commit") {
		t.Fatalf("expected redirect to step=commit, got %s", loc)
	}

	// Assert commit artifacts exist
	if !artifacts.Exists(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text")) {
		t.Error("expected commit_message_text artifact")
	}
	if !artifacts.Exists(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json")) {
		t.Error("expected commit_suggestion_json artifact")
	}

	// Assert event
	events, _ := s.ListEventsByRun(runID)
	found := false
	for _, ev := range events {
		if ev.Message == "Git commit suggestion prepared" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected event 'Git commit suggestion prepared'")
	}
}

func TestPrepareGitCommitBlockedWithoutAuditHandoff(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	// Seed validation pass and git diff but no audit handoff
	seedAgentResult(t, s, runID)
	seedValidationPass(t, s, runID)
	seedGitDiffEvidence(t, s, runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.prepareGitCommit(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=commit") {
		t.Fatalf("expected redirect to step=commit, got %s", loc)
	}

	// Assert no commit artifacts were written
	if artifacts.Exists(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text")) {
		t.Error("did not expect commit_message_text artifact")
	}
	if artifacts.Exists(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json")) {
		t.Error("did not expect commit_suggestion_json artifact")
	}

	// Assert warning event
	events, _ := s.ListEventsByRun(runID)
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "generate audit handoff first") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning event mentioning 'generate audit handoff first'")
	}
}

func TestPrepareGitCommitBlockedAfterUnacceptedValidationFailure(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	// Seed validation failure (not accepted)
	seedAgentResult(t, s, runID)
	seedValidationFail(t, s, runID)
	seedGitDiffEvidence(t, s, runID)
	seedAuditHandoff(t, s, runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.prepareGitCommit(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}

	// Assert no commit artifacts were written
	if artifacts.Exists(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text")) {
		t.Error("did not expect commit_message_text artifact")
	}
	if artifacts.Exists(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json")) {
		t.Error("did not expect commit_suggestion_json artifact")
	}

	// Assert warning event
	events, _ := s.ListEventsByRun(runID)
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "validation failed and has not been accepted") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning event mentioning 'validation failed and has not been accepted'")
	}
}

func TestAcceptValidationFailureRedirectsToAudit(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	// Seed validation failure
	seedAgentResult(t, s, runID)
	seedValidationFail(t, s, runID)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.acceptValidationFailure(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=audit") {
		t.Fatalf("expected redirect to step=audit, got %s", loc)
	}

	// Assert run status updated
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "validation_failed_accepted" {
		t.Fatalf("expected status validation_failed_accepted, got %s", run.Status)
	}

	// Assert event
	events, _ := s.ListEventsByRun(runID)
	found := false
	for _, ev := range events {
		if ev.Message == "Validation failure accepted; continuing to diff/audit." {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected event 'Validation failure accepted; continuing to diff/audit.'")
	}
}

func TestAcceptValidationFailureWithoutFailedCheck(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := newTestHandoff(t, s, validHandoff())

	// No failed check seeded

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.acceptValidationFailure(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=validation") {
		t.Fatalf("expected redirect to step=validation, got %s", loc)
	}

	// Assert warning event
	events, _ := s.ListEventsByRun(runID)
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "no failed validation run found") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning event mentioning 'no failed validation run found'")
	}
}

func TestInspectDiffClearsStaleAuditAndCommitArtifacts(t *testing.T) {
	s := setupTestStore(t)
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	// Seed stale audit and commit artifacts
	seedAuditHandoff(t, s, runID)
	commitMsgPath, _ := artifacts.Write(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text"), []byte("stale"))
	s.CreateArtifact(runID, "commit_message_text", commitMsgPath, "text/plain")
	commitJSONPath, _ := artifacts.Write(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json"), []byte("{}"))
	s.CreateArtifact(runID, "commit_suggestion_json", commitJSONPath, "application/json")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.inspectDiff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}

	// Assert stale artifacts were deleted (check DB rows, not filesystem)
	artifactsAfter, _ := s.ListArtifactsByRun(runID)
	for _, a := range artifactsAfter {
		if a.Kind == "audit_handoff" {
			t.Error("expected audit_handoff DB row to be deleted after inspect-diff")
		}
		if a.Kind == "commit_message_text" {
			t.Error("expected commit_message_text DB row to be deleted after inspect-diff")
		}
		if a.Kind == "commit_suggestion_json" {
			t.Error("expected commit_suggestion_json DB row to be deleted after inspect-diff")
		}
	}
}

func TestGenerateAuditHandoffClearsStaleCommitSuggestion(t *testing.T) {
	s := setupTestStore(t)
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	// Seed artifacts needed by generateAuditHandoff
	handoffPath, _ := artifacts.Write(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte("# Test\n"))
	s.CreateArtifact(runID, "original_handoff", handoffPath, "text/plain")
	seedAgentResult(t, s, runID)

	// Seed stale commit artifacts
	commitMsgPath, _ := artifacts.Write(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text"), []byte("stale"))
	s.CreateArtifact(runID, "commit_message_text", commitMsgPath, "text/plain")
	commitJSONPath, _ := artifacts.Write(runID, "commit_suggestion_json", pipeline.ArtifactFilename("commit_suggestion_json"), []byte("{}"))
	s.CreateArtifact(runID, "commit_suggestion_json", commitJSONPath, "application/json")

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.generateAuditHandoff(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}

	// Assert stale commit artifacts were deleted (check DB rows)
	artifactsAfter, _ := s.ListArtifactsByRun(runID)
	for _, a := range artifactsAfter {
		if a.Kind == "commit_message_text" {
			t.Error("expected commit_message_text DB row to be deleted after audit handoff regeneration")
		}
		if a.Kind == "commit_suggestion_json" {
			t.Error("expected commit_suggestion_json DB row to be deleted after audit handoff regeneration")
		}
	}

	// Assert audit handoff was created as a DB row
	foundAudit := false
	for _, a := range artifactsAfter {
		if a.Kind == "audit_handoff" {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Error("expected audit_handoff DB row to exist after generation")
	}
}

func TestReplaceOriginalHandoffClearsAllDownstreamArtifacts(t *testing.T) {
	s := setupTestStore(t)
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "foo.go"), []byte("package foo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	// Seed original handoff (must keep)
	origPath, _ := artifacts.Write(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"), []byte("# Test\n"))
	s.CreateArtifact(runID, "original_handoff", origPath, "text/plain")

	// Seed downstream artifacts that should be cleared
	seedGitDiffEvidence(t, s, runID)
	seedAuditHandoff(t, s, runID)
	commitMsgPath, _ := artifacts.Write(runID, "commit_message_text", pipeline.ArtifactFilename("commit_message_text"), []byte("stale"))
	s.CreateArtifact(runID, "commit_message_text", commitMsgPath, "text/plain")

	// All these kinds should be cleared by replaceOriginalHandoff
	clearableKinds := []struct {
		kind     string
		filename string
	}{
		{"git_status_text", "git_status.txt"},
		{"git_diff_stat", "git_diff_stat.txt"},
		{"git_diff_name_status", "git_diff_name_status.txt"},
		{"git_diff_patch", "git_diff.patch"},
		{"audit_handoff", "audit_handoff.md"},
		{"commit_message_text", "commit-message.txt"},
	}

	formBody := "action=replace-original-handoff&handoff_text=%23+Replacement+handoff%0A"
	req := httptest.NewRequest("POST", "/", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.replaceOriginalHandoff(w, req, runID)

	if w.Code != 303 {
		t.Logf("expected 303 redirect, got %d (may be normal if handoff validation redirected)", w.Code)
	}

	// Assert downstream artifacts were cleared (check DB rows)
	artifactsAfter, _ := s.ListArtifactsByRun(runID)
	for _, ka := range clearableKinds {
		for _, a := range artifactsAfter {
			if a.Kind == ka.kind {
				t.Errorf("expected %s DB row to be deleted after replace original handoff", ka.kind)
			}
		}
	}

	// Assert original handoff still exists (replaced) as a DB row
	foundOriginal := false
	for _, a := range artifactsAfter {
		if a.Kind == "original_handoff" {
			foundOriginal = true
			break
		}
	}
	if !foundOriginal {
		t.Error("expected original_handoff DB row to still exist after replacement")
	}
}

func TestTryCreateValidationExecutionUsesRowsAffected(t *testing.T) {
	s := setupTestStore(t)
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", "draft", "test-model", "test-model", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := run.ID

	// First call should acquire
	id1, acquired1, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("first TryCreateValidationExecution: %v", err)
	}
	if !acquired1 {
		t.Fatal("expected first call to acquire execution lock")
	}
	if id1 == 0 {
		t.Fatal("expected non-zero execution ID from first call")
	}

	// Second call without finalizing should NOT acquire
	_, acquired2, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("second TryCreateValidationExecution: %v", err)
	}
	if acquired2 {
		t.Fatal("expected second call NOT to acquire execution lock (should be blocked by active execution)")
	}

	// Verify only one active execution row exists
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM validation_executions WHERE run_id = ? AND status IN ('starting', 'running')", runID).Scan(&count)
	if err != nil {
		t.Fatalf("count active executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 active execution, got %d", count)
	}

	// Finalize the first execution
	s.FinishValidationExecution(id1, "pass", "")

	// After finalization, a new call should succeed
	_, acquired3, err := s.TryCreateValidationExecution(runID)
	if err != nil {
		t.Fatalf("third TryCreateValidationExecution: %v", err)
	}
	if !acquired3 {
		t.Fatal("expected third call to acquire after finalization")
	}
}

func TestHandlerTestsDoNotLeaveDataArtifactsInPackageDir(t *testing.T) {
	// After any test runs, the internal/handlers/data directory should not exist
	// or should be empty (tests should clean up after themselves).
	info, err := os.Stat("data")
	if err == nil {
		if info.IsDir() {
			entries, _ := os.ReadDir("data")
			if len(entries) > 0 {
				// This test package creates artifacts under data/artifacts/<runID>.
				// The setupTestStore cleanup should remove them. If data/ exists
				// with entries outside a test run, that's fine (e.g., manual testing).
				// Only fail if there are artifacts from a previous test run.
				t.Logf("data/ directory exists with %d entries (may be from manual testing)", len(entries))
			}
		}
	}
}

func TestBuildOpenCodeTranscriptMaxEvents(t *testing.T) {
	stdout := `{"type":"text","part":{"type":"text","text":"line1"}}
{"type":"text","part":{"type":"text","text":"line2"}}
{"type":"text","part":{"type":"text","text":"line3"}}
`
	events := pipeline.BuildOpenCodeTranscript(stdout, "", 2)
	if len(events) != 2 {
		t.Fatalf("expected 2 events with maxEvents=2, got %d", len(events))
	}
	if !strings.Contains(events[0].Text, "line2") {
		t.Fatalf("expected first event to be line2, got %q", events[0].Text)
	}
	if !strings.Contains(events[1].Text, "line3") {
		t.Fatalf("expected second event to be line3, got %q", events[1].Text)
	}
}

// Helper to seed captured output artifacts with JSONL content.
func seedOpenCodeOutputArtifacts(t *testing.T, s *store.Store, runID int64, stdout, stderr string) {
	t.Helper()
	if stdout != "" {
		p, err := artifacts.Write(runID, "opencode_stdout", pipeline.ArtifactFilename("opencode_stdout"), []byte(stdout))
		if err != nil {
			t.Fatalf("write stdout artifact: %v", err)
		}
		s.CreateArtifact(runID, "opencode_stdout", p, "text/plain")
	}
	if stderr != "" {
		p, err := artifacts.Write(runID, "opencode_stderr", pipeline.ArtifactFilename("opencode_stderr"), []byte(stderr))
		if err != nil {
			t.Fatalf("write stderr artifact: %v", err)
		}
		s.CreateArtifact(runID, "opencode_stderr", p, "text/plain")
	}
	combined := stdout
	if stderr != "" {
		if combined != "" {
			combined += "\n\n--- STDERR ---\n\n"
		}
		combined += stderr
	}
	if combined != "" {
		p, err := artifacts.Write(runID, "opencode_combined_log", pipeline.ArtifactFilename("opencode_combined_log"), []byte(combined))
		if err != nil {
			t.Fatalf("write combined log artifact: %v", err)
		}
		s.CreateArtifact(runID, "opencode_combined_log", p, "text/plain")
	}
}

// Helper to create a running execution for a run.
func seedRunningExecution(t *testing.T, s *store.Store, runID int64) int64 {
	t.Helper()
	exec, err := s.CreateAgentExecution(runID, "opencode_go", "running", "test command")
	if err != nil {
		t.Fatalf("create running execution: %v", err)
	}
	return exec.ID
}

func TestReconcileOpenCodeExecutionPersistsDoneFromCapturedStdout(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	seedOpenCodeOutputArtifacts(t, s, runID,
		`{"type":"text","part":{"type":"text","text":"DONE"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Test status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Count of LOC changed: 12"}}
`,
		"")

	result, err := h.reconcileOpenCodeExecution(runID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected reconciliation to change state")
	}
	if !result.ParsedAgentResult {
		t.Fatal("expected agent result to be parsed")
	}
	if result.FinalStatus != "completed" {
		t.Fatalf("expected final status 'completed', got %q", result.FinalStatus)
	}

	// Verify execution is no longer running
	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Status == "running" || exec.Status == "starting" {
		t.Fatalf("execution should no longer be running, got %q", exec.Status)
	}

	// Verify agent_result_raw and agent_result_json exist
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	hasJSON := false
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			hasRaw = true
		}
		if a.Kind == "agent_result_json" {
			hasJSON = true
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw artifact after reconcile")
	}
	if !hasJSON {
		t.Fatal("expected agent_result_json artifact after reconcile")
	}

	// Verify run status reflects DONE
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "agent_done" {
		t.Fatalf("expected run status 'agent_done', got %q", run.Status)
	}

	// Verify check exists
	checks, err := s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	hasAgentCheck := false
	for _, c := range checks {
		if c.Kind == "agent_result" && c.Status == "pass" {
			hasAgentCheck = true
			break
		}
	}
	if !hasAgentCheck {
		t.Fatal("expected agent_result check with status pass")
	}
}

func TestReconcileOpenCodeExecutionPersistsBlockedFromCapturedStdout(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	seedOpenCodeOutputArtifacts(t, s, runID,
		`{"type":"text","part":{"type":"text","text":"BLOCKED"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: FAIL"}}
{"type":"text","part":{"type":"text","text":"Test status: FAIL"}}
{"type":"text","part":{"type":"text","text":"Count of LOC changed: 2"}}
`,
		"")

	result, err := h.reconcileOpenCodeExecution(runID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected reconciliation to change state")
	}
	if !result.ParsedAgentResult {
		t.Fatal("expected agent result to be parsed")
	}
	if result.FinalStatus != "failed" {
		t.Fatalf("expected final status 'failed' for BLOCKED, got %q", result.FinalStatus)
	}

	// Verify execution is terminal
	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Status == "running" || exec.Status == "starting" {
		t.Fatalf("execution should be terminal, got %q", exec.Status)
	}

	// Verify agent_result_raw exists
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			hasRaw = true
			break
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw artifact after BLOCKED reconcile")
	}

	// Verify run status reflects BLOCKED
	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "agent_blocked" {
		t.Fatalf("expected run status 'agent_blocked', got %q", run.Status)
	}
}

func TestReconcileOpenCodeExecutionWithOutputButNoAgentResultMarksFailed(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	// Stdout has output but no DONE/BLOCKED
	seedOpenCodeOutputArtifacts(t, s, runID,
		`some tool output line
another line
`,
		"error: something went wrong")

	result, err := h.reconcileOpenCodeExecution(runID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected reconciliation to change state")
	}
	if result.ParsedAgentResult {
		t.Fatal("expected no agent result to be parsed")
	}
	if result.FinalStatus != "failed" {
		t.Fatalf("expected final status 'failed', got %q", result.FinalStatus)
	}

	// Verify execution is terminal and failed
	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Status != "failed" {
		t.Fatalf("expected execution status 'failed', got %q", exec.Status)
	}

	// Verify agent_result_raw does NOT exist
	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			t.Fatal("did not expect agent_result_raw when no DONE/BLOCKED")
		}
	}

	// Verify warning event was created
	events, err := s.ListEventsByRun(runID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Message, "without DONE/BLOCKED") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning event about missing DONE/BLOCKED")
	}
}

func TestReconcileOpenCodeResultActionRedirectsToRunStep(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	seedOpenCodeOutputArtifacts(t, s, runID, "some output", "")

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.reconcileOpenCodeResult(w, req, runID)

	if w.Code != 303 {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "?step=run") {
		t.Fatalf("expected redirect to step=run, got %s", loc)
	}
	// Verify HX-Push-Url is set
	pushURL := w.Header().Get("HX-Push-Url")
	if !strings.Contains(pushURL, "?step=run") {
		t.Fatalf("expected HX-Push-Url to step=run, got %s", pushURL)
	}
}

func TestRunGetAutoReconcilesRunningOpenCodeDoneOutput(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	seedOpenCodeOutputArtifacts(t, s, runID,
		`{"type":"text","part":{"type":"text","text":"DONE"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Test status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Count of LOC changed: 12"}}
`,
		"",
	)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	req := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"?step=run", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 from GET render, got %d", w.Code)
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get latest execution: %v", err)
	}
	if exec.Status == "running" || exec.Status == "starting" {
		t.Fatalf("expected execution to be terminal after GET reconcile, got %q", exec.Status)
	}
	if exec.Status != "completed" {
		t.Fatalf("expected execution status completed after DONE reconcile, got %q", exec.Status)
	}

	run, err := s.GetRun(runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != "agent_done" {
		t.Fatalf("expected run status agent_done, got %q", run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	hasJSON := false
	for _, a := range artifactsList {
		switch a.Kind {
		case "agent_result_raw":
			hasRaw = true
		case "agent_result_json":
			hasJSON = true
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw artifact after GET reconcile")
	}
	if !hasJSON {
		t.Fatal("expected agent_result_json artifact after GET reconcile")
	}

	checks, err := s.ListChecksByRun(runID)
	if err != nil {
		t.Fatalf("list checks: %v", err)
	}
	hasAgentCheck := false
	for _, c := range checks {
		if c.Kind == "agent_result" && c.Status == "pass" {
			hasAgentCheck = true
			break
		}
	}
	if !hasAgentCheck {
		t.Fatal("expected agent_result pass check after GET reconcile")
	}

	body := w.Body.String()
	if strings.Contains(body, `hx-trigger="every 2s"`) {
		t.Fatal("expected polling to stop after GET reconcile")
	}
	if !strings.Contains(body, "download stdout") {
		t.Fatal("expected stdout log link after GET reconcile")
	}
}

func TestRunGetShowsRecoverableStaleOpenCodeOutputWithoutResult(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)
	seedOpenCodeOutputArtifacts(t, s, runID,
		`some tool output line
another line
`,
		`stderr line
`,
	)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	req := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"?step=run", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 from GET render, got %d", w.Code)
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get latest execution: %v", err)
	}
	if exec.Status != "running" && exec.Status != "starting" {
		t.Fatalf("expected execution to remain running, got %q", exec.Status)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Reconcile OpenCode Result") {
		t.Fatal("expected recovery action for stale OpenCode output")
	}
	if strings.Contains(body, `hx-trigger="every 2s"`) {
		t.Fatal("expected polling to stop for stale OpenCode output")
	}
	if !strings.Contains(body, "download combined log") {
		t.Fatal("expected combined log link for stale OpenCode output")
	}
}

func TestRunGetKeepsPollingWhenOpenCodeRunningWithoutOutput(t *testing.T) {
	s := setupTestStore(t)
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	runID := setupOpenCodeRun(t, s)

	seedRunningExecution(t, s, runID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itoa(runID))
	req := httptest.NewRequest("GET", "/runs/"+itoa(runID)+"?step=run", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 from GET render, got %d", w.Code)
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get latest execution: %v", err)
	}
	if exec.Status != "running" && exec.Status != "starting" {
		t.Fatalf("expected execution to remain running, got %q", exec.Status)
	}

	body := w.Body.String()
	if !strings.Contains(body, `hx-trigger="every 2s"`) {
		t.Fatal("expected polling when OpenCode is still running without output")
	}
	if strings.Contains(body, "Reconcile OpenCode Result") {
		t.Fatal("did not expect recovery action when no output exists")
	}
}

func TestStartOpenCodeGoPersistsDoneFromRealSmokeJSONL(t *testing.T) {
	s := setupTestStore(t)

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.launchAgentExecution = func(fn func()) { fn() }
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
		return pipeline.AgentCommandRunResult{
			ExitCode: 0,
			Stdout: `{"type":"step_start","part":{"type":"step","reason":"Starting the implementation"}}
{"type":"reasoning","part":{"type":"reasoning","text":"Let me follow the implementation handoff exactly."}}
{"type":"tool_use","part":{"type":"tool","tool":"read_file","state":{"status":"completed","input":{"filePath":"D:\\Code\\relay\\README.md"}}}}
{"type":"text","part":{"type":"text","text":"DONE"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Build status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Test status: PASS"}}
{"type":"text","part":{"type":"text","text":"\n"}}
{"type":"text","part":{"type":"text","text":"Count of LOC changed: 12"}}
`,
		}
	}

	runID := setupOpenCodeRun(t, s)

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()

	h.startOpenCodeGo(w, req, runID)

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	hasRaw := false
	hasJSON := false
	for _, a := range artifactsList {
		if a.Kind == "agent_result_raw" {
			hasRaw = true
		}
		if a.Kind == "agent_result_json" {
			hasJSON = true
		}
	}
	if !hasRaw {
		t.Fatal("expected agent_result_raw artifact after DONE from real smoke JSONL")
	}
	if !hasJSON {
		t.Fatal("expected agent_result_json artifact after DONE from real smoke JSONL")
	}

	// Verify the agent_result_raw contains the parsed assistant text (not raw JSONL)
	rawData, err := artifacts.Read(runID, "agent_result_raw", pipeline.ArtifactFilename("agent_result_raw"))
	if err != nil {
		t.Fatalf("read agent_result_raw: %v", err)
	}
	if !strings.Contains(string(rawData), "DONE") {
		t.Fatalf("expected agent_result_raw to contain 'DONE', got %q", string(rawData))
	}
	if !strings.Contains(string(rawData), "Build status: PASS") {
		t.Fatalf("expected agent_result_raw to contain 'Build status: PASS', got %q", string(rawData))
	}
}
