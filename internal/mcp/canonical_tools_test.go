package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fakeCanonicalArtifactFetcher struct {
	content map[string]FileParameterContent
	err     *FileParameterError
}

func (f *fakeCanonicalArtifactFetcher) FetchCanonicalArtifact(_ context.Context, ref ChatGPTFileReference) (FileParameterContent, *FileParameterError) {
	if f.err != nil {
		return FileParameterContent{}, f.err
	}
	content, ok := f.content[ref.FileID]
	if !ok {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadFailed, "artifact_file could not be downloaded")
	}
	return content, nil
}

type canonicalTestHarness struct {
	server       *Server
	store        *workflowstore.Store
	fetcher      *fakeCanonicalArtifactFetcher
	artifactRoot string
	root         string
}

func newCanonicalTestHarness(t *testing.T, profile ToolProfile) *canonicalTestHarness {
	t.Helper()
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifacts")
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher := &fakeCanonicalArtifactFetcher{content: map[string]FileParameterContent{}}
	return &canonicalTestHarness{
		server: NewServer(discardLogger(), &MCPDeps{
			WorkflowStore:        store,
			ToolProfile:          profile,
			CanonicalFileFetcher: fetcher,
		}),
		store:        store,
		fetcher:      fetcher,
		artifactRoot: artifactRoot,
		root:         root,
	}
}

func (h *canonicalTestHarness) registerRepo(t *testing.T, repoTarget string) {
	t.Helper()
	repoPath := filepath.Join(h.root, "repos", repoTarget)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := workflowrepos.NewRegistry(h.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(context.Background(), repoTarget, repoPath); err != nil {
		t.Fatal(err)
	}
}

func (h *canonicalTestHarness) createProject(t *testing.T) workflowstore.Project {
	t.Helper()
	var project workflowstore.Project
	if err := h.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		var err error
		project, err = tx.CreateProject(context.Background(), workflowstore.CreateProjectParams{
			ProjectID: "project-canonical-tests",
			Name:      "Canonical tests",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return project
}

func (h *canonicalTestHarness) put(fileID, name string, data []byte) ChatGPTFileReference {
	h.fetcher.content[fileID] = FileParameterContent{Bytes: append([]byte(nil), data...), DisplayName: name}
	return ChatGPTFileReference{
		DownloadURL: "https://files.example.test/" + fileID,
		FileID:      fileID,
		FileName:    name,
		MIMEType:    "application/json",
	}
}

func canonicalTestSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canonicalPlanBytes(repoTarget string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "canonical-test",
  "goal": "Test canonical Plan submission.",
  "context": "Canonical Plan context that must never be returned as an artifact body.",
  "scope": {
    "in_scope": [
      "Persist the canonical test Plan."
    ],
    "out_of_scope": [
      "Do not execute the Plan."
    ]
  },
  "repo_targets": [
    {
      "repo_target": %q,
      "branch": "main",
      "planning_base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  ],
  "passes": [
    {
      "number": 1,
      "name": "Foundation",
      "repo_target": %q,
      "goal": "Implement the canonical test foundation.",
      "context": "Canonical pass context.",
      "scope": {
        "in_scope": [
          "Implement the test foundation."
        ],
        "out_of_scope": [
          "Do not add unrelated behavior."
        ]
      },
      "depends_on": [],
      "outcomes": [
        "The test foundation exists."
      ],
      "source_targets": [
        {
          "path": "internal/canonical-test",
          "purpose": "Contain the canonical test implementation."
        }
      ],
      "validation_intent": [
        "Prove the canonical test foundation."
      ],
      "completion_criteria": [
        "The canonical test foundation is complete."
      ]
    }
  ],
  "completion_criteria": [
    "The canonical test Plan is complete."
  ]
}
`, repoTarget, repoTarget))
}

func canonicalExecutionSpecBytes(repoTarget string) []byte {
	return []byte(fmt.Sprintf(`{
  "schema_version": "1.0",
  "feature_slug": "canonical-test",
  "repo_target": %q,
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Implement the canonical test execution.",
  "context": "Canonical Execution Spec context that must never be returned as an artifact body.",
  "scope": {
    "in_scope": [
      "Create the canonical test implementation."
    ],
    "out_of_scope": [
      "Do not add unrelated behavior."
    ]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Create the canonical test implementation.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Create the canonical test source file.",
          "files": [
            {
              "path": "internal/canonicaltest/canonical.go",
              "operation": "create",
              "purpose": "Provide the canonical test implementation.",
              "implementation": {
                "content": "package canonicaltest\n\nfunc Enabled() bool {\n\treturn true\n}\n"
              }
            }
          ],
          "completion_criteria": [
            "The canonical test source file exists."
          ]
        }
      ],
      "completion_criteria": [
        "The canonical test implementation is complete."
      ]
    }
  ],
  "validation": {
    "commands": [
      {
        "command": "go test ./internal/canonicaltest",
        "expected": "The canonical test package passes."
      }
    ]
  },
  "completion_criteria": [
    "The canonical test execution is complete."
  ]
}
`, repoTarget))
}

func canonicalArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func canonicalToolText(t *testing.T, result ToolCallResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1", len(result.Content))
	}
	return result.Content[0].Text
}

func canonicalBlockerCode(t *testing.T, result ToolCallResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected blocked result, got %s", canonicalToolText(t, result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var blocked MCPBlockedResponse
	if err := json.Unmarshal(data, &blocked); err != nil {
		t.Fatal(err)
	}
	if len(blocked.Blockers) != 1 {
		t.Fatalf("blockers = %+v", blocked.Blockers)
	}
	return blocked.Blockers[0].Code
}

func workflowRowCount(t *testing.T, store *workflowstore.Store, table string) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func artifactFileCount(t *testing.T, root string) int {
	t.Helper()
	count := 0
	if err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func submitCanonicalTestPlan(t *testing.T, h *canonicalTestHarness, repoTarget string) canonicalPlanOutput {
	t.Helper()
	data := canonicalPlanBytes(repoTarget)
	ref := h.put("plan-"+repoTarget, "canonical-test.plan.json", data)
	project := h.createProject(t)
	result := h.server.HandleSubmitPlan(canonicalArgs(t, canonicalSubmissionArgs{
		ProjectID:      project.ProjectID,
		ArtifactFile:   ref,
		ExpectedSHA256: canonicalTestSHA(data),
	}))
	if result.IsError {
		t.Fatalf("submit Plan failed: %s", canonicalToolText(t, result))
	}
	var out canonicalPlanOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func createCanonicalTestRun(t *testing.T, h *canonicalTestHarness, repoTarget string, input canonicalSubmissionArgs) canonicalRunOutput {
	t.Helper()
	data := canonicalExecutionSpecBytes(repoTarget)
	input.ArtifactFile = h.put("run-"+repoTarget+"-"+input.RemediatesRunID+fmt.Sprint(input.PassNumber), "canonical-test.execution-spec.json", data)
	input.ExpectedSHA256 = canonicalTestSHA(data)
	result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, input))
	if result.IsError {
		t.Fatalf("create Run failed: %s", canonicalToolText(t, result))
	}
	var out canonicalRunOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCanonicalToolDefinitionsByProfile(t *testing.T) {
	tests := []struct {
		profile ToolProfile
		want    []string
	}{
		{
			profile: ToolProfilePlanner,
			want:    []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"},
		},
		{profile: ToolProfileAuditor, want: []string{"validate_artifact", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
		{
			profile: ToolProfileLocalOperator,
			want:    []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run", "get_audit_packet", "get_run_artifact", "record_audit_decision"},
		},
		{
			profile: ToolProfile("restricted"),
			want:    []string{"validate_artifact", "list_projects", "submit_plan", "get_plan", "create_run"},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			if got := toolNames(canonicalToolDefinitions(tt.profile)); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListProjectsReturnsBoundedPlannerMetadata(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	project := h.createProject(t)
	result := h.server.HandleListCanonicalProjects(canonicalArgs(t, listCanonicalProjectsArgs{Limit: 1}))
	if result.IsError {
		t.Fatalf("list Projects failed: %s", canonicalToolText(t, result))
	}
	var out canonicalProjectsOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || len(out.Projects) != 1 || out.Projects[0].ProjectID != project.ProjectID {
		t.Fatalf("Projects output = %+v", out)
	}
}

func TestValidateArtifactIsNonMutatingAndBounded(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	data := canonicalPlanBytes("relay")
	ref := h.put("validate-plan", "canonical-test.plan.json", data)
	result := h.server.HandleValidateArtifact(canonicalArgs(t, canonicalArtifactArgs{ArtifactFile: ref}))
	if result.IsError {
		t.Fatalf("validate failed: %s", canonicalToolText(t, result))
	}
	var out canonicalValidationOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Status != "valid" || out.Kind != "plan" || out.SHA256 != canonicalTestSHA(data) {
		t.Fatalf("unexpected validation output: %+v", out)
	}
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := workflowRowCount(t, h.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
	if got := artifactFileCount(t, h.artifactRoot); got != 0 {
		t.Fatalf("artifact files = %d, want 0", got)
	}
	text := canonicalToolText(t, result)
	if strings.Contains(text, "Canonical Plan context") || strings.Contains(text, `"repo_targets"`) {
		t.Fatalf("validation response leaked canonical body: %s", text)
	}
}

func TestSubmitPlanAndGetPlanPersistBoundedMetadata(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	h.registerRepo(t, "relay")
	submitted := submitCanonicalTestPlan(t, h, "relay")
	if !submitted.OK || submitted.Project.ProjectID == "" || submitted.Plan.Status != workflowstore.PlanStatusActive || len(submitted.Passes) != 1 || len(submitted.Artifacts) != 2 {
		t.Fatalf("unexpected Plan output: %+v", submitted)
	}
	if submitted.Passes[0].Status != workflowstore.PassStatusPlanned {
		t.Fatalf("pass status = %q", submitted.Passes[0].Status)
	}
	result := h.server.HandleGetCanonicalPlan(canonicalArgs(t, getCanonicalPlanArgs{PlanID: submitted.Plan.PlanID}))
	if result.IsError {
		t.Fatalf("get Plan failed: %s", canonicalToolText(t, result))
	}
	var got canonicalPlanOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan.PlanID != submitted.Plan.PlanID || len(got.Passes) != 1 || len(got.Artifacts) != 2 {
		t.Fatalf("unexpected get Plan output: %+v", got)
	}
	for _, text := range []string{canonicalToolText(t, result), canonicalToolText(t, h.server.HandleGetCanonicalPlan(canonicalArgs(t, getCanonicalPlanArgs{PlanID: submitted.Plan.PlanID})))} {
		if strings.Contains(text, "Canonical Plan context") || strings.Contains(text, `"repo_targets"`) {
			t.Fatalf("Plan response leaked artifact body: %s", text)
		}
	}
}

func TestGetPlanMissingReturnsRecoverableUnknownResource(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	beforePlans := workflowRowCount(t, h.store, "plans")
	beforePasses := workflowRowCount(t, h.store, "plan_passes")
	beforeArtifacts := workflowRowCount(t, h.store, "artifacts")

	result := h.server.HandleGetCanonicalPlan(canonicalArgs(t, getCanonicalPlanArgs{PlanID: "plan-missing"}))
	if code := canonicalBlockerCode(t, result); code != MCPBlockerUnknownResource {
		t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
	}
	text := canonicalToolText(t, result)
	if !strings.Contains(text, `"recoverable":true`) {
		t.Fatalf("missing Plan blocker is not recoverable: %s", text)
	}
	for _, forbidden := range []string{"sql.ErrNoRows", "no rows in result set", "database/sql", "persistence_failed"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("missing Plan blocker leaked persistence detail %q: %s", forbidden, text)
		}
	}
	if workflowRowCount(t, h.store, "plans") != beforePlans ||
		workflowRowCount(t, h.store, "plan_passes") != beforePasses ||
		workflowRowCount(t, h.store, "artifacts") != beforeArtifacts {
		t.Fatal("missing Plan lookup mutated workflow state")
	}
}

func TestCreateRunPersistsSetupReadyMetadataAndArtifacts(t *testing.T) {
	t.Setenv("RELAY_WEB_BASE_URL", "http://localhost:3000/")
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	h.registerRepo(t, "relay")
	out := createCanonicalTestRun(t, h, "relay", canonicalSubmissionArgs{})
	if !out.OK || out.Run.Status != workflowstore.RunStatusSetupReady || len(out.Artifacts) != 2 {
		t.Fatalf("unexpected Run output: %+v", out)
	}
	if out.ReviewURL != "http://localhost:3000/runs/"+out.Run.RunID+"/specification" {
		t.Fatalf("review URL = %q", out.ReviewURL)
	}
	if workflowRowCount(t, h.store, "runs") != 1 || workflowRowCount(t, h.store, "artifacts") != 2 {
		t.Fatal("Run persistence rows were not created")
	}
	text := canonicalArgs(t, out)
	if strings.Contains(string(text), "Canonical Execution Spec context") || strings.Contains(string(text), `"steps"`) {
		t.Fatalf("Run response leaked artifact body: %s", text)
	}
}

func TestCreateRunEnforcesPassQualifiedFilenames(t *testing.T) {
	t.Run("managed matching qualifier", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		plan := submitCanonicalTestPlan(t, h, "relay")
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("managed-match", "canonical-test.pass-1.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
			PlanID:         plan.Plan.PlanID,
			PassNumber:     1,
		}))
		if result.IsError {
			t.Fatalf("create managed Run failed: %s", canonicalToolText(t, result))
		}
		var out canonicalRunOutput
		if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
			t.Fatal(err)
		}
		if out.Run.PlanID != plan.Plan.PlanID || out.Run.PassNumber != 1 {
			t.Fatalf("unexpected managed association: %+v", out.Run)
		}
	})

	t.Run("managed missing qualifier", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		plan := submitCanonicalTestPlan(t, h, "relay")
		beforeArtifacts := workflowRowCount(t, h.store, "artifacts")
		beforeFiles := artifactFileCount(t, h.artifactRoot)
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("managed-missing", "canonical-test.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
			PlanID:         plan.Plan.PlanID,
			PassNumber:     1,
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerAssociationInvalid {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if workflowRowCount(t, h.store, "runs") != 0 || workflowRowCount(t, h.store, "artifacts") != beforeArtifacts || artifactFileCount(t, h.artifactRoot) != beforeFiles {
			t.Fatal("missing qualifier created Run or artifact state")
		}
	})

	t.Run("managed mismatched qualifier", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		plan := submitCanonicalTestPlan(t, h, "relay")
		beforeArtifacts := workflowRowCount(t, h.store, "artifacts")
		beforeFiles := artifactFileCount(t, h.artifactRoot)
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("managed-mismatch", "canonical-test.pass-2.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
			PlanID:         plan.Plan.PlanID,
			PassNumber:     1,
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerAssociationInvalid {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if workflowRowCount(t, h.store, "runs") != 0 || workflowRowCount(t, h.store, "artifacts") != beforeArtifacts || artifactFileCount(t, h.artifactRoot) != beforeFiles {
			t.Fatal("mismatched qualifier created Run or artifact state")
		}
	})

	t.Run("standalone qualified filename", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("standalone-qualified", "canonical-test.pass-1.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerAssociationInvalid {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		assertCanonicalNoWrites(t, h)
	})
}

func TestCanonicalSubmissionFailuresAreAtomicAndClassified(t *testing.T) {
	t.Run("hash mismatch", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("hash-mismatch", "canonical-test.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: strings.Repeat("0", 64),
		}))
		if code := canonicalBlockerCode(t, result); code != MCPBlockerExpectedHashMismatch {
			t.Fatalf("code = %q", code)
		}
		assertCanonicalNoWrites(t, h)
	})

	t.Run("compiler rejection", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		data := []byte("{")
		ref := h.put("compiler-reject", "canonical-test.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerCompilerRejected {
			t.Fatalf("code = %q", code)
		}
		assertCanonicalNoWrites(t, h)
	})

	t.Run("unknown repository", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		data := canonicalExecutionSpecBytes("missing")
		ref := h.put("unknown-repo", "canonical-test.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
		}))
		if code := canonicalBlockerCode(t, result); code != MCPBlockerUnknownRepository {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if strings.Contains(strings.ToLower(canonicalToolText(t, result)), "no rows") {
			t.Fatalf("response leaked database details: %s", canonicalToolText(t, result))
		}
		assertCanonicalNoWrites(t, h)
	})

	t.Run("unknown managed Plan", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("unknown-plan", "canonical-test.pass-1.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
			PlanID:         "plan-missing",
			PassNumber:     1,
		}))
		if code := canonicalBlockerCode(t, result); code != MCPBlockerUnknownResource {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if workflowRowCount(t, h.store, "runs") != 0 || workflowRowCount(t, h.store, "artifacts") != 0 {
			t.Fatal("unknown Plan created Run state")
		}
		if artifactFileCount(t, h.artifactRoot) != 0 {
			t.Fatal("unknown Plan left artifact files")
		}
	})

	t.Run("Plan pass repository mismatch", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		h.registerRepo(t, "other")
		plan := submitCanonicalTestPlan(t, h, "relay")
		beforeArtifacts := workflowRowCount(t, h.store, "artifacts")
		data := canonicalExecutionSpecBytes("other")
		ref := h.put("association-mismatch", "canonical-test.pass-1.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:   ref,
			ExpectedSHA256: canonicalTestSHA(data),
			PlanID:         plan.Plan.PlanID,
			PassNumber:     1,
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerAssociationInvalid {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if workflowRowCount(t, h.store, "runs") != 0 || workflowRowCount(t, h.store, "artifacts") != beforeArtifacts {
			t.Fatal("invalid Plan/pass association created Run state")
		}
		storedPlan, err := h.store.GetPlanByPlanID(context.Background(), plan.Plan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		pass, err := h.store.GetPlanPassByPlanAndNumber(context.Background(), storedPlan.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if pass.Status != workflowstore.PassStatusPlanned {
			t.Fatalf("failed Run changed pass status to %q", pass.Status)
		}
	})

	t.Run("invalid remediation source", func(t *testing.T) {
		h := newCanonicalTestHarness(t, ToolProfilePlanner)
		h.registerRepo(t, "relay")
		original := createCanonicalTestRun(t, h, "relay", canonicalSubmissionArgs{})
		beforeRuns := workflowRowCount(t, h.store, "runs")
		beforeArtifacts := workflowRowCount(t, h.store, "artifacts")
		data := canonicalExecutionSpecBytes("relay")
		ref := h.put("invalid-remediation", "canonical-test.execution-spec.json", data)
		result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
			ArtifactFile:    ref,
			ExpectedSHA256:  canonicalTestSHA(data),
			RemediatesRunID: original.Run.RunID,
		}))
		if code := canonicalBlockerCode(t, result); code != canonicalBlockerAssociationInvalid {
			t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
		}
		if workflowRowCount(t, h.store, "runs") != beforeRuns || workflowRowCount(t, h.store, "artifacts") != beforeArtifacts {
			t.Fatal("invalid remediation source created state")
		}
	})
}

func TestCanonicalPersistenceFailureIsBoundedAndRollsBackArtifacts(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	h.registerRepo(t, "relay")
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	data := canonicalExecutionSpecBytes("relay")
	ref := h.put("closed-db", "canonical-test.execution-spec.json", data)
	result := h.server.HandleCreateCanonicalRun(canonicalArgs(t, canonicalSubmissionArgs{
		ArtifactFile:   ref,
		ExpectedSHA256: canonicalTestSHA(data),
	}))
	if code := canonicalBlockerCode(t, result); code != canonicalBlockerPersistenceFailed {
		t.Fatalf("code = %q; response = %s", code, canonicalToolText(t, result))
	}
	text := strings.ToLower(canonicalToolText(t, result))
	if strings.Contains(text, "closed") || strings.Contains(text, strings.ToLower(h.root)) {
		t.Fatalf("persistence blocker leaked local details: %s", text)
	}
	if artifactFileCount(t, h.artifactRoot) != 0 {
		t.Fatal("persistence failure left artifact files")
	}
}

func assertCanonicalNoWrites(t *testing.T, h *canonicalTestHarness) {
	t.Helper()
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := workflowRowCount(t, h.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
	if got := artifactFileCount(t, h.artifactRoot); got != 0 {
		t.Fatalf("artifact files = %d, want 0", got)
	}
}
