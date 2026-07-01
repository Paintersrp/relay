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

// TestNormalizeCloseoutPathForwardSlash ensures that closeout write paths can
// be returned and persisted as repo-relative forward-slash paths on all OSes,
// including Windows where filepath.Join uses backslashes.
func TestNormalizeCloseoutPathForwardSlash(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"relative_dot", filepath.Join(".", "handoffs", "closeout", "2026-06-30_slug.closeout-evidence.json"), "handoffs/closeout/2026-06-30_slug.closeout-evidence.json"},
		{"already_slash", "handoffs/closeout/2026-06-30_slug.closeout-evidence.json", "handoffs/closeout/2026-06-30_slug.closeout-evidence.json"},
		{"double_dot_prefix", "./handoffs/closeout/x.json", "handoffs/closeout/x.json"},
		{"absolute_is_returned_as_is", "/repo/handoffs/closeout/x.json", "/repo/handoffs/closeout/x.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeCloseoutPath(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizeCloseoutPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "\\") {
				t.Fatalf("normalized path %q contains backslash", got)
			}
		})
	}
}

// TestCloseoutWritePathsAreRepoRelativeOnWindowsSim verifies that a closeout
// write (which uses filepath.Join internally) returns paths that, after
// normalization, are repo-relative and forward-slash separated, regardless of
// the host OS path separator.
func TestCloseoutWritePathsAreRepoRelativeOnWindowsSim(t *testing.T) {
	orig := BaseDir
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed reading cwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("failed chdir: %v", err)
	}
	SetBaseDir(".")
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		SetBaseDir(orig)
	})

	jsonPath, err := WriteCloseout("2026-06-30", "windows-sim-closeout", "closeout_evidence_json", []byte("{}"))
	if err != nil {
		t.Fatalf("WriteCloseout json returned unexpected error: %v", err)
	}
	normalizedJSON := NormalizeCloseoutPath(jsonPath)
	wantJSONSuffix := "handoffs/closeout/2026-06-30_windows-sim-closeout.closeout-evidence.json"
	if !strings.HasSuffix(normalizedJSON, wantJSONSuffix) {
		t.Fatalf("normalized json = %q, want suffix %q", normalizedJSON, wantJSONSuffix)
	}
	if strings.Contains(normalizedJSON, "\\") {
		t.Fatalf("normalized json %q contains backslash", normalizedJSON)
	}
	if strings.Contains(normalizedJSON, "..") {
		t.Fatalf("normalized json %q contains ..", normalizedJSON)
	}

	mdPath, err := WriteCloseout("2026-06-30", "windows-sim-closeout", "closeout_evidence_markdown", []byte("# Closeout\n"))
	if err != nil {
		t.Fatalf("WriteCloseout markdown returned unexpected error: %v", err)
	}
	normalizedMD := NormalizeCloseoutPath(mdPath)
	wantMDSuffix := "handoffs/closeout/2026-06-30_windows-sim-closeout.closeout-evidence.md"
	if !strings.HasSuffix(normalizedMD, wantMDSuffix) {
		t.Fatalf("normalized md = %q, want suffix %q", normalizedMD, wantMDSuffix)
	}
	if strings.Contains(normalizedMD, "\\") {
		t.Fatalf("normalized md %q contains backslash", normalizedMD)
	}
}
