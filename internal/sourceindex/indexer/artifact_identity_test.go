package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactFilesRejectHardLinkedArtifacts(t *testing.T) {
	for _, tc := range []struct{ name, alias string }{
		{"manifest", "manifest.json"},
		{"shard", "shards/000000.zoekt"},
		{"unlisted alias", "unlisted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			coverage := filepath.Join(root, "coverage.json")
			if err := os.WriteFile(coverage, []byte("coverage"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, tc.alias)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(coverage, filepath.Join(root, tc.alias)); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}
			if _, err := artifactFiles(root); err == nil {
				t.Fatal("accepted hard-linked artifact")
			}
		})
	}
}
