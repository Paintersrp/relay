package db

import (
	"context"
	"database/sql"
	"fmt"
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
