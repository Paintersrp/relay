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
			executor_adapter TEXT NOT NULL DEFAULT 'opencode_go',
			branch_name TEXT NOT NULL DEFAULT '',
			base_commit TEXT NOT NULL DEFAULT '',
			head_commit TEXT NOT NULL DEFAULT '',
			plan_row_id INTEGER REFERENCES plans(id) ON DELETE SET NULL,
			plan_pass_row_id INTEGER REFERENCES plan_passes(id) ON DELETE SET NULL,
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
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
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
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
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
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
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

func TestDispatchBrief_TimeoutWithoutVerifiedTerminationFailsClosed(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	done := make(chan struct{})
	_, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: &fakeAdapter{},
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			return pipeline.AgentCommandRunResult{
				ExitCode:            -2,
				TimedOut:            true,
				Error:               "terminate timed out process tree: still alive",
				StartedAt:           time.Now(),
				FinishedAt:          time.Now(),
				TerminationVerified: false,
				TerminationError:    "terminate timed out process tree: still alive",
			}
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
	if exec.Status != ExecutionStatusTerminationPending {
		t.Fatalf("expected nonterminal status %s, got %s", ExecutionStatusTerminationPending, exec.Status)
	}
	if exec.TerminalizedAt.Valid {
		t.Fatalf("expected unverified timeout to leave terminalized_at unset, got %s", exec.TerminalizedAt.String)
	}
	if !exec.TerminationLastError.Valid || !strings.Contains(exec.TerminationLastError.String, "still alive") {
		t.Fatalf("expected termination error to be retained, got %+v", exec.TerminationLastError)
	}
}

func TestDispatchBrief_UnknownAdapterBlocks(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")
	_, err := s.DB().Exec("UPDATE runs SET executor_adapter = 'unknown_adapter' WHERE id = ?", runID)
	if err != nil {
		t.Fatalf("update run executor adapter: %v", err)
	}

	res, err := DispatchBrief(&DispatchParams{
		Store: s,
		Log:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID: runID,
	})
	if err == nil {
		t.Fatal("expected error for unsupported adapter, got nil")
	}
	if !strings.Contains(err.Error(), "unknown executor adapter") {
		t.Errorf("expected unknown adapter error, got: %v", err)
	}
	if res.Dispatched {
		t.Error("expected Dispatched=false")
	}

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status %s, got %s", StatusExecutorBlocked, run.Status)
	}
}

func TestNormalizeAdapterID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "opencode_go"},
		{"opencode", "opencode_go"},
		{"opencode_go", "opencode_go"},
		{"codex", "codex"},
		{"agy", "antigravity"},
		{"antigravity", "antigravity"},
		{"kiro", "kiro_cli"},
		{"kiro_cli", "kiro_cli"},
		{"invalid", "invalid"},
	}
	for _, c := range cases {
		got := NormalizeAdapterID(c.in)
		if got != c.want {
			t.Errorf("NormalizeAdapterID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeKnownAdapterID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "opencode_go", false},
		{" opencode ", "opencode_go", false},
		{"OPENCODE_GO", "opencode_go", false},
		{"codex", "codex", false},
		{" CODEX ", "codex", false},
		{"agy", "antigravity", false},
		{"Antigravity", "antigravity", false},
		{"kiro", "kiro_cli", false},
		{"kiro_cli", "kiro_cli", false},
		{" KIRO_CLI ", "kiro_cli", false},
		{"invalid", "", true},
		{"deepseek-v4-flash", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeKnownAdapterID(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeKnownAdapterID(%q) expected error, got nil", c.in)
			}
		} else {
			if err != nil {
				t.Errorf("NormalizeKnownAdapterID(%q) expected nil error, got %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("NormalizeKnownAdapterID(%q) = %q, want %q", c.in, got, c.want)
			}
		}
	}
}

func TestNewAdapterFromID_CodexReturnsAdapter(t *testing.T) {
	adapter, err := NewAdapterFromID("codex")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if adapter.ID() != AdapterCodex {
		t.Errorf("expected AdapterCodex, got %s", adapter.ID())
	}
}

func TestNewAdapterFromID_AntigravityReturnsAdapter(t *testing.T) {
	adapter, err := NewAdapterFromID("antigravity")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if adapter.ID() != AdapterAntigravity {
		t.Errorf("expected AdapterAntigravity, got %s", adapter.ID())
	}
}

func TestNewAdapterFromID_AgyReturnsAntigravityAdapter(t *testing.T) {
	adapter, err := NewAdapterFromID("agy")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if adapter.ID() != AdapterAntigravity {
		t.Errorf("expected AdapterAntigravity, got %s", adapter.ID())
	}
}

func TestCodexAdapter_BuildInvocationPreservesCommand(t *testing.T) {
	adapter := CodexAdapter{
		Config: CodexAdapterConfig{Binary: "codex", Sandbox: "workspace-write"},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief\nDo the thing.",
		BriefPath:     "/tmp/repo/executor_brief.md",
		SelectedModel: "test-model",
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Adapter != AdapterCodex {
		t.Errorf("expected adapter %s, got %s", AdapterCodex, inv.Adapter)
	}
	if inv.Binary != "codex" {
		t.Errorf("expected binary codex, got %s", inv.Binary)
	}

	expectedPrefixArgs := []string{"exec", "--cd", "/tmp/repo", "--ask-for-approval", "never", "--sandbox", "workspace-write", "--json", "--output-last-message"}
	for i, a := range expectedPrefixArgs {
		if inv.Args[i] != a {
			t.Errorf("arg %d expected %s, got %s", i, a, inv.Args[i])
		}
	}
	if inv.Args[len(inv.Args)-1] != "-" {
		t.Errorf("expected last arg to be -, got %s", inv.Args[len(inv.Args)-1])
	}
	if inv.Stdin != "# Brief\nDo the thing." {
		t.Errorf("expected stdin to be brief content, got %s", inv.Stdin)
	}
	if !strings.Contains(inv.Preview, "codex exec") || !strings.Contains(inv.Preview, "/tmp/repo") || !strings.Contains(inv.Preview, " < ") {
		t.Errorf("preview missing expected components: %s", inv.Preview)
	}
}

func TestCodexAdapter_DefaultModelUsesConfigDefault(t *testing.T) {
	adapter := CodexAdapter{
		Config: CodexAdapterConfig{Binary: "codex", Sandbox: "workspace-write", Model: ""},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief",
		BriefPath:     "/tmp/brief.md",
		SelectedModel: "any-model", // not used by adapter config directly, config empty
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Model != "codex-config-default" {
		t.Errorf("expected model sentinel codex-config-default, got %q", inv.Model)
	}
	for _, a := range inv.Args {
		if a == "--model" {
			t.Errorf("expected no --model arg when config model is empty")
		}
	}
}

func TestCodexAdapter_ExplicitModelAddsModelArg(t *testing.T) {
	adapter := CodexAdapter{Config: CodexAdapterConfig{Binary: "codex", Sandbox: "workspace-write", Model: "gpt-5.1-codex-max"}}
	req := ExecutorAdapterRequest{RunID: 1, RepoPath: "/tmp/repo", BriefContent: "# Brief", BriefPath: "/tmp/brief.md"}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Model != "gpt-5.1-codex-max" {
		t.Fatalf("expected explicit model, got %q", inv.Model)
	}
	found := false
	for i, arg := range inv.Args {
		if arg == "--model" && i+1 < len(inv.Args) && inv.Args[i+1] == "gpt-5.1-codex-max" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --model gpt-5.1-codex-max in args: %v", inv.Args)
	}
}

func TestCodexAdapter_RejectsDangerFullAccess(t *testing.T) {
	adapter := CodexAdapter{
		Config: CodexAdapterConfig{Binary: "codex", Sandbox: "danger-full-access"},
	}
	req := ExecutorAdapterRequest{
		RunID:        1,
		RepoPath:     "/tmp/repo",
		BriefContent: "# Brief",
		BriefPath:    "/tmp/brief.md",
	}
	_, err := adapter.BuildInvocation(req)
	if err == nil {
		t.Fatal("expected error for danger-full-access, got nil")
	}
	if !strings.Contains(err.Error(), "invalid sandbox value") {
		t.Errorf("expected invalid sandbox error, got %v", err)
	}
}

func TestCodexAdapter_NormalizeResultDone(t *testing.T) {
	adapter := CodexAdapter{}
	raw := "STATUS: DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 5"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: DONE") {
		t.Errorf("expected text to contain STATUS: DONE, got %s", res.ExecutorResultText)
	}
}

func TestCodexAdapter_NormalizeResultBlocked(t *testing.T) {
	adapter := CodexAdapter{}
	raw := "STATUS: BLOCKED\nBLOCKER: syntax error"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultBlocked {
		t.Errorf("expected BLOCKED, got %v", res.Status)
	}
	if !strings.Contains(res.BlockerText, "syntax error") {
		t.Errorf("expected blocker text to contain syntax error, got %s", res.BlockerText)
	}
}

func TestDispatchBrief_CodexIntegrationWritesArtifact(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	_, err := s.UpdateRunExecutorAdapter(runID, "codex")
	if err != nil {
		t.Fatalf("update run executor adapter: %v", err)
	}

	done := make(chan struct{})
	var recordedBin string

	adapter := &CodexAdapter{Config: CodexAdapterConfig{Binary: "codex", Sandbox: "workspace-write"}}
	_, err = DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: adapter,
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			recordedBin = binary

			var resultFile string
			for i, a := range args {
				if a == "--output-last-message" && i+1 < len(args) {
					resultFile = args[i+1]
				}
			}

			if resultFile != "" {
				os.WriteFile(resultFile, []byte("STATUS: DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 12\n"), 0644)
			}

			return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "{\"event\": \"start\"}\n{\"event\": \"chunk\", \"text\": \"hello\"}\n{\"event\": \"end\"}"}
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
	if exec.Provider != string(AdapterCodex) {
		t.Errorf("expected provider codex, got %s", exec.Provider)
	}
	if recordedBin != "codex" {
		t.Errorf("expected runner binary codex, got %s", recordedBin)
	}
	if exec.Status != ExecutionStatusSucceeded {
		t.Errorf("expected exec status %s, got %s", ExecutionStatusSucceeded, exec.Status)
	}

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorDone {
		t.Errorf("expected run status %s, got %s", StatusExecutorDone, run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("get artifacts: %v", err)
	}
	foundCodexMsg := false
	foundResult := false
	for _, a := range artifactsList {
		if a.Kind == ArtifactKindCodexLastMessage {
			foundCodexMsg = true
		}
		if a.Kind == ArtifactKindExecutorResult {
			foundResult = true
			content, _ := os.ReadFile(a.Path)
			if !strings.Contains(string(content), "STATUS: DONE") {
				t.Errorf("executor_result content mismatch: %s", string(content))
			}
		}
	}
	if !foundCodexMsg {
		t.Errorf("expected codex_last_message artifact")
	}
	if !foundResult {
		t.Errorf("expected executor_result artifact")
	}
}

func TestDispatchBrief_CodexDoesNotReuseStaleLastMessage(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	_, err := s.UpdateRunExecutorAdapter(runID, "codex")
	if err != nil {
		t.Fatalf("update run executor adapter: %v", err)
	}

	stalePath, err := artifacts.Write(runID, ArtifactKindCodexLastMessage, "codex_last_message.txt", []byte("STATUS: DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 99\n"))
	if err != nil {
		t.Fatalf("write stale codex message: %v", err)
	}
	if _, err := s.CreateArtifact(runID, ArtifactKindCodexLastMessage, stalePath, "text/plain"); err != nil {
		t.Fatalf("record stale artifact: %v", err)
	}

	done := make(chan struct{})

	adapter := &CodexAdapter{Config: CodexAdapterConfig{Binary: "codex", Sandbox: "workspace-write"}}
	_, err = DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: adapter,
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			// fake runner does not write the --output-last-message file
			return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: "{\"event\": \"done\"}"}
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

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status %s, got %s", StatusExecutorBlocked, run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("get artifacts: %v", err)
	}

	for _, a := range artifactsList {
		if a.Kind == ArtifactKindCodexLastMessage {
			t.Errorf("expected no codex_last_message artifact in DB, found one: %s", a.Path)
		}
		if a.Kind == ArtifactKindExecutorResult {
			content, _ := os.ReadFile(a.Path)
			if strings.Contains(string(content), "STATUS: DONE") {
				t.Errorf("executor_result contains stale DONE content: %s", string(content))
			}
		}
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("expected stale path %s to not exist, got err %v", stalePath, err)
	}
}

func TestAntigravityAdapter_BuildInvocationNonInteractive(t *testing.T) {
	adapter := AntigravityAdapter{
		Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "--yes", Model: "gpt-4"},
	}
	req := ExecutorAdapterRequest{
		RunID:        1,
		RepoPath:     "/tmp/repo",
		BriefContent: "# Brief\nDo the thing.",
		BriefPath:    "/tmp/repo/executor_brief.md",
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Adapter != AdapterAntigravity {
		t.Errorf("expected adapter %s, got %s", AdapterAntigravity, inv.Adapter)
	}
	if inv.Binary != "antigravity" {
		t.Errorf("expected binary antigravity, got %s", inv.Binary)
	}

	expectedArgs := []string{"run", "--prompt-file", "/tmp/repo/executor_brief.md", "--yes", "--no-color", "--output", "json", "--model", "gpt-4"}
	for i, a := range expectedArgs {
		if i >= len(inv.Args) || inv.Args[i] != a {
			t.Errorf("arg %d expected %s, got %v", i, a, inv.Args)
		}
	}
	if inv.Stdin != "" {
		t.Errorf("expected empty stdin, got %q", inv.Stdin)
	}
	if inv.StdinSource != "/dev/null" {
		t.Errorf("expected stdin source /dev/null, got %s", inv.StdinSource)
	}
	if !strings.Contains(inv.Preview, "< /dev/null") {
		t.Errorf("preview missing expected < /dev/null: %s", inv.Preview)
	}
	if !inv.RequireZeroExit {
		t.Errorf("expected RequireZeroExit true")
	}
}

func TestAntigravityAdapter_ApproveFlagNone(t *testing.T) {
	adapter := AntigravityAdapter{
		Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "none"},
	}
	req := ExecutorAdapterRequest{RunID: 1, RepoPath: "/tmp/repo", BriefContent: "# Brief", BriefPath: "/tmp/brief.md"}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	for _, a := range inv.Args {
		if a == "--yes" || a == "none" {
			t.Errorf("expected no approve flag, got %s", a)
		}
	}
}

func TestAntigravityAdapter_NormalizeResultJSONSuccess(t *testing.T) {
	adapter := AntigravityAdapter{}
	raw := `{"status":"success"}`
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: DONE") {
		t.Errorf("expected text to contain STATUS: DONE, got %s", res.ExecutorResultText)
	}
}

func TestAntigravityAdapter_NormalizeResultJSONError(t *testing.T) {
	adapter := AntigravityAdapter{}
	raw := `{"status":"error","error":"auth failed"}`
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultBlocked {
		t.Errorf("expected BLOCKED, got %v", res.Status)
	}
	if !strings.Contains(res.BlockerText, "auth failed") {
		t.Errorf("expected blocker text to contain auth failed, got %s", res.BlockerText)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: BLOCKED") {
		t.Errorf("expected ExecutorResultText to contain STATUS: BLOCKED, got %s", res.ExecutorResultText)
	}
	if !strings.Contains(res.ExecutorResultText, "auth failed") {
		t.Errorf("expected ExecutorResultText to contain auth failed, got %s", res.ExecutorResultText)
	}
}

func TestAntigravityAdapter_NormalizeResultJSONMissingStatus(t *testing.T) {
	adapter := AntigravityAdapter{}
	raw := `{"message":"no status"}`
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultUnknown {
		t.Errorf("expected UNKNOWN, got %v", res.Status)
	}
	if !strings.Contains(res.ParseError, "missing status") {
		t.Errorf("expected parse error to contain missing status, got %s", res.ParseError)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: UNKNOWN") {
		t.Errorf("expected ExecutorResultText to contain STATUS: UNKNOWN, got %s", res.ExecutorResultText)
	}
	if !strings.Contains(res.ExecutorResultText, raw) {
		t.Errorf("expected ExecutorResultText to contain raw output, got %s", res.ExecutorResultText)
	}
}

func TestDispatchBrief_AntigravityJSONErrorWritesExecutorResult(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	_, err := s.UpdateRunExecutorAdapter(runID, "antigravity")
	if err != nil {
		t.Fatalf("update run executor adapter: %v", err)
	}

	done := make(chan struct{})

	adapter := &AntigravityAdapter{Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "--yes"}}
	_, err = DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: adapter,
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			return pipeline.AgentCommandRunResult{ExitCode: 0, Stdout: `{"status":"error","error":"auth failed"}`}
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
	if exec.Status != ExecutionStatusSucceeded {
		t.Errorf("expected exec status %s, got %s", ExecutionStatusSucceeded, exec.Status)
	}

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status %s, got %s", StatusExecutorBlocked, run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("get artifacts: %v", err)
	}
	foundResult := false
	for _, a := range artifactsList {
		if a.Kind == ArtifactKindExecutorResult {
			foundResult = true
			content, _ := os.ReadFile(a.Path)
			if !strings.Contains(string(content), "STATUS: BLOCKED") {
				t.Errorf("executor_result should contain STATUS: BLOCKED, got: %s", string(content))
			}
			if !strings.Contains(string(content), "auth failed") {
				t.Errorf("executor_result should contain auth failed, got: %s", string(content))
			}
		}
	}
	if !foundResult {
		t.Errorf("expected executor_result artifact")
	}
}

func TestDispatchBrief_AntigravityNonZeroExitBlocksDespiteSuccessJSON(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	_, err := s.UpdateRunExecutorAdapter(runID, "antigravity")
	if err != nil {
		t.Fatalf("update run executor adapter: %v", err)
	}

	done := make(chan struct{})

	adapter := &AntigravityAdapter{Config: AntigravityAdapterConfig{Binary: "antigravity", ApproveFlag: "--yes"}}
	_, err = DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: adapter,
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			// Non-zero exit code but success JSON
			return pipeline.AgentCommandRunResult{ExitCode: 1, Stdout: `{"status":"success"}`}
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
	if exec.Provider != string(AdapterAntigravity) {
		t.Errorf("expected provider antigravity, got %s", exec.Provider)
	}
	if exec.Status != "failed" {
		t.Errorf("expected exec status failed, got %s", exec.Status)
	}

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status %s, got %s", StatusExecutorBlocked, run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("get artifacts: %v", err)
	}
	foundResult := false
	for _, a := range artifactsList {
		if a.Kind == ArtifactKindExecutorResult {
			foundResult = true
			content, _ := os.ReadFile(a.Path)
			if strings.Contains(string(content), "STATUS: DONE") {
				t.Errorf("executor_result content mismatch, expected BLOCKED, got: %s", string(content))
			}
			if !strings.Contains(string(content), "STATUS: BLOCKED") {
				t.Errorf("executor_result should be BLOCKED, got: %s", string(content))
			}
		}
	}
	if !foundResult {
		t.Errorf("expected executor_result artifact")
	}
}

func TestDefaultExecutorPreflight_MissingBinaryBlocks(t *testing.T) {
	workDir := t.TempDir()
	stdinSource := filepath.Join(workDir, "source.txt")
	os.WriteFile(stdinSource, []byte("content"), 0644)

	res := defaultExecutorPreflight(ExecutorInvocation{
		Adapter:     "fake",
		Binary:      "relay-definitely-missing-executor-bin",
		WorkDir:     workDir,
		StdinSource: stdinSource,
		Preview:     "relay-definitely-missing-executor-bin < source.txt",
	})

	if res.OK {
		t.Errorf("expected preflight to fail due to missing binary")
	}
	if res.BlockerText == "" || (!strings.Contains(res.BlockerText, "relay-definitely-missing-executor-bin") && !strings.Contains(res.BlockerText, "not found")) {
		t.Errorf("expected blocker text mentioning binary not found, got %q", res.BlockerText)
	}

	foundFailedBinaryCheck := false
	for _, check := range res.Checks {
		if check.Name == "binary_available" && !check.OK {
			foundFailedBinaryCheck = true
		}
	}
	if !foundFailedBinaryCheck {
		t.Errorf("expected failed binary_available check in checks: %v", res.Checks)
	}
}

func TestDispatchBrief_PreflightMissingBinaryBlocksWithoutRunner(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	_, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: &fakeAdapter{},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			t.Fatal("runner should not be called when preflight fails")
			return pipeline.AgentCommandRunResult{}
		},
	})

	if err == nil {
		t.Fatal("expected error due to preflight failure")
	}

	exec, _ := s.GetLatestAgentExecutionByRun(runID)
	if exec != nil {
		t.Fatalf("expected no execution row created, found one")
	}

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status to be %s, got %s", StatusExecutorBlocked, run.Status)
	}

	artifactsList, _ := s.ListArtifactsByRun(runID)
	foundLog := false
	foundResult := false
	for _, a := range artifactsList {
		if a.Kind == ArtifactKindCommandLog {
			foundLog = true
			content, _ := os.ReadFile(a.Path)
			if !strings.Contains(string(content), "Preflight: BLOCKED") {
				t.Errorf("expected command_log to contain Preflight: BLOCKED, got: %s", string(content))
			}
		}
		if a.Kind == ArtifactKindExecutorResult {
			foundResult = true
			content, _ := os.ReadFile(a.Path)
			if !strings.Contains(string(content), "STATUS: BLOCKED") {
				t.Errorf("expected executor_result to contain STATUS: BLOCKED, got: %s", string(content))
			}
		}
	}

	if !foundLog {
		t.Errorf("expected command_log artifact to be written on preflight block")
	}
	if !foundResult {
		t.Errorf("expected executor_result artifact to be written on preflight block")
	}
}

func TestDispatchBrief_PreflightInjectionAllowsExistingFakeRunnerTests(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	done := make(chan struct{})
	runnerCalled := false

	res, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: &fakeAdapter{},
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			runnerCalled = true
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
	if !res.Dispatched {
		t.Errorf("expected Dispatched=true")
	}
	<-done
	if !runnerCalled {
		t.Errorf("expected runner to be called")
	}
}

func TestDispatchBrief_ProcessRegistrationFailureBlocksExecution(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	writeExecutorBrief(t, s, runID, "# Brief")

	done := make(chan struct{})
	var callbackErr error

	_, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: &fakeAdapter{},
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			if callbacks.OnProcessStarted == nil {
				t.Fatal("expected process-start callback")
			}
			exec, err := s.GetActiveAgentExecutionByRun(runID)
			if err != nil || exec == nil {
				t.Fatalf("expected active execution before process registration, got exec=%+v err=%v", exec, err)
			}
			if _, err := s.DB().Exec("UPDATE agent_executions SET ownership_token = 'stale-token' WHERE id = ?", exec.ID); err != nil {
				t.Fatalf("stale ownership token: %v", err)
			}
			callbackErr = callbacks.OnProcessStarted(pipeline.ProcessIdentity{
				PID:       1234,
				GroupID:   1234,
				StartedAt: "fingerprint",
				Platform:  "test",
			})
			return pipeline.AgentCommandRunResult{
				ExitCode:            -1,
				Error:               callbackErr.Error(),
				StartedAt:           time.Now(),
				FinishedAt:          time.Now(),
				TerminationVerified: false,
				TerminationError:    callbackErr.Error(),
			}
		},
		LaunchAsync: func(fn func()) {
			exec, err := s.GetActiveAgentExecutionByRun(runID)
			if err != nil || exec == nil {
				t.Fatalf("expected active execution before launch, got exec=%+v err=%v", exec, err)
			}
			fn()
			close(done)
		},
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	<-done
	if callbackErr == nil {
		t.Fatal("expected process registration callback to fail")
	}

	exec, err := s.GetLatestAgentExecutionByRun(runID)
	if err != nil {
		t.Fatalf("get latest execution: %v", err)
	}
	if exec.Status != ExecutionStatusTerminationPending {
		t.Fatalf("expected pending termination execution, got %s", exec.Status)
	}
	if !exec.TerminationLastError.Valid || !strings.Contains(exec.TerminationLastError.String, "process identity registration") {
		t.Fatalf("expected registration error to be retained, got %+v", exec.TerminationLastError)
	}
	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorDispatched {
		t.Fatalf("expected run status to remain %s while cleanup is pending, got %s", StatusExecutorDispatched, run.Status)
	}
}

func TestKiroCLIAdapter_BuildInvocationSelectedModel(t *testing.T) {
	adapter := KiroCLIAdapter{
		Config: KiroCLIAdapterConfig{
			Binary:     "kiro-cli",
			Effort:     "high",
			TrustTools: defaultKiroTrustTools,
		},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief",
		BriefPath:     "/tmp/brief.md",
		SelectedModel: "claude-sonnet-4.6",
	}
	inv, err := adapter.BuildInvocation(req)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if inv.Model != "claude-sonnet-4.6" {
		t.Errorf("expected model claude-sonnet-4.6, got %s", inv.Model)
	}
	foundModelArg := false
	for i, a := range inv.Args {
		if a == "--model" && i+1 < len(inv.Args) && inv.Args[i+1] == "claude-sonnet-4.6" {
			foundModelArg = true
		}
	}
	if !foundModelArg {
		t.Errorf("expected --model claude-sonnet-4.6 in args: %v", inv.Args)
	}
}

func TestKiroCLIAdapter_BuildInvocationModelPrecedence(t *testing.T) {
	t.Run("selected model wins over configured default", func(t *testing.T) {
		adapter := KiroCLIAdapter{
			Config: KiroCLIAdapterConfig{
				Binary:     "kiro-cli",
				Model:      "claude-opus-4.6",
				Effort:     "high",
				TrustTools: defaultKiroTrustTools,
			},
		}
		req := ExecutorAdapterRequest{
			RunID:         1,
			RepoPath:      "/tmp/repo",
			BriefContent:  "# Brief",
			BriefPath:     "/tmp/brief.md",
			SelectedModel: "claude-sonnet-4.6",
		}
		inv, err := adapter.BuildInvocation(req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if inv.Model != "claude-sonnet-4.6" {
			t.Errorf("expected selected model claude-sonnet-4.6, got %s", inv.Model)
		}
	})

	t.Run("configured default used when selected model is empty", func(t *testing.T) {
		adapter := KiroCLIAdapter{
			Config: KiroCLIAdapterConfig{
				Binary:     "kiro-cli",
				Model:      "claude-opus-4.6",
				Effort:     "high",
				TrustTools: defaultKiroTrustTools,
			},
		}
		req := ExecutorAdapterRequest{
			RunID:        1,
			RepoPath:     "/tmp/repo",
			BriefContent: "# Brief",
			BriefPath:    "/tmp/brief.md",
		}
		inv, err := adapter.BuildInvocation(req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if inv.Model != "claude-opus-4.6" {
			t.Errorf("expected configured default claude-opus-4.6, got %s", inv.Model)
		}
	})

	t.Run("auto used when selected model and default are empty", func(t *testing.T) {
		adapter := KiroCLIAdapter{
			Config: KiroCLIAdapterConfig{
				Binary:     "kiro-cli",
				Effort:     "high",
				TrustTools: defaultKiroTrustTools,
			},
		}
		req := ExecutorAdapterRequest{
			RunID:        1,
			RepoPath:     "/tmp/repo",
			BriefContent: "# Brief",
			BriefPath:    "/tmp/brief.md",
		}
		inv, err := adapter.BuildInvocation(req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if inv.Model != "auto" {
			t.Errorf("expected auto, got %s", inv.Model)
		}
	})
}

func TestKiroCLIAdapter_BuildInvocationInvalidModel(t *testing.T) {
	adapter := KiroCLIAdapter{
		Config: KiroCLIAdapterConfig{
			Binary:     "kiro-cli",
			Effort:     "high",
			TrustTools: defaultKiroTrustTools,
		},
	}
	req := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      "/tmp/repo",
		BriefContent:  "# Brief",
		BriefPath:     "/tmp/brief.md",
		SelectedModel: "gpt-5.1-codex-max",
	}
	_, err := adapter.BuildInvocation(req)
	if err == nil {
		t.Fatalf("expected invalid model error")
	}
	if !strings.Contains(err.Error(), "unsupported Kiro model") {
		t.Fatalf("expected unsupported Kiro model error, got %v", err)
	}
}

func TestKiroCLIAdapter_BuildInvocationSupportsObservedModels(t *testing.T) {
	models := []string{
		"auto",
		"claude-opus-4.8",
		"claude-opus-4.7",
		"claude-opus-4.6",
		"claude-sonnet-4.6",
		"claude-opus-4.5",
		"claude-sonnet-4.5",
		"claude-sonnet-4",
		"claude-haiku-4.5",
		"deepseek-3.2",
		"minimax-m2.5",
		"minimax-m2.1",
		"glm-5",
		"qwen3-coder-next",
	}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			adapter := KiroCLIAdapter{
				Config: KiroCLIAdapterConfig{
					Binary:     "kiro-cli",
					Effort:     "high",
					TrustTools: defaultKiroTrustTools,
				},
			}
			req := ExecutorAdapterRequest{
				RunID:         1,
				RepoPath:      "/tmp/repo",
				BriefContent:  "# Brief",
				BriefPath:     "/tmp/brief.md",
				SelectedModel: model,
			}
			inv, err := adapter.BuildInvocation(req)
			if err != nil {
				t.Fatalf("expected model %s to be accepted: %v", model, err)
			}
			if inv.Model != model {
				t.Fatalf("expected model %s, got %s", model, inv.Model)
			}
		})
	}
}

func TestNewKiroCLIAdapterFromEnvModelFallback(t *testing.T) {
	t.Run("default trust tools include command exec", func(t *testing.T) {
		t.Setenv("RELAY_KIRO_TRUST_TOOLS", "")
		adapter := NewKiroCLIAdapterFromEnv()
		if adapter.Config.TrustTools != defaultKiroTrustTools {
			t.Fatalf("expected default trust tools %q, got %q", defaultKiroTrustTools, adapter.Config.TrustTools)
		}
		if !strings.Contains(adapter.Config.TrustTools, "execute_cmd") {
			t.Fatalf("expected execute_cmd in default trust tools, got %q", adapter.Config.TrustTools)
		}
	})

	t.Run("default model env wins over deprecated fallback", func(t *testing.T) {
		t.Setenv("RELAY_KIRO_DEFAULT_MODEL", "claude-sonnet-4.6")
		t.Setenv("RELAY_KIRO_MODEL", "claude-opus-4.6")
		adapter := NewKiroCLIAdapterFromEnv()
		if adapter.Config.Model != "claude-sonnet-4.6" {
			t.Fatalf("expected RELAY_KIRO_DEFAULT_MODEL, got %q", adapter.Config.Model)
		}
	})

	t.Run("deprecated model env is fallback only", func(t *testing.T) {
		t.Setenv("RELAY_KIRO_DEFAULT_MODEL", "")
		t.Setenv("RELAY_KIRO_MODEL", "claude-opus-4.6")
		adapter := NewKiroCLIAdapterFromEnv()
		if adapter.Config.Model != "claude-opus-4.6" {
			t.Fatalf("expected deprecated fallback model, got %q", adapter.Config.Model)
		}
	})
}

func TestKiroCLIAdapter_BuildInvocationRequireMCPStartupOptIn(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		adapter := KiroCLIAdapter{
			Config: KiroCLIAdapterConfig{
				Binary:     "kiro-cli",
				Effort:     "high",
				TrustTools: defaultKiroTrustTools,
			},
		}
		req := ExecutorAdapterRequest{
			RunID:         1,
			RepoPath:      "/tmp/repo",
			BriefContent:  "# Brief",
			BriefPath:     "/tmp/brief.md",
			SelectedModel: "",
		}
		inv, err := adapter.BuildInvocation(req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		for _, a := range inv.Args {
			if a == "--require-mcp-startup" {
				t.Errorf("expected no --require-mcp-startup by default")
			}
		}
	})

	t.Run("on when configured true", func(t *testing.T) {
		adapter := KiroCLIAdapter{
			Config: KiroCLIAdapterConfig{
				Binary:            "kiro-cli",
				Effort:            "high",
				TrustTools:        defaultKiroTrustTools,
				RequireMCPStartup: true,
			},
		}
		req := ExecutorAdapterRequest{
			RunID:         1,
			RepoPath:      "/tmp/repo",
			BriefContent:  "# Brief",
			BriefPath:     "/tmp/brief.md",
			SelectedModel: "",
		}
		inv, err := adapter.BuildInvocation(req)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		found := false
		for _, a := range inv.Args {
			if a == "--require-mcp-startup" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected --require-mcp-startup flag, args: %v", inv.Args)
		}
	})
}

func TestKiroCLIAdapter_NormalizeResultDone(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "> STATUS: DONE\n> Build status: PASS\n> Test status: PASS\n> Count of LOC changed: 5"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: DONE") {
		t.Errorf("expected text to contain STATUS: DONE, got %s", res.ExecutorResultText)
	}
}

func TestKiroCLIAdapter_NormalizeResultBlocked(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "> STATUS: BLOCKED\n> BLOCKER: auth failed"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultBlocked {
		t.Errorf("expected BLOCKED, got %v", res.Status)
	}
	if !strings.Contains(res.BlockerText, "auth failed") {
		t.Errorf("expected blocker text to contain auth failed, got %s", res.BlockerText)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: BLOCKED") {
		t.Errorf("expected text to contain STATUS: BLOCKED, got %s", res.ExecutorResultText)
	}
}

func TestKiroCLIAdapter_NormalizeResultCanonicalWithoutPromptPrefix(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "STATUS: DONE\nBuild status: PASS\nTest status: PASS\nCount of LOC changed: 5"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
}

func TestKiroCLIAdapter_NormalizeResultProgressBeforePrefixedStatus(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "Using tool fs_read\nProgress: inspected files\n> STATUS: DONE\n> Build status: PASS\n> Test status: PASS\n> Count of LOC changed: 0"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
}

func TestKiroCLIAdapter_NormalizeResultUnknown(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "Some random output without status block"
	res := adapter.NormalizeResult(raw)
	if res.Status != pipeline.AgentResultUnknown {
		t.Errorf("expected UNKNOWN, got %v", res.Status)
	}
	if !strings.Contains(res.ParseError, "missing or invalid STATUS line") {
		t.Errorf("expected parse error, got %s", res.ParseError)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: UNKNOWN") {
		t.Errorf("expected text to contain STATUS: UNKNOWN, got %s", res.ExecutorResultText)
	}
}

func TestDispatchBrief_KiroCLIZeroExitBlocked(t *testing.T) {
	s := setupExecutorTestStore(t)
	runID := createExecutorReadyRun(t, s, "approved_for_executor")
	if _, err := s.UpdateRunModel(runID, "claude-sonnet-4.6", "claude-sonnet-4.6"); err != nil {
		t.Fatalf("update run model: %v", err)
	}
	writeExecutorBrief(t, s, runID, "# Brief")

	done := make(chan struct{})

	adapter := &KiroCLIAdapter{
		Config: KiroCLIAdapterConfig{
			Binary:     "kiro-cli",
			Effort:     "high",
			TrustTools: defaultKiroTrustTools,
		},
	}
	_, err := DispatchBrief(&DispatchParams{
		Store:   s,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		RunID:   runID,
		Adapter: adapter,
		Preflight: func(ExecutorInvocation) ExecutorPreflightResult {
			return ExecutorPreflightResult{OK: true}
		},
		RunAgentCmd: func(ctx context.Context, workDir, binary string, args []string, stdin string, timeout time.Duration, callbacks pipeline.AgentCommandStreamCallbacks) pipeline.AgentCommandRunResult {
			return pipeline.AgentCommandRunResult{ExitCode: 1, Error: "execution failed"}
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

	run, _ := s.GetRun(runID)
	if run.Status != StatusExecutorBlocked {
		t.Errorf("expected run status %s, got %s", StatusExecutorBlocked, run.Status)
	}

	artifactsList, err := s.ListArtifactsByRun(runID)
	if err != nil {
		t.Fatalf("get artifacts: %v", err)
	}
	foundResult := false
	for _, a := range artifactsList {
		if a.Kind == ArtifactKindExecutorResult {
			foundResult = true
			content, _ := os.ReadFile(a.Path)
			if !strings.Contains(string(content), "STATUS: BLOCKED") {
				t.Errorf("executor_result should contain STATUS: BLOCKED, got: %s", string(content))
			}
		}
	}
	if !foundResult {
		t.Errorf("expected executor_result artifact")
	}
}

func TestKiroRedactSensitive(t *testing.T) {
	t.Setenv("KIRO_API_KEY", "sk-kiro-secret-key-12345")
	result := redactSensitive("using sk-kiro-secret-key-12345 here")
	if strings.Contains(result, "sk-kiro-secret-key-12345") {
		t.Errorf("expected redacted output, got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED], got %q", result)
	}
}

func TestNewAdapterFromID_KiroCLIReturnsAdapter(t *testing.T) {
	adapter, err := NewAdapterFromID("kiro_cli")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if adapter.ID() != AdapterKiroCLI {
		t.Errorf("expected AdapterKiroCLI, got %s", adapter.ID())
	}
}

func TestNewAdapterFromID_KiroAliasReturnsKiroCLIAdapter(t *testing.T) {
	adapter, err := NewAdapterFromID("kiro")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if adapter.ID() != AdapterKiroCLI {
		t.Errorf("expected AdapterKiroCLI, got %s", adapter.ID())
	}
}
func TestKiroCLIAdapter_NormalizeResultANSI_Decorated(t *testing.T) {
	adapter := KiroCLIAdapter{}
	// Simulates ANSI-decorated Kiro output as observed
	raw := "\x1b[0m\x1b[38;5;10mSTATUS: DONE\n\x1b[0m\x1b[38;5;10mBUILD: not_run\n\x1b[0m\x1b[38;5;10mTESTS: not_run\nCount of LOC changed: 15"
	res := adapter.NormalizeResult(raw)

	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: DONE") {
		t.Errorf("expected text to contain STATUS: DONE, got %s", res.ExecutorResultText)
	}
	if !strings.Contains(res.ExecutorResultText, "not_run") {
		t.Errorf("expected text to contain not_run, got %s", res.ExecutorResultText)
	}
	// Verify no ESC bytes in output
	if strings.Contains(res.ExecutorResultText, "\x1b") {
		t.Errorf("executor result should not contain ESC byte")
	}
}

func TestKiroCLIAdapter_NormalizeResultANSI_Blocked(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "\x1b[0m\x1b[38;5;9mSTATUS: BLOCKED\n\x1b[0m\x1b[38;5;9mBLOCKER: auth failed"
	res := adapter.NormalizeResult(raw)

	if res.Status != pipeline.AgentResultBlocked {
		t.Errorf("expected BLOCKED, got %v", res.Status)
	}
	if !strings.Contains(res.BlockerText, "auth failed") {
		t.Errorf("expected blocker text to contain auth failed, got %s", res.BlockerText)
	}
}

func TestKiroCLIAdapter_NormalizeResultANSI_PromptPrefix(t *testing.T) {
	adapter := KiroCLIAdapter{}
	raw := "> \x1b[38;5;10mSTATUS: DONE\n> \x1b[38;5;10mBUILD: not_run\n> \x1b[38;5;10mTESTS: not_run"
	res := adapter.NormalizeResult(raw)

	if res.Status != pipeline.AgentResultDone {
		t.Errorf("expected DONE, got %v", res.Status)
	}
	if !strings.Contains(res.ExecutorResultText, "STATUS: DONE") {
		t.Errorf("expected text to contain STATUS: DONE, got %s", res.ExecutorResultText)
	}
}
