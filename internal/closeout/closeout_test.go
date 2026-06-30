package closeout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/artifacts"
)

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
	if report.Validation.Status != "failed" {
		t.Fatalf("validation status = %q, want failed", report.Validation.Status)
	}
	if report.CommitStatus != "skipped_dry_run" || report.PushStatus != "skipped_dry_run" {
		t.Fatalf("commit/push statuses = %q/%q, want skipped_dry_run", report.CommitStatus, report.PushStatus)
	}
	assertCalled(t, runner.calls, "git add -A")
	assertNotCalled(t, runner.calls, "git commit")
	assertNotCalled(t, runner.calls, "git push")
	assertFileExists(t, report.EvidencePaths.JSON)
	assertFileExists(t, report.EvidencePaths.Markdown)
}

func TestMissingMessageBlocks(t *testing.T) {
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{Message: "   ", DryRun: true, Runner: runner})
	if err == nil {
		t.Fatal("expected missing message to block")
	}
	if report.MechanicalBlocker == nil || report.MechanicalBlocker.Stage != "commit_message" {
		t.Fatalf("blocker = %#v, want commit_message", report.MechanicalBlocker)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no commands, got %v", runner.calls)
	}
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
	if report.MechanicalBlocker == nil || report.MechanicalBlocker.Stage != "evidence_write" {
		t.Fatalf("blocker = %#v, want evidence_write", report.MechanicalBlocker)
	}
}

func TestStageFailureBlocks(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "git add -A" {
			return CommandResult{Command: command, ExitCode: 1, Stderr: "stage failed\n", Err: errors.New("exit status 1")}
		}
		return defaultFakeResult(command)
	}}

	report, err := Run(context.Background(), Options{Message: "stage failure", DryRun: true, Now: fixedNow, Runner: runner})
	if err == nil {
		t.Fatal("expected stage failure")
	}
	if report.Validation.Status != "passed" {
		t.Fatalf("validation status = %q, want passed", report.Validation.Status)
	}
	if report.MechanicalBlocker == nil || report.MechanicalBlocker.Stage != "git_stage" {
		t.Fatalf("blocker = %#v, want git_stage", report.MechanicalBlocker)
	}
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
	if report.MechanicalBlocker == nil || report.MechanicalBlocker.Stage != "git_commit" {
		t.Fatalf("blocker = %#v, want git_commit", report.MechanicalBlocker)
	}
	assertNotCalled(t, runner.calls, "git push")
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
	if report.MechanicalBlocker == nil || report.MechanicalBlocker.Stage != "git_push" {
		t.Fatalf("blocker = %#v, want git_push", report.MechanicalBlocker)
	}
}

func TestCommandOrder(t *testing.T) {
	withTempWorkingDir(t)
	runner := &fakeRunner{run: func(ctx context.Context, name string, args ...string) CommandResult {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if command == "git add -A" {
			assertFileExists(t, filepath.Join("handoffs", "closeout", "2026-06-30_order-check.closeout-evidence.json"))
		}
		return defaultFakeResult(command)
	}}

	_, err := Run(context.Background(), Options{Message: "order check", Slug: "order-check", Now: fixedNow, Runner: runner})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	assertOrder(t, runner.calls, "bash -lc make validate-full", "git add -A")
	assertOrder(t, runner.calls, "git commit -m order check", "git push")
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
	data, err := os.ReadFile(report.EvidencePaths.JSON)
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

func defaultFakeResult(command string) CommandResult {
	switch command {
	case "git branch --show-current":
		return CommandResult{Command: command, Stdout: "main\n"}
	case "git rev-parse HEAD":
		return CommandResult{Command: command, Stdout: "abc123456789\n"}
	case "bash -lc make validate-full":
		return CommandResult{Command: command}
	case "git add -A":
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
