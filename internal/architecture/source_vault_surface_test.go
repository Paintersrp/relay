package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceVaultDoesNotLeakIntoPublicTransportOrPacketPolicy(t *testing.T) {
	root := repoRoot(t)
	walkSourceVaultSurfaceFiles(t, root, false, func(filePath, text string) {
		if strings.Contains(text, "internal/sourcevault") {
			t.Fatalf("%s imports source-vault policy into a public transport package", filePath)
		}
	})
}

func TestSourceVaultAddsNoPublicSourceRouteOrTool(t *testing.T) {
	root := repoRoot(t)
	walkSourceVaultSurfaceFiles(t, root, true, func(filePath, text string) {
		if strings.Contains(text, "/source-vault") || strings.Contains(text, "source_vault_read") || strings.Contains(text, "SourceVaultResult") {
			t.Fatalf("%s exposes a source-vault route, tool, or result contract", filePath)
		}
	})
}

func walkSourceVaultSurfaceFiles(t *testing.T, root string, includeTests bool, check func(filePath, text string)) {
	t.Helper()
	for _, relative := range []string{"internal/api", "internal/mcp", "internal/server"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		err := filepath.WalkDir(path, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") || (!includeTests && strings.HasSuffix(filePath, "_test.go")) {
				return nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			check(rel(t, root, filePath), string(data))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
