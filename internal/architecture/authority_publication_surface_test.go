package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthorityPublicationHasNoPublicSurfaceIntegration(t *testing.T) {
	root := repoRoot(t)
	for _, relative := range []string{
		"internal/api",
		"internal/mcp",
		"internal/server",
		"internal/operations/registry",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		err := filepath.WalkDir(path, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") || strings.HasSuffix(filePath, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			text := string(data)
			for _, forbidden := range []string{
				"NewAuthorityPublicationService",
				"CommitOperationPacketPublication",
				"BeginPublication(",
				"PrepareRetention(",
			} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s exposes PASS-5 authority publication symbol %q", rel(t, root, filePath), forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestAuthorityPublicationCoordinatorKeepsTransportBoundary(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal", "app", "operations", "authority_publication.go")
	for _, imp := range importsForFile(t, path) {
		if isAPIImport(imp.path) ||
			imp.path == modulePath+"/internal/server" || strings.HasPrefix(imp.path, modulePath+"/internal/server/") ||
			(imp.path == modulePath+"/internal/mcp" || strings.HasPrefix(imp.path, modulePath+"/internal/mcp/")) && imp.path != modulePath+"/internal/mcp/semanticidentity" {
			t.Fatalf("%s imports transport policy %q", rel(t, root, path), imp.path)
		}
	}
}

func TestAuthorityPublicationReconcilesBeforeServerConstruction(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "cmd", "relay", "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	constructor := strings.Index(text, "operations.NewAuthorityPublicationService")
	reconcile := strings.Index(text, "authorityPublications.Reconcile")
	server := strings.Index(text, "server.NewWorkflow")
	if constructor < 0 || reconcile < 0 || server < 0 || constructor >= reconcile || reconcile >= server {
		t.Fatalf("%s does not reconcile packet authority before server construction", rel(t, root, path))
	}
}
