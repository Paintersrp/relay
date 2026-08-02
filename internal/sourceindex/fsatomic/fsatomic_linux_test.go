//go:build linux

package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/sourceindex"
)

func TestPrivateBuildNameIsCanonical(t *testing.T) {
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	rel, err := sourceindex.PrivateBuildRelativeDirectory(id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if want := "staging/.relay-build-" + id + "-" + nonce; rel != want {
		t.Fatalf("private build name = %q, want %q", rel, want)
	}
}

func TestRemoveOwnedGenerationAndStaging(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	generation := filepath.Join(root, "generations", id)
	staging := filepath.Join(root, "staging", id+"-"+nonce)
	other := filepath.Join(root, "generations", strings.Repeat("c", 64))
	for _, path := range []string{generation, staging, other} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(generation, "generation.json"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnedGeneration(root, id); err != nil {
		t.Fatalf("RemoveOwnedGeneration: %v", err)
	}
	if _, err := os.Stat(generation); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation remains: %v", err)
	}
	if err := RemoveOwnedGenerationAttempt(root, id, nonce); err != nil {
		t.Fatalf("RemoveOwnedGenerationAttempt: %v", err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging remains: %v", err)
	}
	if info, err := os.Stat(other); err != nil || !info.IsDir() {
		t.Fatalf("other generation changed: %v", err)
	}
}

func TestRemoveOwnedRejectsUnsafeOwnership(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "generations"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "staging")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := RemoveOwnedGeneration(root, "../generation"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("invalid generation id error = %v", err)
	}
	if err := RemoveOwnedGenerationAttempt(root, strings.Repeat("a", 64), strings.Repeat("b", 31)); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("invalid nonce error = %v", err)
	}
	if err := RemoveOwnedGenerationAttempt(root, strings.Repeat("a", 64), strings.Repeat("B", 32)); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("uppercase nonce error = %v", err)
	}
	if err := RemoveOwnedGenerationAttempt(root, strings.Repeat("a", 64), strings.Repeat("b", 32)); err == nil {
		t.Fatal("symlink staging parent was accepted")
	}
}

func TestRemoveOwnedAttemptRemovesCanonicalAndPrivateOnly(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	otherNonce := strings.Repeat("c", 32)
	private, err := sourceindex.PrivateBuildRelativeDirectory(id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	otherPrivate, err := sourceindex.PrivateBuildRelativeDirectory(id, otherNonce)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{id + "-" + nonce, filepath.Base(private), filepath.Base(otherPrivate)} {
		if err := os.MkdirAll(filepath.Join(root, "staging", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveOwnedGenerationAttempt(root, id, nonce); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{id + "-" + nonce, filepath.Base(private)} {
		if _, err := os.Lstat(filepath.Join(root, "staging", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned attempt remains: %s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "staging", filepath.Base(otherPrivate))); err != nil {
		t.Fatalf("other attempt changed: %v", err)
	}
}

func TestRemoveOwnedRejectsUnknownContentAndPreservesIt(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	generation := filepath.Join(root, "generations", id)
	if err := os.MkdirAll(generation, 0700); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(generation, "unknown")
	if err := os.WriteFile(unknown, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnedGeneration(root, id); err == nil {
		t.Fatal("unknown content was accepted")
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatalf("unknown content was removed: %v", err)
	}
}

func TestRemoveAllOwnedGenerationAttemptsConvergesAndPreservesOtherGeneration(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	otherID := strings.Repeat("c", 64)
	nonces := []string{strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)}
	for _, nonce := range nonces {
		canonical, err := sourceindex.StagingRelativeDirectory(id, nonce)
		if err != nil {
			t.Fatal(err)
		}
		private, err := sourceindex.PrivateBuildDirectory(root, id, nonce)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(canonical)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(private, 0700); err != nil {
			t.Fatal(err)
		}
	}
	other, err := sourceindex.StagingRelativeDirectory(otherID, nonces[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(other)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllOwnedGenerationAttempts(root, id); err != nil {
		t.Fatalf("RemoveAllOwnedGenerationAttempts: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, sourceindex.StagingDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(other) {
		t.Fatalf("staging entries = %#v, want only %q", entries, filepath.Base(other))
	}
	// A second cleanup is the final idempotence check after convergence.
	if err := RemoveAllOwnedGenerationAttempts(root, id); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestRemoveAllOwnedGenerationAttemptsRejectsMalformedOwnedNames(t *testing.T) {
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	malformed := []string{
		id + "-",
		id + "-" + strings.Repeat("b", 31),
		id + "-" + strings.Repeat("B", 32),
		id + "-" + nonce + "-extra",
		".relay-build-" + id + "-",
		".relay-build-" + id + "-" + strings.Repeat("b", 31),
		".relay-build-" + id + "-" + nonce + "-extra",
	}
	for _, name := range malformed {
		t.Run(name[len(name)-1:], func(t *testing.T) {
			caseRoot := t.TempDir()
			path := filepath.Join(caseRoot, sourceindex.StagingDirectoryName, name)
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := RemoveAllOwnedGenerationAttempts(caseRoot, id); !errors.Is(err, os.ErrInvalid) {
				t.Fatalf("error = %v, want invalid", err)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("malformed attempt was not preserved: %v", err)
			}
		})
	}
}

func TestRemoveOwnedGenerationAcceptsOnlyCanonicalContent(t *testing.T) {
	id := strings.Repeat("a", 64)
	valid := func(t *testing.T, root string) string {
		t.Helper()
		generation := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
		if err := os.MkdirAll(filepath.Join(generation, sourceindex.ShardDirectoryName), 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{sourceindex.GenerationManifestFileName, sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName} {
			if err := os.WriteFile(filepath.Join(generation, name), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return generation
	}
	t.Run("canonical empty shards", func(t *testing.T) {
		root := t.TempDir()
		valid(t, root)
		if err := RemoveOwnedGeneration(root, id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("canonical contiguous shards", func(t *testing.T) {
		root := t.TempDir()
		generation := valid(t, root)
		for _, name := range []string{"000000.zoekt", "000001.zoekt"} {
			if err := os.WriteFile(filepath.Join(generation, sourceindex.ShardDirectoryName, name), []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := RemoveOwnedGeneration(root, id); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name string
		add  func(*testing.T, string)
	}{
		{"unknown root file", func(t *testing.T, g string) { mustWrite(t, filepath.Join(g, "unknown")) }},
		{"unknown root directory", func(t *testing.T, g string) {
			if err := os.Mkdir(filepath.Join(g, "unknown"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed shard", func(t *testing.T, g string) {
			mustWrite(t, filepath.Join(g, sourceindex.ShardDirectoryName, "bad.zoekt"))
		}},
		{"noncontiguous shards", func(t *testing.T, g string) {
			mustWrite(t, filepath.Join(g, sourceindex.ShardDirectoryName, "000001.zoekt"))
		}},
		{"nested shards", func(t *testing.T, g string) {
			if err := os.Mkdir(filepath.Join(g, sourceindex.ShardDirectoryName, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink member", func(t *testing.T, g string) {
			if err := os.Symlink("outside", filepath.Join(g, "link")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
		{"hard linked member", func(t *testing.T, g string) {
			if err := os.Link(filepath.Join(g, sourceindex.GenerationManifestFileName), filepath.Join(g, "linked")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			generation := valid(t, root)
			tc.add(t, generation)
			if err := RemoveOwnedGeneration(root, id); !errors.Is(err, os.ErrInvalid) {
				t.Fatalf("error = %v, want invalid", err)
			}
			if _, err := os.Lstat(generation); err != nil {
				t.Fatalf("unsafe generation was removed: %v", err)
			}
		})
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
}
