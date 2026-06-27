package agentrefs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestValidateRepoRelativePath_AcceptsNormal(t *testing.T) {
	cases := []string{
		"foo.txt",
		"foo/bar.txt",
		"a/b/c/d.go",
		"docs/generated/agent-references/index.json",
		"internal/agentrefs/types.go",
	}
	for _, c := range cases {
		if err := ValidateRepoRelativePath(c); err != nil {
			t.Errorf("ValidateRepoRelativePath(%q) = %v, want nil", c, err)
		}
	}
}

func TestValidateRepoRelativePath_RejectsInvalid(t *testing.T) {
	cases := []string{
		"/absolute/path",
		"../escape",
		"nested/../../escape",
		"path\\with\\backslashes",
		"path/with\nnewline",
		"path/with\rnewline",
	}
	for _, c := range cases {
		if err := ValidateRepoRelativePath(c); err == nil {
			t.Errorf("ValidateRepoRelativePath(%q) = nil, want error", c)
		}
	}
}

func TestValidateRepoRelativePath_RejectsEmpty(t *testing.T) {
	if err := ValidateRepoRelativePath(""); err == nil {
		t.Error("ValidateRepoRelativePath(\"\") = nil, want error")
	}
}

func TestDeterministicSorting_SourceInputs(t *testing.T) {
	doc := &ReferenceDocument{
		SourceInputs: []SourceInput{
			{Path: "z.go", Role: "third", SHA256: "a"},
			{Path: "a.go", Role: "first", SHA256: "b"},
			{Path: "m.go", Role: "second", SHA256: "c"},
			{Path: "a.go", Role: "second", SHA256: "d"},
		},
		FactLabels: []FactLabel{
			FactLabelConflict,
			FactLabelDerived,
			FactLabelProven,
		},
		Facts: []Fact{
			{ID: "z-fact", Statement: "z"},
			{ID: "a-fact", Statement: "a"},
		},
		References: []ReferenceEntry{
			{ID: "ref-b", Path: "b.go", Description: "b"},
			{ID: "ref-a", Path: "a.go", Description: "a"},
		},
	}

	sortDocument(doc)

	if doc.SourceInputs[0].Path != "a.go" || doc.SourceInputs[0].Role != "first" {
		t.Errorf("source input 0: got %s/%s", doc.SourceInputs[0].Path, doc.SourceInputs[0].Role)
	}
	if doc.SourceInputs[1].Path != "a.go" || doc.SourceInputs[1].Role != "second" {
		t.Errorf("source input 1: got %s/%s", doc.SourceInputs[1].Path, doc.SourceInputs[1].Role)
	}
	if doc.SourceInputs[2].Path != "m.go" {
		t.Errorf("source input 2: got %s", doc.SourceInputs[2].Path)
	}
	if doc.SourceInputs[3].Path != "z.go" {
		t.Errorf("source input 3: got %s", doc.SourceInputs[3].Path)
	}

	expectedLabelOrder := []FactLabel{FactLabelProven, FactLabelDerived, FactLabelConflict}
	for i, l := range doc.FactLabels {
		if l != expectedLabelOrder[i] {
			t.Errorf("fact label %d: got %s, want %s", i, l, expectedLabelOrder[i])
		}
	}

	if doc.Facts[0].ID != "a-fact" || doc.Facts[1].ID != "z-fact" {
		t.Errorf("fact order: got %s, %s", doc.Facts[0].ID, doc.Facts[1].ID)
	}

	if doc.References[0].ID != "ref-a" || doc.References[1].ID != "ref-b" {
		t.Errorf("reference order: got %s, %s", doc.References[0].ID, doc.References[1].ID)
	}
}

func TestMarkdownFromJSON(t *testing.T) {
	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "test-ref",
		Repo: RepoIdentity{
			ProjectID: "test-project",
			RepoID:    "test/test",
			Branch:    "main",
		},
		GeneratedBy: GeneratorIdentity{Name: "test", Version: "0.1.0"},
		Rendering: RenderingContract{
			JSONPrimary: true, MarkdownFromJSON: true,
			DeterministicSort: true, NoTimestamps: true, RelativePathsOnly: true,
		},
		SourceInputs: []SourceInput{
			{Path: "a.txt", SHA256: "abc123", Role: "test-input"},
		},
		FactLabels: []FactLabel{FactLabelProven, FactLabelDerived},
		Facts: []Fact{
			{ID: "fact-1", Label: FactLabelProven, Statement: "Test statement."},
		},
		References: []ReferenceEntry{
			{ID: "ref-1", Kind: "source", Path: "a.txt", Description: "A test file"},
		},
	}

	md, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	mdStr := string(md)

	if !strings.Contains(mdStr, "test-ref") {
		t.Error("Markdown should contain reference ID")
	}
	if !strings.Contains(mdStr, "Test statement.") {
		t.Error("Markdown should contain fact statement")
	}
	if !strings.Contains(mdStr, "test-project") {
		t.Error("Markdown should contain project ID")
	}
	if !strings.Contains(mdStr, "abc123") {
		t.Error("Markdown should contain SHA256")
	}
	if !strings.Contains(mdStr, "A test file") {
		t.Error("Markdown should contain reference description")
	}

	jsonData, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var parsed ReferenceDocument
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Unmarshal JSON: %v", err)
	}

	md2, err := RenderMarkdown(&parsed)
	if err != nil {
		t.Fatalf("RenderMarkdown from JSON: %v", err)
	}
	if string(md) != string(md2) {
		t.Error("Markdown from original doc and from JSON roundtrip should match")
	}
}

func TestNoWallClockTimestamps(t *testing.T) {
	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "timestamp-test",
		Repo:          RepoIdentity{ProjectID: "p", RepoID: "r", Branch: "main"},
		GeneratedBy:   GeneratorIdentity{Name: "test", Version: "0.1.0"},
		Rendering: RenderingContract{
			JSONPrimary: true, MarkdownFromJSON: true,
			DeterministicSort: true, NoTimestamps: true, RelativePathsOnly: true,
		},
		FactLabels: []FactLabel{FactLabelProven},
		Facts:      []Fact{{ID: "f1", Label: FactLabelProven, Statement: "test"}},
	}

	jsonData, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	jsonStr := string(jsonData)
	if strings.Contains(jsonStr, "generated_at") {
		t.Error("JSON should not contain 'generated_at'")
	}
	if strings.Contains(jsonStr, "\"generated_ts\"") {
		t.Error("JSON should not contain 'generated_ts'")
	}

	md, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	mdStr := string(md)
	if strings.Contains(mdStr, "generated_at") {
		t.Error("Markdown should not contain 'generated_at'")
	}
}

func TestCheckModeDetectsStale(t *testing.T) {
	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "check-test",
		Repo:          RepoIdentity{ProjectID: "p", RepoID: "r", Branch: "main"},
		GeneratedBy:   GeneratorIdentity{Name: "test", Version: "0.1.0"},
		Rendering: RenderingContract{
			JSONPrimary: true, MarkdownFromJSON: true,
			DeterministicSort: true, NoTimestamps: true, RelativePathsOnly: true,
		},
		FactLabels: []FactLabel{FactLabelProven},
		Facts:      []Fact{{ID: "f1", Label: FactLabelProven, Statement: "test"}},
	}

	diffs, err := CheckOutputs(doc)
	if err != nil {
		t.Fatalf("CheckOutputs: %v", err)
	}

	if len(diffs) < 1 {
		t.Fatal("expected at least one diff (missing files), got none")
	}

	foundMissing := false
	for _, d := range diffs {
		if d.Status == "missing" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Error("expected missing-file diffs when files don't exist")
	}
}

func TestComputeSHA256(t *testing.T) {
	tmp, err := os.CreateTemp("", "agentrefs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString("test content"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	hash, err := ComputeSHA256(tmp.Name())
	if err != nil {
		t.Fatalf("ComputeSHA256: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(hash))
	}
}

func TestValidateRepoRelativePath_RejectsNewline(t *testing.T) {
	cases := []string{
		"path/with\nnewline",
		"path/with\rnewline",
		"path/with\r\nnewline",
	}
	for _, c := range cases {
		if err := ValidateRepoRelativePath(c); err == nil {
			t.Errorf("ValidateRepoRelativePath(%q) = nil, want error", c)
		}
	}
}
