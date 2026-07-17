package architecture_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileAcquisitionDoesNotImportRuntimeOrPersistenceLayers(t *testing.T) {
	root := repoRoot(t)
	packageRoot := filepath.Join(root, "internal", "mcp", "fileacquisition")
	for _, file := range goFiles(t, packageRoot) {
		for _, imp := range importsForFile(t, file) {
			if !strings.HasPrefix(imp.path, modulePath+"/internal/") {
				continue
			}
			if strings.HasPrefix(imp.path, modulePath+"/internal/app") ||
				strings.HasPrefix(imp.path, modulePath+"/internal/store") ||
				strings.HasPrefix(imp.path, modulePath+"/internal/artifacts") ||
				strings.HasPrefix(imp.path, modulePath+"/internal/operations/packet") ||
				strings.HasPrefix(imp.path, modulePath+"/internal/server") ||
				strings.HasPrefix(imp.path, modulePath+"/internal/api") {
				t.Fatalf("%s imports runtime or persistence package %q", rel(t, root, file), imp.path)
			}
		}
	}
}

func TestSemanticIdentityImportsOnlyAcquisitionAndRegistryInternally(t *testing.T) {
	root := repoRoot(t)
	packageRoot := filepath.Join(root, "internal", "mcp", "semanticidentity")
	allowed := map[string]bool{
		modulePath + "/internal/mcp/fileacquisition": true,
		modulePath + "/internal/operations/registry": true,
	}
	for _, file := range goFiles(t, packageRoot) {
		for _, imp := range importsForFile(t, file) {
			if strings.HasPrefix(imp.path, modulePath+"/internal/") && !allowed[imp.path] {
				t.Fatalf("%s imports unauthorized internal package %q", rel(t, root, file), imp.path)
			}
		}
	}
}

func TestIdempotencyAppImportsOnlyPureSemanticIdentityFromMCP(t *testing.T) {
	root := repoRoot(t)
	packageRoot := filepath.Join(root, "internal", "app", "idempotency")
	const allowedSemanticIdentity = modulePath + "/internal/mcp/semanticidentity"
	for _, file := range goFiles(t, packageRoot) {
		for _, imp := range importsForFile(t, file) {
			if imp.path == allowedSemanticIdentity {
				continue
			}
			if imp.path == modulePath+"/internal/mcp" || strings.HasPrefix(imp.path, modulePath+"/internal/mcp/") ||
				imp.path == modulePath+"/internal/api" || strings.HasPrefix(imp.path, modulePath+"/internal/api/") ||
				imp.path == modulePath+"/internal/server" || strings.HasPrefix(imp.path, modulePath+"/internal/server/") {
				t.Fatalf("%s imports transport package %q", rel(t, root, file), imp.path)
			}
		}
	}
}
