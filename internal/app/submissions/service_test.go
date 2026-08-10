package submissions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	workflowprojects "relay/internal/app/projects/workflow"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

type submissionFixture struct {
	store   *workflowstore.Store
	root    string
	service *Service
	project workflowstore.Project
}

func newSubmissionFixture(t *testing.T) *submissionFixture {
	t.Helper()
	store := workflowfixture.Open(t, workflowstore.Open)
	root := filepath.Dir(store.ArtifactStore().Root())
	repositoryPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(context.Background(), "relay", repositoryPath); err != nil {
		t.Fatal(err)
	}
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.CreateProject(context.Background(), workflowprojects.CreateProjectInput{Name: "Relay"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return &submissionFixture{store: store, root: root, service: service, project: project}
}

func TestValidateArtifactPreservesCanonicalIdentityWithoutWorkflowStorage(t *testing.T) {
	validBytes := canonicalPlanBytes("relay")
	valid := validateArtifact(ValidationInput{
		DisplayName:    "canonical-service.plan.json",
		CanonicalBytes: validBytes,
	})
	if !valid.OK ||
		valid.Status != "valid" ||
		valid.Kind != "plan" ||
		valid.SHA256 != SHA256(validBytes) ||
		len(valid.Diagnostics) != 0 ||
		len(valid.Notices) != 0 {
		t.Fatalf("valid result = %+v", valid)
	}

	invalidBytes := []byte(`{"feature_slug":`)
	invalidCompiled := speccompiler.Compile("canonical-service.plan.json", invalidBytes)
	blocked := validateArtifact(ValidationInput{
		DisplayName:    "canonical-service.plan.json",
		CanonicalBytes: invalidBytes,
	})
	if blocked.OK ||
		blocked.Status != "blocked" ||
		blocked.Kind != "plan" ||
		blocked.SHA256 != SHA256(invalidBytes) ||
		len(blocked.Diagnostics) == 0 ||
		blocked.Diagnostics[0].Code != "invalid_json" {
		t.Fatalf("invalid content result = %+v", blocked)
	}
	assertDiagnosticsMatch(t, "invalid diagnostics", blocked.Diagnostics, invalidCompiled.Errors)
	assertDiagnosticsMatch(t, "invalid notices", blocked.Notices, invalidCompiled.Notices)

	fallbackBytes := bytes.Replace(
		validBytes,
		[]byte("  \"schema_version\": \"1.0\",\n"),
		nil,
		1,
	)
	if bytes.Equal(fallbackBytes, validBytes) {
		t.Fatal("schema_version line was not removed")
	}
	anomalousCompiled := speccompiler.Compile("canonical-service.plan.json", fallbackBytes)
	anomalous := validateArtifact(ValidationInput{
		DisplayName:    "canonical-service.plan.json",
		CanonicalBytes: fallbackBytes,
	})
	if !anomalous.OK ||
		anomalous.Status != "valid" ||
		anomalous.Kind != "plan" ||
		anomalous.SHA256 != SHA256(fallbackBytes) ||
		len(anomalous.Notices) != 1 ||
		anomalous.Notices[0].Code != "schema_version_anomaly" {
		t.Fatalf("anomalous result = %+v", anomalous)
	}
	assertDiagnosticsMatch(t, "anomalous diagnostics", anomalous.Diagnostics, anomalousCompiled.Errors)
	assertDiagnosticsMatch(t, "anomalous notices", anomalous.Notices, anomalousCompiled.Notices)

	unnormalized := validateArtifact(ValidationInput{
		DisplayName:    " canonical-service.plan.json",
		CanonicalBytes: validBytes,
	})
	if unnormalized.OK ||
		unnormalized.Status != "blocked" ||
		unnormalized.Kind != "unknown" ||
		unnormalized.SHA256 != SHA256(validBytes) ||
		len(unnormalized.Diagnostics) == 0 {
		t.Fatalf("whitespace filename result = %+v", unnormalized)
	}
}

func TestValidateArtifactValidatesAuthoredMarkdownWithoutAdmission(t *testing.T) {
	fixture := newSubmissionFixture(t)
	validBytes := []byte("# Requirements\n\n## Goal\n\n## Scope\n\n## Requirements\n\n## Acceptance Criteria\n")
	valid := validateArtifact(ValidationInput{DisplayName: "relay.requirements.md", CanonicalBytes: validBytes})
	if !valid.OK || valid.Status != "valid" || valid.Kind != "requirements" || valid.SHA256 != SHA256(validBytes) || valid.Diagnostics == nil || valid.Notices == nil || len(valid.Diagnostics) != 0 || len(valid.Notices) != 0 {
		t.Fatalf("valid authored Markdown result = %+v", valid)
	}

	// The Ticket Design Brief is no longer a supported canonical artifact
	// kind: its filename is blocked as an unsupported artifact and no durable
	// row or file is created.
	invalidBytes := []byte("# Ticket Design Brief\n\n## Selected Ticket\n")
	blocked := validateArtifact(ValidationInput{DisplayName: "relay.ticket-P2-T5.r1.design-brief.md", CanonicalBytes: invalidBytes})
	if blocked.OK || blocked.Status != "blocked" || blocked.Kind != "unknown" || blocked.SHA256 != SHA256(invalidBytes) || len(blocked.Diagnostics) == 0 || blocked.Diagnostics[0].Code != "unsupported_artifact_filename" {
		t.Fatalf("blocked authored Markdown result = %+v", blocked)
	}
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := tableCount(t, fixture.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
}

// Legacy Plan submission is retired: validating a well-formed canonical Plan
// creates no Plan, Pass, Run, artifact row, or durable artifact file.
func TestValidatingCanonicalPlanAdmitsNoPlan(t *testing.T) {
	fixture := newSubmissionFixture(t)
	data := canonicalPlanBytes("relay")
	result, err := fixture.service.ValidateArtifact(context.Background(), ValidationInput{
		DisplayName:    "canonical-service.plan.json",
		CanonicalBytes: data,
	})
	if err != nil || !result.OK || result.Kind != "plan" {
		t.Fatalf("validation result = %+v, error = %v", result, err)
	}
	for _, table := range []string{"plans", "plan_passes", "plan_repository_targets", "runs", "artifacts"} {
		if got := tableCount(t, fixture.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
	if got := regularFileCount(t, filepath.Join(fixture.root, "artifacts")); got != 0 {
		t.Fatalf("durable artifact files = %d, want 0", got)
	}
}

func assertDiagnosticsMatch(
	t *testing.T,
	label string,
	got []speccompiler.Diagnostic,
	want []speccompiler.Diagnostic,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d: got=%+v want=%+v", label, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %+v, want %+v", label, i, got[i], want[i])
		}
	}
}

func TestBoundedDiagnosticsPreservesOrderAndLimit(t *testing.T) {
	tests := []struct {
		name   string
		values []speccompiler.Diagnostic
		want   int
	}{
		{name: "nil", values: nil, want: 0},
		{name: "empty", values: []speccompiler.Diagnostic{}, want: 0},
		{name: "below limit", values: make([]speccompiler.Diagnostic, 3), want: 3},
		{name: "exact limit", values: make([]speccompiler.Diagnostic, MaxDiagnostics), want: MaxDiagnostics},
		{name: "over limit", values: make([]speccompiler.Diagnostic, MaxDiagnostics+1), want: MaxDiagnostics},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := range tt.values {
				tt.values[i].Code = fmt.Sprintf("diagnostic_%d", i)
			}
			result := boundedDiagnostics(tt.values)
			if len(result) != tt.want {
				t.Fatalf("len(boundedDiagnostics(values)) = %d, want %d", len(result), tt.want)
			}
			for i := range result {
				if result[i].Code != fmt.Sprintf("diagnostic_%d", i) {
					t.Fatalf("diagnostic %d = %#v", i, result[i])
				}
			}
		})
	}
}

func canonicalPlanBytes(repoTarget string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "canonical-service",
  "goal": "Test canonical Plan validation.",
  "context": "Canonical service test context.",
  "scope": {
    "in_scope": ["Validate the Plan."],
    "out_of_scope": ["Do not execute it."]
  },
  "repo_targets": [{
    "repo_target": %q,
    "branch": "main",
    "planning_base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }],
  "passes": [{
    "number": 1,
    "name": "Foundation",
    "repo_target": %q,
    "goal": "Create the foundation.",
    "context": "Canonical service pass context.",
    "scope": {
      "in_scope": ["Create the foundation."],
      "out_of_scope": ["No extra behavior."]
    },
    "depends_on": [],
    "outcomes": ["The foundation exists."],
    "source_targets": [{
      "path": "internal/canonicalservice",
      "purpose": "Contain the test implementation."
    }],
    "validation_intent": ["Prove the foundation."],
    "completion_criteria": ["The foundation is complete."]
  }],
  "completion_criteria": ["The Plan is complete."]
}
`, repoTarget, repoTarget))
}

func tableCount(t *testing.T, store *workflowstore.Store, table string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func regularFileCount(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return count
}
