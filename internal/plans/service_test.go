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
	raw := mustMarshalPlan(t, validPlannerPassPlan())

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{
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
	if len(result.Passes) != 2 {
		t.Fatalf("expected 2 plan passes, got %d", len(result.Passes))
	}
	if result.Passes[0].Sequence != 1 || result.Passes[1].Sequence != 2 {
		t.Fatalf("expected ordered passes by sequence, got %d then %d", result.Passes[0].Sequence, result.Passes[1].Sequence)
	}
	if result.Passes[0].DependenciesJson != "[]" {
		t.Fatalf("expected first pass dependencies to be []")
	}
	if result.Passes[1].DependenciesJson != `["PASS-001"]` {
		t.Fatalf("unexpected stored dependencies JSON: %s", result.Passes[1].DependenciesJson)
	}

	if got := countRows(t, st.DB(), "plans"); got != 1 {
		t.Fatalf("expected 1 plan row, got %d", got)
	}
	if got := countRows(t, st.DB(), "plan_passes"); got != 2 {
		t.Fatalf("expected 2 plan_passes rows, got %d", got)
	}
}

func TestSubmitPlanRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{
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

	if _, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw}); err != nil {
		t.Fatalf("first SubmitPlan returned error: %v", err)
	}

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: raw})
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

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, plan)})
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

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, plan)})
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

			result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, plan)})
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

			result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, plan)})
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

	result, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, plan)})
	if err != nil {
		t.Fatalf("SubmitPlan returned error: %v", err)
	}
	assertIssueCode(t, result.Report, IssuePlanSecretDetected)
	if got := countRows(t, st.DB(), "plans"); got != 0 {
		t.Fatalf("expected 0 plan rows, got %d", got)
	}
}

func TestSubmitPlanRollsBackOnPassInsertFailure(t *testing.T) {
	t.Parallel()

	svc, st := newTestService(t)
	if _, err := st.DB().Exec(`CREATE TRIGGER fail_plan_pass_insert BEFORE INSERT ON plan_passes BEGIN SELECT RAISE(FAIL, 'pass insert failed'); END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := svc.SubmitPlan(context.Background(), SubmitPlanRequest{RawJSON: mustMarshalPlan(t, validPlannerPassPlan())})
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

	return NewService(st), st
}

func validPlannerPassPlan() PlannerPassPlan {
	return PlannerPassPlan{
		PlanMeta: PlanMeta{
			PlanID:        "plan-123",
			SchemaVersion: "1.0.0",
			CreatedAt:     "2026-06-21T16:10:00Z",
			Title:         "Relay plan submission service",
			Goal:          "Store validated planner plans",
			RepoTarget:    "Paintersrp/relay",
			BranchContext: "main",
			Status:        "active",
		},
		SourceIntent: SourceIntent{
			Summary: "Add a backend service for validated plan submission.",
		},
		Passes: []PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "Validate plans",
				Goal:                   "Validate syntax and semantics.",
				IntendedExecutionScope: []string{"internal/plans/validator.go"},
				NonGoals:               []string{"No API routes"},
				Dependencies:           []string{},
				Status:                 "planned",
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

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", table, err)
	}
	return count
}
