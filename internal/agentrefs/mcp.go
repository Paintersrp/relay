package agentrefs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	IsBroker    bool
	IsRefactor  bool
	IsPlanAttempt bool
}

var mcpSourceFiles = []string{
	"internal/mcp/server.go",
	"internal/mcp/context_broker_tools.go",
	"internal/mcp/plan_attempt_tools.go",
	"internal/mcp/refactor_backlog_tools.go",
}

var profileGatedToolNames = map[string]bool{
	"get_project":                        true,
	"get_plan":                           true,
	"get_pass":                           true,
	"get_pass_context":                   true,
	"get_next_pass_work":                 true,
	"get_next_audit_work":                true,
	"create_source_snapshot":             true,
	"list_project_files":                 true,
	"search_project_files":               true,
	"read_project_file":                  true,
	"get_repository_git_status":          true,
	"get_repository_recent_commit":       true,
	"list_repository_changed_files":      true,
	"get_repository_diff":                true,
	"create_context_packet":              true,
	"get_context_packet":                 true,
	"create_local_audit":                 true,
	"get_local_audit":                    true,
	"list_project_local_audits":          true,
	"search_project_context_memory":      true,
	"list_project_context_records":       true,
	"get_project_context_record":         true,
	"create_project_context_record":      true,
	"supersede_project_context_record":   true,
	"list_refactor_discovery_tasks":      true,
	"get_refactor_discovery_task":        true,
	"create_refactor_discovery_task":     true,
	"update_refactor_discovery_task":     true,
	"complete_refactor_discovery_task":   true,
	"close_refactor_discovery_task":      true,
	"supersede_refactor_discovery_task":  true,
	"list_refactor_candidates":           true,
	"get_refactor_candidate":             true,
	"search_refactor_candidates":         true,
	"create_refactor_candidate":          true,
	"update_refactor_candidate":          true,
	"defer_refactor_candidate":           true,
	"reject_refactor_candidate":          true,
	"supersede_refactor_candidate":       true,
	"suggest_refactor_candidate_placement": true,
	"promote_refactor_candidate_to_plan": true,
	"generate_refactor_only_plan":        true,
}

var mutationToolNames = map[string]bool{
	"create_run_from_planner_handoff":   true,
	"submit_planner_pass_plan":          true,
	"submit_test_audit_packet":          true,
	"submit_audit_packet":               true,
	"create_plan_attempt_with_intent":   true,
	"submit_intent_drift_review":        true,
	"revise_plan_attempt":               true,
	"void_plan_attempt":                 true,
	"approve_plan_attempt":              true,
	"submit_plan_attempt":               true,
	"create_plan_seed":                  true,
	"update_plan_seed":                  true,
	"defer_plan_seed":                   true,
	"reject_plan_seed":                  true,
	"create_plan_attempt_from_seed":     true,
	"create_source_snapshot":            true,
	"create_context_packet":             true,
	"create_local_audit":                true,
	"create_project_context_record":     true,
	"supersede_project_context_record":  true,
	"create_refactor_discovery_task":    true,
	"update_refactor_discovery_task":    true,
	"complete_refactor_discovery_task":  true,
	"close_refactor_discovery_task":     true,
	"supersede_refactor_discovery_task": true,
	"create_refactor_candidate":         true,
	"update_refactor_candidate":         true,
	"defer_refactor_candidate":          true,
	"reject_refactor_candidate":         true,
	"supersede_refactor_candidate":      true,
	"promote_refactor_candidate_to_plan": true,
	"generate_refactor_only_plan":       true,
}

func ScanMCPRegistry(repoRoot string) (*MCPRegistryInventory, error) {
	inv := &MCPRegistryInventory{}
	toolHandlerMap := make(map[string]string)
	seenTools := make(map[string]bool)

	for _, srcFile := range mcpSourceFiles {
		fullPath := filepath.Join(repoRoot, srcFile)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Handle") {
				base := strings.TrimPrefix(fn.Name.Name, "Handle")
				if base != "" && len(base) != len(fn.Name.Name) {
					toolHandlerMap[base] = findHandlerSourceFile(fn.Name.Name, fullPath)
				}
			}
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if !name.IsExported() || !strings.HasPrefix(name.Name, "Tool") {
						continue
					}
					toolName := inferToolName(name.Name)
					if toolName == "" || seenTools[toolName] {
						continue
					}
					seenTools[toolName] = true
					entry := MCPToolEntry{
						Name:       toolName,
						SchemaVar:  name.Name,
						SourceFile: srcFile,
					}
					if src, ok2 := toolHandlerMapInFile(toolName, toolHandlerMap, srcFile); ok2 {
						entry.Handler = src[:len(src)-len(filepath.Ext(src))]
					} else {
						handlerName2 := toolHandlerNameFromToolName(toolName)
						if _, ok3 := toolHandlerMap[strings.TrimPrefix(handlerName2, "Handle")]; ok3 {
							entry.Handler = handlerName2
						}
					}

					entry.ProfileGate = "always"
					if profileGatedToolNames[toolName] {
						entry.ProfileGate = "context_broker_profile_required"
					}
					entry.Mutation = mutationToolNames[toolName]
					if strings.Contains(srcFile, "context_broker") {
						entry.IsBroker = true
					}
					if strings.Contains(srcFile, "plan_attempt") {
						entry.IsPlanAttempt = true
					}
					if strings.Contains(srcFile, "refactor") {
						entry.IsRefactor = true
					}
					inv.Tools = append(inv.Tools, entry)
				}
			}
		}
	}

	sort.Slice(inv.Tools, func(i, j int) bool {
		return inv.Tools[i].Name < inv.Tools[j].Name
	})

	return inv, nil
}

func toolHandlerNameFromToolName(toolName string) string {
	parts := strings.Split(toolName, "_")
	var b strings.Builder
	b.WriteString("Handle")
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

func inferToolName(varName string) string {
	if strings.HasPrefix(varName, "Tool") {
		remainder := strings.TrimPrefix(varName, "Tool")
		if remainder == "" {
			return ""
		}
		var parts []string
		var current strings.Builder
		for i, r := range remainder {
			if i > 0 && r >= 'A' && r <= 'Z' {
				parts = append(parts, strings.ToLower(current.String()))
				current.Reset()
			}
			current.WriteRune(r)
		}
		if current.Len() > 0 {
			parts = append(parts, strings.ToLower(current.String()))
		}
		return strings.Join(parts, "_")
	}
	return ""
}

func toolCamel(varName string) string {
	if strings.HasPrefix(varName, "Tool") {
		return strings.TrimPrefix(varName, "Tool")
	}
	return varName
}

func findHandlerSourceFile(handlerName, fallback string) string {
	return fallback
}

func toolHandlerMapInFile(toolName string, handlerMap map[string]string, srcFile string) (string, bool) {
	candidate := toolHandlerNameFromToolName(toolName)
	for k, v := range handlerMap {
		if strings.TrimPrefix(candidate, "Handle") == k {
			return v, true
		}
		if strings.Contains(strings.ToLower(k), strings.ToLower(toolName)) {
			return v, true
		}
	}
	return "", false
}

func BuildMCPSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	inv, err := ScanMCPRegistry(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan MCP registry: %w", err)
	}

	var sourceInputs []SourceInput
	seenPaths := make(map[string]bool)
	for _, f := range mcpSourceFiles {
		if seenPaths[f] {
			continue
		}
		seenPaths[f] = true
		hash, err := ComputeSHA256(filepath.Join(repoRoot, f))
		if err != nil {
			hash = "unavailable"
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   f,
			SHA256: hash,
			Role:   "mcp_tool_source",
		})
	}

	var facts []Fact
	ordinal := 0
	for _, tool := range inv.Tools {
		evidence := []Evidence{
			{Kind: "source", Value: tool.SourceFile},
		}
		if tool.SchemaVar != "" {
			evidence = append(evidence, Evidence{Kind: "schema_var", Value: tool.SchemaVar + " (in " + tool.SourceFile + ")"})
		}
		statement := fmt.Sprintf("MCP tool %q registered from %s with profile gate %s", tool.Name, tool.SourceFile, tool.ProfileGate)
		if tool.Mutation {
			statement += "; mutating tool"
		}
		if tool.IsBroker {
			statement += "; context broker surface"
		}
		if tool.IsRefactor {
			statement += "; refactor backlog surface"
		}
		if tool.IsPlanAttempt {
			statement += "; plan attempt surface"
		}
		facts = append(facts, Fact{
			ID:        fmt.Sprintf("mcp-tool-%d", ordinal),
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  evidence,
		})
		ordinal++
	}

	facts = append(facts, Fact{
		ID:        "mcp-registry-profile-gating",
		Label:     FactLabelProven,
		Statement: "Context broker and refactor backlog tools are profile-gated behind context_broker_profile_required. Plan-attempt, plan-seed, run-status, and audit-submission tools are always registered.",
		Evidence: []Evidence{
			{Kind: "source", Value: "internal/mcp/server.go"},
		},
	})

	facts = append(facts, Fact{
		ID:        "mcp-registry-dispatch-switch",
		Label:     FactLabelProven,
		Statement: "Tools are dispatched via switch on params.Name in server.handleToolsCall with profile-gate checks for gated tools returning CodeMethodNotFound when the profile is not enabled.",
		Evidence: []Evidence{
			{Kind: "source", Value: "internal/mcp/server.go"},
		},
	})

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
		},
		Facts:  facts,
		References: []ReferenceEntry{},
	}
	return doc, nil
}
