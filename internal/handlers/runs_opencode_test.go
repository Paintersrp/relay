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

	// DB-backed execution should exist in starting/running state
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
