package store

import (
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestMigrationCompatibility(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "migration_compat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Verify store.Open succeeds and automigrates
	st, err := Open(tempDB, logger)
	if err != nil {
		t.Fatalf("Open store failed: %v", err)
	}
	defer st.Close()

	db := st.DB()

	// 2. Verify foreign keys are enabled (must return 1)
	var fkEnabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys pragma to be 1 (enabled), got %d", fkEnabled)
	}

	// 3. Verify goose_db_version exists and reports the latest version
	var gooseCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='goose_db_version'").Scan(&gooseCount); err != nil {
		t.Fatalf("check goose_db_version table: %v", err)
	}
	if gooseCount != 1 {
		t.Fatalf("expected goose_db_version table to exist, got %d", gooseCount)
	}

	var latestVersion int
	if err := db.QueryRow("SELECT MAX(version_id) FROM goose_db_version").Scan(&latestVersion); err != nil {
		t.Fatalf("query max version from goose_db_version: %v", err)
	}
	if latestVersion <= 0 {
		t.Errorf("expected latest goose migration version > 0, got %d", latestVersion)
	}

	// 4. Verify critical columns on tables using PRAGMA table_info
	assertTableColumns(t, db, "projects", []string{"id", "project_id", "name", "status"})
	assertTableColumns(t, db, "project_repositories", []string{"id", "project_row_id", "repo_id", "role", "local_path", "enabled"})
	assertTableColumns(t, db, "source_snapshots", []string{"id", "source_snapshot_id", "project_row_id", "project_id", "snapshot_kind", "status"})
	assertTableColumns(t, db, "context_packets", []string{"id", "context_packet_id", "project_row_id", "project_id", "plan_id", "pass_id", "task_slug", "source_snapshot_row_id", "status"})
	assertTableColumns(t, db, "project_context_records", []string{"id", "context_record_id", "project_row_id", "project_id", "kind", "title", "body", "status"})
	assertTableColumns(t, db, "local_audits", []string{"id", "audit_id", "project_row_id", "project_id", "mode", "status", "manifest_path"})

	// 5. Test minimal create / query workflows to verify query capability
	project, err := st.CreateProject("relay-compat", "Relay Compatibility Project", "Testing schema compatibility", "active", "")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if project.ProjectID != "relay-compat" {
		t.Errorf("expected project_id 'relay-compat', got %s", project.ProjectID)
	}

	gotProject, err := st.GetProjectByProjectID("relay-compat")
	if err != nil {
		t.Fatalf("GetProjectByProjectID failed: %v", err)
	}
	if gotProject.ID != project.ID {
		t.Errorf("expected project row ID %d, got %d", project.ID, gotProject.ID)
	}

	audit, err := st.CreateLocalAudit(CreateLocalAuditParams{
		AuditID:      "compat-audit-1",
		ProjectRowID: project.ID,
		ProjectID:    project.ProjectID,
		Mode:         "full_repository",
		Title:        "Compat Audit",
		Status:       "created",
		ManifestPath: "data/audit.json",
	})
	if err != nil {
		t.Fatalf("CreateLocalAudit failed: %v", err)
	}
	if audit.AuditID != "compat-audit-1" {
		t.Errorf("expected audit ID 'compat-audit-1', got %s", audit.AuditID)
	}

	gotAudit, err := st.GetLocalAuditByAuditID("compat-audit-1")
	if err != nil {
		t.Fatalf("GetLocalAuditByAuditID failed: %v", err)
	}
	if gotAudit.ID != audit.ID {
		t.Errorf("expected audit row ID %d, got %d", audit.ID, gotAudit.ID)
	}
}

func assertTableColumns(t *testing.T, db *sql.DB, tableName string, requiredColumns []string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		t.Fatalf("table info query failed for %s: %v", tableName, err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan %s column info failed: %v", tableName, err)
		}
		columns[name] = true
	}

	for _, col := range requiredColumns {
		if !columns[col] {
			t.Errorf("table %s is missing required column %q", tableName, col)
		}
	}
}
