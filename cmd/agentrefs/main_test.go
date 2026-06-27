package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/agentrefs"
)

func findRepoRootFromTests(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}

func TestBuildAllOutputSpecs_CoversEveryIndexedGeneratedReference(t *testing.T) {
	repoRoot := findRepoRootFromTests(t)
	specs, err := buildAllOutputSpecs(repoRoot)
	if err != nil {
		t.Fatalf("buildAllOutputSpecs: %v", err)
	}

	if len(specs) == 0 {
		t.Fatal("expected at least one output spec")
	}

	indexSpec := specs[0]
	if indexSpec.JSONPath != agentrefs.IndexJSONPath {
		t.Fatalf("expected index spec first, got %s", indexSpec.JSONPath)
	}

	indexRefs := indexSpec.Document.References
	if len(indexRefs) == 0 {
		t.Fatal("index document has no references")
	}

	refIDs := make(map[string]bool)
	refPaths := make(map[string]bool)
	for _, r := range indexRefs {
		if refIDs[r.ID] {
			t.Errorf("duplicate reference ID %q in index", r.ID)
		}
		refIDs[r.ID] = true
		if refPaths[r.Path] {
			t.Errorf("duplicate reference path %q in index", r.Path)
		}
		refPaths[r.Path] = true
	}

	nonIndexPaths := make(map[string]bool)
	for _, spec := range specs[1:] {
		nonIndexPaths[spec.JSONPath] = true
	}

	for _, r := range indexRefs {
		if !nonIndexPaths[r.Path] {
			t.Errorf("index reference %q path %q not found in non-index output specs", r.ID, r.Path)
		}
	}

	for _, spec := range specs[1:] {
		if !refPaths[spec.JSONPath] {
			t.Errorf("non-index output spec %q not referenced in index", spec.JSONPath)
		}
	}
}

func TestBuildAllOutputSpecs_RenderingIsDeterministic(t *testing.T) {
	repoRoot := findRepoRootFromTests(t)
	specs1, err := buildAllOutputSpecs(repoRoot)
	if err != nil {
		t.Fatalf("buildAllOutputSpecs first call: %v", err)
	}
	specs2, err := buildAllOutputSpecs(repoRoot)
	if err != nil {
		t.Fatalf("buildAllOutputSpecs second call: %v", err)
	}

	if len(specs1) != len(specs2) {
		t.Fatalf("spec count mismatch: %d vs %d", len(specs1), len(specs2))
	}

	for i := range specs1 {
		if specs1[i].JSONPath != specs2[i].JSONPath {
			t.Fatalf("spec %d JSON path mismatch: %s vs %s", i, specs1[i].JSONPath, specs2[i].JSONPath)
		}

		j1, err := agentrefs.RenderJSON(specs1[i].Document)
		if err != nil {
			t.Fatalf("RenderJSON first call for %s: %v", specs1[i].JSONPath, err)
		}
		j2, err := agentrefs.RenderJSON(specs2[i].Document)
		if err != nil {
			t.Fatalf("RenderJSON second call for %s: %v", specs2[i].JSONPath, err)
		}
		if string(j1) != string(j2) {
			t.Errorf("rendering not deterministic for %s: two runs produced different JSON", specs1[i].JSONPath)
		}
	}
}

func TestGeneratedReferenceSpecs_SourceInputsAndEvidenceAreRepoRelative(t *testing.T) {
	repoRoot := findRepoRootFromTests(t)
	specs, err := buildAllOutputSpecs(repoRoot)
	if err != nil {
		t.Fatalf("buildAllOutputSpecs: %v", err)
	}

	for _, spec := range specs {
		for _, si := range spec.Document.SourceInputs {
			if si.Path == "" {
				t.Errorf("spec %s: empty source input path", spec.JSONPath)
				continue
			}
			if err := agentrefs.ValidateRepoRelativePath(si.Path); err != nil {
				t.Errorf("spec %s: source input path %q invalid: %v", spec.JSONPath, si.Path, err)
			}
		}

		for _, fact := range spec.Document.Facts {
			for _, e := range fact.Evidence {
				if e.Value == "" {
					t.Errorf("spec %s fact %s: empty evidence value", spec.JSONPath, fact.ID)
					continue
				}
				evidenceKindsToValidate := map[string]bool{
					"source":    true,
					"schema":    true,
					"contract":  true,
					"policy":    true,
					"reference": true,
				}
				if !evidenceKindsToValidate[e.Kind] {
					continue
				}
				if err := agentrefs.ValidateRepoRelativePath(e.Value); err != nil {
					t.Errorf("spec %s fact %s: evidence %q (%s) invalid: %v", spec.JSONPath, fact.ID, e.Value, e.Kind, err)
				}
			}
		}
	}
}

func TestGeneratedReferenceSpecs_NoWallClockMetadata(t *testing.T) {
	repoRoot := findRepoRootFromTests(t)
	specs, err := buildAllOutputSpecs(repoRoot)
	if err != nil {
		t.Fatalf("buildAllOutputSpecs: %v", err)
	}

	wallClockKeys := []string{"generated_at", "created_at", "updated_at", "timestamp"}

	for _, spec := range specs {
		jsonData, err := agentrefs.RenderJSON(spec.Document)
		if err != nil {
			t.Fatalf("RenderJSON for %s: %v", spec.JSONPath, err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(jsonData, &raw); err != nil {
			t.Fatalf("unmarshal %s: %v", spec.JSONPath, err)
		}

		for _, key := range wallClockKeys {
			if _, ok := raw[key]; ok {
				t.Errorf("spec %s: rendered JSON contains wall-clock metadata key %q", spec.JSONPath, key)
			}
		}

		jsonStr := string(jsonData)
		for _, key := range wallClockKeys {
			if strings.Contains(jsonStr, fmt.Sprintf("%q:", key)) {
				t.Errorf("spec %s: rendered JSON string contains key %q", spec.JSONPath, key)
			}
		}

		mdData, err := agentrefs.RenderMarkdown(spec.Document)
		if err != nil {
			t.Fatalf("RenderMarkdown for %s: %v", spec.JSONPath, err)
		}
		mdStr := string(mdData)
		if strings.Contains(mdStr, "generated_at") {
			t.Errorf("spec %s: rendered Markdown contains %q", spec.JSONPath, "generated_at")
		}
	}
}
