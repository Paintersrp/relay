package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func quoteArg(arg string) string {
	if !strings.ContainsAny(arg, " 	") {
		return arg
	}
	return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
}

func quoteCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func helperCommand(args ...string) string {
	parts := []string{os.Args[0], "-test.run=TestHelperProcess", "--"}
	parts = append(parts, args...)
	return quoteCommand(parts)
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}

	if len(args) == 0 {
		os.Exit(2)
	}

	switch args[0] {
	case "success":
		fmt.Fprintln(os.Stdout, "hello world")
		fmt.Fprintln(os.Stderr, "warning text")
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "forced failure")
		os.Exit(7)
	case "sleep":
		time.Sleep(2 * time.Second)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func TestRunCommandSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	cmd := ValidationCommand{
		Label:   "echo",
		Command: helperCommand("success"),
		Source:  "test",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	result := RunValidationCommand(context.Background(), dir, cmd, DefaultValidationCommandTimeout)

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "warning text") {
		t.Errorf("expected stderr to contain 'warning text', got %q", result.Stderr)
	}
	if result.TimedOut {
		t.Error("expected not timed out")
	}
	if result.DurationMS < 0 {
		t.Errorf("expected non-negative duration, got %d", result.DurationMS)
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	cmd := ValidationCommand{
		Label:   "fail",
		Command: helperCommand("fail"),
		Source:  "test",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	result := RunValidationCommand(context.Background(), dir, cmd, 10*time.Second)

	if result.ExitCode != 7 {
		t.Errorf("expected exit code 7, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "forced failure") {
		t.Errorf("expected stderr to contain 'forced failure', got %q", result.Stderr)
	}
	if result.TimedOut {
		t.Error("expected not timed out")
	}
}

func TestRunInvalidCommand(t *testing.T) {
	cmd := ValidationCommand{
		Label:   "nonexistent",
		Command: "nonexistent-command-12345",
		Source:  "test",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	result := RunValidationCommand(context.Background(), dir, cmd, DefaultValidationCommandTimeout)

	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 for failed start, got %d", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("expected not timed out")
	}
}

func TestRunCommandTimeout(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	cmd := ValidationCommand{
		Label:   "timeout",
		Command: helperCommand("sleep"),
		Source:  "test",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	result := RunValidationCommand(context.Background(), dir, cmd, 50*time.Millisecond)

	if !result.TimedOut {
		t.Error("expected timed out to be true")
	}
	if result.ExitCode != -2 {
		t.Errorf("expected exit code -2 for timeout, got %d", result.ExitCode)
	}
}

func TestSplitCommandBasic(t *testing.T) {
	args, err := splitCommand("go test ./...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %#v", len(args), args)
	}
	if args[0] != "go" {
		t.Errorf("expected 'go', got %q", args[0])
	}
	if args[1] != "test" {
		t.Errorf("expected 'test', got %q", args[1])
	}
	if args[2] != "./..." {
		t.Errorf("expected './...', got %q", args[2])
	}
}

func TestSplitCommandQuoted(t *testing.T) {
	args, err := splitCommand(`npm run "build:dev"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %#v", len(args), args)
	}
	if args[2] != "build:dev" {
		t.Errorf("expected 'build:dev', got %q", args[2])
	}
}

func TestSplitCommandSingleQuoted(t *testing.T) {
	args, err := splitCommand(`rtk.exe 'go test ./...'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %#v", len(args), args)
	}
	if args[1] != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", args[1])
	}
}

func TestSplitCommandUnclosedQuote(t *testing.T) {
	_, err := splitCommand(`go test "unclosed`)
	if err == nil {
		t.Fatal("expected error for unclosed quote")
	}
}

func TestSplitCommandEmpty(t *testing.T) {
	args, err := splitCommand("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("expected 0 args, got %d", len(args))
	}
}

func TestRunValidationCommandEmptyCommand(t *testing.T) {
	cmd := ValidationCommand{Label: "empty", Command: "", Source: "test"}
	dir, _ := os.Getwd()
	result := RunValidationCommand(context.Background(), dir, cmd, DefaultValidationCommandTimeout)
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 for empty command, got %d", result.ExitCode)
	}
}

func TestRunValidationCommandUnclosedQuote(t *testing.T) {
	cmd := ValidationCommand{Label: "badquote", Command: `echo "hello`, Source: "test"}
	dir, _ := os.Getwd()
	result := RunValidationCommand(context.Background(), dir, cmd, DefaultValidationCommandTimeout)
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 for unclosed quote, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "unclosed quote") {
		t.Errorf("expected stderr to mention unclosed quote, got %q", result.Stderr)
	}
}
