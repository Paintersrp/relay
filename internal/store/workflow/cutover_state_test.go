package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	relaydb "relay/internal/db"
	workflowgenerated "relay/internal/store/workflowgenerated"

	"github.com/pressly/goose/v3"
)

func TestCutoverStateBindsExactTransitionPlanAndCrossesOneWayBoundary(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	queries := workflowgenerated.New(store.DB())

	if _, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-wrong-plan", workflowHash('f'), "eligible")); err == nil {
		t.Fatal("cutover activation accepted a Transition Plan hash that was not the current authority layer")
	}

	activation, criterion := activateCutoverState(t, ctx, store, seed, "cutover-one-way", "eligible")
	current, err := queries.GetCurrentCutoverActivation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.CutoverActivationID != activation.CutoverActivationID ||
		current.TransitionPlanTicketRevisionRowID != seed.revision.ID ||
		current.TransitionPlanSha256 != seed.transitionPlanSHA256 ||
		current.AuthorityRevisionRowID != seed.authority.ID ||
		current.AuthoritySha256 != seed.authoritySHA256 ||
		current.ActivationStatus != "active" ||
		current.RollbackStatus != "available" ||
		!current.ActivatedAt.Valid {
		t.Fatalf("current cutover activation = %#v", current)
	}
	prerequisites, err := queries.ListCutoverActivationPrerequisiteEvidence(ctx, activation.ID)
	if err != nil || len(prerequisites) != 1 || prerequisites[0].Prerequisite != "The current Transition Plan authority is available." {
		t.Fatalf("cutover prerequisite evidence = %#v, %v", prerequisites, err)
	}
	if _, err := store.DB().Exec(`UPDATE cutover_activations SET authority_sha256 = ? WHERE id = ?`, workflowHash('e'), activation.ID); err == nil {
		t.Fatal("cutover activation identity was mutable")
	}
	if _, err := store.DB().Exec(`UPDATE cutover_activation_prerequisite_evidence SET evidence = 'rewritten' WHERE activation_row_id = ?`, activation.ID); err == nil {
		t.Fatal("cutover prerequisite evidence was mutable")
	}
	if _, err := queries.SetCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second current cutover state error = %v, want sql.ErrNoRows", err)
	}
	if _, err := queries.ClearCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true}); err == nil {
		t.Fatal("active cutover state cleared before rollback")
	}

	run := createCutoverBoundaryRun(t, ctx, store, seed)
	boundary, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: run.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if boundary.ExecutionBoundaryStatus != "crossed" || !boundary.FirstNewExecutionRunRowID.Valid ||
		boundary.FirstNewExecutionRunRowID.Int64 != run.ID || boundary.RollbackStatus != "forbidden" ||
		boundary.RollForwardStatus != "required" {
		t.Fatalf("cutover execution boundary = %#v", boundary)
	}
	if _, err := queries.RollbackCutoverActivation(ctx, activation.CutoverActivationID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback after execution boundary error = %v, want sql.ErrNoRows", err)
	}
	if _, err := store.DB().Exec(`UPDATE cutover_activations SET activation_status = 'rolled_back' WHERE id = ?`, activation.ID); err == nil {
		t.Fatal("execution boundary allowed a direct rollback")
	}
	if _, err := queries.CompleteCutoverRollForward(ctx, activation.CutoverActivationID); err == nil {
		t.Fatal("roll-forward completed without evidence for every criterion")
	}
	if _, err := queries.CreateCutoverRollForwardEvidence(ctx, workflowgenerated.CreateCutoverRollForwardEvidenceParams{
		ActivationRowID: activation.ID,
		CriterionRowID:  criterion.ID,
		Evidence:        "The first ticket-oriented execution is recorded against the exact package.",
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := queries.CompleteCutoverRollForward(ctx, activation.CutoverActivationID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.RollForwardStatus != "completed" {
		t.Fatalf("completed cutover roll-forward = %#v", completed)
	}
	if _, err := store.DB().Exec(`UPDATE cutover_roll_forward_evidence SET evidence = 'rewritten' WHERE activation_row_id = ?`, activation.ID); err == nil {
		t.Fatal("cutover roll-forward evidence was mutable")
	}
}

func TestCutoverStateAllowsEligibleRollbackOnlyBeforeBoundary(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-rollback", "eligible")
	queries := workflowgenerated.New(store.DB())

	rolledBack, err := queries.RollbackCutoverActivation(ctx, activation.CutoverActivationID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.ActivationStatus != "rolled_back" || rolledBack.RollbackStatus != "rolled_back" ||
		rolledBack.RollForwardStatus != "not_required" || !rolledBack.RolledBackAt.Valid {
		t.Fatalf("rolled-back cutover activation = %#v", rolledBack)
	}
	currentState, err := queries.ClearCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if currentState.ActivationRowID.Valid {
		t.Fatalf("cleared cutover current state = %#v", currentState)
	}
	if _, err := queries.GetCurrentCutoverActivation(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("current cutover after rollback error = %v, want sql.ErrNoRows", err)
	}
	if _, err := queries.SetCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true}); err == nil {
		t.Fatal("rolled-back activation became current again")
	}
}

func TestCutoverActivationRollsBackAsOneTransaction(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	errInjected := errors.New("injected cutover activation rollback")

	err := store.WithTx(ctx, func(tx *Tx) error {
		queries := workflowgenerated.New(tx.tx)
		activation, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-transaction", seed.transitionPlanSHA256, "eligible"))
		if err != nil {
			return err
		}
		if _, err := queries.CreateCutoverActivationPrerequisiteEvidence(ctx, workflowgenerated.CreateCutoverActivationPrerequisiteEvidenceParams{
			ActivationRowID: activation.ID,
			Sequence:        1,
			Prerequisite:    "The current Transition Plan authority is available.",
			Evidence:        "The current authority layer is exact and immutable.",
		}); err != nil {
			return err
		}
		if _, err := queries.CreateCutoverActivationObligationEvidence(ctx, workflowgenerated.CreateCutoverActivationObligationEvidenceParams{
			ActivationRowID: activation.ID,
			ObligationKind:  "activation",
			Sequence:        1,
			Obligation:      "Record activation evidence in the cutover transaction.",
			Evidence:        "The activation transaction is still open.",
		}); err != nil {
			return err
		}
		if _, err := queries.CreateCutoverActivationObligationEvidence(ctx, workflowgenerated.CreateCutoverActivationObligationEvidenceParams{
			ActivationRowID: activation.ID,
			ObligationKind:  "rollback",
			Sequence:        1,
			Obligation:      "Keep the rollback path available before the execution boundary.",
			Evidence:        "No execution boundary has been recorded.",
		}); err != nil {
			return err
		}
		if _, err := queries.CreateCutoverRollForwardCriterion(ctx, workflowgenerated.CreateCutoverRollForwardCriterionParams{
			ActivationRowID:     activation.ID,
			Sequence:            1,
			CompletionCriterion: "The ticket-oriented execution boundary is recorded safely.",
		}); err != nil {
			return err
		}
		if _, err := queries.SetCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true}); err != nil {
			return err
		}
		if _, err := queries.ActivateCutoverActivation(ctx, activation.CutoverActivationID); err != nil {
			return err
		}
		return errInjected
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("cutover activation transaction error = %v, want injected rollback", err)
	}
	assertWorkflowCount(t, store.DB(), "cutover_activations", 0)
	assertWorkflowCount(t, store.DB(), "cutover_activation_prerequisite_evidence", 0)
	assertWorkflowCount(t, store.DB(), "cutover_activation_obligation_evidence", 0)
	assertWorkflowCount(t, store.DB(), "cutover_roll_forward_criteria", 0)
	assertWorkflowCount(t, store.DB(), "cutover_current_states", 0)
}

func TestGatewayCutoverConfigurationTablesRejectPartialAndDuplicateIdentity(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	queries := workflowgenerated.New(store.DB())
	activation, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-gateway-config", seed.transitionPlanSHA256, "eligible"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_configurations (
    activation_row_id, configuration_sha256, relay_repository, relay_commit_oid,
    standing_repository, standing_commit_oid
) VALUES (?, ?, 'Paintersrp/relay', ?, 'Paintersrp/relay-specs', ?)`,
		activation.ID, workflowHash('a'), strings.Repeat("b", 40), strings.Repeat("c", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_routes (
    activation_row_id, sequence, route_path, role, surface_contract_id,
    manifest_sha256, authority_commit_oid, authority_blob_oid
) VALUES (?, 1, '/mcp/v1/wayfinder/workspace', 'wayfinder', 'wayfinder-workspace.v1', ?, ?, ?)`,
		activation.ID, workflowHash('d'), strings.Repeat("c", 40), strings.Repeat("e", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_routes (
    activation_row_id, sequence, route_path, role, surface_contract_id,
    manifest_sha256, authority_commit_oid, authority_blob_oid
) VALUES (?, 2, '/mcp/v1/wayfinder/workspace', 'wayfinder', 'wayfinder-workspace.v1', ?, ?, ?)`,
		activation.ID, workflowHash('f'), strings.Repeat("c", 40), strings.Repeat("f", 40)); err == nil {
		t.Fatal("duplicate route path was accepted")
	}
	counts, err := queries.GetCutoverGatewayConfigurationCounts(ctx, activation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.RouteCount != 1 || counts.MappingCount != 0 || counts.StandingAuthorityCount != 0 {
		t.Fatalf("partial configuration counts = %#v", counts)
	}
}

func TestGatewayCutoverConfigurationRowsAreImmutable(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	queries := workflowgenerated.New(store.DB())
	activation, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-gateway-immutable", seed.transitionPlanSHA256, "eligible"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_configurations (
    activation_row_id, configuration_sha256, relay_repository, relay_commit_oid,
    standing_repository, standing_commit_oid
) VALUES (?, ?, 'Paintersrp/relay', ?, 'Paintersrp/relay-specs', ?)`,
		activation.ID, workflowHash('a'), strings.Repeat("b", 40), strings.Repeat("c", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE cutover_gateway_configurations SET relay_repository = 'other' WHERE activation_row_id = ?`, activation.ID); err == nil {
		t.Fatal("gateway configuration was mutable")
	}
}
func TestCutoverStateMigrationPreservesHistoricalPlanPassAndRunState(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workflow.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(relaydb.WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, "workflow_migrations", 15); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO repository_targets (repo_target, local_path) VALUES ('relay', '/repo')`); err != nil {
		t.Fatal(err)
	}
	var projectRowID, planRowID int64
	if err := database.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-cutover-upgrade', 'Cutover Upgrade') RETURNING id`).Scan(&projectRowID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256)
VALUES (?, 'plan-cutover-history', 'cutover-history', ?)
RETURNING id`, projectRowID, workflowHash('a')).Scan(&planRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO plan_repository_targets (plan_row_id, sequence, repo_target, branch, planning_base_commit)
VALUES (?, 1, 'relay', 'main', ?)`, planRowID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	var planPassRowID int64
	if err := database.QueryRow(`
INSERT INTO plan_passes (pass_id, plan_row_id, pass_number, name, repo_target, status)
VALUES ('pass-cutover-history', ?, 1, 'Historical Pass', 'relay', 'planned')
RETURNING id`, planRowID).Scan(&planPassRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
UPDATE plan_passes
SET status = 'in_progress',
    started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ?`, planPassRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
INSERT INTO runs (run_id, feature_slug, repo_target, plan_row_id, plan_pass_row_id, status, branch, base_commit, canonical_sha256)
VALUES ('run-cutover-history', 'cutover-history', 'relay', ?, ?, 'created', 'main', ?, ?)`, planRowID, planPassRowID, strings.Repeat("a", 40), workflowHash('b')); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE runs SET status = 'setup_ready' WHERE run_id = 'run-cutover-history'`); err != nil {
		t.Fatal(err)
	}

	if err := relaydb.AutoMigrateWorkflow(database); err != nil {
		t.Fatal(err)
	}
	assertHistoricalCutoverRows(t, database)
	if err := goose.DownTo(database, "workflow_migrations", 15); err != nil {
		t.Fatal(err)
	}
	assertHistoricalCutoverRows(t, database)
	var cutoverTables int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'cutover_activations'`).Scan(&cutoverTables); err != nil {
		t.Fatal(err)
	}
	if cutoverTables != 0 {
		t.Fatal("cutover activation table survived rollback to the prior schema")
	}
}

func assertHistoricalCutoverRows(t *testing.T, database *sql.DB) {
	t.Helper()
	var planStatus, passStatus, runStatus string
	if err := database.QueryRow(`SELECT status FROM plans WHERE plan_id = 'plan-cutover-history'`).Scan(&planStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM plan_passes WHERE pass_id = 'pass-cutover-history'`).Scan(&passStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT status FROM runs WHERE run_id = 'run-cutover-history'`).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if planStatus != "active" || passStatus != "in_progress" || runStatus != "setup_ready" {
		t.Fatalf("historical cutover rows = plan:%q pass:%q run:%q", planStatus, passStatus, runStatus)
	}
}

type cutoverStateSeed struct {
	closure              SourceVaultClosure
	workspace            FeatureWorkspace
	authority            FeatureWorkspaceAuthorityRevision
	ticket               DeliveryTicket
	revision             DeliveryTicketRevision
	layer                FeatureWorkspaceAuthorityLayer
	transitionPlanSHA256 string
	authoritySHA256      string
}

func seedCutoverState(t *testing.T, ctx context.Context, store *Store) cutoverStateSeed {
	t.Helper()
	_, closure := seedReadySourceVaultClosure(t, ctx, store)
	workspace := seedDeliveryTicketWorkspace(t, ctx, store)
	seed := cutoverStateSeed{
		closure:              closure,
		workspace:            workspace,
		transitionPlanSHA256: workflowHash('c'),
		authoritySHA256:      workflowHash('d'),
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		plan, err := tx.CreatePlan(ctx, CreatePlanParams{
			ProjectRowID:    workspace.ProjectRowID,
			PlanID:          "plan-cutover-state",
			FeatureSlug:     workspace.FeatureSlug,
			CanonicalSHA256: workflowHash('a'),
		})
		if err != nil {
			return err
		}
		artifact, err := tx.CreateArtifact(ctx, CreateArtifactParams{
			ArtifactID:   "artifact-cutover-transition-plan",
			OwnerType:    ArtifactOwnerPlan,
			PlanRowID:    sql.NullInt64{Int64: plan.ID, Valid: true},
			Kind:         "transition_plan",
			RelativePath: "plans/delivery-ticket/p7-t1.r1.transition-plan.json",
			MediaType:    "application/json",
			SHA256:       seed.transitionPlanSHA256,
			SizeBytes:    1,
		})
		if err != nil {
			return err
		}
		seed.authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-cutover-1",
			WorkspaceRowID:      workspace.ID,
			RevisionNumber:      1,
			SourceClosureRowID:  sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		seed.layer, err = tx.CreateFeatureWorkspaceAuthorityLayer(ctx, CreateFeatureWorkspaceAuthorityLayerParams{
			AuthorityRevisionRowID: seed.authority.ID,
			LayerKind:              "plan",
			Sequence:               1,
			ArtifactRowID:          sql.NullInt64{Int64: artifact.ID, Valid: true},
			ArtifactSha256:         seed.transitionPlanSHA256,
		})
		if err != nil {
			return err
		}
		seed.workspace, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, seed.authority.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		seed.ticket, err = tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{
			TicketID: "P7-T1", WorkspaceRowID: workspace.ID, ExternalPriority: 40,
		})
		if err != nil {
			return err
		}
		seed.revision, err = tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID:     seed.ticket.ID,
			RevisionNumber:          1,
			RepoTarget:              "relay",
			Branch:                  "main",
			BaseCommit:              closure.CommitOID,
			SourceClosureRowID:      closure.ID,
			SourcePath:              "tickets/p7-t1.r1.delivery-ticket.json",
			Goal:                    "Persist the ticket-oriented cutover state.",
			Context:                 "The Transition Plan is exact current activation authority.",
			TransitionApplicability: "required",
		})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(ctx, seed.ticket.TicketID, seed.revision.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return seed
}

func cutoverActivationParams(seed cutoverStateSeed, activationID, transitionPlanSHA256, rollbackEligibility string) workflowgenerated.CreateCutoverActivationParams {
	return workflowgenerated.CreateCutoverActivationParams{
		CutoverActivationID:               activationID,
		WorkspaceRowID:                    seed.workspace.ID,
		TransitionPlanTicketRevisionRowID: seed.revision.ID,
		TransitionPlanTicketID:            seed.ticket.TicketID,
		TransitionPlanTicketRevision:      seed.revision.RevisionNumber,
		TransitionPlanAuthorityLayerRowID: seed.layer.ID,
		TransitionPlanSha256:              transitionPlanSHA256,
		AuthorityRevisionRowID:            seed.authority.ID,
		AuthorityRevisionID:               seed.authority.AuthorityRevisionID,
		AuthorityRevisionNumber:           seed.authority.RevisionNumber,
		AuthoritySha256:                   seed.authoritySHA256,
		RollbackEligibility:               rollbackEligibility,
	}
}

func activateCutoverState(
	t *testing.T,
	ctx context.Context,
	store *Store,
	seed cutoverStateSeed,
	activationID, rollbackEligibility string,
) (workflowgenerated.CutoverActivation, workflowgenerated.CutoverRollForwardCriterium) {
	t.Helper()
	var activation workflowgenerated.CutoverActivation
	var criterion workflowgenerated.CutoverRollForwardCriterium
	if err := store.WithTx(ctx, func(tx *Tx) error {
		queries := workflowgenerated.New(tx.tx)
		var err error
		activation, err = queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, activationID, seed.transitionPlanSHA256, rollbackEligibility))
		if err != nil {
			return err
		}
		if err := seedCompleteGatewayConfigurationForTest(ctx, tx, activation.ID); err != nil {
			return err
		}
		if _, err := queries.CreateCutoverActivationPrerequisiteEvidence(ctx, workflowgenerated.CreateCutoverActivationPrerequisiteEvidenceParams{
			ActivationRowID: activation.ID,
			Sequence:        1,
			Prerequisite:    "The current Transition Plan authority is available.",
			Evidence:        "The immutable authority layer SHA-256 is bound to this activation.",
		}); err != nil {
			return err
		}
		if _, err := queries.CreateCutoverActivationObligationEvidence(ctx, workflowgenerated.CreateCutoverActivationObligationEvidenceParams{
			ActivationRowID: activation.ID,
			ObligationKind:  "activation",
			Sequence:        1,
			Obligation:      "Activate only after every prerequisite has recorded evidence.",
			Evidence:        "The activation transaction records every prerequisite and obligation before activation.",
		}); err != nil {
			return err
		}
		if rollbackEligibility == "eligible" {
			if _, err := queries.CreateCutoverActivationObligationEvidence(ctx, workflowgenerated.CreateCutoverActivationObligationEvidenceParams{
				ActivationRowID: activation.ID,
				ObligationKind:  "rollback",
				Sequence:        1,
				Obligation:      "Restore the prior route before the first new execution boundary.",
				Evidence:        "The prior route is retained while the execution boundary remains open.",
			}); err != nil {
				return err
			}
		}
		criterion, err = queries.CreateCutoverRollForwardCriterion(ctx, workflowgenerated.CreateCutoverRollForwardCriterionParams{
			ActivationRowID:     activation.ID,
			Sequence:            1,
			CompletionCriterion: "The first ticket-oriented execution is recorded and the route is safe to roll forward.",
		})
		if err != nil {
			return err
		}
		if _, err := queries.SetCutoverCurrentState(ctx, sql.NullInt64{Int64: activation.ID, Valid: true}); err != nil {
			return err
		}
		activation, err = queries.ActivateCutoverActivation(ctx, activation.CutoverActivationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return activation, criterion
}

func seedCompleteGatewayConfigurationForTest(ctx context.Context, tx *Tx, activationRowID int64) error {
	if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_configurations (
    activation_row_id, configuration_sha256, relay_repository, relay_commit_oid,
    standing_repository, standing_commit_oid
) VALUES (?, ?, 'Paintersrp/relay', ?, 'Paintersrp/relay-specs', ?)`,
		activationRowID, strings.Repeat("1", 64), strings.Repeat("a", 40), strings.Repeat("b", 40)); err != nil {
		return err
	}
	for sequence := int64(1); sequence <= 7; sequence++ {
		routePath := fmt.Sprintf("/mcp/v1/test/%d", sequence)
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_routes (
    activation_row_id, sequence, route_path, role, surface_contract_id,
    manifest_sha256, authority_commit_oid, authority_blob_oid
) VALUES (?, ?, ?, 'wayfinder', ?, ?, ?, ?)`,
			activationRowID, sequence, routePath, fmt.Sprintf("test-contract-%d", sequence),
			strings.Repeat("2", 64), strings.Repeat("a", 40), strings.Repeat("b", 40)); err != nil {
			return err
		}
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_mappings (
    activation_row_id, sequence, mapping_id, route_path, listener_identity,
    upstream_identity, health_evidence_sha256, trace_evidence_sha256
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			activationRowID, sequence, fmt.Sprintf("test-mapping-%d", sequence), routePath,
			fmt.Sprintf("test-listener-%d", sequence), fmt.Sprintf("test-upstream-%d", sequence),
			strings.Repeat("3", 64), strings.Repeat("4", 64)); err != nil {
			return err
		}
	}
	for _, role := range []string{"wayfinder", "planner", "auditor"} {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_standing_authorities (
    activation_row_id, role, repository, commit_oid, path, blob_oid, content_sha256
) VALUES (?, ?, 'Paintersrp/relay-specs', ?, ?, ?, ?)`,
			activationRowID, role, strings.Repeat("a", 40), "standing/"+role, strings.Repeat("b", 40), strings.Repeat("5", 64)); err != nil {
			return err
		}
	}
	for sequence := int64(1); sequence <= 3; sequence++ {
		if _, err := tx.tx.ExecContext(ctx, `
INSERT INTO cutover_gateway_dependency_outcomes (
    activation_row_id, sequence, ticket_id, ticket_revision, outcome, evidence_sha256
) VALUES (?, ?, ?, 1, 'completed_accepted', ?)`,
			activationRowID, sequence, fmt.Sprintf("dependency-%d", sequence), strings.Repeat("6", 64)); err != nil {
			return err
		}
	}
	return nil
}

func createCutoverBoundaryRun(t *testing.T, ctx context.Context, store *Store, seed cutoverStateSeed) Run {
	t.Helper()
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID:             "approval-cutover-1",
			RevisionRowID:          seed.revision.ID,
			ApprovalKind:           "delivery",
			ApprovalState:          "approved",
			Rationale:              "The exact transition ticket is approved against the current authority.",
			SourceClosureRowID:     seed.closure.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: seed.authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID:        "selection-cutover-1",
			WorkspaceRowID:     seed.workspace.ID,
			State:              "active",
			Rationale:          "Compose the exact transition ticket as the first new execution package.",
			SourceClosureRowID: sql.NullInt64{Int64: seed.closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selectionMember, err := tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID,
			Sequence:       1,
			RevisionRowID:  seed.revision.ID,
			ApprovalRowID:  approval.ID,
		})
		if err != nil {
			return err
		}
		queries := workflowgenerated.New(tx.tx)
		executionPackage, err := queries.CreateExecutionPackage(ctx, workflowgenerated.CreateExecutionPackageParams{
			PackageID:              "package-cutover-boundary",
			SelectionRowID:         selection.ID,
			WorkspaceRowID:         seed.workspace.ID,
			RepoTarget:             "relay",
			Branch:                 "main",
			BaseCommit:             seed.closure.CommitOID,
			SourceClosureRowID:     seed.closure.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			PackageSha256:          workflowHash('1'),
			AuthoritySha256:        seed.authoritySHA256,
			SourceSha256:           workflowHash('2'),
			DesignBriefSha256:      workflowHash('3'),
			ExecutionSpecSha256:    workflowHash('4'),
		})
		if err != nil {
			return err
		}
		member, err := queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
			PackageRowID:         executionPackage.ID,
			SelectionMemberRowID: selectionMember.ID,
			Sequence:             1,
			RevisionRowID:        seed.revision.ID,
			MemberSha256:         workflowHash('5'),
		})
		if err != nil {
			return err
		}
		if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
			PackageRowID:           executionPackage.ID,
			PackageMemberRowID:     member.ID,
			ApprovalRowID:          approval.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			SourceClosureRowID:     seed.closure.ID,
			ApprovalBasisSha256:    workflowHash('6'),
		}); err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, selection.SelectionID); err != nil {
			return err
		}
		packageApproval, err := queries.CreateExecutionPackageApproval(ctx, workflowgenerated.CreateExecutionPackageApprovalParams{
			ApprovalID:                   "pkg-approval-cutover-boundary",
			PackageRowID:                 executionPackage.ID,
			PackageSha256:                workflowHash('1'),
			OperatorConfirmationEvidence: "package approved for cutover boundary crossing test",
		})
		if err != nil {
			return err
		}
		run, err = tx.CreateRun(ctx, CreateRunParams{
			RunID:           "run-cutover-boundary",
			FeatureSlug:     seed.workspace.FeatureSlug,
			RepoTarget:      "relay",
			Status:          RunStatusCreated,
			Branch:          "main",
			BaseCommit:      seed.closure.CommitOID,
			CanonicalSHA256: workflowHash('7'),
		})
		if err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackage(ctx, run.RunID, executionPackage.ID); err != nil {
			return err
		}
		_, err = tx.LinkRunToExecutionPackageApproval(ctx, LinkRunToExecutionPackageApprovalParams{
			PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true},
			RunID:                run.RunID,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func workflowHash(character rune) string {
	return strings.Repeat(string(character), 64)
}

func TestOrdinaryTicketRunCrossesBoundaryWithoutTransitionPlanMember(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, criterion := activateCutoverState(t, ctx, store, seed, "cutover-ordinary", "eligible")
	queries := workflowgenerated.New(store.DB())

	ordinaryRun := createOrdinaryTicketRun(t, ctx, store, seed, "run-ordinary-ticket")
	boundary, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: ordinaryRun.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	})
	if err != nil {
		t.Fatalf("ordinary ticket run failed to cross: %v", err)
	}
	if boundary.ExecutionBoundaryStatus != "crossed" || boundary.RollbackStatus != "forbidden" {
		t.Fatalf("ordinary ticket boundary crossing = %#v", boundary)
	}

	_ = criterion
}

func TestPackageApprovedSetupReadyRunLeavesBoundaryOpen(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-setup-ready", "eligible")
	queries := workflowgenerated.New(store.DB())

	setupReadyRun := createOrdinaryTicketRun(t, ctx, store, seed, "run-setup-ready-open")
	current, err := queries.GetCurrentCutoverActivation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.ExecutionBoundaryStatus != "open" {
		t.Fatalf("boundary crossed at setup-ready: %s", current.ExecutionBoundaryStatus)
	}
	if current.RollbackStatus != "available" {
		t.Fatalf("rollback not eligible after setup-ready: %s", current.RollbackStatus)
	}
	_ = activation
	_ = setupReadyRun
}

func TestInvalidCandidateRunFailsCrossing(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-invalid", "eligible")
	queries := workflowgenerated.New(store.DB())

	runWithoutApproval := createOrdinaryTicketRunWithoutApproval(t, ctx, store, seed, "run-no-approval")
	if _, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: runWithoutApproval.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	}); err == nil {
		t.Fatal("run without package approval crossed the boundary")
	}

	legacyRun := createLegacyRun(t, ctx, store, seed, "run-legacy-plain")
	if _, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: legacyRun.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	}); err == nil {
		t.Fatal("legacy run without execution package crossed the boundary")
	}
}

func TestTwoConcurrentRunsOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-concurrent", "eligible")
	queries := workflowgenerated.New(store.DB())

	first := createOrdinaryTicketRun(t, ctx, store, seed, "run-first-winner")
	second := createOrdinaryTicketRun(t, ctx, store, seed, "run-second-loser")

	boundary, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: first.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if boundary.ExecutionBoundaryStatus != "crossed" {
		t.Fatalf("first run failed to cross: %#v", boundary)
	}

	if _, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: second.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second run crossed already-crossed boundary: want sql.ErrNoRows, got %v", err)
	}
}

func TestAtomicRollbackRevocationAfterCrossing(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-rollback-revoke", "eligible")
	queries := workflowgenerated.New(store.DB())

	run := createOrdinaryTicketRun(t, ctx, store, seed, "run-atomic-rollback")
	_, err := queries.RecordCutoverExecutionBoundary(ctx, workflowgenerated.RecordCutoverExecutionBoundaryParams{
		FirstNewExecutionRunRowID: sql.NullInt64{Int64: run.ID, Valid: true},
		CutoverActivationID:       activation.CutoverActivationID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := queries.RollbackCutoverActivation(ctx, activation.CutoverActivationID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback after boundary crossing should fail, got %v", err)
	}

	current, err := queries.GetCurrentCutoverActivation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.RollbackStatus != "forbidden" {
		t.Fatalf("rollback status not forbidden after crossing: %s", current.RollbackStatus)
	}
}

func TestCrossingMonotonicBoundary(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	activation, _ := activateCutoverState(t, ctx, store, seed, "cutover-monotonic", "eligible")

	run := createOrdinaryTicketRun(t, ctx, store, seed, "run-monotonic-boundary")
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.ConditionalCrossCutoverExecutionBoundary(ctx, activation.CutoverActivationID, run.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	current, found, err := store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		t.Fatalf("current cutover activation: found=%v err=%v", found, err)
	}
	if current.ExecutionBoundaryStatus != "crossed" {
		t.Fatalf("boundary not crossed: %s", current.ExecutionBoundaryStatus)
	}
	if current.RollbackStatus != "forbidden" {
		t.Fatalf("rollback not forbidden: %s", current.RollbackStatus)
	}
}

func createOrdinaryTicketRun(t *testing.T, ctx context.Context, store *Store, seed cutoverStateSeed, runID string) Run {
	t.Helper()
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		return createOrdinaryTicketRunTx(t, ctx, tx, seed, runID, &run)
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func createOrdinaryTicketRunTx(t *testing.T, ctx context.Context, tx *Tx, seed cutoverStateSeed, runID string, run *Run) error {
	t.Helper()
	_ = runID

	ordTicketID := "P7-T2-9"
	if runID == "run-first-winner" {
		ordTicketID = "P7-T2-1"
	} else if runID == "run-second-loser" {
		ordTicketID = "P7-T2-2"
	}

	ordinaryTicket, err := tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{
		TicketID: ordTicketID, WorkspaceRowID: seed.workspace.ID, ExternalPriority: 30,
	})
	if err != nil {
		return err
	}
	ordinaryRevision, err := tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
		DeliveryTicketRowID:     ordinaryTicket.ID,
		RevisionNumber:          1,
		RepoTarget:              "relay",
		Branch:                  "main",
		BaseCommit:              seed.closure.CommitOID,
		SourceClosureRowID:      seed.closure.ID,
		SourcePath:              "tickets/p7-t2.r1.delivery-ticket.json",
		Goal:                    "Ordinary delivery ticket for cutover boundary crossing.",
		Context:                 "This is a non-Transition Plan ticket.",
		TransitionApplicability: "not_required",
	})
	if err != nil {
		return err
	}
	approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, CreateDeliveryTicketRevisionApprovalParams{
		ApprovalID:             "approval-ordinary-" + runID,
		RevisionRowID:          ordinaryRevision.ID,
		ApprovalKind:           "delivery",
		ApprovalState:          "approved",
		Rationale:              "The ordinary delivery ticket is approved.",
		SourceClosureRowID:     seed.closure.ID,
		AuthorityRevisionRowID: sql.NullInt64{Int64: seed.authority.ID, Valid: true},
	})
	if err != nil {
		return err
	}
	selection, err := tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
		SelectionID:        "selection-ordinary-" + runID,
		WorkspaceRowID:     seed.workspace.ID,
		State:              "active",
		Rationale:          "Select ordinary ticket for cutover boundary crossing.",
		SourceClosureRowID: sql.NullInt64{Int64: seed.closure.ID, Valid: true},
	})
	if err != nil {
		return err
	}
	selectionMember, err := tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
		SelectionRowID: selection.ID,
		Sequence:       1,
		RevisionRowID:  ordinaryRevision.ID,
		ApprovalRowID:  approval.ID,
	})
	if err != nil {
		return err
	}
	queries := workflowgenerated.New(tx.tx)
	executionPackage, err := queries.CreateExecutionPackage(ctx, workflowgenerated.CreateExecutionPackageParams{
		PackageID:              "package-ordinary-" + runID,
		SelectionRowID:         selection.ID,
		WorkspaceRowID:         seed.workspace.ID,
		RepoTarget:             "relay",
		Branch:                 "main",
		BaseCommit:             seed.closure.CommitOID,
		SourceClosureRowID:     seed.closure.ID,
		AuthorityRevisionRowID: seed.authority.ID,
		PackageSha256:          workflowHash('0'),
		AuthoritySha256:        seed.authoritySHA256,
		SourceSha256:           workflowHash('1'),
		DesignBriefSha256:      workflowHash('2'),
		ExecutionSpecSha256:    workflowHash('3'),
	})
	if err != nil {
		return err
	}
	member, err := queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
		PackageRowID:         executionPackage.ID,
		SelectionMemberRowID: selectionMember.ID,
		Sequence:             1,
		RevisionRowID:        ordinaryRevision.ID,
		MemberSha256:         workflowHash('e'),
	})
	if err != nil {
		return err
	}
	if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
		PackageRowID:           executionPackage.ID,
		PackageMemberRowID:     member.ID,
		ApprovalRowID:          approval.ID,
		AuthorityRevisionRowID: seed.authority.ID,
		SourceClosureRowID:     seed.closure.ID,
		ApprovalBasisSha256:    workflowHash('f'),
	}); err != nil {
		return err
	}
	if _, err := queries.ConsumeDeliveryTicketSelection(ctx, selection.SelectionID); err != nil {
		return err
	}
	packageApproval, err := queries.CreateExecutionPackageApproval(ctx, workflowgenerated.CreateExecutionPackageApprovalParams{
		ApprovalID:                   "pkg-approval-" + runID,
		PackageRowID:                 executionPackage.ID,
		PackageSha256:                workflowHash('0'),
		OperatorConfirmationEvidence: "package approved for ordinary ticket cutover boundary crossing",
	})
	if err != nil {
		return err
	}
	created, err := tx.CreateRun(ctx, CreateRunParams{
		RunID:           runID,
		FeatureSlug:     seed.workspace.FeatureSlug,
		RepoTarget:      "relay",
		Status:          RunStatusCreated,
		Branch:          "main",
		BaseCommit:      seed.closure.CommitOID,
		CanonicalSHA256: workflowHash('8'),
	})
	if err != nil {
		return err
	}
	linked, err := tx.LinkRunToExecutionPackage(ctx, created.RunID, executionPackage.ID)
	if err != nil {
		return err
	}
	linked, err = tx.LinkRunToExecutionPackageApproval(ctx, LinkRunToExecutionPackageApprovalParams{
		PackageApprovalRowID: sql.NullInt64{Int64: packageApproval.ID, Valid: true},
		RunID:                created.RunID,
	})
	if err != nil {
		return err
	}
	*run = linked
	return nil
}

func createOrdinaryTicketRunWithoutApproval(t *testing.T, ctx context.Context, store *Store, seed cutoverStateSeed, runID string) Run {
	t.Helper()
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		ordinaryTicket, err := tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{
			TicketID: "P7-T3-NOAPPR", WorkspaceRowID: seed.workspace.ID, ExternalPriority: 20,
		})
		if err != nil {
			return err
		}
		ordinaryRevision, err := tx.CreateDeliveryTicketRevision(ctx, CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID:     ordinaryTicket.ID,
			RevisionNumber:          1,
			RepoTarget:              "relay",
			Branch:                  "main",
			BaseCommit:              seed.closure.CommitOID,
			SourceClosureRowID:      seed.closure.ID,
			SourcePath:              "tickets/p7-t3.r1.delivery-ticket.json",
			Goal:                    "Ordinary ticket without package approval.",
			Context:                 "No package approval exists.",
			TransitionApplicability: "not_required",
		})
		if err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, CreateDeliveryTicketRevisionApprovalParams{
			ApprovalID:             "approval-noappr-" + runID,
			RevisionRowID:          ordinaryRevision.ID,
			ApprovalKind:           "delivery",
			ApprovalState:          "approved",
			Rationale:              "The ticket revision is approved.",
			SourceClosureRowID:     seed.closure.ID,
			AuthorityRevisionRowID: sql.NullInt64{Int64: seed.authority.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(ctx, CreateDeliveryTicketSelectionParams{
			SelectionID:        "selection-noappr-" + runID,
			WorkspaceRowID:     seed.workspace.ID,
			State:              "active",
			Rationale:          "Select ticket without package approval.",
			SourceClosureRowID: sql.NullInt64{Int64: seed.closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		selectionMember, err := tx.CreateDeliveryTicketSelectionMember(ctx, CreateDeliveryTicketSelectionMemberParams{
			SelectionRowID: selection.ID,
			Sequence:       1,
			RevisionRowID:  ordinaryRevision.ID,
			ApprovalRowID:  approval.ID,
		})
		if err != nil {
			return err
		}
		queries := workflowgenerated.New(tx.tx)
		executionPackage, err := queries.CreateExecutionPackage(ctx, workflowgenerated.CreateExecutionPackageParams{
			PackageID:              "package-noappr-" + runID,
			SelectionRowID:         selection.ID,
			WorkspaceRowID:         seed.workspace.ID,
			RepoTarget:             "relay",
			Branch:                 "main",
			BaseCommit:             seed.closure.CommitOID,
			SourceClosureRowID:     seed.closure.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			PackageSha256:          workflowHash('8'),
			AuthoritySha256:        seed.authoritySHA256,
			SourceSha256:           workflowHash('9'),
			DesignBriefSha256:      workflowHash('0'),
			ExecutionSpecSha256:    workflowHash('1'),
		})
		if err != nil {
			return err
		}
		member, err := queries.CreateExecutionPackageMember(ctx, workflowgenerated.CreateExecutionPackageMemberParams{
			PackageRowID:         executionPackage.ID,
			SelectionMemberRowID: selectionMember.ID,
			Sequence:             1,
			RevisionRowID:        ordinaryRevision.ID,
			MemberSha256:         workflowHash('2'),
		})
		if err != nil {
			return err
		}
		if _, err := queries.CreateExecutionPackageApprovalBinding(ctx, workflowgenerated.CreateExecutionPackageApprovalBindingParams{
			PackageRowID:           executionPackage.ID,
			PackageMemberRowID:     member.ID,
			ApprovalRowID:          approval.ID,
			AuthorityRevisionRowID: seed.authority.ID,
			SourceClosureRowID:     seed.closure.ID,
			ApprovalBasisSha256:    workflowHash('3'),
		}); err != nil {
			return err
		}
		if _, err := queries.ConsumeDeliveryTicketSelection(ctx, selection.SelectionID); err != nil {
			return err
		}
		created, err := tx.CreateRun(ctx, CreateRunParams{
			RunID:           runID,
			FeatureSlug:     seed.workspace.FeatureSlug,
			RepoTarget:      "relay",
			Status:          RunStatusCreated,
			Branch:          "main",
			BaseCommit:      seed.closure.CommitOID,
			CanonicalSHA256: workflowHash('4'),
		})
		if err != nil {
			return err
		}
		linked, err := tx.LinkRunToExecutionPackage(ctx, created.RunID, executionPackage.ID)
		if err != nil {
			return err
		}
		run = linked
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func createLegacyRun(t *testing.T, ctx context.Context, store *Store, seed cutoverStateSeed, runID string) Run {
	t.Helper()
	var run Run
	if err := store.WithTx(ctx, func(tx *Tx) error {
		created, err := tx.CreateRun(ctx, CreateRunParams{
			RunID:           runID,
			FeatureSlug:     seed.workspace.FeatureSlug,
			RepoTarget:      "relay",
			Status:          RunStatusCreated,
			Branch:          "main",
			BaseCommit:      seed.closure.CommitOID,
			CanonicalSHA256: workflowHash('5'),
		})
		if err != nil {
			return err
		}
		run = created
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCutoverGatewayAppSurfacesRoundTripAndRejectInvalidMemberships(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	seed := seedCutoverState(t, ctx, store)
	queries := workflowgenerated.New(store.DB())
	activation, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-gateway-app-surfaces", seed.transitionPlanSHA256, "eligible"))
	if err != nil {
		t.Fatal(err)
	}
	configuration := appSurfaceGatewayConfigurationForTest()
	if err := store.WithTx(ctx, func(tx *Tx) error {
		return tx.CreateCutoverGatewayConfiguration(ctx, activation.ID, configuration)
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadCutoverGatewayConfiguration(ctx, activation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.AppSurfaces) != 3 || len(loaded.RouteMemberships) != 7 || len(loaded.AppSurfaceMappings) != 3 {
		t.Fatalf("app-surface round trip = %#v", loaded)
	}
	publicPaths := map[string]string{"wayfinder": "/mcp/wayfinder", "planner": "/mcp/planner", "auditor": "/mcp/auditor"}
	for _, surface := range loaded.AppSurfaces {
		if publicPaths[surface.Surface] != surface.PublicPath {
			t.Fatalf("app surface = %#v", surface)
		}
	}
	memberships := map[string]string{}
	for _, membership := range loaded.RouteMemberships {
		memberships[membership.RoutePath] = membership.PublicSurface
	}
	for _, route := range loaded.Routes {
		if memberships[route.RoutePath] != route.Role {
			t.Fatalf("route membership for %q = %q, want %q", route.RoutePath, memberships[route.RoutePath], route.Role)
		}
	}
	for _, mapping := range loaded.AppSurfaceMappings {
		if mapping.MappingID != mapping.PublicSurface || publicPaths[mapping.PublicSurface] != mapping.PublicPath {
			t.Fatalf("app-surface mapping = %#v", mapping)
		}
	}

	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_route_memberships (activation_row_id, route_path, app_surface)
VALUES (?, '/mcp/v1/wayfinder/missing', 'wayfinder')`, activation.ID); err == nil {
		t.Fatal("membership accepted a route absent from the configuration")
	}
	firstMembership := configuration.RouteMemberships[0]
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_route_memberships (activation_row_id, route_path, app_surface)
VALUES (?, ?, ?)`, activation.ID, firstMembership.RoutePath, firstMembership.PublicSurface); err == nil {
		t.Fatal("duplicate route membership was accepted")
	}

	orphan, err := queries.CreateCutoverActivation(ctx, cutoverActivationParams(seed, "cutover-gateway-app-surfaces-orphan", seed.transitionPlanSHA256, "eligible"))
	if err != nil {
		t.Fatal(err)
	}
	orphanConfiguration := CutoverGatewayConfiguration{
		ConfigurationSHA256: workflowHash('f'),
		RelayRepository:     configuration.RelayRepository,
		RelayCommitOID:      configuration.RelayCommitOID,
		StandingRepository:  configuration.StandingRepository,
		StandingCommitOID:   configuration.StandingCommitOID,
		Routes:              []CutoverGatewayRoute{configuration.Routes[0]},
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		return tx.CreateCutoverGatewayConfiguration(ctx, orphan.ID, orphanConfiguration)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO cutover_gateway_route_memberships (activation_row_id, route_path, app_surface)
VALUES (?, ?, 'wayfinder')`, orphan.ID, configuration.Routes[0].RoutePath); err == nil {
		t.Fatal("membership accepted an app surface absent from the configuration")
	}
}

func appSurfaceGatewayConfigurationForTest() CutoverGatewayConfiguration {
	routes := []struct {
		path, role, contract string
	}{
		{"/mcp/v1/wayfinder/workspace", "wayfinder", "wayfinder-workspace.v1"},
		{"/mcp/v1/wayfinder/discovery", "wayfinder", "wayfinder-discovery.v1"},
		{"/mcp/v1/wayfinder/investigation", "wayfinder", "wayfinder-investigation.v1"},
		{"/mcp/v1/planner/authoring", "planner", "planner-authoring.v1"},
		{"/mcp/v1/planner/frontier", "planner", "planner-ticket-frontier.v1"},
		{"/mcp/v1/auditor/review", "auditor", "auditor-review.v1"},
		{"/mcp/v1/auditor/audit", "auditor", "auditor-audit.v1"},
	}
	configuration := CutoverGatewayConfiguration{
		ConfigurationSHA256: workflowHash('a'),
		RelayRepository:     "Paintersrp/relay",
		RelayCommitOID:      strings.Repeat("a", 40),
		StandingRepository:  "Paintersrp/relay-specs",
		StandingCommitOID:   strings.Repeat("b", 40),
	}
	for index, route := range routes {
		configuration.Routes = append(configuration.Routes, CutoverGatewayRoute{
			Sequence: int64(index + 1), RoutePath: route.path, Role: route.role, SurfaceContractID: route.contract,
			ManifestSHA256: workflowHash('c'), AuthorityCommitOID: strings.Repeat("a", 40), AuthorityBlobOID: strings.Repeat("b", 40),
		})
		configuration.RouteMemberships = append(configuration.RouteMemberships, CutoverGatewayRouteMembership{
			RoutePath: route.path, PublicSurface: route.role,
		})
	}
	for index, surface := range []struct{ name, path string }{
		{"wayfinder", "/mcp/wayfinder"},
		{"planner", "/mcp/planner"},
		{"auditor", "/mcp/auditor"},
	} {
		configuration.AppSurfaces = append(configuration.AppSurfaces, CutoverGatewayAppSurface{
			Sequence: int64(index + 1), Surface: surface.name, PublicPath: surface.path, ManifestSHA256: workflowHash('d'),
		})
		configuration.AppSurfaceMappings = append(configuration.AppSurfaceMappings, CutoverGatewayAppSurfaceMapping{
			Sequence: int64(index + 1), MappingID: surface.name, PublicSurface: surface.name, PublicPath: surface.path,
			ListenerIdentity: "127.0.0.1:1810" + string(rune('1'+index)), UpstreamIdentity: "http://127.0.0.1:8080" + surface.path,
			HealthEvidenceSHA256: workflowHash('e'), TraceEvidenceSHA256: workflowHash('f'),
		})
	}
	return configuration
}
