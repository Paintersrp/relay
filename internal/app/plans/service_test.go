package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

func TestSubmitPlanStoresValidPlan(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	plan := validPlannerPassPlan()
	raw := mustMarshalPlan(t, plan)

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON:            raw,
		SourceArtifactPath: "handoffs/planner/plan.json",
	})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid report, got %+v", result.Report.Issues)
	}
	if result.Plan.PlanID != "plan-123" {
		t.Fatalf("expected plan_id plan-123, got %q", result.Plan.PlanID)
	}
	if result.Plan.SubmissionNote != plan.PlanMeta.SubmissionNote {
		t.Fatalf("expected submission_note %q, got %q", plan.PlanMeta.SubmissionNote, result.Plan.SubmissionNote)
	}
	assertJSONEqual(t, result.Plan.PlanMetaJson, plan.PlanMeta)
	assertJSONEqual(t, result.Plan.ProjectContextJson, plan.PlanMeta.ProjectContext)
	assertJSONEqual(t, result.Plan.McpCapabilityProfileJson, plan.PlanMeta.MCPCapabilityProfile)
	assertJSONEqual(t, result.Plan.GlobalContextRulesJson, plan.GlobalContextRules)
	assertJSONEqual(t, result.Plan.RawPlanJson, json.RawMessage(raw))

	if len(result.Passes) != 2 {
		t.Fatalf("expected 2 plan passes, got %d", len(result.Passes))
	}
	if result.Passes[0].Sequence != 1 || result.Passes[1].Sequence != 2 {
		t.Fatalf("expected ordered passes by sequence, got %d then %d", result.Passes[0].Sequence, result.Passes[1].Sequence)
	}
	if result.Passes[0].PassType != plan.Passes[0].PassType {
		t.Fatalf("expected first pass_type %q, got %q", plan.Passes[0].PassType, result.Passes[0].PassType)
	}
	if result.Passes[1].DependenciesJson != `["PASS-001"]` {
		t.Fatalf("unexpected stored dependencies JSON: %s", result.Passes[1].DependenciesJson)
	}
	assertJSONEqual(t, result.Passes[0].ContextPlanJson, plan.Passes[0].ContextPlan)
	assertJSONEqual(t, result.Passes[0].SourceSnapshotRequirementsJson, plan.Passes[0].SourceSnapshotRequirements)
	assertJSONEqual(t, result.Passes[0].HandoffReadinessCriteriaJson, plan.Passes[0].HandoffReadinessCriteria)
	assertJSONEqual(t, result.Passes[0].ContextBudgetJson, plan.Passes[0].ContextBudget)
	assertJSONEqual(t, result.Passes[0].RawPassJson, plan.Passes[0])

	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 2 {
		t.Fatalf("expected 2 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRequiresUnmanagedAcknowledgement(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	raw := mustMarshalPlan(t, validPlannerPassPlan())

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanUnmanagedAcknowledgementRequired)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON: []byte(`{"plan_meta":`),
	})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanJSONSyntax)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRejectsDuplicatePlanID(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	raw := mustMarshalPlan(t, validPlannerPassPlan())

	if _, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw}); err != nil {
		t.Fatalf("first SubmitPlan returned error: %v", err)
	}

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil {
		t.Fatalf("second SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanDuplicatePlanID)
	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row after duplicate submit, got %d", got)
	}
}

func TestSubmitPlanRejectsDuplicatePassID(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	plan := validPlannerPassPlan()
	plan.Passes[1].PassID = plan.Passes[0].PassID

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanDuplicatePassID)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
}

func TestSubmitPlanRejectsDuplicateSequence(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	plan := validPlannerPassPlan()
	plan.Passes[1].Sequence = plan.Passes[0].Sequence

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanDuplicateSequence)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
}

func TestSubmitPlanRejectsInvalidDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*PlannerPassPlan)
		issueCode string
	}{
		{
			name: "unknown dependency",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[1].Dependencies = []string{"PASS-999"}
			},
			issueCode: IssuePlanDependencyUnknown,
		},
		{
			name: "self dependency",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[0].Dependencies = []string{"PASS-001"}
			},
			issueCode: IssuePlanDependencySelf,
		},
		{
			name: "duplicate dependency",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[1].Dependencies = []string{"PASS-001", "PASS-001"}
			},
			issueCode: IssuePlanDependencyDuplicate,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, st := newTestService(t)
			plan := validPlannerPassPlan()
			tc.mutate(&plan)

			result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
			if err != nil {
				t.Fatalf("SubmitPlan returned error: %v", err)
			}
			assertIssueCode(t, result.Report, tc.issueCode)
			if got := countRows(t, st.DB(), "plans"); got != 0 {
				t.Fatalf("expected 0 plan rows, got %d", got)
			}
		})
	}
}

func TestSubmitPlanRejectsSubmittedStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*PlannerPassPlan)
		issueCode string
	}{
		{
			name: "terminal plan status",
			mutate: func(plan *PlannerPassPlan) {
				plan.PlanMeta.Status = "complete"
			},
			issueCode: IssuePlanStatusInvalidForSubmission,
		},
		{
			name: "runtime pass status",
			mutate: func(plan *PlannerPassPlan) {
				plan.Passes[1].Status = "completed"
			},
			issueCode: IssuePlanPassStatusInvalid,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, st := newTestService(t)
			plan := validPlannerPassPlan()
			tc.mutate(&plan)

			result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
			if err != nil {
				t.Fatalf("SubmitPlan returned error: %v", err)
			}
			assertIssueCode(t, result.Report, tc.issueCode)
			if got := countRows(t, st.DB(), "plans"); got != 0 {
				t.Fatalf("expected 0 plan rows, got %d", got)
			}
		})
	}
}

func TestSubmitPlanRejectsSecretLikeContent(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	plan := validPlannerPassPlan()
	plan.SourceIntent.Summary = "client_secret=ABCDEFGHIJKLMNOP"

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, plan)})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanSecretDetected)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
}

func TestSubmitPlanRejectsMissingPlanV2PassFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
	}{
		{name: "missing pass_type", field: "pass_type"},
		{name: "missing context_plan", field: "context_plan"},
		{name: "missing source_snapshot_requirements", field: "source_snapshot_requirements"},
		{name: "missing handoff_readiness_criteria", field: "handoff_readiness_criteria"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, st := newTestService(t)
			doc := mustPlanDocument(t, validPlannerPassPlan())
			removeFirstPassField(t, doc, tc.field)

			result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlanDocument(t, doc)})
			if err != nil {
				t.Fatalf("SubmitPlan returned error: %v", err)
			}
			assertIssueCode(t, result.Report, IssuePlanSchemaInvalid)
			if got := countRows(t, st.DB(), "plans"); got != 0 {
				t.Fatalf("expected 0 plan rows, got %d", got)
			}
			if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
				t.Fatalf("expected 0 plan_passes rows, got %d", got)
			}
		})
	}
}

func TestSubmitPlanRejectsLegacyMinimalPlan(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	raw := []byte(`{
		"plan_meta": {
			"plan_id": "plan-legacy",
			"schema_version": "1.0.0",
			"created_at": "2026-06-21T00:00:00Z",
			"title": "Legacy plan",
			"goal": "Old minimal shape",
			"repo_target": "Paintersrp/relay",
			"branch_context": "main",
			"status": "active"
		},
		"source_intent": {
			"summary": "Legacy plan payload."
		},
		"passes": [
			{
				"pass_id": "PASS-001",
				"sequence": 1,
				"name": "Legacy pass",
				"goal": "Missing Plan v2 fields.",
				"intended_execution_scope": ["internal/plans/service.go"],
				"non_goals": ["No UI"],
				"dependencies": [],
				"status": "planned"
			}
		]
	}`)

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: raw})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanSchemaInvalid)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRejectsStringMCPCapabilityProfile(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	doc := mustPlanDocument(t, validPlannerPassPlan())
	planMeta, ok := doc["plan_meta"].(map[string]any)
	if !ok {
		t.Fatal("expected plan_meta object")
	}
	planMeta["mcp_capability_profile"] = "relay-context-broker-v2"

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlanDocument(t, doc)})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanSchemaInvalid)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRollsBackOnPassInsertFailure(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	if _, err := st.DB().Exec(`CREATE TRIGGER fail_plan_pass_insert BEFORE INSERT ON plan_passes BEGIN SELECT RAISE(FAIL, 'pass insert failed'); END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true, RawJSON: mustMarshalPlan(t, validPlannerPassPlan())})
	if err == nil {
		t.Fatal("expected SubmitPlan to fail when pass insert trigger fires")
	}

	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows after rollback, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 0 {
		t.Fatalf("expected 0 pass rows after rollback, got %d", got)
	}
}

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "relay.sqlite")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})

	// Create default test project "relay"
	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("st.CreateProject: %v", err)
	}

	return NewService(st), st
}

func validPlannerPassPlan() PlannerPassPlan {
	return PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        "plan-123",
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T16:10:00Z",
			Title:         "Relay plan submission service",
			Goal:          "Store validated planner plans",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectContext: &ProjectContext{
				PrimaryProject:        "relay",
				PrimaryRepository:     "relay",
				ContractRepository:    "relay-contracts",
				GitHubRole:            "repo_host_and_origin_only",
				ExcludedGitHubDomains: []string{"issues", "pull request comments"},
				LocalFirstAssumption:  "Relay remains the local source of runtime context.",
			},
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "relay-context-broker-v2",
				Mode:                 "submission_only",
				ContextBrokerEnabled: boolPtr(false),
				Notes:                "PASS-006 stores the profile without exposing PASS-007 tools.",
			},
			SubmissionNote: "Reviewed plan submissions still require explicit user confirmation.",
		},
		SourceIntent: SourceIntent{
			Summary: "Add a backend service for validated Plan v2 submission and persistence.",
		},
		GlobalContextRules: &GlobalContextRules{
			DefaultSourceOfTruth:   "Source-controlled plan artifacts and Relay-managed local metadata.",
			PlannerContextBoundary: "Planner operates on structured context, not arbitrary shell access.",
			ForbiddenContextDomains: []string{
				"GitHub issues",
				"GitHub Actions runtime state",
			},
			Notes: []string{"Context plans describe acquisition intent only."},
		},
		Passes: []PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Validate plans",
				Goal:                   "Validate syntax, schema, and Plan v2 semantics.",
				IntendedExecutionScope: []string{"internal/plans/validator.go"},
				NonGoals:               []string{"No API routes"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "schema_contract",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay", "relay-contracts"},
					SeedSearchTerms: []ContextSearchTerm{
						{
							RepoID:   "relay",
							Query:    "internal/plans validator schema",
							Purpose:  "Find the Plan v2 validation flow.",
							Required: boolPtr(true),
						},
						{
							RepoID:   "relay-contracts",
							Query:    "planner_pass_plan schema",
							Purpose:  "Ground validation against the source-controlled contract.",
							Required: boolPtr(false),
						},
					},
					SeedFilesToRead: []ContextFileRead{
						{
							RepoID:   "relay",
							Path:     "internal/plans/validator.go",
							Purpose:  "Update validation semantics.",
							Required: boolPtr(true),
						},
					},
					ContextCoverageExpectations: []string{
						"Pass-level required Plan v2 fields remain enforced.",
					},
					BlockedIfMissing: []string{
						"Plan schema cannot be loaded.",
					},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   boolPtr(true),
					RequireCommitSHA:   boolPtr(true),
					AllowDirtyWorktree: boolPtr(false),
				},
				HandoffReadinessCriteria: []string{
					"Managed plan validation fails closed for missing Plan v2 pass fields.",
				},
				RiskLevel: "medium",
				ContextBudget: &ContextBudget{
					MaxFiles:         int64Ptr(12),
					MaxBytes:         int64Ptr(131072),
					MaxSearchResults: int64Ptr(40),
					MaxContextLines:  int64Ptr(600),
				},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Store plans",
				Goal:                   "Store validated plans transactionally.",
				IntendedExecutionScope: []string{"internal/plans/service.go"},
				NonGoals:               []string{"No UI changes"},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []ContextSearchTerm{
						{
							RepoID:   "relay",
							Query:    "CreatePlan CreatePlanPass",
							Purpose:  "Locate persistence seams.",
							Required: boolPtr(true),
						},
					},
					SeedFilesToRead: []ContextFileRead{
						{
							RepoID:   "relay",
							Path:     "internal/plans/service.go",
							Purpose:  "Persist Plan v2 JSON fields.",
							Required: boolPtr(true),
						},
					},
					ContextCoverageExpectations: []string{
						"Full Plan v2 metadata is preserved in durable storage.",
					},
					BlockedIfMissing: []string{
						"Plan persistence queries cannot be updated.",
					},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   boolPtr(true),
					RequireCommitSHA:   boolPtr(false),
					AllowDirtyWorktree: boolPtr(true),
				},
				HandoffReadinessCriteria: []string{
					"Stored plan rows preserve pass context for later workflows.",
				},
				RiskLevel: "low",
				ContextBudget: &ContextBudget{
					MaxFiles:         int64Ptr(8),
					MaxBytes:         int64Ptr(65536),
					MaxSearchResults: int64Ptr(20),
					MaxContextLines:  int64Ptr(300),
				},
			},
		},
	}
}

func mustMarshalPlan(t *testing.T, plan PlannerPassPlan) []byte {
	t.Helper()

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return raw
}

func mustPlanDocument(t *testing.T, plan PlannerPassPlan) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(mustMarshalPlan(t, plan), &doc); err != nil {
		t.Fatalf("json.Unmarshal plan doc: %v", err)
	}
	return doc
}

func mustMarshalPlanDocument(t *testing.T, doc map[string]any) []byte {
	t.Helper()

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal doc: %v", err)
	}
	return raw
}

func removeFirstPassField(t *testing.T, doc map[string]any, field string) {
	t.Helper()

	passes, ok := doc["passes"].([]any)
	if !ok || len(passes) == 0 {
		t.Fatal("expected passes array")
	}
	firstPass, ok := passes[0].(map[string]any)
	if !ok {
		t.Fatal("expected first pass object")
	}
	delete(firstPass, field)
}

func assertIssueCode(t *testing.T, report PlanValidationReport, code string) {
	t.Helper()

	if report.Valid {
		t.Fatalf("expected invalid report for issue %s", code)
	}
	for _, issue := range report.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected issue code %s, got %+v", code, report.Issues)
}

func assertJSONEqual(t *testing.T, actual string, expected any) {
	t.Helper()

	var actualValue any
	if err := json.Unmarshal([]byte(actual), &actualValue); err != nil {
		t.Fatalf("json.Unmarshal actual: %v", err)
	}

	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("json.Marshal expected: %v", err)
	}
	var expectedValue any
	if err := json.Unmarshal(expectedBytes, &expectedValue); err != nil {
		t.Fatalf("json.Unmarshal expected: %v", err)
	}

	actualBytes, err := json.Marshal(actualValue)
	if err != nil {
		t.Fatalf("json.Marshal actual normalized: %v", err)
	}
	expectedNormalizedBytes, err := json.Marshal(expectedValue)
	if err != nil {
		t.Fatalf("json.Marshal expected normalized: %v", err)
	}

	if string(actualBytes) != string(expectedNormalizedBytes) {
		t.Fatalf("expected JSON %s, got %s", string(expectedNormalizedBytes), string(actualBytes))
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", table, err)
	}
	return count
}

func boolPtr(value bool) *bool {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func TestSubmitPlanRequiresProject(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)

	// Test 1: project ID resolved to "non-existent"
	plan := validPlannerPassPlan()
	plan.PlanMeta.ProjectContext.PrimaryProject = "non-existent"
	raw := mustMarshalPlan(t, plan)

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON: raw,
	})
	if err != nil {
		t.Fatalf("SubmitPlan error: %v", err)
	}
	if result.Report.Valid {
		t.Fatal("expected invalid report due to unknown project")
	}
	assertIssueCode(t, result.Report, IssuePlanProjectUnknown)

	// Test 2: explicit project ID is used and exists
	if _, err := st.CreateProject("another-project", "Another", "", "active", ""); err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	result, err = svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON:   raw,
		ProjectID: "another-project",
	})
	if err != nil {
		t.Fatalf("SubmitPlan error: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid report, got: %+v", result.Report.Issues)
	}
	if result.Plan.ProjectID != "another-project" {
		t.Errorf("expected ProjectID another-project, got %q", result.Plan.ProjectID)
	}

	// Test 3: project ID is entirely missing
	planNoProj := validPlannerPassPlan()
	planNoProj.PlanMeta.ProjectContext = nil
	rawNoProj := mustMarshalPlan(t, planNoProj)

	result, err = svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON: rawNoProj,
	})
	if err != nil {
		t.Fatalf("SubmitPlan error: %v", err)
	}
	if result.Report.Valid {
		t.Fatal("expected invalid report due to missing project")
	}
	assertIssueCode(t, result.Report, IssuePlanProjectRequired)
}
