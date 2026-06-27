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
		References:   []agentrefs.ReferenceEntry{},
	}

	return doc
}

func runGenerate() error {
	if err := os.MkdirAll(agentrefs.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	doc := buildFoundationDoc()

	jsonData, err := agentrefs.RenderJSON(doc)
	if err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}

	if err := os.WriteFile(agentrefs.IndexJSONPath, jsonData, 0644); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	fmt.Printf("wrote %s\n", agentrefs.IndexJSONPath)

	mdData, err := agentrefs.RenderMarkdown(doc)
	if err != nil {
		return fmt.Errorf("render Markdown: %w", err)
	}

	if err := os.WriteFile(agentrefs.IndexMarkdownPath, mdData, 0644); err != nil {
		return fmt.Errorf("write Markdown: %w", err)
	}
	fmt.Printf("wrote %s\n", agentrefs.IndexMarkdownPath)

	return nil
}

func runCheck() error {
	doc := buildFoundationDoc()
	diffs, err := agentrefs.CheckOutputs(doc)
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
