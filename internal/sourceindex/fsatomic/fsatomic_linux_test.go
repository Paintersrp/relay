//go:build linux

package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := RemoveOwnedGenerationAttempt(root, strings.Repeat("a", 64), strings.Repeat("b", 32)); err == nil {
		t.Fatal("symlink staging parent was accepted")
	}
}

func TestRemoveOwnedAttemptRemovesCanonicalAndPrivateOnly(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	otherNonce := strings.Repeat("c", 32)
	for _, name := range []string{id + "-" + nonce, ".relay-build-" + id + "-" + nonce + "-abcdef", ".relay-build-" + id + "-" + otherNonce + "-abcdef"} {
		if err := os.MkdirAll(filepath.Join(root, "staging", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveOwnedGenerationAttempt(root, id, nonce); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{id + "-" + nonce, ".relay-build-" + id + "-" + nonce + "-abcdef"} {
		if _, err := os.Lstat(filepath.Join(root, "staging", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned attempt remains: %s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "staging", ".relay-build-"+id+"-"+otherNonce+"-abcdef")); err != nil {
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
