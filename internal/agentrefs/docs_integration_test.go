package agentrefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentationIntegration_AllIndexReferencesAppearInAgentReference(t *testing.T) {
	repoRoot := findRepoRoot(t)

	indexPath := filepath.Join(repoRoot, "docs", "generated", "agent-references", "index.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Read index.json: %v", err)
	}

	var indexDoc ReferenceDocument
	if err := json.Unmarshal(indexData, &indexDoc); err != nil {
		t.Fatalf("Unmarshal index.json: %v", err)
	}

	agentRefPath := filepath.Join(repoRoot, "docs", "agent-reference.md")
	agentRefData, err := os.ReadFile(agentRefPath)
	if err != nil {
		t.Fatalf("Read agent-reference.md: %v", err)
	}
	agentRefContent := string(agentRefData)

	for _, ref := range indexDoc.References {
		if !strings.Contains(agentRefContent, ref.Path) {
			t.Errorf("agent-reference.md must contain reference path %q", ref.Path)
		}
	}
}

func TestDocumentationIntegration_AgentReferenceContainsGeneratedIndexPaths(t *testing.T) {
	repoRoot := findRepoRoot(t)

	agentRefPath := filepath.Join(repoRoot, "docs", "agent-reference.md")
	agentRefData, err := os.ReadFile(agentRefPath)
	if err != nil {
		t.Fatalf("Read agent-reference.md: %v", err)
	}
	content := string(agentRefData)

	if !strings.Contains(content, "docs/generated/agent-references/index.json") {
		t.Error("agent-reference.md must contain docs/generated/agent-references/index.json")
	}
	if !strings.Contains(content, "docs/generated/agent-references/index.md") {
		t.Error("agent-reference.md must contain docs/generated/agent-references/index.md")
	}
}

func TestDocumentationIntegration_BackendMapIsRetiredPointer(t *testing.T) {
	repoRoot := findRepoRoot(t)

	mapPath := filepath.Join(repoRoot, "docs", "backend-code-surface-map.md")
	mapData, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatalf("Read backend-code-surface-map.md: %v", err)
	}
	content := string(mapData)

	if !strings.Contains(content, "Retired Manual Map") {
		t.Error("backend-code-surface-map.md must contain 'Retired Manual Map'")
	}
	if !strings.Contains(content, "docs/generated/agent-references/index.json") {
		t.Error("backend-code-surface-map.md must contain 'docs/generated/agent-references/index.json'")
	}
	if strings.Contains(content, "source_snapshot_id") {
		t.Error("backend-code-surface-map.md must not contain 'source_snapshot_id'")
	}
	if strings.Contains(content, "dirty_worktree") {
		t.Error("backend-code-surface-map.md must not contain 'dirty_worktree'")
	}
	if strings.Contains(content, "Backend Service Package Index") {
		t.Error("backend-code-surface-map.md must not contain 'Backend Service Package Index'")
	}
}
