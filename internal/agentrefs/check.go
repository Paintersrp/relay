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

func CheckOutputs(doc *ReferenceDocument) ([]DiffEntry, error) {
	var diffs []DiffEntry

	expectedJSON, err := RenderJSON(doc)
	if err != nil {
		return nil, fmt.Errorf("render expected JSON: %w", err)
	}

	expectedMD, err := RenderMarkdown(doc)
	if err != nil {
		return nil, fmt.Errorf("render expected Markdown: %w", err)
	}

	existingJSON, err := os.ReadFile(IndexJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			diffs = append(diffs, DiffEntry{Path: IndexJSONPath, Status: "missing"})
		} else {
			return nil, fmt.Errorf("read existing JSON: %w", err)
		}
	} else if !bytes.Equal(bytes.TrimSpace(expectedJSON), bytes.TrimSpace(existingJSON)) {
		diffs = append(diffs, DiffEntry{Path: IndexJSONPath, Status: "stale"})
	}

	existingMD, err := os.ReadFile(IndexMarkdownPath)
	if err != nil {
		if os.IsNotExist(err) {
			diffs = append(diffs, DiffEntry{Path: IndexMarkdownPath, Status: "missing"})
		} else {
			return nil, fmt.Errorf("read existing Markdown: %w", err)
		}
	} else if !bytes.Equal(bytes.TrimSpace(expectedMD), bytes.TrimSpace(existingMD)) {
		diffs = append(diffs, DiffEntry{Path: IndexMarkdownPath, Status: "stale"})
	}

	return diffs, nil
}
