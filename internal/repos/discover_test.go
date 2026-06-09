package repos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_DetectsChildRepos(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, "alpha", ".git"))
	mkdirAll(t, filepath.Join(root, "beta", ".git"))
	mkdirAll(t, filepath.Join(root, "notrepo"))

	result := Discover(root, 3)

	if len(result.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(result.Repos))
	}

	names := make(map[string]string)
	for _, r := range result.Repos {
		names[r.Name] = r.Path
	}

	alphaPath := filepath.ToSlash(filepath.Clean(filepath.Join(root, "alpha")))
	betaPath := filepath.ToSlash(filepath.Clean(filepath.Join(root, "beta")))

	if names["alpha"] != alphaPath {
		t.Errorf("expected alpha path %s, got %s", alphaPath, names["alpha"])
	}
	if names["beta"] != betaPath {
		t.Errorf("expected beta path %s, got %s", betaPath, names["beta"])
	}
}

func TestDiscover_NestedRepoWithinMaxDepth(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, "group", "project", ".git"))

	result := Discover(root, 3)

	if len(result.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(result.Repos))
	}
	if result.Repos[0].Name != "project" {
		t.Errorf("expected name 'project', got '%s'", result.Repos[0].Name)
	}
}

func TestDiscover_NestedRepoBeyondMaxDepth(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, "a", "b", "c", "d", ".git"))

	result := Discover(root, 3)

	if len(result.Repos) != 0 {
		t.Fatalf("expected 0 repos beyond depth 3, got %d", len(result.Repos))
	}
}

func TestDiscover_SkipsIgnoredDirs(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, "node_modules", "pkg", ".git"))
	mkdirAll(t, filepath.Join(root, "vendor", "dep", ".git"))
	mkdirAll(t, filepath.Join(root, ".cache", "stuff", ".git"))

	result := Discover(root, 3)

	if len(result.Repos) != 0 {
		t.Fatalf("expected 0 repos from ignored dirs, got %d", len(result.Repos))
	}
}

func TestDiscover_MissingRootReturnsWarning(t *testing.T) {
	result := Discover("/tmp/relay_nonexistent_test_root", 3)

	if len(result.Warnings) == 0 {
		t.Error("expected warning for missing root")
	}
	if len(result.Repos) != 0 {
		t.Errorf("expected 0 repos for missing root, got %d", len(result.Repos))
	}
}

func TestDiscover_RootIsRepo(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, ".git"))

	result := Discover(root, 3)

	if len(result.Repos) != 1 {
		t.Fatalf("expected root itself to be a repo, got %d repos", len(result.Repos))
	}

	normPath := filepath.ToSlash(filepath.Clean(root))
	if result.Repos[0].Path != normPath {
		t.Errorf("expected path %s, got %s", normPath, result.Repos[0].Path)
	}
}

func TestDiscover_DoesNotDescendIntoRepo(t *testing.T) {
	root := t.TempDir()

	mkdirAll(t, filepath.Join(root, "alpha", ".git"))
	mkdirAll(t, filepath.Join(root, "alpha", "nested", ".git"))

	result := Discover(root, 3)

	if len(result.Repos) != 1 {
		t.Fatalf("expected only top-level repo (not nested inside it), got %d", len(result.Repos))
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`D:\Code\relay`, "D:/Code/relay"},
		{`D:/Code/relay`, "D:/Code/relay"},
		{`D:\Code\relay\`, "D:/Code/relay"},
		{`  D:\Code\relay  `, "D:/Code/relay"},
		{`/home/user/projects`, "/home/user/projects"},
		{``, "."},
	}

	for _, tc := range tests {
		got := NormalizePath(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
