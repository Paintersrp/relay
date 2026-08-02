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
	for _, path := range []string{filepath.Join(generation, "nested"), staging, other} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(generation, "nested", "artifact"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnedGeneration(root, id); err != nil {
		t.Fatalf("RemoveOwnedGeneration: %v", err)
	}
	if _, err := os.Stat(generation); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation remains: %v", err)
	}
	if err := RemoveOwnedGenerationStaging(root, id, nonce); err != nil {
		t.Fatalf("RemoveOwnedGenerationStaging: %v", err)
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
	if err := RemoveOwnedGenerationStaging(root, strings.Repeat("a", 64), strings.Repeat("b", 31)); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("invalid nonce error = %v", err)
	}
	if err := RemoveOwnedGenerationStaging(root, strings.Repeat("a", 64), strings.Repeat("b", 32)); err == nil {
		t.Fatal("symlink staging parent was accepted")
	}
}
