package handlers

import (
	"context"
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
