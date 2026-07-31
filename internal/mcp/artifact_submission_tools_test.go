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

type fakeArtifactFetcher struct {
	content map[string]FileParameterContent
	err     *FileParameterError
}

func (f *fakeArtifactFetcher) FetchArtifact(_ context.Context, ref ChatGPTFileReference) (FileParameterContent, *FileParameterError) {
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
	fetcher      *fakeArtifactFetcher
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
	fetcher := &fakeArtifactFetcher{content: map[string]FileParameterContent{}}
	return &canonicalTestHarness{
		server: NewServer(discardLogger(), &MCPDeps{
			WorkflowStore:       store,
			ToolProfile:         profile,
			ArtifactFileFetcher: fetcher,
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

func workflowBlockerCode(t *testing.T, result ToolCallResult) string {
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

// seedCanonicalTestPlan writes one historical Plan and Pass directly through
// the store. Legacy Plan write admission is retired, so the MCP surface has no
// Plan-creating tool.
func (h *canonicalTestHarness) seedCanonicalTestPlan(t *testing.T, repoTarget string) (workflowstore.Plan, workflowstore.PlanPass) {
	t.Helper()
	ctx := context.Background()
	project := h.createProject(t)
	var plan workflowstore.Plan
	var pass workflowstore.PlanPass
	if err := h.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		stored, err := tx.GetProjectByProjectID(ctx, project.ProjectID)
		if err != nil {
			return err
		}
		plan, err = tx.CreatePlan(ctx, workflowstore.CreatePlanParams{
			ProjectRowID:    stored.ID,
			PlanID:          "plan-canonical-test",
			FeatureSlug:     "canonical-test",
			CanonicalSHA256: strings.Repeat("a", 64),
		})
		if err != nil {
			return err
		}
		if _, err := tx.CreatePlanRepositoryTarget(ctx, workflowstore.CreatePlanRepositoryTargetParams{
			PlanRowID:          plan.ID,
			Sequence:           1,
			RepoTarget:         repoTarget,
			Branch:             "main",
			PlanningBaseCommit: strings.Repeat("a", 40),
		}); err != nil {
			return err
		}
		pass, err = tx.CreatePlanPass(ctx, workflowstore.CreatePlanPassParams{
			PassID:     "pass-canonical-test",
			PlanRowID:  plan.ID,
			PassNumber: 1,
			Name:       "Foundation",
			RepoTarget: repoTarget,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return plan, pass
}

func TestCanonicalToolDefinitionsByProfile(t *testing.T) {
	tests := []struct {
		profile ToolProfile
		want    []string
	}{
		{
			profile: ToolProfilePlanner,
			want:    []string{"validate_artifact", "list_projects", "get_plan"},
		},
		{profile: ToolProfileAuditor, want: []string{"validate_artifact", "get_audit_packet", "get_run_artifact", "record_audit_decision"}},
		{
			profile: ToolProfile("local_operator"),
			want:    []string{"validate_artifact", "list_projects", "get_plan"},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			if got := toolNames(workflowToolDefinitions(tt.profile)); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("tools = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListProjectsReturnsBoundedPlannerMetadata(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	project := h.createProject(t)
	result := h.server.HandleListProjects(canonicalArgs(t, listProjectsArgs{Limit: 1}))
	if result.IsError {
		t.Fatalf("list Projects failed: %s", canonicalToolText(t, result))
	}
	var out projectsOutput
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
	result := h.server.HandleValidateArtifact(canonicalArgs(t, artifactArgs{ArtifactFile: ref}))
	if result.IsError {
		t.Fatalf("validate failed: %s", canonicalToolText(t, result))
	}
	var out artifactValidationOutput
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

func TestValidateArtifactSupportsAuthoredMarkdownWithoutAdmission(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	data := []byte("# Shared Design\n\n## Context\n\n## Design\n\n## Risks\n\n## Validation\n")
	ref := h.put("validate-design", "relay.design.md", data)
	result := h.server.HandleValidateArtifact(canonicalArgs(t, artifactArgs{ArtifactFile: ref}))
	if result.IsError {
		t.Fatalf("validate failed: %s", canonicalToolText(t, result))
	}
	var out artifactValidationOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Status != "valid" || out.Kind != "shared_design" || out.SHA256 != canonicalTestSHA(data) || out.Diagnostics == nil || out.Notices == nil {
		t.Fatalf("unexpected validation output: %+v", out)
	}
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := workflowRowCount(t, h.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
}

func TestGetPlanReturnsBoundedMetadataWithoutArtifactBodies(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	h.registerRepo(t, "relay")
	plan, pass := h.seedCanonicalTestPlan(t, "relay")

	result := h.server.HandleGetPlan(canonicalArgs(t, getPlanArgs{PlanID: plan.PlanID}))
	if result.IsError {
		t.Fatal(canonicalToolText(t, result))
	}
	var got planOutput
	if err := json.Unmarshal([]byte(canonicalToolText(t, result)), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Plan.PlanID != plan.PlanID || got.Plan.Status != workflowstore.PlanStatusActive {
		t.Fatalf("Plan output = %+v", got)
	}
	if len(got.Passes) != 1 || got.Passes[0].PassID != pass.PassID || got.Passes[0].Status != workflowstore.PassStatusPlanned {
		t.Fatalf("pass output = %+v", got.Passes)
	}
	text := canonicalToolText(t, result)
	if strings.Contains(text, "Canonical Plan context") || strings.Contains(text, `"repo_targets"`) {
		t.Fatalf("Plan response leaked artifact body: %s", text)
	}
}

// Legacy Plan write admission is retired: no profile publishes a Plan-creating
// tool, and the aggregate dispatcher rejects the retired tool name.
func TestSubmitPlanToolIsRetiredAcrossProfilesAndDispatch(t *testing.T) {
	for _, profile := range []ToolProfile{ToolProfilePlanner, ToolProfileAuditor, ToolProfile("local_operator")} {
		for _, name := range toolNames(workflowToolDefinitions(profile)) {
			if name == "submit_plan" {
				t.Fatalf("profile %q still publishes submit_plan", profile)
			}
		}
	}
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	response := h.server.handleLine(mustMarshal(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": "submit_plan", "arguments": map[string]any{}},
	}))
	if response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("submit_plan dispatch = %+v", response)
	}
	for _, table := range []string{"plans", "plan_passes", "runs", "artifacts"} {
		if got := workflowRowCount(t, h.store, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
}

func TestGetPlanMissingReturnsRecoverableUnknownResource(t *testing.T) {
	h := newCanonicalTestHarness(t, ToolProfilePlanner)
	beforePlans := workflowRowCount(t, h.store, "plans")
	beforePasses := workflowRowCount(t, h.store, "plan_passes")
	beforeArtifacts := workflowRowCount(t, h.store, "artifacts")

	result := h.server.HandleGetPlan(canonicalArgs(t, getPlanArgs{PlanID: "plan-missing"}))
	if code := workflowBlockerCode(t, result); code != MCPBlockerUnknownResource {
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
