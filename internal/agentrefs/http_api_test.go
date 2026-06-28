package agentrefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootForTests(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{".", "..", "../.."} {
		if _, err := os.Stat(filepath.Join(cand, "internal", "api", "runs", "routes.go")); err == nil {
			return cand
		}
	}
	t.Skip("cannot find repo root from test working directory")
	return "."
}

func TestBuildHTTPAPISurfaceDoc_SourceInputsIncludeRouteFiles(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	requiredFiles := []string{
		"internal/api/plans/routes.go",
		"internal/api/runs/routes.go",
		"internal/api/artifacts/routes.go",
		"internal/api/intake/routes.go",
		"internal/api/projects/routes.go",
		"internal/api/audits/routes.go",
		"internal/server/routes.go",
	}

	found := make(map[string]bool)
	for _, si := range doc.SourceInputs {
		found[si.Path] = true
	}

	for _, rf := range requiredFiles {
		absPath := filepath.Join(root, rf)
		if _, err := os.Stat(absPath); err != nil {
			continue
		}
		if !found[rf] {
			t.Errorf("expected source input %q, not found", rf)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_KnownRoutes(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	expectedRoutes := []string{
		"/runs/{id}/execute",
		"/runs/{id}/events",
		"/plans",
		"/projects/{projectId}/plans/{planId}/next-pass-work",
		"/projects/{projectId}/plans/{planId}/passes/{passId}/next-pass-work-preview",
		"/mcp",
	}

	for _, expected := range expectedRoutes {
		found := false
		for _, f := range doc.Facts {
			if strings.Contains(f.Statement, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected route %q not found in facts", expected)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_Deterministic(t *testing.T) {
	root := repoRootForTests(t)
	doc1, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	doc2, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}

	b1, _ := RenderJSON(doc1)
	b2, _ := RenderJSON(doc2)

	if string(b1) != string(b2) {
		t.Error("output is not deterministic across two builds")
	}
}

func TestBuildHTTPAPISurfaceDoc_NoWallClockMetadata(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	b, _ := RenderJSON(doc)
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("failed to parse output JSON: %v", err)
	}

	text := string(b)
	for _, forbidden := range []string{"wall_clock", "generated_at", "created_at", "updated_at"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Errorf("output contains wall-clock field: %q", forbidden)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_SourceInputsAreRepoRelative(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	for _, si := range doc.SourceInputs {
		if err := ValidateRepoRelativePath(si.Path); err != nil {
			t.Errorf("source input path %q is not repo-relative: %v", si.Path, err)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_APIPrefixOnFeatureRoutes(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	featureRoutes := []string{
		"/api/runs",
		"/api/plans",
		"/api/projects",
		"/api/audits/local",
		"/api/intake/planner-handoff",
	}

	for _, expected := range featureRoutes {
		found := false
		for _, f := range doc.Facts {
			if strings.Contains(f.Statement, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected API-prefixed route %q not found in facts", expected)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_ServerLevelRoutesPresent(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	serverRoutes := []string{
		"/mcp",
		"/handoffs",
		"/instructions",
		"/settings/repos",
		"/static",
	}

	for _, expected := range serverRoutes {
		found := false
		for _, f := range doc.Facts {
			if strings.Contains(f.Statement, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected server-level route %q not found in facts", expected)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_RendersJSONAndMarkdown(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	jsonData, err := RenderJSON(doc)
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}
	if len(jsonData) == 0 {
		t.Error("rendered JSON is empty")
	}

	mdData, err := RenderMarkdown(doc)
	if err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}
	if len(mdData) == 0 {
		t.Error("rendered Markdown is empty")
	}
}

func TestHTTPAPISurfaceWriteOutputSpec(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "test.json")
	mdPath := filepath.Join(dir, "test.md")

	spec := OutputSpec{
		JSONPath:     jsonPath,
		MarkdownPath: mdPath,
		Document:     doc,
	}

	err = WriteOutputSpec(spec)
	if err != nil {
		t.Fatalf("WriteOutputSpec failed: %v", err)
	}

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Errorf("JSON output not created at %s", jsonPath)
	}
	if _, err := os.Stat(mdPath); os.IsNotExist(err) {
		t.Errorf("Markdown output not created at %s", mdPath)
	}
}

func TestHTTPAPISurfaceCheckModeCoverage(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "test.json")
	mdPath := filepath.Join(dir, "test.md")

	spec := OutputSpec{
		JSONPath:     jsonPath,
		MarkdownPath: mdPath,
		Document:     doc,
	}

	err = WriteOutputSpec(spec)
	if err != nil {
		t.Fatalf("WriteOutputSpec failed: %v", err)
	}

	diffs, err := CheckOutputSpecs([]OutputSpec{
		{
			JSONPath:     jsonPath,
			MarkdownPath: mdPath,
			Document:     doc,
		},
	})
	if err != nil {
		t.Fatalf("CheckOutputSpecs failed: %v", err)
	}

	if len(diffs) != 0 {
		for _, d := range diffs {
			t.Errorf("output %s is %s", d.Path, d.Status)
		}
	}
}

func TestBuildHTTPAPISurfaceDoc_EvidenceIsRepoRelative(t *testing.T) {
	root := repoRootForTests(t)
	doc, err := BuildHTTPAPISurfaceDoc(root)
	if err != nil {
		t.Fatalf("BuildHTTPAPISurfaceDoc failed: %v", err)
	}

	for _, f := range doc.Facts {
		for _, e := range f.Evidence {
			if filepath.IsAbs(e.Value) {
				t.Errorf("evidence value %q is absolute in fact %s", e.Value, f.ID)
			}
		}
	}
}
