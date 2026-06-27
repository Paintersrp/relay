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

func TestBuildWorkflowSurfaceDoc_ContractSourceInputs(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	requiredContractPaths := []string{
		"relay-contracts/contracts/intent_drift_review_contract.md",
		"relay-contracts/contracts/planner_mcp_plan_attempt_contract.md",
		"relay-contracts/contracts/refactor_backlog_contract.md",
		"relay-contracts/policies/pipeline_lifecycle_policy.md",
	}

	sourceInputPaths := make(map[string]SourceInput)
	for _, si := range doc.SourceInputs {
		sourceInputPaths[si.Path] = si
	}

	for _, p := range requiredContractPaths {
		si, ok := sourceInputPaths[p]
		if !ok {
			t.Errorf("required relay-contracts source input %q not found in source_inputs", p)
			continue
		}
		if si.Path != p {
			t.Errorf("source input path mismatch: got %q, want %q", si.Path, p)
		}
		if len(si.SHA256) != 64 {
			t.Errorf("source input %q SHA256 length: got %d, want 64", p, len(si.SHA256))
		}
	}
}

func TestBuildWorkflowSurfaceDoc_RefactorCandidateStatuses(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	var fact *Fact
	for i := range doc.Facts {
		if doc.Facts[i].ID == "workflow-refactor-backlog-candidate-model" {
			fact = &doc.Facts[i]
			break
		}
	}
	if fact == nil {
		t.Fatal("workflow-refactor-backlog-candidate-model fact not found")
	}

	requiredStatuses := []string{
		"ready",
		"scheduled",
		"scheduled_revision_required",
		"completed",
		"completed_with_warnings",
		"deferred",
		"rejected",
		"superseded",
	}

	for _, s := range requiredStatuses {
		if !strings.Contains(fact.Statement, s) {
			t.Errorf("refactor candidate fact statement missing status %q", s)
		}
	}

	expectedEvidences := map[string]bool{
		"internal/refactors/types.go":                            false,
		"relay-contracts/contracts/refactor_backlog_contract.md": false,
	}
	for _, e := range fact.Evidence {
		expectedEvidences[e.Value] = true
	}
	for path, found := range expectedEvidences {
		if !found {
			t.Errorf("refactor candidate fact missing evidence for %q", path)
		}
	}
}

func TestBuildWorkflowSurfaceDoc_GapFacts(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	requiredGapFactIDs := []string{
		"workflow-gap-contract-runtime-comparison",
		"workflow-gap-untested-state-values",
		"workflow-gap-lifecycle-observed-writes",
		"workflow-gap-transport-coverage",
	}

	factMap := make(map[string]Fact)
	for _, f := range doc.Facts {
		factMap[f.ID] = f
	}

	for _, id := range requiredGapFactIDs {
		fact, ok := factMap[id]
		if !ok {
			t.Errorf("required gap fact ID %q not found", id)
			continue
		}
		if fact.Label == FactLabelProven {
			t.Errorf("gap fact %q is labeled proven but should not be unless implementation includes full deterministic coverage", id)
		}
	}
}

func TestBuildWorkflowSurfaceDoc_EvidenceRelativity(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	for _, fact := range doc.Facts {
		for _, e := range fact.Evidence {
			if e.Value == "" {
				t.Errorf("fact %q has empty evidence value", fact.ID)
				continue
			}
			if err := ValidateRepoRelativePath(e.Value); err != nil {
				t.Errorf("fact %q evidence %q is not a valid repo-relative path: %v", fact.ID, e.Value, err)
			}
		}
	}
}

func TestBuildWorkflowSurfaceDoc_MissingContractReturnsError(t *testing.T) {
	dir := t.TempDir()

	contractsDir := filepath.Join(dir, "relay-contracts", "contracts")
	if err := os.MkdirAll(contractsDir, 0755); err != nil {
		t.Fatal(err)
	}
	policiesDir := filepath.Join(dir, "relay-contracts", "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		filepath.Join(contractsDir, "intent_drift_review_contract.md"),
		filepath.Join(contractsDir, "planner_mcp_plan_attempt_contract.md"),
		filepath.Join(contractsDir, "refactor_backlog_contract.md"),
	} {
		if err := os.WriteFile(p, []byte("placeholder"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := BuildWorkflowSurfaceDoc(dir)
	if err == nil {
		t.Fatal("expected error for missing pipeline lifecycle policy, got nil")
	}
	missingPath := "relay-contracts/policies/pipeline_lifecycle_policy.md"
	if !strings.Contains(err.Error(), missingPath) {
		t.Errorf("error should name missing path %q, got: %v", missingPath, err)
	}
}

func TestBuildWorkflowSurfaceDoc_NoEvidenceIsAbsolute(t *testing.T) {
	doc, err := BuildWorkflowSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildWorkflowSurfaceDoc: %v", err)
	}

	for _, fact := range doc.Facts {
		for _, e := range fact.Evidence {
			if strings.HasPrefix(e.Value, "/") {
				t.Errorf("fact %q evidence %q should not be absolute", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "..") {
				t.Errorf("fact %q evidence %q should not contain '..'", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "\\") {
				t.Errorf("fact %q evidence %q should not contain backslashes", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "\n") {
				t.Errorf("fact %q evidence %q should not contain newlines", fact.ID, e.Value)
			}
		}
	}
}

func buildTempStorageRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	sqlcYAML := `version: "2"
sql:
  - engine: "sqlite"
    queries: "internal/db/queries"
    schema: "internal/db/migrations"
    gen:
      go:
        package: "generated"
        out: "internal/store/generated"
        emit_json_tags: true
        emit_empty_slices: true
`
	if err := os.MkdirAll(filepath.Join(dir, "internal", "store"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sqlc.yaml"), []byte(sqlcYAML), 0644); err != nil {
		t.Fatal(err)
	}

	queries := map[string]string{
		"internal/db/queries/plans.sql": `-- name: CreatePlan :one
INSERT INTO plans (plan_id) VALUES (?) RETURNING *;
`,
		"internal/db/queries/plan_attempts.sql": `-- name: CreatePlanAttempt :one
INSERT INTO plan_attempts (plan_attempt_id) VALUES (?) RETURNING *;
`,
		"internal/db/queries/refactor_backlog.sql": `-- name: CreateRefactorCandidate :one
INSERT INTO refactor_candidates (candidate_id) VALUES (?) RETURNING *;
`,
	}
	for p, content := range queries {
		abs := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	migration := `-- +goose Up
CREATE TABLE plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT
);
`
	migrationPath := filepath.Join(dir, "internal", "db", "migrations", "0001_init.sql")
	if err := os.MkdirAll(filepath.Dir(migrationPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(migrationPath, []byte(migration), 0644); err != nil {
		t.Fatal(err)
	}

	storeDB := `package store

type Repo = generated.Repo

type Store struct{}

func (s *Store) CreateRepo(name, path string) (*Repo, error) { return nil, nil }
`
	if err := os.WriteFile(filepath.Join(dir, "internal", "store", "db.go"), []byte(storeDB), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestBuildStorageSurfaceDoc_EmitsRequiredFacts(t *testing.T) {
	doc, err := BuildStorageSurfaceDoc(buildTempStorageRepo(t))
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
	}

	if doc.ReferenceID != "storage-surface" {
		t.Errorf("ReferenceID = %q, want storage-surface", doc.ReferenceID)
	}

	var hasSQLCConfig bool
	for _, f := range doc.Facts {
		if strings.Contains(f.Statement, "sqlc configuration") {
			hasSQLCConfig = true
			break
		}
	}
	if !hasSQLCConfig {
		t.Error("no fact describes sqlc configuration")
	}

	requiredNames := []string{"CreatePlan", "CreatePlanAttempt", "CreateRefactorCandidate"}
	found := make(map[string]bool)
	for _, f := range doc.Facts {
		for _, name := range requiredNames {
			if strings.Contains(f.Statement, name) {
				found[name] = true
			}
		}
	}
	for _, name := range requiredNames {
		if !found[name] {
			t.Errorf("required query name %q not found in any storage surface fact statement", name)
		}
	}
}

func TestBuildStorageSurfaceDoc_Deterministic(t *testing.T) {
	dir := buildTempStorageRepo(t)

	doc1, err := BuildStorageSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc (1): %v", err)
	}
	doc2, err := BuildStorageSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc (2): %v", err)
	}

	j1, err := RenderJSON(doc1)
	if err != nil {
		t.Fatalf("RenderJSON (1): %v", err)
	}
	j2, err := RenderJSON(doc2)
	if err != nil {
		t.Fatalf("RenderJSON (2): %v", err)
	}
	if string(j1) != string(j2) {
		t.Error("BuildStorageSurfaceDoc is not deterministic: two runs produced different JSON")
	}
}

func TestBuildStorageSurfaceDoc_RendersJSONAndMarkdown(t *testing.T) {
	doc, err := BuildStorageSurfaceDoc(buildTempStorageRepo(t))
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
	}

	jsonData, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var parsed ReferenceDocument
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Unmarshal JSON: %v", err)
	}

	md, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(string(md), "storage-surface") {
		t.Error("Markdown should contain reference ID")
	}
}

func TestBuildStorageSurfaceDoc_SourceInputsAreRepoRelative(t *testing.T) {
	doc, err := BuildStorageSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
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

func TestBuildStorageSurfaceDoc_NoEvidenceIsAbsolute(t *testing.T) {
	doc, err := BuildStorageSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
	}

	for _, fact := range doc.Facts {
		for _, e := range fact.Evidence {
			if strings.HasPrefix(e.Value, "/") {
				t.Errorf("fact %q evidence %q should not be absolute", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "..") {
				t.Errorf("fact %q evidence %q should not contain '..'", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "\\") {
				t.Errorf("fact %q evidence %q should not contain backslashes", fact.ID, e.Value)
			}
			if strings.Contains(e.Value, "\n") {
				t.Errorf("fact %q evidence %q should not contain newlines", fact.ID, e.Value)
			}
		}
	}
}

func TestBuildStorageSurfaceDoc_GeneratedBoundaryIsNotRequired(t *testing.T) {
	dir := buildTempStorageRepo(t)

	doc, err := BuildStorageSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
	}

	for _, f := range doc.Facts {
		if f.Label == FactLabelProven && strings.Contains(f.Statement, "Generated sqlc boundary") {
			t.Errorf("proven fact should not claim generated boundary: %s", f.Statement)
		}
	}
}

func TestStorageOutputSpec_CheckModeCoverage(t *testing.T) {
	doc, err := BuildStorageSurfaceDoc(findRepoRoot(t))
	if err != nil {
		t.Fatalf("BuildStorageSurfaceDoc: %v", err)
	}

	spec := OutputSpec{
		JSONPath:     filepath.Join(os.TempDir(), "opencode-test-storage-check.json"),
		MarkdownPath: filepath.Join(os.TempDir(), "opencode-test-storage-check.md"),
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
