package validationrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRedactSensitiveFromOutput_APIKeys(t *testing.T) {
	// Verify common API key prefixes are redacted
	result := redactSensitiveFromOutput("api key: sk-proj-abcdef123456")
	if strings.Contains(result, "sk-") {
		t.Error("expected sk- token to be redacted")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
}

func TestRedactSensitiveFromOutput_EnvKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-key-12345")
	result := redactSensitiveFromOutput("using key sk-test-key-12345")
	if strings.Contains(result, "sk-test-key-12345") {
		t.Error("expected OPENAI_API_KEY value to be redacted")
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
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected content containing 'hello', got %q", string(data))
	}
}

func TestWriteArtifactFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	err := writeArtifactFile(path, "")
	if err != nil {
		t.Fatalf("write should not error for empty content: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("empty file should be created with empty-output marker")
	} else {
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "[empty output]") {
			t.Errorf("expected [empty output] marker, got %q", string(data))
		}
	}
}

func TestWriteArtifactFile_NonEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "content.txt")
	err := writeArtifactFile(path, "actual output")
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "actual output") {
		t.Errorf("expected actual output in file, got %q", string(data))
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

func TestCommandTimeout_UsesDefault(t *testing.T) {
	if DefaultCommandTimeout != 5*time.Minute {
		t.Errorf("expected DefaultCommandTimeout to be 5m, got %v", DefaultCommandTimeout)
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
