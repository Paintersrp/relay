package agentrefs

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		t.Fatal("expected at least one diff, got none")
	}
}

func TestCheckOutputSpecs_ReturnsDiffsForMissingFile(t *testing.T) {
	spec := OutputSpec{
		JSONPath:     "/tmp/nonexistent-check-test.json",
		MarkdownPath: "/tmp/nonexistent-check-test.md",
		Document: &ReferenceDocument{
			SchemaVersion: "1.0.0",
			ReferenceID:   "spec-test",
			Repo:          RepoIdentity{ProjectID: "p", RepoID: "r", Branch: "main"},
			GeneratedBy:   GeneratorIdentity{Name: "test", Version: "0.1.0"},
			Rendering: RenderingContract{
				JSONPrimary: true, MarkdownFromJSON: true,
				DeterministicSort: true, NoTimestamps: true, RelativePathsOnly: true,
			},
			FactLabels: []FactLabel{FactLabelProven},
			Facts:      []Fact{{ID: "f1", Label: FactLabelProven, Statement: "test"}},
		},
	}

	diffs, err := CheckOutputSpecs([]OutputSpec{spec})
	if err != nil {
		t.Fatalf("CheckOutputSpecs: %v", err)
	}
	if len(diffs) < 2 {
		t.Fatal("expected at least 2 diffs (both JSON and Markdown missing), got", len(diffs))
	}
}

func TestWriteOutputSpec_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	jsonPath := dir + "/test.json"
	mdPath := dir + "/test.md"

	spec := OutputSpec{
		JSONPath:     jsonPath,
		MarkdownPath: mdPath,
		Document: &ReferenceDocument{
			SchemaVersion: "1.0.0",
			ReferenceID:   "write-test",
			Repo:          RepoIdentity{ProjectID: "p", RepoID: "r", Branch: "main"},
			GeneratedBy:   GeneratorIdentity{Name: "test", Version: "0.1.0"},
			Rendering: RenderingContract{
				JSONPrimary: true, MarkdownFromJSON: true,
				DeterministicSort: true, NoTimestamps: true, RelativePathsOnly: true,
			},
			FactLabels: []FactLabel{FactLabelProven},
			Facts:      []Fact{{ID: "f1", Label: FactLabelProven, Statement: "test"}},
		},
	}

	if err := WriteOutputSpec(spec); err != nil {
		t.Fatalf("WriteOutputSpec: %v", err)
	}

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Error("JSON file was not written")
	}
	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		t.Error("Markdown file was not written")
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

func findRepoRoot(t *testing.T) string {
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

func TestWorkflowOutputSpec_CheckModeCoverage(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	spec := OutputSpec{
		JSONPath:     filepath.Join(os.TempDir(), "opencode-test-workflow-check.json"),
		MarkdownPath: filepath.Join(os.TempDir(), "opencode-test-workflow-check.md"),
		Document:     doc,
	}

	diffs, err := CheckOutputSpecs([]OutputSpec{spec})
	if err != nil {
		t.Fatalf("CheckOutputSpecs: %v", err)
	}
	if len(diffs) < 2 {
		t.Fatal("expected at least 2 diffs (both JSON and Markdown missing), got", len(diffs))
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

func TestBuildWorkflowSurfaceDoc_EmitsRequiredFacts(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	requiredIDs := []string{
		"workflow-plan-attempt-status-model",
		"workflow-intent-packet-lineage",
		"workflow-review-packet-retrieval-only",
		"workflow-drift-review-submit-boundary",
		"workflow-approval-submit-gates",
		"workflow-refactor-backlog-candidate-model",
		"workflow-refactor-mcp-safety-boundaries",
		"workflow-next-pass-work-blockers",
		"workflow-work-packet-read-only",
		"workflow-route-touchpoints",
	}

	factMap := make(map[string]bool)
	for _, f := range doc.Facts {
		factMap[f.ID] = true
	}

	for _, id := range requiredIDs {
		if !factMap[id] {
			t.Errorf("required fact ID %q not found in workflow surface doc", id)
		}
	}
}

func TestBuildWorkflowSurfaceDoc_SourceInputsAreRepoRelative(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	for _, si := range doc.SourceInputs {
		if si.Path == "" {
			t.Error("found empty source input path")
			continue
		}
		if err := ValidateRepoRelativePath(si.Path); err != nil {
			t.Errorf("source input path %q is not a valid repo-relative path: %v", si.Path, err)
		}
	}
}

func TestBuildWorkflowSurfaceDoc_NoWallClockTimestamps(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	jsonData, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	jsonStr := string(jsonData)
	if strings.Contains(jsonStr, "generated_at") {
		t.Error("JSON should not contain 'generated_at'")
	}
	if strings.Contains(jsonStr, "\"created_at\"") {
		t.Error("JSON should not contain 'created_at' as a metadata field")
	}
	if strings.Contains(jsonStr, "\"updated_at\"") {
		t.Error("JSON should not contain 'updated_at' as a metadata field")
	}

	mdData, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	mdStr := string(mdData)
	if strings.Contains(mdStr, "generated_at") {
		t.Error("Markdown should not contain 'generated_at'")
	}
}
