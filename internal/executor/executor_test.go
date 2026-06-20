package executor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
)

func setupExecutorTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	artifacts.SetBaseDir(filepath.Join(dir, "data", "artifacts"))
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.Open(dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.DB().Exec(`
		CREATE TABLE IF NOT EXISTS repos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			default_validation_commands TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id INTEGER NOT NULL REFERENCES repos(id),
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'draft',
			recommended_model TEXT NOT NULL DEFAULT '',
			selected_model TEXT NOT NULL DEFAULT '',
			branch_name TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '',
			head_commit TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS artifacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			kind TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			mime_type TEXT NOT NULL DEFAULT 'text/plain',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			level TEXT NOT NULL DEFAULT 'info',
			message TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS agent_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL REFERENCES runs(id),
			provider TEXT NOT NULL DEFAULT 'opencode_go',
			status TEXT NOT NULL DEFAULT 'configured',
			command_preview TEXT NOT NULL DEFAULT '',
			exit_code INTEGER,
			started_at TEXT,
			finished_at TEXT,
			stdout_artifact_path TEXT,
			stderr_artifact_path TEXT,
			combined_artifact_path TEXT,
			result_artifact_path TEXT,
			error TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createExecutorReadyRun(t *testing.T, s *store.Store, status string) int64 {
	t.Helper()
	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# repo"), 0644)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module test"), 0644)
	repo, err := s.CreateRepo("test-repo", repoDir)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	run, err := s.CreateRun(repo.ID, "Test Run", status, "test-model", "anthropic/claude-sonnet-4-5", "main")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run.ID
}

func writeExecutorBrief(t *testing.T, s *store.Store, runID int64, content string) {
	t.Helper()
	path, err := artifacts.Write(runID, "executor_brief", "executor_brief.md", []byte(content))
	if err != nil {
		t.Fatalf("write executor_brief.md: %v", err)
	}
	if _, err := s.CreateArtifact(runID, "executor_brief", path, "text/markdown"); err != nil {
		t.Fatalf("create executor_brief artifact: %v", err)
	}
}

func TestDispatchBrief_RejectsNonApprovedStatus(t *testing.T) {
	s := setupExecutorTestStore(t)
	for _, status := range []string{"draft", "validated", "ready", "executor_dispatched", "executor_done", "executor_blocked"} {
		runID := createExecutorReadyRun(t, s, status)
		writeExecutorBrief(t, s, runID, "# Brief\nDo the thing.\n")

		_, err := DispatchBrief(&DispatchParams{
			Store: s,
			Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
			RunID: runID,
		})
		if err == nil {
			t.Errorf("expected error for status %q, got nil", status)
		}
		if !strings.Contains(err.Error(), "must be approved_for_executor") {
			t.Errorf("expected 'must be approved_for_executor' error for status %q, got: %v", status, err)
		}
	}
}

func TestDispatchBrief_AcceptsApprovedForExecutor(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	briefContent := "# Brief\nDo the thing.\n"
	writeExecutorBrief(t, s, runID, briefContent)

	var recordedStdin string
	runnerCalled := false
	_, err := DispatchBrief(&DispatchParams{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID: runID,
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			runnerCalled = true
			recordedStdin = stdin
			if callbacks.OnStdout != nil {
				callbacks.OnStdout([]byte("DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n"))
			}
			return pipeline.AgentCommandRunResult{
				ExitCode: 0,
				Stdout:   "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n",
			}
		},
		LaunchAsync: func(fn func()) { fn() },
	})
	if err != nil {
		t.Fatalf("expected dispatch to succeed, got: %v", err)
	}
	if !runnerCalled {
		t.Fatal("expected runner to be called")
	}
	if recordedStdin != briefContent {
		t.Fatalf("expected stdin to be brief content, got %q", recordedStdin)
	}
}

func TestDispatchBrief_RejectsMissingBrief(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")

	_, err := DispatchBrief(&DispatchParams{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID: runID,
	})
	if err == nil {
		t.Fatal("expected error for missing brief, got nil")
	}
	if !strings.Contains(err.Error(), "executor_brief.md not found") {
		t.Errorf("expected 'executor_brief.md not found' error, got: %v", err)
	}
}

func TestParseStrictStatus_Done(t *testing.T) {
	raw := "STATUS: DONE\n\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n"
	status, msg := ParseStrictStatus(raw)
	if status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %s", status)
	}
	if msg != "STATUS: DONE" {
		t.Errorf("expected 'STATUS: DONE', got %q", msg)
	}
}

func TestParseStrictStatus_Blocked(t *testing.T) {
	raw := "STATUS: BLOCKED\n\nBLOCKER: migration failed\n"
	status, msg := ParseStrictStatus(raw)
	if status != pipeline.AgentResultBlocked {
		t.Errorf("expected BLOCKED, got %s", status)
	}
	if !strings.Contains(msg, "migration failed") {
		t.Errorf("expected blocker text in message, got %q", msg)
	}
}

func TestParseStrictStatus_Missing(t *testing.T) {
	raw := "Build status: PASS\n"
	status, _ := ParseStrictStatus(raw)
	if status != pipeline.AgentResultUnknown {
		t.Errorf("expected UNKNOWN, got %s", status)
	}
}

func TestParseStrictStatus_Invalid(t *testing.T) {
	raw := "STATUS: MAYBE\n"
	status, _ := ParseStrictStatus(raw)
	if status != pipeline.AgentResultUnknown {
		t.Errorf("expected UNKNOWN, got %s", status)
	}
}

func TestDispatchBrief_RejectsRunningExecution(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief\nDo the thing.\n")

	_, err := s.CreateAgentExecution(runID, "opencode_go", "running", "existing command")
	if err != nil {
		t.Fatalf("create existing execution: %v", err)
	}

	_, err = DispatchBrief(&DispatchParams{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID: runID,
	})
	if err == nil {
		t.Fatal("expected error for existing running execution, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

func TestRedactSensitive(t *testing.T) {
	t.Setenv("RELAY_OPENCODE_BIN", "secret-path")
	result := redactSensitive("using secret-path here")
	if strings.Contains(result, "secret-path") {
		t.Errorf("expected redacted output, got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED], got %q", result)
	}
}

func TestOpenCodeAdapter_BuildInvocationPreservesCommand(t *testing.T) {
	adapter := OpenCodeAdapter{
		Config: pipeline.OpenCodeRunConfig{Binary: "opencode", Agent: "build", Variant: ""},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief\nDo the thing.",
		BriefPath:     "/tmp/repo/executor_brief.md",
		SelectedModel: "anthropic/claude-3-5-sonnet-20241022",
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Adapter != AdapterOpenCodeGo {
		t.Errorf("expected adapter %s, got %s", AdapterOpenCodeGo, inv.Adapter)
	}
	if inv.Binary != "opencode" {
		t.Errorf("expected binary opencode, got %s", inv.Binary)
	}
	expectedArgs := []string{"run", "--format", "json", "--dir", "/tmp/repo", "--agent", "build", "--model", "anthropic/claude-3-5-sonnet-20241022", "--thinking", "max"}
	if len(inv.Args) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d", len(expectedArgs), len(inv.Args))
	}
	for i, a := range expectedArgs {
		if inv.Args[i] != a {
			t.Errorf("arg %d expected %s, got %s", i, a, inv.Args[i])
		}
	}
	if inv.Stdin != "# Brief\nDo the thing." {
		t.Errorf("expected stdin to be brief content, got %s", inv.Stdin)
	}
	if !strings.Contains(inv.Preview, "opencode") || !strings.Contains(inv.Preview, "/tmp/repo") || !strings.Contains(inv.Preview, " < ") {
		t.Errorf("preview missing expected components: %s", inv.Preview)
	}
}

func TestOpenCodeAdapter_BuildInvocationIncludesVariant(t *testing.T) {
	adapter := OpenCodeAdapter{
		Config: pipeline.OpenCodeRunConfig{Binary: "opencode", Agent: "build", Variant: "test-variant"},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief",
		BriefPath:     "/tmp/brief.md",
		SelectedModel: "anthropic/claude-sonnet-4-5",
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	hasVariant := false
	hasVariantValue := false
	for _, a := range inv.Args {
		if a == "--variant" {
			hasVariant = true
		}
		if a == "test-variant" {
			hasVariantValue = true
		}
	}
	if !hasVariant || !hasVariantValue {
		t.Errorf("expected variant args included, got args: %v", inv.Args)
	}
}

func TestDispatchBrief_DefaultProviderRemainsOpenCodeGo(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief\nDo the thing.\n")

	done := make(chan struct{})
	_, err := DispatchBrief(&DispatchParams{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID: runID,
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 0\n"}
		},
		LaunchAsync: func(fn func()) {
			fn()
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	<-done

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Provider != "opencode_go" {
		t.Errorf("expected provider opencode_go, got %s", exec.Provider)
	}
}

type fakeAdapter struct{}

func (a *fakeAdapter) ID() AdapterID {
	return "fake"
}

func (a *fakeAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	return ExecutorInvocation{
		Adapter: "fake",
		Binary:  "fake-bin",
		Args:    []string{"fake-arg"},
		Stdin:   "fake-stdin",
		WorkDir: req.RepoPath,
		Preview: "fake-preview",
	}, nil
}

func (a *fakeAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	return NormalizedExecutorResult{
		Status:             pipeline.AgentResultDone,
		ExecutorResultText: "STATUS: DONE",
	}
}

func TestDispatchBrief_UsesInjectedAdapter(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	done := make(chan struct{})
	var recordedBin string
	var recordedArgs []string

	_, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: &fakeAdapter{},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			recordedBin = binary
			recordedArgs = args
			return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "fake output"}
		},
		LaunchAsync: func(fn func()) {
			fn()
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	<-done

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	if exec.Provider != "fake" {
		t.Errorf("expected provider fake, got %s", exec.Provider)
	}
	if recordedBin != "fake-bin" {
		t.Errorf("expected runner binary fake-bin, got %s", recordedBin)
	}
	if len(recordedArgs) == 0 || recordedArgs[0] != "fake-arg" {
		t.Errorf("expected runner args fake-arg, got %v", recordedArgs)
	}
}
