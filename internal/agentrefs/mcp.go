package agentrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type MCPRegistryInventory struct {
	Tools []MCPToolEntry
}

type MCPToolEntry struct {
	Name        string
	SchemaVar   string
	Handler     string
	SourceFile  string
	ProfileGate string
	Mutation    bool
}

var mcpSourceFiles = []string{
	"internal/mcp/server.go",
	"internal/mcp/canonical_tools.go",
	"internal/mcp/audit_tools.go",
	"internal/mcp/project_tools.go",
	"internal/mcp/plan_tools.go",
}

var canonicalMutationTools = map[string]bool{
	"submit_plan":           true,
	"create_run":            true,
	"record_audit_decision": true,
}

func getCanonicalTools() *MCPRegistryInventory {
	inv := &MCPRegistryInventory{}
	canonicalTools := map[string][]string{
		"planner":       {"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"},
		"auditor":       {"validate_artifact", "create_run", "get_audit_packet", "record_audit_decision"},
		"local_operator": {"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run", "get_audit_packet", "record_audit_decision"},
	}

	for profile, tools := range canonicalTools {
		for _, toolName := range tools {
			entry := MCPToolEntry{
				Name:        toolName,
				SourceFile:  "internal/mcp/canonical_tools.go",
				ProfileGate: profile,
				Mutation:    canonicalMutationTools[toolName],
			}
			inv.Tools = append(inv.Tools, entry)
		}
	}

	sort.Slice(inv.Tools, func(i, j int) bool {
		return inv.Tools[i].Name < inv.Tools[j].Name
	})

	return inv
}

func BuildMCPSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	_ = getCanonicalTools() // canonical tool inventory for future use

	var sourceInputs []SourceInput
	seenPaths := make(map[string]bool)
	for _, f := range mcpSourceFiles {
		if seenPaths[f] {
			continue
		}
		seenPaths[f] = true
		fullPath := filepath.Join(repoRoot, f)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}
		hash, err := ComputeSHA256(fullPath)
		if err != nil {
			return nil, fmt.Errorf("compute hash for %s: %w", f, err)
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   f,
			SHA256: hash,
			Role:   "mcp_tool_source",
		})
	}

	facts := []Fact{
		{
			ID:        "mcp-canonical-tools",
			Label:     FactLabelProven,
			Statement: "Relay exposes canonical MCP tools: planner (validate_artifact, list_projects, submit_plan, get_plan, create_run), auditor (validate_artifact, create_run, get_audit_packet, record_audit_decision), and local_operator (union).",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/mcp/server.go"},
			},
		},
		{
			ID:        "mcp-profile-based-registration",
			Label:     FactLabelProven,
			Statement: "Tool registration is controlled by RELAY_MCP_PROFILE environment variable selecting planner, auditor, or local_operator profile.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/mcp/server.go"},
			},
		},
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "project-mcp-registry",
		Repo: RepoIdentity{
			ProjectID: "relay",
			RepoID:    "Paintersrp/relay",
			Branch:    "main",
		},
		GeneratedBy: GeneratorIdentity{
			Name:    "relay-agentrefs",
			Version: "0.1.0",
		},
		Rendering: RenderingContract{
			JSONPrimary:       true,
			MarkdownFromJSON:  true,
			DeterministicSort: true,
			NoTimestamps:      true,
			RelativePathsOnly: true,
		},
		SourceInputs: sourceInputs,
		FactLabels: []FactLabel{
			FactLabelProven,
			FactLabelDerived,
			FactLabelConvention,
			FactLabelUnresolved,
			FactLabelConflict,
		},
		Facts:      facts,
		References: []ReferenceEntry{},
	}
	return doc, nil
}