package main

import (
	"fmt"
	"log"
	"os"

	"relay/internal/agentrefs"
)

func buildFoundationDoc() *agentrefs.ReferenceDocument {
	sourcePaths := []struct {
		path string
		role string
	}{
		{"AGENTS.md", "agent instructions"},
		{"docs/agent-reference.md", "agent reference"},
		{"docs/backend-code-surface-map.md", "backend surface map"},
		{"Makefile", "build configuration"},
		{"schema/project_agent_reference.schema.json", "schema contract"},
	}

	var inputs []agentrefs.SourceInput
	for _, sp := range sourcePaths {
		hash, err := agentrefs.ComputeSHA256(sp.path)
		if err != nil {
			log.Printf("WARNING: could not hash %s: %v", sp.path, err)
			hash = "unavailable"
		}
		inputs = append(inputs, agentrefs.SourceInput{
			Path:   sp.path,
			SHA256: hash,
			Role:   sp.role,
		})
	}

	labels := []agentrefs.FactLabel{
		agentrefs.FactLabelProven,
		agentrefs.FactLabelDerived,
		agentrefs.FactLabelConvention,
		agentrefs.FactLabelUnresolved,
		agentrefs.FactLabelConflict,
	}

	facts := []agentrefs.Fact{
		{
			ID:        "foundation-scope",
			Label:     agentrefs.FactLabelProven,
			Statement: "PASS-001 creates only foundation/index output. No domain scanners are implemented in this pass.",
			Evidence: []agentrefs.Evidence{
				{Kind: "schema", Value: "schema/project_agent_reference.schema.json"},
				{Kind: "source", Value: "cmd/agentrefs/main.go"},
			},
		},
	}

	doc := &agentrefs.ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "project-agent-references-index",
		Repo: agentrefs.RepoIdentity{
			ProjectID: "relay",
			RepoID:    "Paintersrp/relay",
			Branch:    "main",
		},
		GeneratedBy: agentrefs.GeneratorIdentity{
			Name:    "relay-agentrefs",
			Version: "0.1.0",
		},
		Rendering: agentrefs.RenderingContract{
			JSONPrimary:       true,
			MarkdownFromJSON:  true,
			DeterministicSort: true,
			NoTimestamps:      true,
			RelativePathsOnly: true,
		},
		SourceInputs: inputs,
		FactLabels:   labels,
		Facts:        facts,
		References: []agentrefs.ReferenceEntry{
			{
				ID:          "backend-surface",
				Kind:        "generated_reference",
				Path:        agentrefs.BackendSurfaceJSONPath,
				Description: "Generated backend package, service, handler, symbol, import-edge, and adjacent-test surface reference.",
			},
			{
				ID:          "storage-surface",
				Kind:        "generated_reference",
				Path:        agentrefs.StorageSurfaceJSONPath,
				Description: "Generated storage, migration, SQL query, sqlc-boundary, and store-wrapper surface reference.",
			},
			{
				ID:          "workflow-surfaces",
				Kind:        "generated_reference",
				Path:        agentrefs.WorkflowSurfaceJSONPath,
				Description: "Generated Plan v2 workflow, intent packet, drift review, refactor backlog, and work-packet lifecycle surface reference.",
			},
			{
				ID:          "mcp-registry",
				Kind:        "generated_reference",
				Path:        agentrefs.MCPSurfaceJSONPath,
				Description: "Generated MCP action registry reference: tool definitions, dispatch handlers, profile gating, mutating vs retrieval-only behavior, and forbidden side effects.",
			},
			{
				ID:          "http-api-surface",
				Kind:        "generated_reference",
				Path:        agentrefs.HTTPAPISurfaceJSONPath,
				Description: "Generated HTTP/API route surface reference: method, path, handler, source file, and route group from route source files.",
			},
		},
	}

	return doc
}

func runGenerate() error {
	if err := os.MkdirAll(agentrefs.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	indexDoc := buildFoundationDoc()
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.IndexJSONPath,
		MarkdownPath: agentrefs.IndexMarkdownPath,
		Document:     indexDoc,
	}); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	backendDoc, err := agentrefs.BuildBackendSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build backend surface doc: %w", err)
	}
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.BackendSurfaceJSONPath,
		MarkdownPath: agentrefs.BackendSurfaceMarkdownPath,
		Document:     backendDoc,
	}); err != nil {
		return fmt.Errorf("write backend surface: %w", err)
	}

	workflowDoc, err := agentrefs.BuildWorkflowSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build workflow surface doc: %w", err)
	}
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.WorkflowSurfaceJSONPath,
		MarkdownPath: agentrefs.WorkflowSurfaceMarkdownPath,
		Document:     workflowDoc,
	}); err != nil {
		return fmt.Errorf("write workflow surface: %w", err)
	}

	storageDoc, err := agentrefs.BuildStorageSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build storage surface doc: %w", err)
	}
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.StorageSurfaceJSONPath,
		MarkdownPath: agentrefs.StorageSurfaceMarkdownPath,
		Document:     storageDoc,
	}); err != nil {
		return fmt.Errorf("write storage surface: %w", err)
	}

	mcpDoc, err := agentrefs.BuildMCPSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build MCP surface doc: %w", err)
	}
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.MCPSurfaceJSONPath,
		MarkdownPath: agentrefs.MCPSurfaceMarkdownPath,
		Document:     mcpDoc,
	}); err != nil {
		return fmt.Errorf("write MCP surface: %w", err)
	}

	httpAPIDoc, err := agentrefs.BuildHTTPAPISurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build HTTP API surface doc: %w", err)
	}
	if err := agentrefs.WriteOutputSpec(agentrefs.OutputSpec{
		JSONPath:     agentrefs.HTTPAPISurfaceJSONPath,
		MarkdownPath: agentrefs.HTTPAPISurfaceMarkdownPath,
		Document:     httpAPIDoc,
	}); err != nil {
		return fmt.Errorf("write HTTP API surface: %w", err)
	}

	return nil
}

func runCheck() error {
	indexDoc := buildFoundationDoc()

	backendDoc, err := agentrefs.BuildBackendSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build backend surface doc: %w", err)
	}

	workflowDoc, err := agentrefs.BuildWorkflowSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build workflow surface doc: %w", err)
	}

	storageDoc, err := agentrefs.BuildStorageSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build storage surface doc: %w", err)
	}

	mcpDoc, err := agentrefs.BuildMCPSurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build MCP surface doc: %w", err)
	}

	httpAPIDoc, err := agentrefs.BuildHTTPAPISurfaceDoc(".")
	if err != nil {
		return fmt.Errorf("build HTTP API surface doc: %w", err)
	}

	diffs, err := agentrefs.CheckOutputSpecs([]agentrefs.OutputSpec{
		{
			JSONPath:     agentrefs.IndexJSONPath,
			MarkdownPath: agentrefs.IndexMarkdownPath,
			Document:     indexDoc,
		},
		{
			JSONPath:     agentrefs.BackendSurfaceJSONPath,
			MarkdownPath: agentrefs.BackendSurfaceMarkdownPath,
			Document:     backendDoc,
		},
		{
			JSONPath:     agentrefs.StorageSurfaceJSONPath,
			MarkdownPath: agentrefs.StorageSurfaceMarkdownPath,
			Document:     storageDoc,
		},
		{
			JSONPath:     agentrefs.WorkflowSurfaceJSONPath,
			MarkdownPath: agentrefs.WorkflowSurfaceMarkdownPath,
			Document:     workflowDoc,
		},
		{
			JSONPath:     agentrefs.MCPSurfaceJSONPath,
			MarkdownPath: agentrefs.MCPSurfaceMarkdownPath,
			Document:     mcpDoc,
		},
		{
			JSONPath:     agentrefs.HTTPAPISurfaceJSONPath,
			MarkdownPath: agentrefs.HTTPAPISurfaceMarkdownPath,
			Document:     httpAPIDoc,
		},
	})
	if err != nil {
		return fmt.Errorf("check outputs: %w", err)
	}
	if len(diffs) == 0 {
		fmt.Println("all outputs up to date")
		return nil
	}
	for _, d := range diffs {
		fmt.Printf("%s: %s\n", d.Path, d.Status)
	}
	return fmt.Errorf("found %d stale or missing output(s)", len(diffs))
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: agentrefs [generate|check]")
	}
	switch os.Args[1] {
	case "generate":
		if err := runGenerate(); err != nil {
			log.Fatal(err)
		}
	case "check":
		if err := runCheck(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown mode: %s (use generate or check)", os.Args[1])
	}
}
