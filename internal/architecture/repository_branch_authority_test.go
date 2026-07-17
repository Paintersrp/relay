package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryBranchAuthorityRemainsInWorkflowLayers(t *testing.T) {
	root := repositoryRootForBranchAuthority(t)
	checks := []struct {
		path       string
		required   []string
		prohibited []string
	}{
		{
			path: "internal/repos/workflow/revision.go",
			required: []string{
				"func (r *Registry) ResolveRevision",
				"RequireCleanWorktree",
				"RequireGovernanceAuthority",
			},
			prohibited: []string{
				"relay/internal/api/",
				"relay/internal/app/",
				"relay/internal/mcp/",
				"relay/internal/operations/packet",
			},
		},
		{
			path: "internal/app/workflow/service.go",
			required: []string{
				"ResolveRepositoryRevision",
				"s.registry.ResolveRevision",
			},
			prohibited: []string{
				"relay/internal/api/",
				"relay/internal/mcp/",
			},
		},
		{
			path: "internal/api/repositories/workflow.go",
			required: []string{
				`r.Get("/repositories", handler.List)`,
				`r.Post("/repositories/inspect", handler.Inspect)`,
				`r.Post("/repositories", handler.Create)`,
				`r.Get("/repositories/{repoTarget}", handler.Get)`,
			},
			prohibited: []string{
				"/mcp/",
				"CreateOperationPacket",
				"PublishOperationPacket",
			},
		},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.path)))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, required := range check.required {
			if !strings.Contains(content, required) {
				t.Fatalf("%s is missing required boundary %q", check.path, required)
			}
		}
		for _, prohibited := range check.prohibited {
			if strings.Contains(content, prohibited) {
				t.Fatalf("%s contains prohibited cross-layer dependency %q", check.path, prohibited)
			}
		}
	}
}

func repositoryRootForBranchAuthority(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root containing go.mod was not found")
		}
		directory = parent
	}
}
