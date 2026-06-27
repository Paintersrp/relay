package agentrefs

import (
	"bytes"
	"fmt"
	"os"
)

type DiffEntry struct {
	Path   string
	Status string
}

type OutputSpec struct {
	JSONPath     string
	MarkdownPath string
	Document     *ReferenceDocument
}

func CheckOutputs(doc *ReferenceDocument) ([]DiffEntry, error) {
	return CheckOutputSpecs([]OutputSpec{
		{
			JSONPath:     IndexJSONPath,
			MarkdownPath: IndexMarkdownPath,
			Document:     doc,
		},
	})
}

func CheckOutputSpecs(specs []OutputSpec) ([]DiffEntry, error) {
	var diffs []DiffEntry

	for _, spec := range specs {
		expectedJSON, err := RenderJSON(spec.Document)
		if err != nil {
			return nil, fmt.Errorf("render expected JSON for %s: %w", spec.JSONPath, err)
		}

		expectedMD, err := RenderMarkdown(spec.Document)
		if err != nil {
			return nil, fmt.Errorf("render expected Markdown for %s: %w", spec.MarkdownPath, err)
		}

		existingJSON, err := os.ReadFile(spec.JSONPath)
		if err != nil {
			if os.IsNotExist(err) {
				diffs = append(diffs, DiffEntry{Path: spec.JSONPath, Status: "missing"})
			} else {
				return nil, fmt.Errorf("read existing JSON %s: %w", spec.JSONPath, err)
			}
		} else if !bytes.Equal(bytes.TrimSpace(expectedJSON), bytes.TrimSpace(existingJSON)) {
			diffs = append(diffs, DiffEntry{Path: spec.JSONPath, Status: "stale"})
		}

		existingMD, err := os.ReadFile(spec.MarkdownPath)
		if err != nil {
			if os.IsNotExist(err) {
				diffs = append(diffs, DiffEntry{Path: spec.MarkdownPath, Status: "missing"})
			} else {
				return nil, fmt.Errorf("read existing Markdown %s: %w", spec.MarkdownPath, err)
			}
		} else if !bytes.Equal(bytes.TrimSpace(expectedMD), bytes.TrimSpace(existingMD)) {
			diffs = append(diffs, DiffEntry{Path: spec.MarkdownPath, Status: "stale"})
		}
	}

	return diffs, nil
}

func WriteOutputSpec(spec OutputSpec) error {
	jsonData, err := RenderJSON(spec.Document)
	if err != nil {
		return fmt.Errorf("render JSON: %w", err)
	}

	if err := os.MkdirAll(OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if err := os.WriteFile(spec.JSONPath, jsonData, 0644); err != nil {
		return fmt.Errorf("write JSON %s: %w", spec.JSONPath, err)
	}
	fmt.Printf("wrote %s\n", spec.JSONPath)

	mdData, err := RenderMarkdown(spec.Document)
	if err != nil {
		return fmt.Errorf("render Markdown: %w", err)
	}

	if err := os.WriteFile(spec.MarkdownPath, mdData, 0644); err != nil {
		return fmt.Errorf("write Markdown %s: %w", spec.MarkdownPath, err)
	}
	fmt.Printf("wrote %s\n", spec.MarkdownPath)

	return nil
}
