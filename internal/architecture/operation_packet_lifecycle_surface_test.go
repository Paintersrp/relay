package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationPacketLifecycleIsInternalAndLegacyPublicationIsDisabled(t *testing.T) {
	root := repoRoot(t)
	deps := readLifecycleSurfaceFile(t, filepath.Join(root, "internal", "mcp", "deps.go"))
	server := readLifecycleSurfaceFile(t, filepath.Join(root, "internal", "mcp", "server.go"))
	mainSource := readLifecycleSurfaceFile(t, filepath.Join(root, "cmd", "relay", "main.go"))
	legacy := readLifecycleSurfaceFile(t, filepath.Join(root, "internal", "app", "operations", "service.go"))
	if strings.Contains(deps, "OperationPacketLifecycleHandler") || strings.Contains(server, "OperationPacketLifecycleHandler") || strings.Contains(mainSource, "NewOperationPacketLifecycleHandler") {
		t.Fatal("PASS-6 lifecycle handler was mounted on a public or aggregate surface")
	}
	for _, method := range []string{"func (s *Service) Create", "func (s *Service) Refresh", "func (s *Service) Close"} {
		index := strings.Index(legacy, method)
		if index < 0 || !strings.Contains(legacy[index:minLifecycleSurface(len(legacy), index+600)], "completePacketLifecycleRequired") {
			t.Fatalf("legacy method %q does not fail closed", method)
		}
	}
}

func readLifecycleSurfaceFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func minLifecycleSurface(left, right int) int {
	if left < right {
		return left
	}
	return right
}
