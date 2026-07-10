package agentrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type requiredSourceInput struct {
	path string
	role string
}

var workflowInputs = []requiredSourceInput{
	{"internal/app/workflow/types.go", "workflow types"},
	{"internal/store/workflow/types.go", "workflow store types"},
}

func BuildWorkflowSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	var sourceInputs []SourceInput
	for _, in := range workflowInputs {
		abs := filepath.Join(repoRoot, in.path)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return nil, fmt.Errorf("required workflow source input missing: %s", in.path)
		}
		hash, err := ComputeSHA256(abs)
		if err != nil {
			return nil, fmt.Errorf("hash workflow source input %s: %w", in.path, err)
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   in.path,
			SHA256: hash,
			Role:   in.role,
		})
	}

	facts := []Fact{
		{
			ID:    "workflow-canonical-model",
			Label: FactLabelProven,
			Statement: "Relay uses a canonical workflow model with Projects, Plans, Runs, and Audit. " +
				"Projects organize Plans; Plans contain Passes; Runs are Managed (by Plan/pass) or Standalone. " +
				"Run stages are Specification, Execute, and Audit.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/app/workflow/types.go"},
			},
		},
		{
			ID:    "workflow-persistence",
			Label: FactLabelProven,
			Statement: "Workflow data is persisted in SQLite through internal/store/workflow. " +
				"Canonical bytes are stored immutably with SHA-256 hashes.",
			Evidence: []Evidence{
				{Kind: "source", Value: "internal/store/workflow/types.go"},
			},
		},
	}

	sort.Slice(facts, func(i, j int) bool {
		return facts[i].ID < facts[j].ID
	})

	labels := []FactLabel{
		FactLabelProven,
		FactLabelDerived,
		FactLabelConvention,
		FactLabelUnresolved,
		FactLabelConflict,
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "workflow-surfaces",
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
		FactLabels:   labels,
		Facts:        facts,
		References:   []ReferenceEntry{},
	}

	return doc, nil
}