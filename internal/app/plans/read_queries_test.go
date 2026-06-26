package plans

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"relay/internal/store"
)

func newReadQueryTestService(t *testing.T) (*Service, *store.Store) {
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

	if _, err := st.CreateProject("relay", "Relay", "Default Test Project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	return NewService(st), st
}

func readQueryBoolPtr(value bool) *bool { return &value }

// validReadQueryPlan builds a schema/semantically valid plan with two planned
// passes. projectID is written to plan_meta.project_id when non-empty.
func validReadQueryPlan(planID, projectID string) PlannerPassPlan {
	plan := PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-21T00:00:00Z",
			Title:         "Managed plan",
			Goal:          "Implement managed plan flow",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     projectID,
			MCPCapabilityProfile: &MCPCapabilityProfile{
				ProfileID:            "relay-app-plan-tests",
				Mode:                 "submission_only",
				ContextBrokerEnabled: readQueryBoolPtr(false),
			},
		},
		SourceIntent: SourceIntent{
			Summary: "Implement managed plan support across phases.",
		},
		Passes: []PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Example",
				Goal:                   "Example pass.",
				IntendedExecutionScope: []string{"Example scope."},
				NonGoals:               []string{"Example non-goal."},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "Locate validation flow.", Required: readQueryBoolPtr(true)},
					},
					SeedFilesToRead: []ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "Validate plans.", Required: readQueryBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Validation remains fail-closed for Plan v2."},
					BlockedIfMissing:            []string{"Validation code cannot be located."},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   readQueryBoolPtr(true),
					RequireCommitSHA:   readQueryBoolPtr(false),
					AllowDirtyWorktree: readQueryBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Validation and persistence requirements are captured."},
			},
			{
				PassID:                 "PASS-002",
				Sequence:               2,
				Name:                   "Follow-up",
				Goal:                   "Follow-up pass.",
				IntendedExecutionScope: []string{"Another scope."},
				NonGoals:               []string{"No UI changes."},
				Dependencies:           []string{"PASS-001"},
				Status:                 "planned",
				PassType:               "schema_contract",
				ContextPlan: ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []ContextSearchTerm{
						{RepoID: "relay", Query: "CreatePlanPass", Purpose: "Locate persistence flow.", Required: readQueryBoolPtr(true)},
					},
					SeedFilesToRead: []ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/service.go", Purpose: "Persist plan fields.", Required: readQueryBoolPtr(true)},
					},
					ContextCoverageExpectations: []string{"Pass metadata is stored transactionally."},
					BlockedIfMissing:            []string{"Service persistence code cannot be located."},
				},
				SourceSnapshotRequirements: SourceSnapshotRequirements{
					RequireGitStatus:   readQueryBoolPtr(true),
					RequireCommitSHA:   readQueryBoolPtr(false),
					AllowDirtyWorktree: readQueryBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Stored pass rows preserve later workflow context."},
			},
		},
	}
	return plan
}

func submitReadQueryPlan(t *testing.T, svc *Service, planID, projectID string) {
	t.Helper()

	raw, err := json.Marshal(validReadQueryPlan(planID, projectID))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON:   raw,
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid submission, got issues %+v", result.Report.Issues)
	}
}

func reportHasIssue(report PlanValidationReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestValidatePlanForSubmissionProjectRequired(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)

	// No project_id in plan and no explicit request project.
	raw, err := json.Marshal(validReadQueryPlan("plan-required", ""))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	report, err := svc.ValidatePlanForSubmission(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("ValidatePlanForSubmission: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report when project is unresolved")
	}
	if !reportHasIssue(report, IssuePlanProjectRequired) {
		t.Fatalf("expected %s issue, got %+v", IssuePlanProjectRequired, report.Issues)
	}
}

func TestValidatePlanForSubmissionProjectUnknown(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)

	raw, err := json.Marshal(validReadQueryPlan("plan-unknown", ""))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	report, err := svc.ValidatePlanForSubmission(context.Background(), raw, "does-not-exist")
	if err != nil {
		t.Fatalf("ValidatePlanForSubmission: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report for unknown project")
	}
	if !reportHasIssue(report, IssuePlanProjectUnknown) {
		t.Fatalf("expected %s issue, got %+v", IssuePlanProjectUnknown, report.Issues)
	}
}

func TestValidatePlanForSubmissionValidProject(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)

	raw, err := json.Marshal(validReadQueryPlan("plan-ok", "relay"))
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	report, err := svc.ValidatePlanForSubmission(context.Background(), raw, "relay")
	if err != nil {
		t.Fatalf("ValidatePlanForSubmission: %v", err)
	}
	if !report.Valid {
		t.Fatalf("expected valid report, got issues %+v", report.Issues)
	}
}

func TestListPlanReadSummariesUnknownProject(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	summaries, err := svc.ListPlanReadSummaries(context.Background(), PlanListQuery{
		Limit:     50,
		ProjectID: "missing-project",
	})
	if err != nil {
		t.Fatalf("ListPlanReadSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected empty list for unknown project, got %d", len(summaries))
	}
}

func TestListPlanReadSummariesReturnsPassCounts(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	summaries, err := svc.ListPlanReadSummaries(context.Background(), PlanListQuery{Limit: 50})
	if err != nil {
		t.Fatalf("ListPlanReadSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Plan.PlanID != "plan-123" {
		t.Fatalf("expected plan-123, got %q", summary.Plan.PlanID)
	}
	if len(summary.Passes) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(summary.Passes))
	}
	if summary.CompletionReady {
		t.Fatalf("expected completion not ready for planned passes")
	}
}

func TestListPlanReadSummariesCompletionReady(t *testing.T) {
	t.Parallel()

	svc, st := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	plan, err := st.GetPlanByPlanID("plan-123")
	if err != nil {
		t.Fatalf("GetPlanByPlanID: %v", err)
	}
	passes, err := st.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		t.Fatalf("ListPlanPassesByPlan: %v", err)
	}
	terminal := []string{StatusPassCompleted, StatusPassSkipped}
	for i, pass := range passes {
		if _, err := st.UpdatePlanPassStatus(pass.ID, terminal[i%len(terminal)]); err != nil {
			t.Fatalf("UpdatePlanPassStatus: %v", err)
		}
	}

	summaries, err := svc.ListPlanReadSummaries(context.Background(), PlanListQuery{Limit: 50})
	if err != nil {
		t.Fatalf("ListPlanReadSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if !summaries[0].CompletionReady {
		t.Fatalf("expected completion ready when all passes terminal")
	}
}

func TestGetPlanDetailNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)

	_, err := svc.GetPlanDetail(context.Background(), PlanDetailQuery{PlanID: "missing"})
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestGetPlanDetailProjectMismatch(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	_, err := svc.GetPlanDetail(context.Background(), PlanDetailQuery{
		PlanID:    "plan-123",
		ProjectID: "other-project",
	})
	if !errors.Is(err, ErrPlanProjectMismatch) {
		t.Fatalf("expected ErrPlanProjectMismatch, got %v", err)
	}
}

func TestGetPlanDetailReturnsComposition(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	detail, err := svc.GetPlanDetail(context.Background(), PlanDetailQuery{PlanID: "plan-123"})
	if err != nil {
		t.Fatalf("GetPlanDetail: %v", err)
	}
	if detail.Plan.PlanID != "plan-123" {
		t.Fatalf("expected plan-123, got %q", detail.Plan.PlanID)
	}
	if len(detail.Passes) != 2 {
		t.Fatalf("expected 2 passes, got %d", len(detail.Passes))
	}
	if detail.RunsByPass == nil {
		t.Fatalf("expected non-nil RunsByPass map")
	}
	if detail.CompletionReady {
		t.Fatalf("expected completion not ready for planned passes")
	}
}

func TestGetPlanPassDetailPassNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	_, err := svc.GetPlanPassDetail(context.Background(), PlanPassDetailQuery{
		PlanID: "plan-123",
		PassID: "PASS-999",
	})
	if !errors.Is(err, ErrPlanPassNotFound) {
		t.Fatalf("expected ErrPlanPassNotFound, got %v", err)
	}
}

func TestGetPlanPassDetailPlanNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)

	_, err := svc.GetPlanPassDetail(context.Background(), PlanPassDetailQuery{
		PlanID: "missing",
		PassID: "PASS-001",
	})
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestGetPlanPassDetailReturnsComposition(t *testing.T) {
	t.Parallel()

	svc, _ := newReadQueryTestService(t)
	submitReadQueryPlan(t, svc, "plan-123", "relay")

	detail, err := svc.GetPlanPassDetail(context.Background(), PlanPassDetailQuery{
		PlanID: "plan-123",
		PassID: "PASS-001",
	})
	if err != nil {
		t.Fatalf("GetPlanPassDetail: %v", err)
	}
	if detail.Pass.PassID != "PASS-001" {
		t.Fatalf("expected PASS-001, got %q", detail.Pass.PassID)
	}
	if len(detail.Passes) != 2 {
		t.Fatalf("expected 2 sibling passes, got %d", len(detail.Passes))
	}
	if len(detail.AssociatedRuns) != 0 {
		t.Fatalf("expected no associated runs, got %d", len(detail.AssociatedRuns))
	}
}
