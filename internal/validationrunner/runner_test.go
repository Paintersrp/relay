package validationrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAndCapture(t *testing.T) {
	dir := t.TempDir()

	cmd := sealedCommand{
		ID:       "V1",
		Command:  "echo hello",
		Required: true,
	}

	out := runAndCapture(context.Background(), cmd, dir)
	if out.exitCode != 0 {
		t.Errorf("expected exit 0, got %d", out.exitCode)
	}
	if !strings.Contains(out.stdout, "hello") {
		t.Errorf("expected stdout 'hello', got %q", out.stdout)
	}
	if out.notRunReason != "" {
		t.Errorf("unexpected notRunReason: %s", out.notRunReason)
	}
}

func TestRunAndCapture_Failure(t *testing.T) {
	dir := t.TempDir()

	cmd := sealedCommand{
		ID:       "V2",
		Command:  "exit 1",
		Required: true,
	}

	out := runAndCapture(context.Background(), cmd, dir)
	if out.exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", out.exitCode)
	}
}

func TestRedactSensitiveFromOutput(t *testing.T) {
	t.Setenv("RELAY_OPENCODE_BIN", "sensitive-path")
	result := redactSensitiveFromOutput("using sensitive-path here")
	if strings.Contains(result, "sensitive-path") {
		t.Error("expected sensitive data to be redacted")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
}

func TestWriteArtifactFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	err := writeArtifactFile(path, "hello")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestWriteArtifactFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	err := writeArtifactFile(path, "")
	if err != nil {
		t.Fatalf("write should not error for empty content: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("empty file should not be created")
	}
}

func TestStatusFromExit(t *testing.T) {
	if s := statusFromExit(0, ""); s != "pass" {
		t.Errorf("expected pass, got %s", s)
	}
	if s := statusFromExit(1, ""); s != "fail" {
		t.Errorf("expected fail, got %s", s)
	}
	if s := statusFromExit(0, "reason"); s != "fail" {
		t.Errorf("expected fail for non-empty reason, got %s", s)
	}
}

func TestBuildCommandOutput(t *testing.T) {
	out := commandOutput{
		exitCode:   0,
		durationMs: 1500,
		workdir:    "/repo",
	}
	text := buildCommandOutput(out)
	if !strings.Contains(text, "pass") {
		t.Error("expected pass status")
	}
	if !strings.Contains(text, "1500ms") {
		t.Error("expected duration")
	}
}

func TestRunAndCapture_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	cmd := sealedCommand{
		ID:       "V3",
		Command:  "",
		Required: false,
	}
	out := runAndCapture(context.Background(), cmd, dir)
	if out.notRunReason == "" {
		t.Error("expected notRunReason for empty command")
	}
}
