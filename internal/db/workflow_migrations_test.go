package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const migrationTestHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestWayfinderPacketSchemaFreshInstall(t *testing.T) {
	db := openMigrationTestDB(t, "fresh")
	defer db.Close()

	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}

	insertPacketArtifact(t, db, 1, "artifact-fresh-planner", migrationTestHash)
	insertPacketArtifact(t, db, 2, "artifact-fresh-auditor", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	insertPacketArtifact(t, db, 3, "artifact-fresh-wayfinder", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	insertPacket(t, db, 1, "opkt-fresh-planner", migrationTestHash, "planner", "planner-authoring.v1", "project-fresh", 1)
	insertPacket(t, db, 2, "opkt-fresh-auditor", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "auditor", "auditor-review.v1", "project-fresh", 2)
	insertPacket(t, db, 3, "opkt-fresh-wayfinder", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "wayfinder", "wayfinder-discovery.v1", "project-fresh", 3)
	insertMutationResult(t, db, 1, "wayfinder-discovery.v1", "create_operation_packet", "fresh-wayfinder", "create_operation_packet_result")

	assertExecFails(t, db, `INSERT INTO operation_packets (packet_id, packet_sha256, role, operation_id, surface_contract_id, project_id, created_at, packet_artifact_row_id) VALUES ('opkt-fresh-invalid-role', ?, 'operator', 'x', 'planner-authoring.v1', 'project-fresh', ?, 1)`, migrationTestHash, testTimestamp())
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('unknown-surface.v1', 'create_operation_packet', 'fresh-invalid-surface', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
}

func TestWayfinderPacketSchemaUpgradeFromPreChangeDatabase(t *testing.T) {
	db := openMigrationTestDB(t, "upgrade")
	defer db.Close()

	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 25); err != nil {
		t.Fatal(err)
	}

	insertPacketArtifact(t, db, 1, "artifact-upgrade-planner", migrationTestHash)
	insertPacket(t, db, 1, "opkt-upgrade-planner", migrationTestHash, "planner", "planner-authoring.v1", "project-upgrade", 1)
	if _, err := db.Exec(`INSERT INTO operation_packet_retention_dependencies (packet_row_id, dependency_class, dependency_key, required, attached, retained, owner_identity) VALUES (1, 'packet_document', 'artifact-upgrade-planner', 1, 1, 1, 'artifact-upgrade-planner')`); err != nil {
		t.Fatal(err)
	}
	insertMutationResult(t, db, 1, "planner-authoring.v1", "create_operation_packet", "upgrade-planner", "create_operation_packet_result")

	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}

	var packetCount, dependencyCount, mutationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operation_packets WHERE id = 1 AND packet_id = 'opkt-upgrade-planner' AND packet_sha256 = ? AND role = 'planner' AND packet_artifact_row_id = 1`, migrationTestHash).Scan(&packetCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM operation_packet_retention_dependencies WHERE packet_row_id = 1 AND dependency_key = 'artifact-upgrade-planner' AND owner_identity = 'artifact-upgrade-planner'`).Scan(&dependencyCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM mcp_mutation_results WHERE id = 1 AND surface_contract_id = 'planner-authoring.v1' AND mutation_id = 'upgrade-planner'`).Scan(&mutationCount); err != nil {
		t.Fatal(err)
	}
	if packetCount != 1 || dependencyCount != 1 || mutationCount != 1 {
		t.Fatalf("preserved rows = packet %d, dependency %d, mutation %d", packetCount, dependencyCount, mutationCount)
	}

	var foreignKeyErrors int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	if foreignKeyErrors != 0 {
		t.Fatalf("foreign key errors = %d", foreignKeyErrors)
	}
	for _, name := range []string{
		"idx_operation_packets_project_lifecycle",
		"idx_operation_packets_prior",
		"operation_packet_delete_guard",
		"operation_packet_publication_closure_guard",
		"mcp_mutation_result_immutable_update",
		"operation_packet_publication_mutation_result_update_guard",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("schema object %q count = %d", name, count)
		}
	}

	assertExecFails(t, db, `UPDATE operation_packets SET packet_id = 'opkt-mutated' WHERE id = 1`)
	assertExecFails(t, db, `DELETE FROM operation_packets WHERE id = 1`)
	assertExecFails(t, db, `UPDATE mcp_mutation_results SET result_sha256 = ? WHERE id = 1`, strings.Repeat("b", 64))
	assertExecFails(t, db, `DELETE FROM mcp_mutation_results WHERE id = 1`)

	insertPacketArtifact(t, db, 2, "artifact-upgrade-wayfinder", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	insertPacket(t, db, 2, "opkt-upgrade-wayfinder", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "wayfinder", "wayfinder-discovery.v1", "project-upgrade", 2)
	insertMutationResult(t, db, 2, "wayfinder-discovery.v1", "create_operation_packet", "upgrade-wayfinder", "create_operation_packet_result")
	assertExecFails(t, db, `INSERT INTO operation_packets (packet_id, packet_sha256, role, operation_id, surface_contract_id, project_id, created_at, packet_artifact_row_id) VALUES ('opkt-upgrade-invalid-role', ?, 'operator', 'x', 'planner-authoring.v1', 'project-upgrade', ?, 2)`, migrationTestHash, testTimestamp())
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('unknown-surface.v1', 'create_operation_packet', 'upgrade-invalid-surface', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
}

func TestPlannerTicketFrontierMutationResultSchemaUpgradePreservesRows(t *testing.T) {
	db := openMigrationTestDB(t, "planner-ticket-frontier")
	defer db.Close()

	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 35); err != nil {
		t.Fatal(err)
	}

	type mutationRow struct {
		id, surface, tool, mutation, manifest, version, request, kind, document, result, committed string
	}
	rows := []mutationRow{
		{"1", "wayfinder-workspace.v1", "create_operation_packet", "preserve-wayfinder-workspace", migrationTestHash, "v1", migrationTestHash, "create_operation_packet_result", "{}", migrationTestHash, testTimestamp()},
		{"2", "wayfinder-discovery.v1", "refresh_operation_packet", "preserve-wayfinder-discovery", migrationTestHash, "v2", migrationTestHash, "refresh_operation_packet_result", "{}", migrationTestHash, testTimestamp()},
		{"3", "wayfinder-investigation.v1", "close_operation_packet", "preserve-wayfinder-investigation", migrationTestHash, "v3", migrationTestHash, "close_operation_packet_result", "{}", migrationTestHash, testTimestamp()},
		{"4", "planner-authoring.v1", "create_operation_packet", "preserve-planner-authoring", migrationTestHash, "v4", migrationTestHash, "create_operation_packet_result", "{}", migrationTestHash, testTimestamp()},
		{"5", "planner-plan.v1", "submit_plan", "preserve-planner-plan", migrationTestHash, "v5", migrationTestHash, "submit_plan_result", "{}", migrationTestHash, testTimestamp()},
		{"6", "planner-execution.v1", "create_run", "preserve-planner-execution", migrationTestHash, "v6", migrationTestHash, "create_run_result", "{}", migrationTestHash, testTimestamp()},
		{"7", "auditor-review.v1", "create_operation_packet", "preserve-auditor-review", migrationTestHash, "v7", migrationTestHash, "create_operation_packet_result", "{}", migrationTestHash, testTimestamp()},
		{"8", "auditor-audit.v1", "record_audit_decision", "preserve-auditor-audit", migrationTestHash, "v8", migrationTestHash, "record_audit_decision_result", "{}", migrationTestHash, testTimestamp()},
		{"9", "auditor-remediation.v1", "create_run", "preserve-auditor-remediation", migrationTestHash, "v9", migrationTestHash, "create_run_result", "{}", migrationTestHash, testTimestamp()},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO mcp_mutation_results (id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256, committed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, row.id, row.surface, row.tool, row.mutation, row.manifest, row.version, row.request, row.kind, row.document, row.result, row.committed); err != nil {
			t.Fatal(err)
		}
	}
	createMigrationPublicationGraph(t, db)
	preserved := make(map[string][][]any)
	for _, table := range []string{
		"mcp_mutation_results",
		"operation_packets",
		"operation_packet_publications",
		"operation_packet_retention_dependencies",
		"operation_packet_retained_artifacts",
		"operation_packet_artifact_bindings",
		"operation_packet_vault_relationships",
	} {
		preserved[table] = snapshotMigrationTable(t, db, table)
	}

	if err := goose.Up(db, "workflow_migrations"); err != nil {
		t.Fatal(err)
	}
	for table, want := range preserved {
		if got := snapshotMigrationTable(t, db, table); !reflect.DeepEqual(got, want) {
			t.Fatalf("preserved %s = %#v, want %#v", table, got, want)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mcp_mutation_results`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(rows)+1 {
		t.Fatalf("mutation row count = %d, want %d", count, len(rows)+1)
	}
	for _, want := range rows {
		var got mutationRow
		if err := db.QueryRow(`SELECT id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256, committed_at FROM mcp_mutation_results WHERE id = ?`, want.id).Scan(&got.id, &got.surface, &got.tool, &got.mutation, &got.manifest, &got.version, &got.request, &got.kind, &got.document, &got.result, &got.committed); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("preserved mutation row = %#v, want %#v", got, want)
		}
	}
	var foreignKeyErrors int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	if foreignKeyErrors != 0 {
		t.Fatalf("foreign key errors = %d", foreignKeyErrors)
	}
	for _, name := range []string{
		"mcp_mutation_result_immutable_update",
		"mcp_mutation_result_delete_guard",
		"operation_packet_publication_mutation_result_update_guard",
		"operation_packet_publication_mutation_result_delete_guard",
		"operation_packet_publication_closure_guard",
		"operation_packet_publication_mutation_result_authority_guard",
	} {
		var objectCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&objectCount); err != nil {
			t.Fatal(err)
		}
		if objectCount != 1 {
			t.Fatalf("schema object %q count = %d", name, objectCount)
		}
	}

	insertMutationResult(t, db, 10, "planner-ticket-frontier.v1", "create_operation_packet", "planner-ticket-frontier", "create_operation_packet_result")
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('unknown-surface.v1', 'create_operation_packet', 'unknown-surface', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('planner-ticket-frontier.v1', 'unknown.operation', 'unknown-mutation-tool', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('planner-ticket-frontier.v1', 'submit_plan', 'mismatched-valid-pair', ?, 'v1', ?, 'submit_plan_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
	assertExecFails(t, db, `UPDATE mcp_mutation_results SET result_sha256 = ? WHERE id = 1`, strings.Repeat("b", 64))
	assertExecFails(t, db, `DELETE FROM mcp_mutation_results WHERE id = 1`)
}

func TestPlannerTicketFrontierV2SurfaceSchemaAdmitsActiveSurface(t *testing.T) {
	db := openMigrationTestDB(t, "planner-ticket-frontier-v2")
	defer db.Close()

	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "workflow_migrations"); err != nil {
		t.Fatal(err)
	}

	insertMutationResult(t, db, 1, "planner-ticket-frontier.v2", "create_operation_packet", "planner-ticket-frontier-v2", "create_operation_packet_result")
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('unknown-surface.v1', 'create_operation_packet', 'unknown-surface', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
	assertExecFails(t, db, `INSERT INTO mcp_mutation_results (surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES ('planner-ticket-frontier.v2', 'submit_plan', 'mismatched-valid-pair', ?, 'v1', ?, 'submit_plan_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash)
}

func TestSourceIndexGenerationSchemaUpgradeAndGuards(t *testing.T) {
	db := openMigrationTestDB(t, "source-index-generation")
	defer db.Close()

	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 31); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}

	columns := make(map[string]bool)
	rows, err := db.Query(`SELECT name FROM pragma_table_info('source_index_generations')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"generation_id", "identity_version", "vault_id", "commit_oid", "tree_oid", "engine", "engine_revision", "build_contract_version", "build_options_sha256", "state", "attempt_count", "generation_manifest_sha256", "coverage_manifest_sha256", "artifact_manifest_sha256", "failure_code", "failure_message", "created_at", "updated_at", "building_started_at", "ready_at", "failed_at", "retired_at"} {
		if !columns[name] {
			t.Fatalf("missing source generation column %q", name)
		}
	}

	id := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	tree := strings.Repeat("c", 40)
	options := strings.Repeat("d", 64)
	if _, err := db.Exec(`INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, 'relay.source-index-generation-identity.v1', 'vault-migration', ?, ?, 'zoekt', '2b2ce2e398e6bee68d67143f567b6c6199340c7f', 'relay.source-index-build.v1', ?, 'pending')`, id, commit, tree, options); err != nil {
		t.Fatal(err)
	}
	assertExecFails(t, db, `INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, 'relay.source-index-generation-identity.v1', 'vault-migration', ?, ?, 'zoekt', '2b2ce2e398e6bee68d67143f567b6c6199340c7f', 'relay.source-index-build.v1', ?, 'pending')`, id, commit, tree, options)
	assertExecFails(t, db, `INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, 'relay.source-index-generation-identity.v1', 'vault-migration', ?, ?, 'zoekt', '2b2ce2e398e6bee68d67143f567b6c6199340c7f', 'relay.source-index-build.v1', ?, 'pending')`, strings.Repeat("e", 64), commit, tree, options)
	assertExecFails(t, db, `INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, 'relay.source-index-generation-identity.v1', 'vault-invalid', ?, ?, 'zoekt', '2b2ce2e398e6bee68d67143f567b6c6199340c7f', 'relay.source-index-build.v1', ?, 'building')`, strings.Repeat("f", 64), commit, tree, options)
	assertExecFails(t, db, `UPDATE source_index_generations SET vault_id = 'changed' WHERE generation_id = ?`, id)
	assertExecFails(t, db, `UPDATE source_index_generations SET state = 'ready' WHERE generation_id = ?`, id)
	assertExecFails(t, db, `DELETE FROM source_index_generations WHERE generation_id = ?`, id)
}

func TestIntegratedDiscoveryFoundationMigrationPreservesLegacyWorkspacesAndDefaultsDisabled(t *testing.T) {
	db := openMigrationTestDB(t, "integrated-discovery-foundation")
	defer db.Close()
	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 33); err != nil {
		t.Fatal(err)
	}
	var projectID, workspaceID, planID, artifactID, ticketID int64
	if err := db.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-discovery-migration', 'Discovery Migration') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug, state) VALUES ('workspace-discovery-legacy', ?, 'legacy', 'open') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT version FROM feature_workspaces WHERE id = ?`, workspaceID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-discovery-migration', 'legacy', ?) RETURNING id`, projectID, migrationTestHash).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-discovery-migration', 'plan', ?, 'requirements', 'plans/discovery-migration/requirements.json', 'application/json', ?, 2) RETURNING id`, planID, migrationTestHash).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_tickets (discovery_ticket_id, workspace_row_id, ticket_key, subject, state) VALUES ('discovery-legacy', ?, 'legacy', 'legacy ticket', 'resolved') RETURNING id`, workspaceID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range []struct {
		id, key, state, kind string
	}{{"discovery-open", "open", "open", "resolved"}, {"discovery-blocked", "blocked", "blocked", "rejected"}, {"discovery-cancelled", "cancelled", "cancelled", "deferred"}} {
		var rowID int64
		if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_tickets (discovery_ticket_id, workspace_row_id, ticket_key, subject, state) VALUES (?, ?, ?, ?, ?) RETURNING id`, ticket.id, workspaceID, ticket.key, ticket.key, ticket.state).Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO feature_workspace_ticket_resolutions (resolution_id, ticket_row_id, sequence, resolution_kind, artifact_row_id, artifact_sha256) VALUES (?, ?, 1, ?, ?, ?)`, "resolution-"+ticket.id, rowID, ticket.kind, artifactID, migrationTestHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_ticket_resolutions (resolution_id, ticket_row_id, sequence, resolution_kind, artifact_row_id, artifact_sha256) VALUES ('resolution-discovery-legacy', ?, 1, 'resolved', ?, ?)`, ticketID, artifactID, migrationTestHash); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	var enabled, metadata, discoveryArtifacts, discoveryRevisions, discoveryConsequences int
	var currentRevision sql.NullInt64
	var state string
	if err := db.QueryRow(`SELECT discovery_capability_enabled, state FROM feature_workspaces WHERE id = ?`, workspaceID).Scan(&enabled, &state); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM feature_workspace_discovery_work_item_metadata WHERE ticket_row_id = ?`, ticketID).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	var workspaceVersion int
	if err := db.QueryRow(`SELECT version FROM feature_workspaces WHERE id = ?`, workspaceID).Scan(&workspaceVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT current_discovery_revision_row_id FROM feature_workspaces WHERE id = ?`, workspaceID).Scan(&currentRevision); err != nil {
		t.Fatal(err)
	}
	for table, target := range map[string]*int{"feature_workspace_discovery_artifacts": &discoveryArtifacts, "feature_workspace_integrated_discovery_revisions": &discoveryRevisions, "feature_workspace_discovery_integration_consequences": &discoveryConsequences} {
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if enabled != 0 || metadata != 0 || state != "open" || workspaceVersion != 3 || currentRevision.Valid || discoveryArtifacts != 0 || discoveryRevisions != 0 || discoveryConsequences != 0 {
		t.Fatalf("legacy discovery migration = enabled %d metadata %d state %q", enabled, metadata, state)
	}
	var ticketCount, resolutionCount int
	if err := db.QueryRow(`SELECT count(*) FROM feature_workspace_discovery_tickets WHERE workspace_row_id = ?`, workspaceID).Scan(&ticketCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM feature_workspace_ticket_resolutions WHERE ticket_row_id IN (SELECT id FROM feature_workspace_discovery_tickets WHERE workspace_row_id = ?)`, workspaceID).Scan(&resolutionCount); err != nil {
		t.Fatal(err)
	}
	if ticketCount != 4 || resolutionCount != 4 {
		t.Fatalf("legacy ticket history counts = tickets %d resolutions %d", ticketCount, resolutionCount)
	}
	for _, expected := range []struct{ id, state, kind string }{{"discovery-legacy", "resolved", "resolved"}, {"discovery-open", "open", "resolved"}, {"discovery-blocked", "blocked", "rejected"}, {"discovery-cancelled", "cancelled", "deferred"}} {
		var gotState, gotKind string
		if err := db.QueryRow(`SELECT t.state, r.resolution_kind FROM feature_workspace_discovery_tickets AS t JOIN feature_workspace_ticket_resolutions AS r ON r.ticket_row_id = t.id WHERE t.discovery_ticket_id = ?`, expected.id).Scan(&gotState, &gotKind); err != nil {
			t.Fatal(err)
		}
		if gotState != expected.state || gotKind != expected.kind {
			t.Fatalf("legacy ticket %q = state %q kind %q", expected.id, gotState, gotKind)
		}
	}
	if _, err := db.Exec(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug, state) VALUES ('workspace-discovery-new', ?, 'new', 'open')`, projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT discovery_capability_enabled FROM feature_workspaces WHERE workspace_id = 'workspace-discovery-new'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("new workspace discovery capability = %d", enabled)
	}
}

func createMigrationPublicationGraph(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO operation_packet_artifacts (id, artifact_id, relative_path, sha256, size_bytes) VALUES (10, 'artifact-upgrade-publication', 'operation-packets/artifact-upgrade-publication.json', ?, 2)`, migrationTestHash); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO operation_packets (id, packet_id, packet_sha256, role, operation_id, surface_contract_id, project_id, created_at, packet_artifact_row_id, coordinated_publication_id) VALUES (10, 'opkt-upgrade-publication', ?, 'planner', 'planner-authoring', 'planner-authoring.v1', 'project-upgrade', ?, 10, 'publication-upgrade-publication')`, migrationTestHash, testTimestamp()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO operation_packet_retention_dependencies (packet_row_id, dependency_class, dependency_key, required, attached, retained, owner_identity) VALUES (10, 'packet_document', 'artifact-upgrade-publication', 1, 1, 1, 'artifact-upgrade-publication')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO mcp_mutation_results (id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES (20, 'planner-authoring.v1', 'create_operation_packet', 'upgrade-publication', ?, 'v1', ?, 'create_operation_packet_result', '{}', ?)`, migrationTestHash, migrationTestHash, migrationTestHash); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO operation_packet_artifact_bindings (publication_id, packet_row_id, sequence, dependency_class, dependency_key, packet_artifact_row_id) VALUES ('publication-upgrade-publication', 10, 0, 'packet_document', 'artifact-upgrade-publication', 10)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO operation_packet_publications (publication_id, packet_row_id, packet_artifact_row_id, mutation_result_row_id, namespace, manifest_sha256, expected_retained_artifact_count, expected_binding_count, expected_dependency_count, expected_vault_relationship_count) VALUES ('publication-upgrade-publication', 10, 10, 20, 'operation-packet-publications/publication-upgrade-publication', ?, 0, 1, 1, 0)`, migrationTestHash); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func snapshotMigrationTable(t *testing.T, db *sql.DB, table string) [][]any {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM ` + table + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var values [][]any
	for rows.Next() {
		row := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func openMigrationTestDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:relay-migration-%s-%s?mode=memory&cache=shared", name, strings.ReplaceAll(t.Name(), "/", "-")))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func insertPacketArtifact(t *testing.T, db *sql.DB, id int, artifactID, sha256 string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO operation_packet_artifacts (id, artifact_id, relative_path, sha256, size_bytes) VALUES (?, ?, ?, ?, 2)`, id, artifactID, "operation-packets/"+artifactID+".json", sha256); err != nil {
		t.Fatal(err)
	}
}

func insertPacket(t *testing.T, db *sql.DB, id int, packetID, sha256, role, surface, project string, artifactID int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO operation_packets (id, packet_id, packet_sha256, role, operation_id, surface_contract_id, project_id, created_at, packet_artifact_row_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, packetID, sha256, role, strings.TrimSuffix(surface, ".v1"), surface, project, testTimestamp(), artifactID); err != nil {
		t.Fatal(err)
	}
}

func insertMutationResult(t *testing.T, db *sql.DB, id int, surface, tool, mutationID, resultKind string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO mcp_mutation_results (id, surface_contract_id, tool_name, mutation_id, surface_manifest_sha256, semantic_identity_version, semantic_request_sha256, result_kind, result_identity_json, result_sha256) VALUES (?, ?, ?, ?, ?, 'v1', ?, ?, '{}', ?)`, id, surface, tool, mutationID, migrationTestHash, migrationTestHash, resultKind, migrationTestHash); err != nil {
		t.Fatal(err)
	}
}

func assertExecFails(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err == nil {
		t.Fatalf("expected statement to fail: %s", query)
	}
}

func testTimestamp() string { return "2026-07-24T21:00:00.000000000Z" }

func TestPlanningCandidateMigrationFrom40PreservesLegacyWorkspacesWithoutCandidates(t *testing.T) {
	db := openMigrationTestDB(t, "planning-candidate-upgrade")
	defer db.Close()
	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 40); err != nil {
		t.Fatal(err)
	}
	var projectID, workspaceID int64
	if err := db.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-legacy-candidate', 'Legacy Candidate') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-legacy-candidate', ?, 'legacy') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	var candidateTables int
	for _, table := range []string{"planning_candidates", "planning_candidate_approvals", "delivery_ticket_production_links"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		candidateTables += count
	}
	var currentRevision, currentPacket sql.NullInt64
	if err := db.QueryRow(`SELECT current_discovery_revision_row_id, current_discovery_closure_packet_row_id FROM feature_workspaces WHERE id = ?`, workspaceID).Scan(&currentRevision, &currentPacket); err != nil {
		t.Fatal(err)
	}
	if candidateTables != 0 || currentRevision.Valid || currentPacket.Valid {
		t.Fatalf("legacy candidate migration synthesized state: rows=%d revision=%v packet=%v", candidateTables, currentRevision, currentPacket)
	}
}

func TestTicketDesignBriefRemovalMigrationDropsBriefSurface(t *testing.T) {
	db := openMigrationTestDB(t, "ticket-design-brief-removal")
	defer db.Close()
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	// The Ticket Design Brief is no longer an authority surface: its tables,
	// the selection current-Brief pointer, and the package design-brief digest
	// are all gone from the active schema.
	for _, table := range []string{"ticket_design_briefs", "ticket_design_brief_approvals"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("brief table %q still exists", table)
		}
	}
	var packageSchema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'execution_packages'`).Scan(&packageSchema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(packageSchema, "design_brief_sha256") {
		t.Fatalf("execution_packages still carries design_brief_sha256: %s", packageSchema)
	}
	var selectionSchema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'delivery_ticket_selections'`).Scan(&selectionSchema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(selectionSchema, "current_ticket_design_brief_row_id") {
		t.Fatalf("delivery_ticket_selections still carries the current Brief pointer: %s", selectionSchema)
	}
	var briefTriggers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'ticket_design_brief%' OR name LIKE '%current_brief%'`).Scan(&briefTriggers); err != nil {
		t.Fatal(err)
	}
	if briefTriggers != 0 {
		t.Fatalf("brief schema objects = %d", briefTriggers)
	}
	var foreignKeyErrors int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	if foreignKeyErrors != 0 {
		t.Fatalf("foreign key errors = %d", foreignKeyErrors)
	}
}

func TestPlanningCandidateReviewMigrationFrom43PersistsNoReviewAuthority(t *testing.T) {
	db := openMigrationTestDB(t, "planning-candidate-review-upgrade")
	defer db.Close()
	goose.SetBaseFS(WorkflowMigrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "workflow_migrations", 43); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"planning_candidate_reviews", "ticket_design_brief_reviews",
		"planning_candidate_review_binding_guard", "ticket_design_brief_review_binding_guard",
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("durable review schema object %q count = %d", name, count)
		}
	}
	var foreignKeyErrors int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	if foreignKeyErrors != 0 {
		t.Fatalf("foreign key errors = %d", foreignKeyErrors)
	}
}

func TestReviewDispositionMigrationPersistsNothing(t *testing.T) {
	db := openMigrationTestDB(t, "review-disposition-upgrade")
	defer db.Close()
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('planning_candidate_reviews', 'ticket_design_brief_reviews')`).Scan(&count); err != nil { t.Fatal(err) }
	if count != 0 { t.Fatalf("durable review objects = %d", count) }
}

// seedAuthorityClosingBasis creates the source-backed explicit authority basis
// used by the authority route of the completion decision guard.
func seedAuthorityClosingBasis(t *testing.T, db *sql.DB) (workspaceID, authorityID, closureID int64) {
	t.Helper()
	var projectID int64
	if err := db.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-authority-closing', 'Authority Closing') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-authority-closing', ?, 'authority') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID int64
	if err := db.QueryRow(`INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-authority-closing', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-authority-closing', ?, ?, ?, 1, 'refs/relay/closures/authority-closing', 'ready', '2026-08-05T00:00:00.000000000Z', '2026-08-05T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_authority_revisions (authority_revision_id, workspace_row_id, revision_number, source_closure_row_id) VALUES ('authority-closing', ?, 1, ?) RETURNING id`, workspaceID, closureID).Scan(&authorityID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_authority_revision_row_id = ?, version = version + 1 WHERE id = ?`, authorityID, workspaceID); err != nil {
		t.Fatal(err)
	}
	return workspaceID, authorityID, closureID
}

// seedNoDeliveryClosure creates the adopted closed no-delivery discovery state
// the no-delivery closing route records: current integrated revision, current
// closure packet with its manifest and member artifacts, and adoption.
func seedNoDeliveryClosure(t *testing.T, db *sql.DB, destination string) (workspaceID, packetID int64) {
	t.Helper()
	var projectID int64
	if err := db.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-no-delivery-closure', 'No Delivery') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-no-delivery-closure', ?, 'no-delivery') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_discovery_adoptions (workspace_row_id, adoption_id, operator_identity, adopted_workspace_version) VALUES (?, 'discovery-adoption-no-delivery', 'operator', 1)`, workspaceID); err != nil {
		t.Fatal(err)
	}
	var revisionArtifact, revision, manifestArtifact int64
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-no-delivery-revision', ?, 'feature-discovery/workspace-no-delivery-closure/revision-a/revision.md', ?, 'text/markdown', 4) RETURNING id`, workspaceID, strings.Repeat("a", 64)).Scan(&revisionArtifact); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity, settled_destination) VALUES ('discovery-revision-no-delivery', ?, 1, ?, 'operator', 'no_delivery_work') RETURNING id`, workspaceID, revisionArtifact).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-no-delivery-manifest', ?, 'feature-discovery/workspace-no-delivery-closure/closure-b/closure.json', ?, 'application/vnd.relay.feature-discovery-closure+json', 4) RETURNING id`, workspaceID, strings.Repeat("b", 64)).Scan(&manifestArtifact); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES ('discovery-packet-no-delivery', ?, ?, ?, ?, ?, 4, 'application/vnd.relay.feature-discovery-closure+json') RETURNING id`, workspaceID, revision, destination, manifestArtifact, strings.Repeat("b", 64)).Scan(&packetID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revision, packetID, workspaceID); err != nil {
		t.Fatal(err)
	}
	return workspaceID, packetID
}

func insertNoDeliveryDecision(t *testing.T, db *sql.DB, workspaceID, packetID int64, decisionID string) int64 {
	t.Helper()
	var rowID int64
	if err := db.QueryRow(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, discovery_closure_packet_row_id, decision) VALUES (?, ?, NULL, NULL, ?, 'completed') RETURNING id`, decisionID, workspaceID, packetID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	return rowID
}

// seedUnsatisfiedDeliveryTicket creates a current Delivery Ticket revision
// without any audit satisfaction so the delivery guard of the completion
// decision trigger must reject a no-delivery closing basis.
func seedUnsatisfiedDeliveryTicket(t *testing.T, db *sql.DB, workspaceID int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err := db.QueryRow(`INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-no-delivery-guard', 'relay', 'vaults/features') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-no-delivery-guard', ?, ?, ?, 1, 'refs/relay/closures/no-delivery-guard', 'ready', '2026-07-18T00:00:00.000000000Z', '2026-07-18T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("d", 40), strings.Repeat("e", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	var ticketID int64
	if err := db.QueryRow(`INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P-NO-DELIVERY-GUARD', ?, 1) RETURNING id`, workspaceID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	var revisionID int64
	if err := db.QueryRow(`INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, 'tickets/P-NO-DELIVERY-GUARD.delivery-ticket.json', 'goal', 'context', 'not_required') RETURNING id`, ticketID, strings.Repeat("d", 40), closureID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE delivery_tickets SET current_revision_row_id = ? WHERE id = ?`, revisionID, ticketID); err != nil {
		t.Fatal(err)
	}
}

// seedDiscoveryReopenEvent creates the discovery reopen event that records the
// later valid discovery change replacing the no-delivery packet.
func seedDiscoveryReopenEvent(t *testing.T, db *sql.DB, workspaceID, packetID int64) {
	t.Helper()
	var revisionArtifact, replacementRevision, causeArtifact int64
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-no-delivery-replacement', ?, 'feature-discovery/workspace-no-delivery-closure/revision-c/replacement.md', ?, 'text/markdown', 4) RETURNING id`, workspaceID, strings.Repeat("c", 64)).Scan(&revisionArtifact); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, predecessor_revision_row_id, created_identity, settled_destination) VALUES ('discovery-revision-no-delivery-replacement', ?, 2, ?, (SELECT id FROM feature_workspace_integrated_discovery_revisions WHERE workspace_row_id = ? AND revision_number = 1), 'operator', 'no_delivery_work') RETURNING id`, workspaceID, revisionArtifact, workspaceID).Scan(&replacementRevision); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-no-delivery-cause', ?, 'feature-discovery/workspace-no-delivery-closure/cause-d/cause.txt', ?, 'text/plain', 4) RETURNING id`, workspaceID, strings.Repeat("d", 64)).Scan(&causeArtifact); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feature_workspace_discovery_reopen_events (reopen_event_id, workspace_row_id, closure_packet_row_id, replacement_revision_row_id, cause_text, confirmed_operator_identity, cause_artifact_row_id) VALUES ('discovery-reopen-no-delivery', ?, ?, ?, 'later discovery change', 'operator', ?)`, workspaceID, packetID, replacementRevision, causeArtifact); err != nil {
		t.Fatal(err)
	}
}

// TestNoDeliveryFeatureClosingMigrationPreservesHistoryAndGuards covers the
// 00049 forward migration: existing authority-based decisions survive with
// nullable authority columns, the no-delivery closing basis is admitted only
// on the exact current no-delivery packet with no delivery work, and the
// immutability and discovery-reopen protections are retained.
func TestNoDeliveryFeatureClosingMigrationPreservesHistoryAndGuards(t *testing.T) {
	t.Run("upgrade preserves authority decisions and nullable columns", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-upgrade")
		defer db.Close()
		goose.SetBaseFS(WorkflowMigrationsFS)
		if err := goose.SetDialect("sqlite3"); err != nil {
			t.Fatal(err)
		}
		if err := goose.UpTo(db, "workflow_migrations", 48); err != nil {
			t.Fatal(err)
		}
		workspaceID, authorityID, closureID := seedAuthorityClosingBasis(t, db)
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, decision) VALUES ('completion-legacy-authority', ?, ?, ?, 'completed')`, workspaceID, authorityID, closureID); err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM feature_workspace_completion_decisions WHERE completion_decision_id = 'completion-legacy-authority'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("preserved authority decision count=%d err=%v", count, err)
		}
		notNull := map[string]bool{}
		rows, err := db.Query(`SELECT name, "notnull" FROM pragma_table_info('feature_workspace_completion_decisions')`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			var flag int
			if err := rows.Scan(&name, &flag); err != nil {
				t.Fatal(err)
			}
			notNull[name] = flag == 1
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if notNull["authority_revision_row_id"] || notNull["source_closure_row_id"] {
			t.Fatalf("authority columns are still NOT NULL: %v", notNull)
		}
		// The authority route remains guarded: this workspace carries an
		// explicit authority, so a decision without that authority and without
		// a no-delivery packet cannot be inserted.
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, decision) VALUES ('completion-forged-no-authority', ?, NULL, NULL, 'completed')`, workspaceID); err == nil {
			t.Fatal("decision without a current authority or no-delivery packet was accepted")
		}
		var foreignKeyErrors int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
			t.Fatal(err)
		}
		if foreignKeyErrors != 0 {
			t.Fatalf("foreign key errors = %d", foreignKeyErrors)
		}
	})
	t.Run("no-delivery decision allowed on the exact current packet", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-guard")
		defer db.Close()
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		workspaceID, packetID := seedNoDeliveryClosure(t, db, "no_delivery_work")
		decisionID := insertNoDeliveryDecision(t, db, workspaceID, packetID, "completion-no-delivery-ok")
		if decisionID == 0 {
			t.Fatal("no-delivery decision row ID was not assigned")
		}
		if _, err := db.Exec(`UPDATE feature_workspace_completion_decisions SET decision = 'abandoned' WHERE id = ?`, decisionID); err == nil {
			t.Fatal("no-delivery decision was mutable")
		}
		if _, err := db.Exec(`DELETE FROM feature_workspace_completion_decisions WHERE id = ?`, decisionID); err == nil {
			t.Fatal("no-delivery decision was deletable")
		}
	})
	t.Run("no-delivery decision rejected without a packet basis", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-no-packet")
		defer db.Close()
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		workspaceID, _ := seedNoDeliveryClosure(t, db, "no_delivery_work")
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, decision) VALUES ('completion-no-delivery-no-packet', ?, NULL, NULL, 'completed')`, workspaceID); err == nil {
			t.Fatal("no-delivery decision without a packet basis was accepted")
		}
	})
	t.Run("non-no-delivery packet cannot close without authority", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-wrong-packet")
		defer db.Close()
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		workspaceID, packetID := seedNoDeliveryClosure(t, db, "requirements")
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, discovery_closure_packet_row_id, decision) VALUES ('completion-wrong-route', ?, NULL, NULL, ?, 'completed')`, workspaceID, packetID); err == nil {
			t.Fatal("non-no-delivery packet was accepted as a no-delivery closing basis")
		}
	})
	t.Run("unsatisfied delivery work blocks no-delivery decision", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-delivery-guard")
		defer db.Close()
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		workspaceID, packetID := seedNoDeliveryClosure(t, db, "no_delivery_work")
		seedUnsatisfiedDeliveryTicket(t, db, workspaceID)
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_decisions (completion_decision_id, workspace_row_id, authority_revision_row_id, source_closure_row_id, discovery_closure_packet_row_id, decision) VALUES ('completion-delivery-guard', ?, NULL, NULL, ?, 'completed')`, workspaceID, packetID); err == nil {
			t.Fatal("no-delivery decision accepted with unsatisfied delivery work")
		}
	})
	t.Run("discovery reopen guard", func(t *testing.T) {
		db := openMigrationTestDB(t, "no-delivery-closure-reopen-guard")
		defer db.Close()
		if err := AutoMigrateWorkflow(db); err != nil {
			t.Fatal(err)
		}
		workspaceID, packetID := seedNoDeliveryClosure(t, db, "no_delivery_work")
		decisionID := insertNoDeliveryDecision(t, db, workspaceID, packetID, "completion-reopen-guard")
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_reopenings (completion_decision_row_id, reopening_kind) VALUES (?, 'discovery_reopen')`, decisionID); err == nil {
			t.Fatal("discovery_reopen reopening accepted without a reopen event")
		}
		seedDiscoveryReopenEvent(t, db, workspaceID, packetID)
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_reopenings (completion_decision_row_id, reopening_kind) VALUES (?, 'discovery_reopen')`, decisionID); err != nil {
			t.Fatalf("discovery_reopen reopening rejected with the reopen event: %v", err)
		}
		var current int
		if err := db.QueryRow(`SELECT count(*) FROM feature_workspace_completion_decisions AS completion WHERE completion.workspace_row_id = ? AND NOT EXISTS (SELECT 1 FROM feature_workspace_completion_reopenings AS reopening WHERE reopening.completion_decision_row_id = completion.id)`, workspaceID).Scan(&current); err != nil || current != 0 {
			t.Fatalf("current decision count = %d, err=%v", current, err)
		}
		if _, err := db.Exec(`INSERT INTO feature_workspace_completion_reopenings (completion_decision_row_id, reopening_kind) VALUES (?, 'discovery_reopen')`, decisionID); err == nil {
			t.Fatal("duplicate completion reopening was accepted")
		}
	})
}

func TestDeliveryPlanMigrationAddsDurableSurfaceAndExtendsCandidateFamilies(t *testing.T) {
	db := openMigrationTestDB(t, "delivery-plan-surface")
	defer db.Close()
	if err := AutoMigrateWorkflow(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"delivery_plans", "delivery_plan_units", "delivery_plan_unit_dependencies", "delivery_ticket_plan_unit_links"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("delivery plan table %q missing", table)
		}
	}
	// The planning candidate family vocabulary now admits delivery_plan.
	var projectID, workspaceID int64
	if err := db.QueryRow(`INSERT INTO projects (project_id, name) VALUES ('project-plan-migration', 'Plan Migration') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-plan-migration', ?, 'plan-migration') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('plan-migration-repo', 'C:/plan-migration-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var artifactID, manifestID, revisionID, packetID int64
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-migration-plan', ?, 'feature-discovery/workspace-plan-migration/plan/plan.json', ?, 'application/json', 4) RETURNING id`, workspaceID, migrationTestHash).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-migration-manifest', ?, 'feature-discovery/workspace-plan-migration/plan/manifest.json', ?, 'application/vnd.relay.feature-discovery-closure+json', 4) RETURNING id`, workspaceID, migrationTestHash).Scan(&manifestID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity) VALUES ('discovery-revision-migration-plan', ?, 1, ?, 'migration') RETURNING id`, workspaceID, artifactID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES ('discovery-packet-migration-plan', ?, ?, 'shared_design', ?, ?, 4, 'application/vnd.relay.feature-discovery-closure+json') RETURNING id`, workspaceID, revisionID, manifestID, migrationTestHash).Scan(&packetID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revisionID, packetID, workspaceID); err != nil {
		t.Fatal(err)
	}
	var candidateID int64
	if err := db.QueryRow(`INSERT INTO planning_candidates (candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, repo_target, branch, base_commit, destination, created_identity) VALUES ('candidate-migration-plan', ?, 'delivery_plan', 'plan-migration.delivery-plan.json', ?, ?, 4, ?, 'plan-migration-repo', 'main', ?, 'shared_design', 'migration') RETURNING id`, workspaceID, artifactID, migrationTestHash, packetID, strings.Repeat("b", 40)).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	var planRowID int64
	if err := db.QueryRow(`INSERT INTO delivery_plans (plan_id, workspace_row_id, candidate_row_id, artifact_row_id, artifact_sha256, artifact_size_bytes, feature_slug, goal, context, created_identity) VALUES ('delivery-plan-migration', ?, ?, ?, ?, 4, 'plan-migration', 'Goal', 'Context', 'migration') RETURNING id`, workspaceID, candidateID, artifactID, migrationTestHash).Scan(&planRowID); err != nil {
		t.Fatal(err)
	}
	var unitRowID int64
	if err := db.QueryRow(`INSERT INTO delivery_plan_units (plan_row_id, sequence, unit_id, goal) VALUES (?, 1, 'P1-T1', 'Unit goal') RETURNING id`, planRowID).Scan(&unitRowID); err != nil {
		t.Fatal(err)
	}
	// A planned dependency must reference a unit of the same Plan.
	if _, err := db.Exec(`INSERT INTO delivery_plan_unit_dependencies (unit_row_id, sequence, depends_on_unit_row_id) VALUES (?, 1, ?)`, unitRowID, unitRowID+999); err == nil {
		t.Fatal("planned dependency accepted across plans")
	}
	// The current-plan pointer must reference a Plan of the workspace.
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_delivery_plan_row_id = ?, version = version + 1 WHERE id = ?`, int64(999999), workspaceID); err == nil {
		t.Fatal("current delivery plan pointer accepted a non-plan row")
	}
	if _, err := db.Exec(`UPDATE feature_workspaces SET current_delivery_plan_row_id = ?, version = version + 1 WHERE id = ?`, planRowID, workspaceID); err != nil {
		t.Fatalf("current delivery plan pointer rejected: %v", err)
	}
	// The delivery plan surface is immutable retained history.
	if _, err := db.Exec(`UPDATE delivery_plans SET goal = 'mutated' WHERE id = ?`, planRowID); err == nil {
		t.Fatal("delivery plan was mutable")
	}
	if _, err := db.Exec(`DELETE FROM delivery_plans WHERE id = ?`, planRowID); err == nil {
		t.Fatal("delivery plan was deletable")
	}
	// Unknown candidate families remain rejected.
	if _, err := db.Exec(`INSERT INTO planning_candidates (candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, repo_target, branch, base_commit, destination, created_identity) VALUES ('candidate-migration-invalid', ?, 'unknown_family', 'x.json', ?, ?, 4, ?, 'plan-migration-repo', 'main', ?, 'shared_design', 'migration')`, workspaceID, artifactID, migrationTestHash, packetID, strings.Repeat("b", 40)); err == nil {
		t.Fatal("unknown candidate family was accepted")
	}
}
