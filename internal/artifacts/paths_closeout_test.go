package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseoutPathWithRepoBaseDir(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(".")

	got, err := CloseoutPath("2026-06-30", "repo-owned-closeout-command", "closeout_evidence_json")
	if err != nil {
		t.Fatalf("CloseoutPath returned unexpected error: %v", err)
	}
	wantSuffix := filepath.Join("handoffs", "closeout", "2026-06-30_repo-owned-closeout-command.closeout-evidence.json")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("CloseoutPath = %q, want suffix %q", got, wantSuffix)
	}

	got, err = CloseoutPath("2026-06-30", "repo-owned-closeout-command", "closeout_evidence_markdown")
	if err != nil {
		t.Fatalf("CloseoutPath markdown returned unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, ".closeout-evidence.md") {
		t.Fatalf("expected markdown suffix, got %q", got)
	}
}

func TestCloseoutPathRejectsUnsafeSlugs(t *testing.T) {
	badSlugs := []string{
		"..",
		"repo/owned",
		`repo\owned`,
		"repo owned",
		"repo;owned",
		"repo&owned",
		"repo|owned",
		"repo$owned",
	}

	for _, slug := range badSlugs {
		t.Run(slug, func(t *testing.T) {
			if _, err := CloseoutPath("2026-06-30", slug, "closeout_evidence_json"); err == nil {
				t.Fatalf("expected error for unsafe slug %q", slug)
			}
		})
	}
}

func TestWriteCloseoutWritesJSONAndMarkdown(t *testing.T) {
	orig := BaseDir
	t.Cleanup(func() { SetBaseDir(orig) })
	SetBaseDir(t.TempDir())

	jsonData := []byte(`{"status":"passed"}`)
	jsonPath, err := WriteCloseout("2026-06-30", "repo-owned-closeout-command", "closeout_evidence_json", jsonData)
	if err != nil {
		t.Fatalf("WriteCloseout json returned unexpected error: %v", err)
	}
	assertFileBytes(t, jsonPath, jsonData)
	assertInsideCloseoutDir(t, jsonPath)

	mdData := []byte("# Closeout\n")
	mdPath, err := WriteCloseout("2026-06-30", "repo-owned-closeout-command", "closeout_evidence_markdown", mdData)
	if err != nil {
		t.Fatalf("WriteCloseout markdown returned unexpected error: %v", err)
	}
	assertFileBytes(t, mdPath, mdData)
	assertInsideCloseoutDir(t, mdPath)
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading %q: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("file %q = %q, want %q", path, got, want)
	}
}

func assertInsideCloseoutDir(t *testing.T, path string) {
	t.Helper()
	cleanDir := filepath.Clean(CloseoutDir())
	cleanPath := filepath.Clean(path)
	if cleanPath != filepath.Join(cleanDir, filepath.Base(cleanPath)) || !strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
		t.Fatalf("closeout path %q escapes closeout dir %q", path, cleanDir)
	}
}
