package intake

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/store"
)

// newIntakeServiceTestStore builds a store with a registered project and repo and
// points artifact writes at a temp dir so CreateRunFromHandoff can run end to end.
func newIntakeServiceTestStore(t *testing.T) *store.Store {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateProject("relay", "Relay", "Intake Service Test Project", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := st.CreateRepo("relay", filepath.Join(dir, "repo")); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	artifacts.SetBaseDir(dir)
	return st
}

func intakeBoolPtr(value bool) *bool { return &value }

// seedManagedPlanWithSourceContextPass submits a single-pass managed plan whose
// pass declares source/context requirements (populated context plan).
func seedManagedPlanWithSourceContextPass(t *testing.T, st *store.Store, planID string) (*store.Plan, *store.PlanPass) {
	t.Helper()

	plan := plans.PlannerPassPlan{
		PlanMeta: plans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     "2026-06-23T00:00:00Z",
			Title:         "Intake service test plan",
			Goal:          "Exercise managed-pass provenance gate in the shared intake service.",
			RepoTarget:    "relay",
			BranchContext: "main",
			Status:        "active",
			ProjectID:     "relay",
			MCPCapabilityProfile: &plans.MCPCapabilityProfile{
				ProfileID:            "test",
				Mode:                 "submission_only",
				ContextBrokerEnabled: intakeBoolPtr(false),
			},
		},
		SourceIntent: plans.SourceIntent{Summary: "Intake service test."},
		GlobalContextRules: &plans.GlobalContextRules{
			DefaultSourceOfTruth:    "Relay managed plan.",
			PlannerContextBoundary:  "Test only.",
			ForbiddenContextDomains: []string{"GitHub issues"},
		},
		Passes: []plans.PlanPassInput{
			{
				PassID:                 "PASS-001",
				Sequence:               1,
				Name:                   "First pass",
				Goal:                   "First pass goal",
				IntendedExecutionScope: []string{"internal/plans"},
				NonGoals:               []string{"No UI"},
				Dependencies:           []string{},
				Status:                 "planned",
				PassType:               "backend_vertical_slice",
				ContextPlan: plans.ContextPlan{
					RequiredRepositories: []string{"relay"},
					SeedSearchTerms: []plans.ContextSearchTerm{
						{RepoID: "relay", Query: "plans validate", Purpose: "optional", Required: intakeBoolPtr(false)},
					},
					SeedFilesToRead: []plans.ContextFileRead{
						{RepoID: "relay", Path: "internal/plans/validator.go", Purpose: "optional", Required: intakeBoolPtr(false)},
					},
					ContextCoverageExpectations: []string{"coverage ok"},
					BlockedIfMissing:            []string{"not blocked"},
				},
				SourceSnapshotRequirements: plans.SourceSnapshotRequirements{
					RequireGitStatus:   intakeBoolPtr(false),
					RequireCommitSHA:   intakeBoolPtr(false),
					AllowDirtyWorktree: intakeBoolPtr(true),
				},
				HandoffReadinessCriteria: []string{"Pass 1 complete"},
			},
		},
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	result, err := plans.NewService(st).SubmitPlan(context.Background(), plans.SubmitPlanRequest{UnmanagedAcknowledged: true,
		RawJSON:            raw,
		SourceArtifactPath: "handoffs/planner/intake-service-test.json",
		ProjectID:          "relay",
	})
	if err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if !result.Report.Valid {
		t.Fatalf("expected valid plan, got issues: %+v", result.Report.Issues)
	}

	createdPlan, err := st.GetPlanByPlanID(planID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	pass, err := st.GetPlanPassByPassID(createdPlan.ID, "PASS-001")
	if err != nil {
		t.Fatalf("get pass: %v", err)
	}
	return createdPlan, pass
}

func seedIntakeSourceSnapshot(t *testing.T, st *store.Store, plan *store.Plan, snapshotID string) {
	t.Helper()
	if _, err := st.CreateSourceSnapshot(store.CreateSourceSnapshotParams{
		SourceSnapshotID: snapshotID,
		ProjectRowID:     plan.ProjectRowID,
		ProjectID:        "relay",
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "2026-06-23T00:00:00Z",
		SummaryJSON:      "{}",
	}); err != nil {
		t.Fatalf("create source snapshot: %v", err)
	}
}

func validServiceTestMarkdown(title string) string {
	return `---
title: ` + title + `
repo: relay
branch: main
---

<decision_log>
- D1: Test decision for service test.
</decision_log>

<constraints>
- C1: Test constraint for service test.
</constraints>

<compiler_input>
` + "```" + `yaml
compiler_input:
  goal: "Test service-level behavior."
  scope: "Deterministic service testing only."
  file_targets:
    - path: "internal/intake/service.go"
      role: primary
      action: must_edit
      reason: "Service implementation."
  implementation_steps:
    - id: S1
      title: "Run service tests"
      action: modify
      target_paths:
        - "internal/intake/service_test.go"
      instructions: "Run the tests."
      acceptance_criteria:
        - "Tests pass."
  code_requirements:
    - id: CR1
      requirement: "Service must handle provenance gating."
      applies_to:
        - "internal/intake/service.go"
  validation_contract:
    mode: commands
    failure_policy: block
    commands:
      - command: "go test ./internal/intake -count=1"
        required: true
  completion_contract:
    done_when:
      - "Tests pass."
    blocked_when:
      - "Tests fail."
` + "```" + `
</compiler_input>`
}

func serviceMarkdownWithMetadataPath(title, field, value string) string {
	markdown := validServiceTestMarkdown(title)
	if field == "" {
		return markdown
	}
	return strings.Replace(markdown, "branch: main\n---", "branch: main\n"+field+": "+value+"\n---", 1)
}

func serviceMarkdownWithMetadataPaths(title, sourcePath, intendedPath string) string {
	markdown := validServiceTestMarkdown(title)
	insert := ""
	if sourcePath != "" {
		insert += "source_artifact_path: " + sourcePath + "\n"
	}
	if intendedPath != "" {
		insert += "intended_handoff_path: " + intendedPath + "\n"
	}
	return strings.Replace(markdown, "branch: main\n---", "branch: main\n"+insert+"---", 1)
}

func countServiceRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestCreateRunFromHandoff_DurableMetadataPathValidation(t *testing.T) {
	cases := []struct {
		name        string
		field       string
		value       string
		wantPath    string
		wantBlocked bool
	}{
		{name: "safe source path normalized", field: "source_artifact_path", value: `handoffs\planner\reviewed.md`, wantPath: "handoffs/planner/reviewed.md"},
		{name: "safe intended path normalized", field: "intended_handoff_path", value: `handoffs\planner\intended.md`, wantPath: "handoffs/planner/intended.md"},
		{name: "empty persists empty", wantPath: ""},
	}
	unsafeValues := []string{
		"/absolute/path.md",
		`\rooted\path.md`,
		`C:\folder\file.md`,
		"C:/folder/file.md",
		`C:folder\file.md`,
		`\\server\share\file.md`,
		"//server/share/file.md",
		"../file.md",
		`..\file.md`,
		"nested/../../file.md",
		`nested\..\..\file.md`,
		"handoffs/bad\x00path.md",
	}
	for _, field := range []string{"source_artifact_path", "intended_handoff_path"} {
		for _, value := range unsafeValues {
			cases = append(cases, struct {
				name        string
				field       string
				value       string
				wantPath    string
				wantBlocked bool
			}{name: field + " blocks " + value, field: field, value: value, wantBlocked: true})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newIntakeServiceTestStore(t)
			filesBefore := countServiceArtifactFiles(t, artifacts.BaseDir)
			svc := NewService(st)
			out, err := svc.CreateRunFromHandoff(CreateRunInput{
				Markdown:   serviceMarkdownWithMetadataPath(tc.name, tc.field, tc.value),
				RepoTarget: "relay",
			})
			if tc.wantBlocked {
				if err == nil {
					t.Fatal("expected blocked unsafe metadata path")
				}
				var inputErr *InputError
				if !errors.As(err, &inputErr) || inputErr.Code != ErrCodeValidation {
					t.Fatalf("expected validation InputError, got %T: %v", err, err)
				}
				for _, table := range []string{"runs", "artifacts", "run_submission_provenance", "events"} {
					if got := countServiceRows(t, st.DB(), table); got != 0 {
						t.Fatalf("expected no %s rows, got %d", table, got)
					}
				}
				if got := countServiceArtifactFiles(t, artifacts.BaseDir); got != filesBefore {
					t.Fatalf("expected artifact filesystem file count to remain %d, got %d", filesBefore, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if out.Provenance.SourceArtifactPath != tc.wantPath {
				t.Fatalf("output source path = %q, want %q", out.Provenance.SourceArtifactPath, tc.wantPath)
			}
			var stored string
			if err := st.DB().QueryRow("SELECT source_artifact_path FROM run_submission_provenance WHERE run_id = ?", out.RunID).Scan(&stored); err != nil {
				t.Fatalf("load provenance path: %v", err)
			}
			if stored != tc.wantPath {
				t.Fatalf("stored source path = %q, want %q", stored, tc.wantPath)
			}
		})
	}
}

func TestCreateRunFromHandoff_NormalizesBothMetadataPathAliasesBeforePersistence(t *testing.T) {
	st := newIntakeServiceTestStore(t)
	svc := NewService(st)
	out, err := svc.CreateRunFromHandoff(CreateRunInput{
		Markdown:   serviceMarkdownWithMetadataPaths("Both Paths", `handoffs\planner\reviewed.md`, `handoffs\planner\intended.md`),
		RepoTarget: "relay",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if out.Provenance.SourceArtifactPath != "handoffs/planner/reviewed.md" {
		t.Fatalf("output source path = %q", out.Provenance.SourceArtifactPath)
	}

	var storedPath, metadataJSON string
	if err := st.DB().QueryRow("SELECT source_artifact_path, handoff_metadata_json FROM run_submission_provenance WHERE run_id = ?", out.RunID).Scan(&storedPath, &metadataJSON); err != nil {
		t.Fatalf("load provenance row: %v", err)
	}
	if storedPath != "handoffs/planner/reviewed.md" {
		t.Fatalf("stored source path = %q", storedPath)
	}
	assertMetadataPathsNormalizedJSON(t, metadataJSON)
	assertArtifactMetadataPathsNormalized(t, st, out.RunID, "parsed_frontmatter")
	assertArtifactMetadataPathsNormalized(t, st, out.RunID, "planner_handoff_provenance_json")
}

func TestCreateRunFromHandoff_BlocksWhenEitherMetadataAliasUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name         string
		sourcePath   string
		intendedPath string
		wantField    string
	}{
		{name: "safe primary unsafe secondary", sourcePath: "handoffs/reviewed.md", intendedPath: `C:\Users\operator\secret.md`, wantField: "intended_handoff_path"},
		{name: "unsafe primary safe secondary", sourcePath: "/tmp/reviewed.md", intendedPath: "handoffs/reviewed.md", wantField: "source_artifact_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newIntakeServiceTestStore(t)
			plan, pass := seedManagedPlanWithSourceContextPass(t, st, "metadata-alias-"+strings.ReplaceAll(tc.name, " ", "-"))
			seedIntakeSourceSnapshot(t, st, plan, "snapshot-"+strings.ReplaceAll(tc.name, " ", "-"))
			filesBefore := countServiceArtifactFiles(t, artifacts.BaseDir)
			svc := NewService(st)
			_, err := svc.CreateRunFromHandoff(CreateRunInput{
				Markdown:         serviceMarkdownWithMetadataPaths(tc.name, tc.sourcePath, tc.intendedPath),
				RepoTarget:       "relay",
				PlanID:           plan.PlanID,
				PassID:           "PASS-001",
				SourceSnapshotID: "snapshot-" + strings.ReplaceAll(tc.name, " ", "-"),
			})
			if err == nil {
				t.Fatal("expected unsafe alias to block")
			}
			var inputErr *InputError
			if !errors.As(err, &inputErr) || inputErr.Code != ErrCodeValidation || inputErr.Field != tc.wantField {
				t.Fatalf("expected validation InputError for %s, got %T: %+v", tc.wantField, err, err)
			}
			for _, table := range []string{"runs", "artifacts", "run_submission_provenance", "events"} {
				if got := countServiceRows(t, st.DB(), table); got != 0 {
					t.Fatalf("expected no %s rows, got %d", table, got)
				}
			}
			refreshed, err := st.GetPlanPass(pass.ID)
			if err != nil {
				t.Fatalf("reload pass: %v", err)
			}
			if refreshed.Status != pass.Status {
				t.Fatalf("expected pass status %q to remain unchanged, got %q", pass.Status, refreshed.Status)
			}
			if got := countServiceArtifactFiles(t, artifacts.BaseDir); got != filesBefore {
				t.Fatalf("expected artifact filesystem file count to remain %d, got %d", filesBefore, got)
			}
		})
	}
}

func assertMetadataPathsNormalizedJSON(t *testing.T, raw string) {
	t.Helper()
	if strings.Contains(raw, `\`) {
		t.Fatalf("metadata JSON contains backslash: %s", raw)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal metadata JSON: %v", err)
	}
	if payload["source_artifact_path"] != "handoffs/planner/reviewed.md" {
		t.Fatalf("source_artifact_path = %v", payload["source_artifact_path"])
	}
	if payload["intended_handoff_path"] != "handoffs/planner/intended.md" {
		t.Fatalf("intended_handoff_path = %v", payload["intended_handoff_path"])
	}
}

func assertArtifactMetadataPathsNormalized(t *testing.T, st *store.Store, runID int64, kind string) {
	t.Helper()
	var path string
	if err := st.DB().QueryRow("SELECT path FROM artifacts WHERE run_id = ? AND kind = ?", runID, kind).Scan(&path); err != nil {
		t.Fatalf("load artifact %s: %v", kind, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %s: %v", kind, err)
	}
	raw := string(data)
	if kind == "planner_handoff_provenance_json" {
		if strings.Contains(raw, `handoffs\planner`) {
			t.Fatalf("provenance artifact contains unnormalized metadata path: %s", raw)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal provenance artifact: %v", err)
		}
		metadata, ok := payload["handoff_metadata"].(map[string]interface{})
		if !ok {
			t.Fatalf("provenance artifact missing handoff_metadata: %+v", payload)
		}
		if metadata["source_artifact_path"] != "handoffs/planner/reviewed.md" {
			t.Fatalf("provenance metadata source_artifact_path = %v", metadata["source_artifact_path"])
		}
		if metadata["intended_handoff_path"] != "handoffs/planner/intended.md" {
			t.Fatalf("provenance metadata intended_handoff_path = %v", metadata["intended_handoff_path"])
		}
		return
	}
	assertMetadataPathsNormalizedJSON(t, raw)
}

func countServiceArtifactFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk artifact dir: %v", err)
	}
	return count
}

// TestCreateRunFromHandoff_MissingManagedPassSourceContextBlocks verifies the shared
// intake service blocks a managed pass-associated run lacking source/context provenance.
func TestCreateRunFromHandoff_MissingManagedPassSourceContextBlocks(t *testing.T) {
	st := newIntakeServiceTestStore(t)
	plan, pass := seedManagedPlanWithSourceContextPass(t, st, "intake-service-missing")

	svc := NewService(st)
	_, err := svc.CreateRunFromHandoff(CreateRunInput{
		Markdown:   validServiceTestMarkdown("Managed Pass Run Missing Provenance"),
		RepoTarget: "relay",
		PlanID:     plan.PlanID,
		PassID:     "PASS-001",
	})
	if err == nil {
		t.Fatalf("expected error for missing managed-pass provenance, got nil")
	}
	var inputErr *InputError
	if !errors.As(err, &inputErr) {
		t.Fatalf("expected *InputError, got %T: %v", err, err)
	}
	if inputErr.Code != ErrCodeValidation {
		t.Fatalf("expected validation error code, got %q", inputErr.Code)
	}

	// The blocked path must not create a run or mutate the pass status.
	if runs, err := st.ListRunsByPlanPass(pass.ID); err != nil {
		t.Fatalf("ListRunsByPlanPass: %v", err)
	} else if len(runs) != 0 {
		t.Fatalf("expected no runs created on blocked path, got %d", len(runs))
	}
	refreshed, err := st.GetPlanPass(pass.ID)
	if err != nil {
		t.Fatalf("GetPlanPass: %v", err)
	}
	if refreshed.Status != "planned" {
		t.Fatalf("expected pass to remain planned after block, got %q", refreshed.Status)
	}
}

// TestCreateRunFromHandoff_ManagedPassWithValidSourceSnapshotCreatesRun verifies a
// valid source snapshot satisfies the provenance gate via the shared intake service.
func TestCreateRunFromHandoff_ManagedPassWithValidSourceSnapshotCreatesRun(t *testing.T) {
	st := newIntakeServiceTestStore(t)
	plan, pass := seedManagedPlanWithSourceContextPass(t, st, "intake-service-valid")
	seedIntakeSourceSnapshot(t, st, plan, "snapshot-intake-service-valid")

	svc := NewService(st)
	out, err := svc.CreateRunFromHandoff(CreateRunInput{
		Markdown:         validServiceTestMarkdown("Managed Pass Run Valid"),
		RepoTarget:       "relay",
		PlanID:           plan.PlanID,
		PassID:           "PASS-001",
		SourceSnapshotID: "snapshot-intake-service-valid",
	})
	if err != nil {
		t.Fatalf("expected success with valid provenance, got %v", err)
	}
	if out.RunID == 0 {
		t.Fatalf("expected a created run ID, got 0")
	}
	refreshed, err := st.GetPlanPass(pass.ID)
	if err != nil {
		t.Fatalf("GetPlanPass: %v", err)
	}
	if refreshed.Status != "run_created" {
		t.Fatalf("expected pass run_created with valid provenance, got %q", refreshed.Status)
	}
}

// TestCreateRunFromHandoff_StandaloneWithoutSourceContextStillAllowed verifies that
// runs with no plan/pass association are unaffected by the managed-pass gate.
func TestCreateRunFromHandoff_StandaloneWithoutSourceContextStillAllowed(t *testing.T) {
	st := newIntakeServiceTestStore(t)

	svc := NewService(st)
	out, err := svc.CreateRunFromHandoff(CreateRunInput{
		Markdown:   validServiceTestMarkdown("Standalone Run"),
		RepoTarget: "relay",
	})
	if err != nil {
		t.Fatalf("expected standalone run creation to succeed, got %v", err)
	}
	if out.RunID == 0 {
		t.Fatalf("expected a created run ID, got 0")
	}
	if out.PassID != "" {
		t.Fatalf("expected no pass association, got %q", out.PassID)
	}
}

// TestCreateRunFromHandoff_PlanOnlyWithoutSourceContextStillAllowed verifies that
// plan-only runs (plan_id without pass_id) do not trigger the managed-pass gate.
func TestCreateRunFromHandoff_PlanOnlyWithoutSourceContextStillAllowed(t *testing.T) {
	st := newIntakeServiceTestStore(t)
	plan, _ := seedManagedPlanWithSourceContextPass(t, st, "intake-service-plan-only")

	svc := NewService(st)
	out, err := svc.CreateRunFromHandoff(CreateRunInput{
		Markdown:   validServiceTestMarkdown("Plan-only Run"),
		RepoTarget: "relay",
		PlanID:     plan.PlanID,
	})
	if err != nil {
		t.Fatalf("expected plan-only run creation to succeed, got %v", err)
	}
	if out.PassID != "" {
		t.Fatalf("expected no pass association for plan-only run, got %q", out.PassID)
	}
}
