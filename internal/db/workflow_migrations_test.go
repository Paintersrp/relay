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
