package submissions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	workflowprojects "relay/internal/app/projects/workflow"
	workflowrepos "relay/internal/repos/workflow"
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

func TestValidationComputesHashWithoutExpectedHashAndDoesNotNormalizeFilename(t *testing.T) {
	fixture := newSubmissionFixture(t)
	data := canonicalPlanBytes("relay")
	valid, err := fixture.service.ValidateArtifact(context.Background(), ValidationInput{
		DisplayName:    "canonical-service.plan.json",
		CanonicalBytes: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !valid.OK || valid.SHA256 != SHA256(data) || valid.Kind != "plan" {
		t.Fatalf("validation = %+v", valid)
	}
	blocked, err := fixture.service.ValidateArtifact(context.Background(), ValidationInput{
		DisplayName:    " canonical-service.plan.json",
		CanonicalBytes: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.OK || blocked.SHA256 != SHA256(data) || len(blocked.Diagnostics) == 0 {
		t.Fatalf("whitespace filename validation = %+v", blocked)
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
