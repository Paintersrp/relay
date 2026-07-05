package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowOutputCaptureRedactsAcrossChunksAndBoundsLiveTail(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "split-secret-value")
	path := filepath.Join(t.TempDir(), "stdout.log")
	capture, err := newWorkflowOutputCapture(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	capture.Write([]byte("prefix split-sec"))
	capture.Write([]byte("ret-value suffix "))
	capture.Write([]byte(strings.Repeat("x", 32)))
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "split-secret-value") || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("redaction failed: %q", data)
	}
	snapshot := capture.Snapshot()
	if !snapshot.Truncated || len(snapshot.Text) > 16 || snapshot.TotalBytes != int64(len(data)) {
		t.Fatalf("snapshot = %+v, file bytes = %d", snapshot, len(data))
	}
	if strings.Contains(snapshot.Text, "split-secret-value") {
		t.Fatalf("live output leaked secret: %q", snapshot.Text)
	}
}

func TestStreamSecretRedactorFlushesPartialNonSecret(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret-token")
	redactor := newStreamSecretRedactor()
	if got := string(redactor.Write([]byte("sec"))); got != "" {
		t.Fatalf("premature output = %q", got)
	}
	if got := string(redactor.Close()); got != "sec" {
		t.Fatalf("flush output = %q", got)
	}
}

func TestRedactFileToPathStreamsAndRedactsLargeResult(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "result-file-secret")
	root := t.TempDir()
	source := filepath.Join(root, "source.log")
	destination := filepath.Join(root, "redacted.log")
	content := strings.Repeat("prefix-", 10000) + "result-file-secret" + strings.Repeat("-suffix", 10000)
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := redactFileToPath(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "result-file-secret") || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatal("redacted result file contains the configured secret")
	}
	tail, truncated, err := readFileTail(destination, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(tail) > 1024 {
		t.Fatalf("tail bytes=%d truncated=%v", len(tail), truncated)
	}
}
