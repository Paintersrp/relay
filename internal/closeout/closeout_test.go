package closeout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"relay/internal/artifacts"

	"github.com/xeipuuv/gojsonschema"
)

// repoSchemaPath returns the absolute filesystem path to a repo-root schema
// file, independent of the current working directory (which closeout tests
// override with t.TempDir).
func repoSchemaPath(rel string) (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	pkgDir := filepath.Dir(file)
	candidate := filepath.Clean(filepath.Join(pkgDir, "..", "..", rel))
	if _, err := os.Stat(candidate); err != nil {
		return "", false
	}
	return candidate, true
}

type fakeRunner struct {
	calls []string
	run   func(ctx context.Context, name string, args ...string) CommandResult
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	command := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, command)
	if f.run != nil {
		return f.run(ctx, name, args...)
	}
	return defaultFakeResult(command)
}

func TestValidationFailureContinuesInDryRun(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "bash -lc make validate-full" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "validation failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{
		Message: "PASS-005 dry-run closeout smoke",
		DryRun:  true,
		Now:     fixedNow,
		Runner:  runner,
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if report.ValidationStatus() != "failed" {
		t.Fatalf("validation status = %q, want failed", report.ValidationStatus())
	}
	if report.CommitStatus() != "skipped_dry_run" || report.PushStatus() != "skipped_dry_run" {
		t.Fatalf("commit/push statuses = %q/%q, want skipped_dry_run", report.CommitStatus(), report.PushStatus())
	}
	assertNotCalled(t, runner.calls, "git add --")
	assertNotCalled(t, runner.calls, "git commit")
	assertNotCalled(t, runner.calls, "git push")
	assertFileExists(t, report.EvidenceJSONPath())
	assertFileExists(t, report.EvidenceMarkdownPath())
	assertSchemaConformant(t, report.EvidenceJSONPath())
}

// TestValidationFailureContinuesCommitPush covers the PASS-005 boundary for
// the non-dry-run path: a failed final validation alone must not prevent
// stage/commit/push from completing and closeout status must be closed_out.
func TestValidationFailureContinuesCommitPush(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "bash -lc make validate-full" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "validation failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{
		Message: "PASS-005 non-dry-run closeout smoke",
		Now:     fixedNow,
		Runner:  runner,
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if report.ValidationStatus() != "failed" {
		t.Fatalf("validation status = %q, want failed", report.ValidationStatus())
	}
	if report.Status != "closed_out" {
		t.Fatalf("status = %q, want closed_out", report.Status)
	}
	if report.CommitStatus() != "committed" {
		t.Fatalf("commit status = %q, want committed", report.CommitStatus())
	}
	if report.PushStatus() != "pushed" {
		t.Fatalf("push status = %q, want pushed", report.PushStatus())
	}
	assertCalled(t, runner.calls, "git add -- internal/closeout/closeout.go")
	assertNotCalled(t, runner.calls, "git add -- handoffs/validation")
	assertSchemaConformant(t, report.EvidenceJSONPath())
	assertHasIssueSeverity(t, report, "error")
	assertNoIssueSeverity(t, report, "blocker")
}

func TestDryRunRecordsWouldStagePathsWithoutGitMutation(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{}

	report, err := Run(context.Background(), Options{
		Message: "dry-run stage summary",
		DryRun:  true,
		Now:     fixedNow,
		Runner:  runner,
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	got := strings.Join(report.StagedFiles(), "\n")
	if !strings.Contains(got, "internal/closeout/closeout.go") {
		t.Fatalf("would-stage paths = %v, want source path", report.StagedFiles())
	}
	if strings.Contains(got, "handoffs/validation") || strings.Contains(got, "handoffs/closeout") {
		t.Fatalf("would-stage paths include runtime evidence: %v", report.StagedFiles())
	}
	assertNotCalled(t, runner.calls, "git add --")
	assertNotCalled(t, runner.calls, "git commit")
	assertNotCalled(t, runner.calls, "git push")
	assertFileExists(t, report.EvidenceJSONPath())
	assertSchemaConformant(t, report.EvidenceJSONPath())
}

func TestRuntimeEvidencePromotionIncludesEvidencePaths(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{}

	report, err := Run(context.Background(), Options{
		Message:                "promote runtime evidence",
		PromoteRuntimeEvidence: true,
		Now:                    fixedNow,
		Runner:                 runner,
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	assertCalled(t, runner.calls, "git add -- internal/closeout/closeout.go handoffs/validation/latest.validation-report.json handoffs/closeout/2026-06-30_defaults.closeout-evidence.json")
	if report.Status != "closed_out" {
		t.Fatalf("status = %q, want closed_out", report.Status)
	}
}

func TestMissingMessageBlocks(t *testing.T) {
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{Message: "   ", DryRun: true, Runner: runner})
	if err == nil {
		t.Fatal("expected missing message to block")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok {
		t.Fatalf("expected MechanicalBlockerError, got %#v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no commands, got %v", runner.calls)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
	assertHasIssueSeverity(t, report, "blocker")
}

func TestEvidenceWriteFailureBlocks(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("handoffs", []byte("not a dir"), 0644); err != nil {
		t.Fatalf("failed arranging handoffs file: %v", err)
	}

	report, err := Run(context.Background(), Options{
		Message: "closeout evidence write failure",
		DryRun:  true,
		Now:     fixedNow,
		Runner:  &fakeRunner{},
	})
	if err == nil {
		t.Fatal("expected evidence write failure")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok {
		t.Fatalf("expected MechanicalBlockerError, got %#v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
	assertHasIssueSeverity(t, report, "blocker")
}

func TestEvidenceSchemaValidationFailureBlocksBeforeStage(t *testing.T) {
	withTempWorkingDir(t)
	schemaPath := filepath.Join(t.TempDir(), "invalid-closeout-schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","required":["never_present"]}`), 0644); err != nil {
		t.Fatalf("failed writing invalid schema: %v", err)
	}
	runner := &fakeRunner{}

	report, err := Run(context.Background(), Options{
		Message:                    "schema validation failure",
		DryRun:                     true,
		Now:                        fixedNow,
		Runner:                     runner,
		CloseoutEvidenceSchemaPath: schemaPath,
	})
	if err == nil {
		t.Fatal("expected schema validation failure")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok || err.(*MechanicalBlockerError).Stage != "evidence_schema_validation" {
		t.Fatalf("expected evidence_schema_validation blocker, got %#v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
	assertHasIssueSeverity(t, report, "blocker")
	assertNotCalled(t, runner.calls, "git add --")
	assertNotCalled(t, runner.calls, "git commit")
	assertNotCalled(t, runner.calls, "git push")
}

func TestEvidenceSchemaLoadFailureBlocksBeforeStage(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{}

	report, err := Run(context.Background(), Options{
		Message:                    "schema load failure",
		DryRun:                     true,
		Now:                        fixedNow,
		Runner:                     runner,
		CloseoutEvidenceSchemaPath: filepath.Join(t.TempDir(), "missing-schema.json"),
	})
	if err == nil {
		t.Fatal("expected schema load failure")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok || err.(*MechanicalBlockerError).Stage != "evidence_schema_validation" {
		t.Fatalf("expected evidence_schema_validation blocker, got %#v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
	assertNotCalled(t, runner.calls, "git add --")
	assertNotCalled(t, runner.calls, "git commit")
	assertNotCalled(t, runner.calls, "git push")
}

func TestRuntimePathValidationRejectsUnsafeEvidencePath(t *testing.T) {
	report := newReport(resolveMetadata(Options{}), BranchContext{BranchName: "main"}, "ready_for_closeout")
	report.CreatedAt = fixedNow().Format(time.RFC3339)
	report.ArtifactReferences = []ArtifactReference{
		{Kind: "closeout_evidence", Path: "/repo/handoffs/closeout/x.closeout-evidence.json"},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed marshaling report: %v", err)
	}
	err = validateCloseoutEvidencePaths(data)
	if err == nil {
		t.Fatal("expected unsafe evidence path to be rejected")
	}
	if !strings.Contains(err.Error(), "must be repo-relative") {
		t.Fatalf("unexpected path validation error: %v", err)
	}
}

func TestStageFailureBlocks(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if strings.HasPrefix(command, "git add --") {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "stage failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "stage failure", Now: fixedNow, Runner: runner})
	if err == nil {
		t.Fatal("expected stage failure")
	}
	if report.ValidationStatus() != "passed" {
		t.Fatalf("validation status = %q, want passed", report.ValidationStatus())
	}
	if _, ok := err.(*MechanicalBlockerError); !ok || err.(*MechanicalBlockerError).Stage != "git_stage" {
		t.Fatalf("expected git_stage blocker, got %#v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
	assertHasIssueSeverity(t, report, "blocker")
}

func TestCommitFailureBlocks(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if strings.HasPrefix(command, "git commit ") {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "commit failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "commit failure", Now: fixedNow, Runner: runner})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok || err.(*MechanicalBlockerError).Stage != "git_commit" {
		t.Fatalf("expected git_commit blocker, got %#v", err)
	}
	assertNotCalled(t, runner.calls, "git push")
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
}

func TestPushFailureBlocks(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "git push" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "push failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "push failure", Now: fixedNow, Runner: runner})
	if err == nil {
		t.Fatal("expected push failure")
	}
	if _, ok := err.(*MechanicalBlockerError); !ok || err.(*MechanicalBlockerError).Stage != "git_push" {
		t.Fatalf("expected git_push blocker, got %#v", err)
	}
	if report.Status != "blocked" {
		t.Fatalf("status = %q want blocked", report.Status)
	}
}

// TestCommitPushFailureDoesNotContinue verifies that when a mechanical blocker
// (commit failure) occurs, the push step is skipped entirely.
func TestCommitFailureDoesNotCallPush(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if strings.HasPrefix(command, "git commit ") {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "commit failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	_, err := Run(context.Background(), Options{Message: "commit failure then push check", Now: fixedNow, Runner: runner})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	assertNotCalled(t, runner.calls, "git push")
}

func TestCommandOrder(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if strings.HasPrefix(command, "git add --") {
			assertFileExists(t, filepath.Join("handoffs", "closeout", "2026-06-30_order-check.closeout-evidence.json"))
		}
		return defaultFakeResult(command)
	}}

	_, err := Run(context.Background(), Options{Message: "order check", Slug: "order-check", Now: fixedNow, Runner: runner})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	assertOrder(t, runner.calls, "bash -lc make agentrefs-generate", "bash -lc make agentrefs-check")
	assertOrder(t, runner.calls, "bash -lc make agentrefs-check", "bash -lc make validate-full")
	assertOrder(t, runner.calls, "bash -lc make validate-full", "git status --porcelain=v1 --untracked-files=normal")
	assertOrder(t, runner.calls, "git add -- internal/closeout/closeout.go", "git commit -m order check")
	assertOrder(t, runner.calls, "git commit -m order check", "git push")
}

// TestAgentRefsFailureContinues covers the closeout-owned generated-artifact
// step: an agentrefs generate/check failure is recorded as evidence but does
// not block final validation, staging, commit, or push.
func TestAgentRefsFailureContinues(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "bash -lc make agentrefs-generate" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "generate failed\n", Err: errors.New("exit status 1")}
		}
		if command == "bash -lc make agentrefs-check" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "check failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "agentrefs failure", DryRun: true, Now: fixedNow, Runner: runner})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if report.Status == "blocked" {
		t.Fatalf("status = blocked; agentrefs failure should not block closeout")
	}
	// Agentrefs failures are recorded as error-severity issues, not blockers.
	assertHasIssueSeverity(t, report, "error")
	assertNoIssueSeverity(t, report, "blocker")
	// Validation and would-stage summaries still proceed.
	assertCalled(t, runner.calls, "bash -lc make validate-full")
	assertNotCalled(t, runner.calls, "git add --")
	assertSchemaConformant(t, report.EvidenceJSONPath())
}

func TestOutputFiltering(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "bash -lc make validate-full" {
			stdout := "Authorization: Bearer PLACEHOLDER_TOKEN_VALUE_123\nPLACEHOLDER_TOKEN=sample-value\n0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL\n"
			return CommandResult{Command: command, ExitCode: 1, Stdout: stdout, Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "filter output", DryRun: true, Now: fixedNow, Runner: runner})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	data, err := os.ReadFile(report.EvidenceJSONPath())
	if err != nil {
		t.Fatalf("failed reading evidence: %v", err)
	}
	got := string(data)
	for _, forbidden := range []string{"PLACEHOLDER_TOKEN_VALUE_123", "PLACEHOLDER_TOKEN=sample-value", "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("evidence contains unfiltered output %q:\n%s", forbidden, got)
		}
	}
}

// TestMetadataDefaults verifies safe local-closeout defaults when metadata is
// not supplied.
func TestMetadataDefaults(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{Message: "defaults", DryRun: true, Now: fixedNow, Runner: runner})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if report.ProjectID != "relay" {
		t.Fatalf("project_id = %q want relay", report.ProjectID)
	}
	if report.RepoTarget != "Paintersrp/relay" {
		t.Fatalf("repo_target = %q want Paintersrp/relay", report.RepoTarget)
	}
	if report.RunID != "local-closeout" {
		t.Fatalf("run_id = %q want local-closeout", report.RunID)
	}
	if report.PlanID != nil {
		t.Fatalf("plan_id = %v want nil", *report.PlanID)
	}
	if report.PassID != nil {
		t.Fatalf("pass_id = %v want nil", *report.PassID)
	}
}

// TestPathsForwardSlashNormalized ensures no persisted evidence path
// contains a backslash, an absolute leading slash, or "..".
func TestPathsForwardSlashNormalized(t *testing.T) {
	withTempWorkingDir(t)
	report, err := Run(context.Background(), Options{Message: "path normalization", DryRun: true, Now: fixedNow, Runner: &fakeRunner{}})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	for _, ref := range report.ArtifactReferences {
		assertNormalizedRepoPath(t, ref.Path)
	}
	for _, ref := range report.ValidationEvidence.ValidationReports {
		assertNormalizedRepoPath(t, ref.Path)
	}
	assertSchemaConformant(t, report.EvidenceJSONPath())
}

func defaultFakeResult(command string) CommandResult {
	switch command {
	case "git branch --show-current":
		return CommandResult{Command: command, Stdout: "main\n"}
	case "git rev-parse HEAD":
		return CommandResult{Command: command, Stdout: "abc123456789\n"}
	case "bash -lc make validate-full":
		return CommandResult{Command: command}
	case "bash -lc make agentrefs-generate":
		return CommandResult{Command: command}
	case "bash -lc make agentrefs-check":
		return CommandResult{Command: command}
	case "git status --porcelain=v1 --untracked-files=normal":
		return CommandResult{Command: command, Stdout: " M internal/closeout/closeout.go\n?? handoffs/validation/latest.validation-report.json\n?? handoffs/closeout/2026-06-30_defaults.closeout-evidence.json\n"}
	case "git add -- internal/closeout/closeout.go":
		return CommandResult{Command: command}
	case "git diff --cached --name-only":
		return CommandResult{Command: command, Stdout: "internal/closeout/closeout.go\n"}
	case "git push":
		return CommandResult{Command: command}
	default:
		if strings.HasPrefix(command, "git commit ") {
			return CommandResult{Command: command}
		}
		return CommandResult{Command: command}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
}

func withTempWorkingDir(t *testing.T) {
	t.Helper()
	t.Setenv("RELAY_CLOSEOUT_DRY_RUN", "")
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed reading cwd: %v", err)
	}
	origBase := artifacts.BaseDir
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		artifacts.SetBaseDir(origBase)
	})
}

func assertCalled(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, call := range calls {
		if strings.HasPrefix(call, want) {
			return
		}
	}
	t.Fatalf("expected call %q in %v", want, calls)
}

func assertNotCalled(t *testing.T, calls []string, forbidden string) {
	t.Helper()
	for _, call := range calls {
		if strings.HasPrefix(call, forbidden) {
			t.Fatalf("did not expect call %q in %v", forbidden, calls)
		}
	}
}

func assertOrder(t *testing.T, calls []string, first string, second string) {
	t.Helper()
	firstIndex := -1
	secondIndex := -1
	for i, call := range calls {
		if call == first && firstIndex == -1 {
			firstIndex = i
		}
		if call == second && secondIndex == -1 {
			secondIndex = i
		}
	}
	if firstIndex == -1 || secondIndex == -1 || firstIndex >= secondIndex {
		t.Fatalf("expected %q before %q in %v", first, second, calls)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist: %v", path, err)
	}
}

func assertHasIssueSeverity(t *testing.T, report Report, severity string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Severity == severity {
			return
		}
	}
	t.Fatalf("expected a %q-severity issue in %v", severity, report.Issues)
}

func assertNoIssueSeverity(t *testing.T, report Report, severity string) {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Severity == severity {
			t.Fatalf("did not expect %q-severity issue %q", severity, issue.Message)
		}
	}
}

func assertNormalizedRepoPath(t *testing.T, p string) {
	t.Helper()
	if p == "" {
		t.Fatalf("expected non-empty normalized path")
	}
	if strings.Contains(p, "\\") {
		t.Fatalf("path %q contains backslash", p)
	}
	if strings.HasPrefix(p, "/") {
		t.Fatalf("path %q is absolute", p)
	}
	if strings.Contains(p, "..") {
		t.Fatalf("path %q contains ..", p)
	}
}

// assertSchemaConformant parses the generated closeout evidence JSON and
// asserts it has the required schema shape, no legacy custom top-level
// fields, and forward-slash repo-relative path-like values. When the closeout
// evidence schema file is available in the repo, it also runs a full JSON
// schema validation pass via gojsonschema.
func assertSchemaConformant(t *testing.T, jsonPath string) {
	t.Helper()
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed reading evidence JSON %q: %v", jsonPath, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("evidence JSON is not a valid object: %v", err)
	}

	required := []string{
		"evidence_kind", "schema_version", "created_at", "project_id",
		"plan_id", "pass_id", "run_id", "repo_target", "branch_context",
		"status", "validation_evidence", "audit_evidence",
		"repository_evidence", "artifact_references", "issues",
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			t.Fatalf("evidence JSON missing required field %q", field)
		}
	}
	legacy := []string{
		"report_kind", "message", "slug", "branch", "head_sha",
		"validation", "evidence_paths", "staged_files",
		"commit_status", "commit_sha", "push_status", "mechanical_blocker",
	}
	for _, field := range legacy {
		if _, ok := raw[field]; ok {
			t.Fatalf("evidence JSON contains legacy custom field %q", field)
		}
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("failed to unmarshal evidence into Report: %v", err)
	}
	if report.EvidenceKind != "closeout_evidence" {
		t.Fatalf("evidence_kind = %q want closeout_evidence", report.EvidenceKind)
	}
	if report.BranchContext.BranchName == "" {
		t.Fatalf("branch_context.branch_name missing")
	}
	if _, ok := raw["branch_context"]; !ok {
		t.Fatalf("branch_context missing")
	}
	if err := json.Unmarshal(raw["branch_context"], &struct {
		BranchName string  `json:"branch_name"`
		BaseRef    *string `json:"base_ref"`
		HeadRef    *string `json:"head_ref"`
	}{}); err != nil {
		t.Fatalf("branch_context shape invalid: %v", err)
	}
	if report.ValidationEvidence.ValidationReports == nil {
		t.Fatalf("validation_evidence.validation_reports missing")
	}
	if _, ok := raw["validation_evidence"]; !ok {
		t.Fatalf("validation_evidence missing")
	}
	if err := json.Unmarshal(raw["validation_evidence"], &struct {
		ValidationReports []ArtifactReference `json:"validation_reports"`
		Summary           string              `json:"summary"`
	}{}); err != nil {
		t.Fatalf("validation_evidence shape invalid: %v", err)
	}
	if err := json.Unmarshal(raw["audit_evidence"], &struct {
		AuditPackets []ArtifactReference `json:"audit_packets"`
		AuditStatus  string              `json:"audit_status"`
	}{}); err != nil {
		t.Fatalf("audit_evidence shape invalid: %v", err)
	}
	if err := json.Unmarshal(raw["repository_evidence"], &struct {
		GitStatus RepositoryEvidenceReference `json:"git_status"`
		Commit    RepositoryEvidenceReference `json:"commit"`
		Push      RepositoryEvidenceReference `json:"push"`
	}{}); err != nil {
		t.Fatalf("repository_evidence shape invalid: %v", err)
	}
	for _, ref := range report.ArtifactReferences {
		assertNormalizedRepoPath(t, ref.Path)
	}
	for _, ref := range report.ValidationEvidence.ValidationReports {
		assertNormalizedRepoPath(t, ref.Path)
	}

	validateAgainstCloseoutSchema(t, data)
}

// validateAgainstCloseoutSchema runs a full JSON schema validation pass for
// the generated evidence against relay-contracts/schema/closeout_evidence.schema.json.
// The schema uses RE2-incompatible lookahead regexes which are sanitized
// before validation (mirroring internal/validation's approach).
func validateAgainstCloseoutSchema(t *testing.T, evidenceJSON []byte) {
	t.Helper()
	absPath, ok := repoSchemaPath("relay-contracts/schema/closeout_evidence.schema.json")
	if !ok {
		t.Logf("closeout evidence schema not available; skipping full JSON schema validation")
		return
	}
	schemaBytes, err := os.ReadFile(absPath)
	if err != nil {
		t.Logf("failed reading closeout evidence schema: %v; skipping", err)
		return
	}
	cleaned := sanitizeSchemaRegexesForTest(string(schemaBytes))
	schemaLoader := gojsonschema.NewStringLoader(cleaned)
	documentLoader := gojsonschema.NewBytesLoader(evidenceJSON)
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		t.Fatalf("closeout evidence JSON schema validation error: %v", err)
	}
	if !result.Valid() {
		var sb strings.Builder
		for _, rerr := range result.Errors() {
			sb.WriteString("- ")
			sb.WriteString(rerr.String())
			sb.WriteString("\n")
		}
		t.Fatalf("closeout evidence JSON does not conform to schema:\n%s", sb.String())
	}
}

func sanitizeSchemaRegexesForTest(schemaContent string) string {
	schemaContent = strings.ReplaceAll(schemaContent, `(?!/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\\.\\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\\\)`, "")
	return schemaContent
}
