package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h.runAgentCommandArgs = func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration) pipeline.AgentCommandRunResult {
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
