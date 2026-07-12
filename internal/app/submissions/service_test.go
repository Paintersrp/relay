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
)

type submissionFixture struct {
	store   *workflowstore.Store
	root    string
	service *Service
	project workflowstore.Project
}

func newSubmissionFixture(t *testing.T) *submissionFixture {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
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

func (f *submissionFixture) submitPlan(t *testing.T) SubmitPlanResult {
	t.Helper()
	data := canonicalPlanBytes("relay")
	result, err := f.service.SubmitPlan(context.Background(), SubmitPlanInput{
		ProjectID:      f.project.ProjectID,
		DisplayName:    "canonical-service.plan.json",
		ExpectedSHA256: SHA256(data),
		CanonicalBytes: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
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

func TestExecutionVersionResultsPropagateThroughValidationAndRunCreation(t *testing.T) {
	v1 := canonicalExecutionSpecBytes("relay")
	v2 := bytes.Replace(v1, []byte(`"schema_version": "1.0"`), []byte(`"schema_version": "2.0"`), 1)
	compiledV2 := speccompiler.Compile("canonical-service.execution-spec.json", v2)
	validatedV2 := validateArtifact(ValidationInput{DisplayName: "canonical-service.execution-spec.json", CanonicalBytes: v2})
	if !validatedV2.OK || validatedV2.Status != "valid" || validatedV2.Kind != "execution_spec" || validatedV2.SHA256 != SHA256(v2) {
		t.Fatalf("v2 validation result = %+v", validatedV2)
	}
	assertDiagnosticsMatch(t, "v2 diagnostics", validatedV2.Diagnostics, compiledV2.Errors)
	assertDiagnosticsMatch(t, "v2 notices", validatedV2.Notices, compiledV2.Notices)

	anomalous := bytes.Replace(v2, []byte("  \"schema_version\": \"2.0\",\n"), nil, 1)
	compiledAnomalous := speccompiler.Compile("canonical-service.execution-spec.json", anomalous)
	validatedAnomalous := validateArtifact(ValidationInput{DisplayName: "canonical-service.execution-spec.json", CanonicalBytes: anomalous})
	if !validatedAnomalous.OK || len(validatedAnomalous.Notices) != 1 || validatedAnomalous.Notices[0].Code != "schema_version_anomaly" {
		t.Fatalf("anomalous validation result = %+v", validatedAnomalous)
	}
	assertDiagnosticsMatch(t, "anomalous diagnostics", validatedAnomalous.Diagnostics, compiledAnomalous.Errors)
	assertDiagnosticsMatch(t, "anomalous notices", validatedAnomalous.Notices, compiledAnomalous.Notices)

	unsupported := bytes.Replace(v2, []byte(`"schema_version": "2.0"`), []byte(`"schema_version": "3.0"`), 1)
	compiledUnsupported := speccompiler.Compile("canonical-service.execution-spec.json", unsupported)
	validatedUnsupported := validateArtifact(ValidationInput{DisplayName: "canonical-service.execution-spec.json", CanonicalBytes: unsupported})
	if !validatedUnsupported.OK || validatedUnsupported.Status != "valid" || len(validatedUnsupported.Notices) != 1 || validatedUnsupported.Notices[0].Code != "schema_version_anomaly" {
		t.Fatalf("unsupported validation result = %+v", validatedUnsupported)
	}
	assertDiagnosticsMatch(t, "unsupported diagnostics", validatedUnsupported.Diagnostics, compiledUnsupported.Errors)

	fixture := newSubmissionFixture(t)
	created, err := fixture.service.CreateRun(context.Background(), CreateRunInput{
		DisplayName:    "canonical-service.execution-spec.json",
		ExpectedSHA256: SHA256(v2),
		CanonicalBytes: v2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Artifacts) != 2 {
		t.Fatalf("v2 Run artifacts = %+v", created.Artifacts)
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

func TestPlanSubmissionReturnsCommittedProjectAggregate(t *testing.T) {
	fixture := newSubmissionFixture(t)
	result := fixture.submitPlan(t)
	if result.Project.ProjectID != fixture.project.ProjectID ||
		result.Plan.ProjectRowID != fixture.project.ID ||
		len(result.Passes) != 1 ||
		len(result.Artifacts) != 2 {
		t.Fatalf("Plan result = %+v", result)
	}
}

func TestRunSubmissionPreservesSelectedPassFilenameContract(t *testing.T) {
	t.Run("matching managed qualifier succeeds and persists qualified basenames", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		plan := fixture.submitPlan(t)
		data := canonicalExecutionSpecBytes("relay")
		result, err := fixture.service.CreateRun(context.Background(), CreateRunInput{
			DisplayName:    "canonical-service.pass-1.execution-spec.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
			PlanID:         plan.Plan.PlanID,
			PassNumber:     1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Run.PlanRowID.Valid || !result.Run.PlanPassRowID.Valid {
			t.Fatalf("managed Run = %+v", result.Run)
		}
		paths := map[string]bool{}
		for _, artifact := range result.Artifacts {
			paths[filepath.ToSlash(artifact.RelativePath)] = true
		}
		base := "runs/" + result.Run.RunID + "/"
		if !paths[base+"canonical-service.pass-1.execution-spec.json"] ||
			!paths[base+"canonical-service.pass-1.executor-brief.md"] {
			t.Fatalf("qualified artifact paths = %+v", paths)
		}
	})

	for _, test := range []struct {
		name     string
		fileName string
		plan     bool
		pass     int64
	}{
		{name: "managed missing qualifier", fileName: "canonical-service.execution-spec.json", plan: true, pass: 1},
		{name: "managed malformed qualifier", fileName: "canonical-service.pass-01.execution-spec.json", plan: true, pass: 1},
		{name: "managed mismatched qualifier", fileName: "canonical-service.pass-2.execution-spec.json", plan: true, pass: 1},
		{name: "standalone qualified", fileName: "canonical-service.pass-1.execution-spec.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newSubmissionFixture(t)
			input := CreateRunInput{
				DisplayName:    test.fileName,
				ExpectedSHA256: SHA256(canonicalExecutionSpecBytes("relay")),
				CanonicalBytes: canonicalExecutionSpecBytes("relay"),
				PassNumber:     test.pass,
			}
			if test.plan {
				input.PlanID = fixture.submitPlan(t).Plan.PlanID
			}
			beforeArtifacts := tableCount(t, fixture.store, "artifacts")
			beforeFiles := regularFileCount(t, filepath.Join(fixture.root, "artifacts"))
			_, err := fixture.service.CreateRun(context.Background(), input)
			application, ok := AsApplicationError(err)
			if !ok {
				t.Fatalf("error = %v", err)
			}
			if application.Code != ErrorSelectedPassFilename {
				t.Fatalf("code = %q", application.Code)
			}
			if tableCount(t, fixture.store, "runs") != 0 ||
				tableCount(t, fixture.store, "artifacts") != beforeArtifacts ||
				regularFileCount(t, filepath.Join(fixture.root, "artifacts")) != beforeFiles {
				t.Fatal("failed Run submission created durable state")
			}
		})
	}

	t.Run("standalone unqualified succeeds", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		data := canonicalExecutionSpecBytes("relay")
		result, err := fixture.service.CreateRun(context.Background(), CreateRunInput{
			DisplayName:    "canonical-service.execution-spec.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Run.PlanRowID.Valid || result.Run.PlanPassRowID.Valid {
			t.Fatalf("standalone Run = %+v", result.Run)
		}
	})
}

func TestMutationInputsAreStrictAndFailuresAreTypedAndAtomic(t *testing.T) {
	t.Run("whitespace padded hash is malformed", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		data := canonicalPlanBytes("relay")
		_, err := fixture.service.SubmitPlan(context.Background(), SubmitPlanInput{
			ProjectID:      fixture.project.ProjectID,
			DisplayName:    "canonical-service.plan.json",
			ExpectedSHA256: " " + SHA256(data),
			CanonicalBytes: data,
		})
		assertApplicationCode(t, err, ErrorInvalidExpectedHash)
		assertNoPlanSubmission(t, fixture)
	})

	t.Run("whitespace padded filename is compiler rejected", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		data := canonicalPlanBytes("relay")
		_, err := fixture.service.SubmitPlan(context.Background(), SubmitPlanInput{
			ProjectID:      fixture.project.ProjectID,
			DisplayName:    " canonical-service.plan.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
		})
		assertApplicationCode(t, err, ErrorCompilerRejected)
		assertNoPlanSubmission(t, fixture)
	})

	t.Run("missing Project", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		data := canonicalPlanBytes("relay")
		_, err := fixture.service.SubmitPlan(context.Background(), SubmitPlanInput{
			ProjectID:      "project-missing",
			DisplayName:    "canonical-service.plan.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
		})
		assertApplicationCode(t, err, ErrorProjectNotFound)
		assertNoPlanSubmission(t, fixture)
	})

	t.Run("archived Project", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		projects, _ := workflowprojects.NewService(fixture.store)
		if _, err := projects.ArchiveProject(context.Background(), fixture.project.ProjectID); err != nil {
			t.Fatal(err)
		}
		data := canonicalPlanBytes("relay")
		_, err := fixture.service.SubmitPlan(context.Background(), SubmitPlanInput{
			ProjectID:      fixture.project.ProjectID,
			DisplayName:    "canonical-service.plan.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
		})
		assertApplicationCode(t, err, ErrorProjectArchived)
		assertNoPlanSubmission(t, fixture)
	})

	t.Run("unknown repository", func(t *testing.T) {
		fixture := newSubmissionFixture(t)
		data := canonicalPlanBytes("missing")
		_, err := fixture.service.SubmitPlan(context.Background(), SubmitPlanInput{
			ProjectID:      fixture.project.ProjectID,
			DisplayName:    "canonical-service.plan.json",
			ExpectedSHA256: SHA256(data),
			CanonicalBytes: data,
		})
		assertApplicationCode(t, err, ErrorRepositoryNotFound)
		assertNoPlanSubmission(t, fixture)
	})
}

func assertApplicationCode(t *testing.T, err error, expected ErrorCode) {
	t.Helper()
	application, ok := AsApplicationError(err)
	if !ok || application.Code != expected {
		t.Fatalf("error = %#v, want code %q", err, expected)
	}
}

func assertNoPlanSubmission(t *testing.T, fixture *submissionFixture) {
	t.Helper()
	if tableCount(t, fixture.store, "plans") != 0 ||
		tableCount(t, fixture.store, "artifacts") != 0 ||
		regularFileCount(t, filepath.Join(fixture.root, "artifacts")) != 0 {
		t.Fatal("failed Plan submission created durable state")
	}
}

func canonicalPlanBytes(repoTarget string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "canonical-service",
  "goal": "Test canonical Plan submission.",
  "context": "Canonical service test context.",
  "scope": {
    "in_scope": ["Persist the Plan."],
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

func canonicalExecutionSpecBytes(repoTarget string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "canonical-service",
  "repo_target": %q,
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Test canonical Run creation.",
  "context": "Canonical service Execution Spec context.",
  "scope": {
    "in_scope": ["Create the test source."],
    "out_of_scope": ["No extra behavior."]
  },
  "steps": [{
    "number": 1,
    "goal": "Create the test source.",
    "substeps": [{
      "number": 1,
      "instruction": "Create the canonical service test source.",
      "files": [{
        "path": "internal/canonicalservice/test.go",
        "operation": "create",
        "purpose": "Provide the test source.",
        "implementation": {
          "content": "package canonicalservice\n\nfunc Enabled() bool {\n\treturn true\n}\n"
        }
      }],
      "completion_criteria": ["The source is defined."]
    }],
    "completion_criteria": ["The source is complete."]
  }],
  "validation": {
    "commands": [{
      "command": "go test ./internal/canonicalservice",
      "expected": "The canonical service test package passes."
    }]
  },
  "completion_criteria": ["The canonical Run input is complete."]
}
`, repoTarget))
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
