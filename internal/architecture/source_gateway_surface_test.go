package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceGatewayRemainsInternalAndUnmounted(t *testing.T) {
	root := repoRoot(t)
	for _, relative := range []string{"internal/mcp", "internal/server", "internal/api"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		err := filepath.WalkDir(path, func(filePath string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") {
				return nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			text := string(data)
			currentOwner := strings.HasSuffix(filepath.ToSlash(filePath), "/internal/mcp/route_dispatch.go") || strings.HasSuffix(filepath.ToSlash(filePath), "/internal/server/mcp_dependencies.go")
			if !currentOwner && (strings.Contains(text, "internal/sourcegateway") || (strings.Contains(text, "/mcp/v1/") && strings.Contains(text, "source"))) {
				t.Fatalf("%s exposes source-gateway work before route publication", filepath.ToSlash(filePath))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestSourceGatewayDoesNotOwnPacketVaultOrWorkflowLifecycle(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal", "sourcegateway")
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
		for _, forbidden := range []string{"internal/mcp", "internal/server", "internal/repos/workflow", "CreateOperationPacket", "RefreshOperationPacket", "CloseOperationPacket", "ReleaseSourceVaultRetention"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s contains forbidden lifecycle or transport ownership %q", filepath.ToSlash(filePath), forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
