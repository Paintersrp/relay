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

func TestDocumentationIntegration_AGENTSContainsGeneratedIndexAndRetiredMarker(t *testing.T) {
	repoRoot := findRepoRoot(t)

	agentsPath := filepath.Join(repoRoot, "AGENTS.md")
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("Read AGENTS.md: %v", err)
	}
	content := string(agentsData)

	if !strings.Contains(content, "docs/generated/agent-references/index.json") {
		t.Error("AGENTS.md must contain docs/generated/agent-references/index.json")
	}
	if !strings.Contains(content, "docs/generated/agent-references/index.md") {
		t.Error("AGENTS.md must contain docs/generated/agent-references/index.md")
	}

	// The word "retired" should appear near the backend-code-surface-map.md reference.
	backendMapIdx := strings.Index(content, "docs/backend-code-surface-map.md")
	if backendMapIdx < 0 {
		t.Error("AGENTS.md must reference docs/backend-code-surface-map.md")
	}
	// Check for "retired" in the vicinity (within 200 chars after the reference).
	snippet := content[backendMapIdx:min(backendMapIdx+200, len(content))]
	if !strings.Contains(snippet, "retired") {
		t.Error("AGENTS.md must describe docs/backend-code-surface-map.md as retired near its reference")
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
