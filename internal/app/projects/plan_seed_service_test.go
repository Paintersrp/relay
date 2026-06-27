package projects

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestPlanSeedCreateListGetAndUpdate(t *testing.T) {
	t.Parallel()

	svc, _ := newProjectTestService(t)

	// Create project
	_, projIssues, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(projIssues) != 0 {
		t.Fatalf("expected no project issues, got %+v", projIssues)
	}

	// Create plan seed
	seed, seedIssues, err := svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		SeedID:       "seed-1",
		Title:        "First Seed Idea",
		QuickContext: "A test plan seed creation",
		Constraints:  []string{"constraint 1", "constraint 2"},
		NonGoals:     []string{"nongoal 1"},
		Tags:         []string{"tag-1"},
		Priority:     "high",
		SourceType:   PlanSeedSourceManual,
	})
	if err != nil {
		t.Fatalf("CreatePlanSeed error: %v", err)
	}
	if len(seedIssues) != 0 {
		t.Fatalf("expected no plan seed issues, got %+v", seedIssues)
	}

	if seed.SeedID != "seed-1" {
		t.Fatalf("expected seed_id 'seed-1', got %q", seed.SeedID)
	}
	if seed.Title != "First Seed Idea" {
		t.Fatalf("expected title 'First Seed Idea', got %q", seed.Title)
	}
	if seed.Status != PlanSeedStatusCaptured {
		t.Fatalf("expected status 'captured', got %q", seed.Status)
	}
	if len(seed.Constraints) != 2 || seed.Constraints[0] != "constraint 1" {
		t.Fatalf("expected constraints rounding, got %+v", seed.Constraints)
	}
	if len(seed.NonGoals) != 1 || seed.NonGoals[0] != "nongoal 1" {
		t.Fatalf("expected non_goals rounding, got %+v", seed.NonGoals)
	}
	if len(seed.Tags) != 1 || seed.Tags[0] != "tag-1" {
		t.Fatalf("expected tags rounding, got %+v", seed.Tags)
	}

	// Get plan seed
	got, err := svc.GetPlanSeed(t.Context(), "relay", "seed-1")
	if err != nil {
		t.Fatalf("GetPlanSeed error: %v", err)
	}
	if got.Title != seed.Title {
		t.Fatalf("expected same title, got %q vs %q", got.Title, seed.Title)
	}

	// List plan seeds
	list, listIssues, err := svc.ListPlanSeeds(t.Context(), "relay", "", 50)
	if err != nil {
		t.Fatalf("ListPlanSeeds error: %v", err)
	}
	if len(listIssues) != 0 {
		t.Fatalf("expected no issues, got %+v", listIssues)
	}
	if len(list) != 1 {
		t.Fatalf("expected list of length 1, got %d", len(list))
	}

	listCaptured, listCapturedIssues, err := svc.ListPlanSeeds(t.Context(), "relay", PlanSeedStatusCaptured, 50)
	if err != nil {
		t.Fatalf("ListPlanSeeds with status error: %v", err)
	}
	if len(listCapturedIssues) != 0 {
		t.Fatalf("expected no issues, got %+v", listCapturedIssues)
	}
	if len(listCaptured) != 1 {
		t.Fatalf("expected listCaptured of length 1, got %d", len(listCaptured))
	}

	listDeferred, listDeferredIssues, err := svc.ListPlanSeeds(t.Context(), "relay", PlanSeedStatusDeferred, 50)
	if err != nil {
		t.Fatalf("ListPlanSeeds with status error: %v", err)
	}
	if len(listDeferredIssues) != 0 {
		t.Fatalf("expected no issues, got %+v", listDeferredIssues)
	}
	if len(listDeferred) != 0 {
		t.Fatalf("expected listDeferred of length 0, got %d", len(listDeferred))
	}

	// Update plan seed
	updated, updateIssues, err := svc.UpdatePlanSeed(t.Context(), "relay", "seed-1", PlanSeedInput{
		Title:        "Updated Seed Idea",
		QuickContext: "An updated quick context",
		Constraints:  []string{"constraint 1"},
		NonGoals:     []string{},
		Tags:         []string{"tag-2"},
		Priority:     "medium",
	})
	if err != nil {
		t.Fatalf("UpdatePlanSeed error: %v", err)
	}
	if len(updateIssues) != 0 {
		t.Fatalf("expected no update issues, got %+v", updateIssues)
	}
	if updated.Title != "Updated Seed Idea" {
		t.Fatalf("expected updated title, got %q", updated.Title)
	}
	if len(updated.Constraints) != 1 || len(updated.NonGoals) != 0 || len(updated.Tags) != 1 || updated.Tags[0] != "tag-2" {
		t.Fatalf("expected updated lists, got %+v", updated)
	}
}

func TestPlanSeedRejectsUnknownProject(t *testing.T) {
	t.Parallel()

	svc, _ := newProjectTestService(t)

	_, _, err := svc.CreatePlanSeed(t.Context(), "missing", PlanSeedInput{
		Title:        "Title",
		QuickContext: "Context",
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}

	_, _, err = svc.ListPlanSeeds(t.Context(), "missing", "", 10)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

func TestPlanSeedValidationRejectsMissingAndSecretLikeInput(t *testing.T) {
	t.Parallel()

	svc, _ := newProjectTestService(t)

	_, _, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	// Test missing required fields
	_, issues, err := svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		Title:        "",
		QuickContext: "",
	})
	if err != nil {
		t.Fatalf("CreatePlanSeed error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatalf("expected validation issues for missing fields")
	}

	var hasTitleErr, hasContextErr bool
	for _, issue := range issues {
		if issue.Field == "title" && issue.Code == PlanSeedIssueRequired {
			hasTitleErr = true
		}
		if issue.Field == "quick_context" && issue.Code == PlanSeedIssueRequired {
			hasContextErr = true
		}
	}
	if !hasTitleErr || !hasContextErr {
		t.Fatalf("expected title and quick_context required issues, got: %+v", issues)
	}

	// Test secret-like value
	_, issues, err = svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		Title:        "Safe Title",
		QuickContext: "Authorization: Bearer sk-secret",
	})
	if err != nil {
		t.Fatalf("CreatePlanSeed error: %v", err)
	}
	if len(issues) == 0 {
		t.Fatalf("expected validation issues for secret-like value")
	}

	var hasSecretErr bool
	for _, issue := range issues {
		if issue.Field == "quick_context" && issue.Code == PlanSeedIssueSecretLikeValue {
			hasSecretErr = true
		}
	}
	if !hasSecretErr {
		t.Fatalf("expected secret_like_value issue, got: %+v", issues)
	}
}

func TestPlanSeedLifecycleTransitions(t *testing.T) {
	t.Parallel()

	svc, _ := newProjectTestService(t)

	_, _, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	// Create captured seed
	_, createIssues, err := svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		SeedID:       "seed-1",
		Title:        "Lifecycle Seed",
		QuickContext: "A test plan seed lifecycle",
	})
	if err != nil || len(createIssues) > 0 {
		t.Fatalf("CreatePlanSeed error: %v, issues: %+v", err, createIssues)
	}

	// Defer without reason (should fail)
	_, issues, err := svc.DeferPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		DeferReason: "",
	})
	if err != nil {
		t.Fatalf("DeferPlanSeed error: %v", err)
	}
	if len(issues) == 0 || issues[0].Field != "defer_reason" || issues[0].Code != PlanSeedIssueRequired {
		t.Fatalf("expected defer_reason required issue, got %+v", issues)
	}

	// Defer with secret-like reason (should fail)
	_, issues, err = svc.DeferPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		DeferReason: "Defer until sk-key is rotated",
	})
	if err != nil {
		t.Fatalf("DeferPlanSeed error: %v", err)
	}
	if len(issues) == 0 || issues[0].Field != "defer_reason" || issues[0].Code != PlanSeedIssueSecretLikeValue {
		t.Fatalf("expected secret_like_value issue, got %+v", issues)
	}

	// Defer successfully
	deferred, issues, err := svc.DeferPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		DeferReason: "Need feedback from Planner",
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("DeferPlanSeed error: %v, issues: %+v", err, issues)
	}
	if deferred.Status != PlanSeedStatusDeferred || deferred.DeferReason != "Need feedback from Planner" {
		t.Fatalf("unexpected deferred state: %+v", deferred)
	}

	// Relaunch successfully
	relaunched, relaunchIssues, err := svc.RelaunchDeferredPlanSeed(t.Context(), "relay", "seed-1")
	if err != nil || len(relaunchIssues) > 0 {
		t.Fatalf("RelaunchDeferredPlanSeed error: %v, issues: %+v", err, relaunchIssues)
	}
	if relaunched.Status != PlanSeedStatusCaptured || relaunched.DeferReason != "" {
		t.Fatalf("unexpected relaunched state: %+v", relaunched)
	}

	// Defer again
	_, _, err = svc.DeferPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		DeferReason: "Some defer reason",
	})
	if err != nil {
		t.Fatalf("DeferPlanSeed error: %v", err)
	}

	// Reject from deferred status
	rejected, rejectIssues, err := svc.RejectPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		RejectReason: "No longer relevant",
	})
	if err != nil || len(rejectIssues) > 0 {
		t.Fatalf("RejectPlanSeed error: %v, issues: %+v", err, rejectIssues)
	}
	if rejected.Status != PlanSeedStatusRejected || rejected.RejectReason != "No longer relevant" {
		t.Fatalf("unexpected rejected state: %+v", rejected)
	}

	// Attempting to relaunch rejected seed should fail
	_, relaunchIssues2, err := svc.RelaunchDeferredPlanSeed(t.Context(), "relay", "seed-1")
	if err != nil {
		t.Fatalf("RelaunchDeferredPlanSeed error: %v", err)
	}
	if len(relaunchIssues2) == 0 || relaunchIssues2[0].Code != PlanSeedIssueInvalidTransition {
		t.Fatalf("expected invalid_transition issue, got %+v", relaunchIssues2)
	}

	// Attempting to update rejected seed should fail
	_, updateIssues2, err := svc.UpdatePlanSeed(t.Context(), "relay", "seed-1", PlanSeedInput{
		Title:        "Try updating rejected",
		QuickContext: "Some context",
	})
	if err != nil {
		t.Fatalf("UpdatePlanSeed error: %v", err)
	}
	if len(updateIssues2) == 0 || updateIssues2[0].Code != PlanSeedIssueTerminalStatus {
		t.Fatalf("expected terminal_status issue, got %+v", updateIssues2)
	}
}

func TestPlanSeedPlannedLinkageIdempotency(t *testing.T) {
	t.Parallel()

	svc, _ := newProjectTestService(t)

	_, _, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	// Create captured seed
	_, createIssues, err := svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		SeedID:       "seed-1",
		Title:        "Linkage Seed",
		QuickContext: "A test plan seed linkage",
	})
	if err != nil || len(createIssues) > 0 {
		t.Fatalf("CreatePlanSeed error: %v, issues: %+v", err, createIssues)
	}

	// Link attempt
	planned, issues, err := svc.LinkPlanSeedAttempt(t.Context(), "relay", "seed-1", PlanSeedAttemptLinkInput{
		PlanAttemptID: "plan-attempt-1",
	})
	if err != nil || len(issues) > 0 {
		t.Fatalf("LinkPlanSeedAttempt error: %v, issues: %+v", err, issues)
	}
	if planned.Status != PlanSeedStatusPlanned || planned.PlanAttemptID != "plan-attempt-1" || planned.PlannedAt == "" {
		t.Fatalf("unexpected planned state: %+v", planned)
	}

	// Link same attempt again (idempotent, should succeed)
	plannedIdempotent, issues2, err := svc.LinkPlanSeedAttempt(t.Context(), "relay", "seed-1", PlanSeedAttemptLinkInput{
		PlanAttemptID: "plan-attempt-1",
	})
	if err != nil || len(issues2) > 0 {
		t.Fatalf("LinkPlanSeedAttempt idempotent error: %v, issues: %+v", err, issues2)
	}
	if plannedIdempotent.PlanAttemptID != "plan-attempt-1" {
		t.Fatalf("expected planAttemptId plan-attempt-1, got %q", plannedIdempotent.PlanAttemptID)
	}

	// Link different attempt (should fail)
	_, issues3, err := svc.LinkPlanSeedAttempt(t.Context(), "relay", "seed-1", PlanSeedAttemptLinkInput{
		PlanAttemptID: "plan-attempt-2",
	})
	if err != nil {
		t.Fatalf("LinkPlanSeedAttempt different error: %v", err)
	}
	if len(issues3) == 0 || issues3[0].Field != "plan_attempt_id" || issues3[0].Code != PlanSeedIssueDuplicateLinkage {
		t.Fatalf("expected duplicate_linkage issue, got %+v", issues3)
	}

	// Link managed plan
	linkedManaged, issues4, err := svc.LinkPlanSeedManagedPlan(t.Context(), "relay", "seed-1", PlanSeedManagedPlanLinkInput{
		ManagedPlanID: "managed-plan-1",
	})
	if err != nil || len(issues4) > 0 {
		t.Fatalf("LinkPlanSeedManagedPlan error: %v, issues: %+v", err, issues4)
	}
	if linkedManaged.ManagedPlanID != "managed-plan-1" || linkedManaged.Status != PlanSeedStatusPlanned {
		t.Fatalf("unexpected linkedManaged state: %+v", linkedManaged)
	}

	// Link same managed plan again (idempotent, should succeed)
	linkedManagedIdempotent, issues5, err := svc.LinkPlanSeedManagedPlan(t.Context(), "relay", "seed-1", PlanSeedManagedPlanLinkInput{
		ManagedPlanID: "managed-plan-1",
	})
	if err != nil || len(issues5) > 0 {
		t.Fatalf("LinkPlanSeedManagedPlan idempotent error: %v, issues: %+v", err, issues5)
	}
	if linkedManagedIdempotent.ManagedPlanID != "managed-plan-1" {
		t.Fatalf("expected managedPlanId managed-plan-1, got %q", linkedManagedIdempotent.ManagedPlanID)
	}

	// Link different managed plan (should fail)
	_, issues6, err := svc.LinkPlanSeedManagedPlan(t.Context(), "relay", "seed-1", PlanSeedManagedPlanLinkInput{
		ManagedPlanID: "managed-plan-2",
	})
	if err != nil {
		t.Fatalf("LinkPlanSeedManagedPlan different error: %v", err)
	}
	if len(issues6) == 0 || issues6[0].Field != "managed_plan_id" || issues6[0].Code != PlanSeedIssueDuplicateLinkage {
		t.Fatalf("expected duplicate_linkage issue, got %+v", issues6)
	}
}

func getTableRowCount(t *testing.T, db *sql.DB, tableName string) int {
	var count int
	err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query count for %s: %v", tableName, err)
	}
	return count
}

func TestPlanSeedNoDownstreamSideEffects(t *testing.T) {
	t.Parallel()

	svc, st := newProjectTestService(t)
	db := st.DB()

	_, _, err := svc.CreateProject(t.Context(), ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	// Check table row counts are initially 0
	tables := []string{"intent_packets", "plan_attempts", "plans", "plan_passes", "runs"}
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected initial count for %s to be 0, got %d", table, count)
		}
	}

	// Capture seed
	_, createIssues, err := svc.CreatePlanSeed(t.Context(), "relay", PlanSeedInput{
		SeedID:       "seed-1",
		Title:        "Side Effect Seed",
		QuickContext: "A test plan seed for side effect verification",
	})
	if err != nil || len(createIssues) > 0 {
		t.Fatalf("CreatePlanSeed error: %v, issues: %+v", err, createIssues)
	}

	// Check table row counts remain 0
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected count for %s to remain 0 after CreatePlanSeed, got %d", table, count)
		}
	}

	// Update seed
	_, updateIssues, err := svc.UpdatePlanSeed(t.Context(), "relay", "seed-1", PlanSeedInput{
		Title:        "Updated Side Effect Seed",
		QuickContext: "An updated context for side effect verification",
	})
	if err != nil || len(updateIssues) > 0 {
		t.Fatalf("UpdatePlanSeed error: %v, issues: %+v", err, updateIssues)
	}

	// Check table row counts remain 0
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected count for %s to remain 0 after UpdatePlanSeed, got %d", table, count)
		}
	}

	// Defer seed
	_, deferIssues, err := svc.DeferPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		DeferReason: "Reason for deferring",
	})
	if err != nil || len(deferIssues) > 0 {
		t.Fatalf("DeferPlanSeed error: %v, issues: %+v", err, deferIssues)
	}

	// Check table row counts remain 0
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected count for %s to remain 0 after DeferPlanSeed, got %d", table, count)
		}
	}

	// Relaunch seed
	_, relaunchIssues, err := svc.RelaunchDeferredPlanSeed(t.Context(), "relay", "seed-1")
	if err != nil || len(relaunchIssues) > 0 {
		t.Fatalf("RelaunchDeferredPlanSeed error: %v, issues: %+v", err, relaunchIssues)
	}

	// Check table row counts remain 0
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected count for %s to remain 0 after RelaunchDeferredPlanSeed, got %d", table, count)
		}
	}

	// Reject seed
	_, rejectIssues, err := svc.RejectPlanSeed(t.Context(), "relay", "seed-1", PlanSeedLifecycleInput{
		RejectReason: "Reason for rejecting",
	})
	if err != nil || len(rejectIssues) > 0 {
		t.Fatalf("RejectPlanSeed error: %v, issues: %+v", err, rejectIssues)
	}

	// Check table row counts remain 0
	for _, table := range tables {
		count := getTableRowCount(t, db, table)
		if count != 0 {
			t.Fatalf("expected count for %s to remain 0 after RejectPlanSeed, got %d", table, count)
		}
	}
}
