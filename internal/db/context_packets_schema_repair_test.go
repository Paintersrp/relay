package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/store/generated"
)

func TestContextPacketsSchemaRepairStaleSchemaSuccess(t *testing.T) {
	db := openSQLite(t, filepath.Join(t.TempDir(), "stale-success.db"))
	createRepairParents(t, db)
	createStaleContextPacketTables(t, db)
	markGooseAppliedThrough(t, db, 13)

	execRepairSQL(t, db, `
INSERT INTO projects (id, project_id) VALUES (7, 'relay');
INSERT INTO source_snapshots (id, source_snapshot_id) VALUES (11, 'srcsnap_1');
INSERT INTO context_packets (
	id, context_packet_id, project_id, plan_id, pass_id, task_slug,
	source_snapshot_id, status, packet_json_path, packet_markdown_path,
	coverage_report_path, source_count, covered_seed_count, blocked_seed_count,
	missing_seed_count, truncated, blockers_json, created_at
) VALUES (
	3, 'ctxpkt_1', 'relay', 'plan-1', 'PASS-004B', 'schema-repair',
	'srcsnap_1', 'created', 'packet.json', 'packet.md',
	'coverage.json', 1, 1, 0, 0, 0, '[]', '2026-06-23 00:00:00'
);
INSERT INTO context_packet_sources (
	id, context_packet_row_id, source_id, source_type, project_id, repo_id,
	source_snapshot_id, path, line_start, line_end, content_hash, snippet_hash,
	redaction_status, truncated, generated_at, reason, created_at
) VALUES (
	5, 3, 'source_1', 'file_read', 'relay', 'relay',
	'srcsnap_1', 'internal/db/db.go', 1, 2, 'content', 'snippet',
	'not_needed', 0, '2026-06-23 00:00:00', 'seed', '2026-06-23 00:00:00'
);
`)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate stale schema: %v", err)
	}

	assertContextPacketColumns(t, db)
	var projectRowID, sourceSnapshotRowID int64
	var summaryJSON, completedAt string
	err := db.QueryRow(`
SELECT project_row_id, source_snapshot_row_id, summary_json, completed_at
FROM context_packets
WHERE id = 3 AND context_packet_id = 'ctxpkt_1'
`).Scan(&projectRowID, &sourceSnapshotRowID, &summaryJSON, &completedAt)
	if err != nil {
		t.Fatalf("query repaired context packet: %v", err)
	}
	if projectRowID != 7 || sourceSnapshotRowID != 11 || summaryJSON != "{}" || completedAt != "" {
		t.Fatalf("unexpected repaired packet values: project_row_id=%d source_snapshot_row_id=%d summary=%q completed=%q", projectRowID, sourceSnapshotRowID, summaryJSON, completedAt)
	}

	var sourcePacketRowID int64
	if err := db.QueryRow("SELECT context_packet_row_id FROM context_packet_sources WHERE id = 5").Scan(&sourcePacketRowID); err != nil {
		t.Fatalf("query repaired source row: %v", err)
	}
	if sourcePacketRowID != 3 {
		t.Fatalf("expected source row to keep context_packet_row_id 3, got %d", sourcePacketRowID)
	}

	queries := generated.New(db)
	got, err := queries.GetContextPacketByID(context.Background(), "ctxpkt_1")
	if err != nil {
		t.Fatalf("GetContextPacketByID after repair: %v", err)
	}
	if got.ID != 3 || got.ProjectRowID != 7 || got.SourceSnapshotRowID != 11 {
		t.Fatalf("unexpected generated get row: %+v", got)
	}
	created, err := queries.CreateContextPacket(context.Background(), generated.CreateContextPacketParams{
		ContextPacketID:     "ctxpkt_2",
		ProjectRowID:        7,
		ProjectID:           "relay",
		PlanID:              "plan-1",
		PassID:              "PASS-004B",
		TaskSlug:            "schema-repair",
		SourceSnapshotRowID: 11,
		SourceSnapshotID:    "srcsnap_1",
		Status:              "created",
		PacketJsonPath:      "packet-2.json",
		PacketMarkdownPath:  "packet-2.md",
		CoverageReportPath:  "coverage-2.json",
		SourceCount:         0,
		CoveredSeedCount:    0,
		BlockedSeedCount:    0,
		MissingSeedCount:    0,
		Truncated:           0,
		BlockersJson:        "[]",
		SummaryJson:         "{}",
		CompletedAt:         "2026-06-23 01:00:00",
	})
	if err != nil {
		t.Fatalf("CreateContextPacket after repair: %v", err)
	}
	if created.ContextPacketID != "ctxpkt_2" {
		t.Fatalf("unexpected created packet: %+v", created)
	}
	list, err := queries.ListContextPacketsByProject(context.Background(), "relay")
	if err != nil {
		t.Fatalf("ListContextPacketsByProject after repair: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 generated list rows, got %+v", list)
	}
}

func TestContextPacketsSchemaRepairBlocksUnbackfillableRows(t *testing.T) {
	db := openSQLite(t, filepath.Join(t.TempDir(), "stale-blocked.db"))
	createRepairParents(t, db)
	createStaleContextPacketTables(t, db)
	markGooseAppliedThrough(t, db, 13)

	execRepairSQL(t, db, `
INSERT INTO projects (id, project_id) VALUES (7, 'relay');
INSERT INTO source_snapshots (id, source_snapshot_id) VALUES (11, 'srcsnap_1');
INSERT INTO context_packets (
	id, context_packet_id, project_id, plan_id, pass_id, task_slug,
	source_snapshot_id, status, packet_json_path, packet_markdown_path,
	coverage_report_path, source_count, covered_seed_count, blocked_seed_count,
	missing_seed_count, truncated, blockers_json, created_at
) VALUES (
	3, 'ctxpkt_orphan', 'missing-project', 'plan-1', 'PASS-004B', 'schema-repair',
	'srcsnap_1', 'created', 'packet.json', 'packet.md',
	'coverage.json', 1, 1, 0, 0, 0, '[]', '2026-06-23 00:00:00'
);
`)

	err := AutoMigrate(db)
	if err == nil {
		t.Fatal("expected AutoMigrate to block unbackfillable stale rows")
	}
	if !strings.Contains(err.Error(), "context packet schema repair") || !strings.Contains(err.Error(), "cannot be backfilled") {
		t.Fatalf("expected clear backfill repair error, got: %v", err)
	}
}

func TestContextPacketsSchemaRepairCurrentSchemaNoop(t *testing.T) {
	db := openSQLite(t, filepath.Join(t.TempDir(), "current-noop.db"))
	createRepairParents(t, db)
	createCurrentContextPacketTables(t, db)
	markGooseAppliedThrough(t, db, 13)

	execRepairSQL(t, db, `
INSERT INTO projects (id, project_id) VALUES (7, 'relay');
INSERT INTO source_snapshots (id, source_snapshot_id) VALUES (11, 'srcsnap_1');
INSERT INTO context_packets (
	id, context_packet_id, project_row_id, project_id, plan_id, pass_id, task_slug,
	source_snapshot_row_id, source_snapshot_id, status, packet_json_path, packet_markdown_path,
	coverage_report_path, source_count, covered_seed_count, blocked_seed_count,
	missing_seed_count, truncated, blockers_json, summary_json, created_at, completed_at
) VALUES (
	3, 'ctxpkt_current', 7, 'relay', 'plan-1', 'PASS-004B', 'schema-repair',
	11, 'srcsnap_1', 'created', 'packet.json', 'packet.md',
	'coverage.json', 1, 1, 0, 0, 0, '[]', '{"ok":true}', '2026-06-23 00:00:00', '2026-06-23 01:00:00'
);
`)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate current schema: %v", err)
	}
	assertContextPacketColumns(t, db)

	var summaryJSON, completedAt string
	if err := db.QueryRow("SELECT summary_json, completed_at FROM context_packets WHERE id = 3").Scan(&summaryJSON, &completedAt); err != nil {
		t.Fatalf("query current schema row: %v", err)
	}
	if summaryJSON != `{"ok":true}` || completedAt != "2026-06-23 01:00:00" {
		t.Fatalf("current schema no-op did not preserve values: summary=%q completed=%q", summaryJSON, completedAt)
	}
}

func createRepairParents(t *testing.T, db *sql.DB) {
	t.Helper()
	execRepairSQL(t, db, `
CREATE TABLE projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL UNIQUE
);
CREATE TABLE source_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_snapshot_id TEXT NOT NULL UNIQUE
);
`)
}

func createStaleContextPacketTables(t *testing.T, db *sql.DB) {
	t.Helper()
	execRepairSQL(t, db, `
CREATE TABLE context_packets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	context_packet_id TEXT NOT NULL UNIQUE,
	project_id TEXT NOT NULL,
	plan_id TEXT,
	pass_id TEXT,
	task_slug TEXT NOT NULL,
	source_snapshot_id TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('created', 'partial', 'blocked')),
	packet_json_path TEXT NOT NULL,
	packet_markdown_path TEXT NOT NULL,
	coverage_report_path TEXT NOT NULL,
	source_count INTEGER,
	covered_seed_count INTEGER,
	blocked_seed_count INTEGER,
	missing_seed_count INTEGER,
	truncated INTEGER,
	blockers_json TEXT,
	created_at TEXT NOT NULL
);
CREATE TABLE context_packet_sources (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	context_packet_row_id INTEGER NOT NULL,
	source_id TEXT NOT NULL,
	source_type TEXT NOT NULL,
	project_id TEXT NOT NULL,
	repo_id TEXT NOT NULL,
	source_snapshot_id TEXT NOT NULL,
	path TEXT NOT NULL,
	line_start INTEGER NOT NULL DEFAULT 0,
	line_end INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT NOT NULL DEFAULT '',
	snippet_hash TEXT NOT NULL DEFAULT '',
	redaction_status TEXT NOT NULL,
	truncated INTEGER NOT NULL DEFAULT 0,
	generated_at TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
`)
}

func createCurrentContextPacketTables(t *testing.T, db *sql.DB) {
	t.Helper()
	execRepairSQL(t, db, `
CREATE TABLE context_packets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	context_packet_id TEXT NOT NULL UNIQUE,
	project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	project_id TEXT NOT NULL,
	plan_id TEXT NOT NULL DEFAULT '',
	pass_id TEXT NOT NULL DEFAULT '',
	task_slug TEXT NOT NULL,
	source_snapshot_row_id INTEGER NOT NULL REFERENCES source_snapshots(id) ON DELETE CASCADE,
	source_snapshot_id TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('created', 'partial', 'blocked')),
	packet_json_path TEXT NOT NULL,
	packet_markdown_path TEXT NOT NULL,
	coverage_report_path TEXT NOT NULL,
	source_count INTEGER NOT NULL DEFAULT 0,
	covered_seed_count INTEGER NOT NULL DEFAULT 0,
	blocked_seed_count INTEGER NOT NULL DEFAULT 0,
	missing_seed_count INTEGER NOT NULL DEFAULT 0,
	truncated INTEGER NOT NULL DEFAULT 0,
	blockers_json TEXT NOT NULL DEFAULT '[]',
	summary_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	completed_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE context_packet_sources (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	context_packet_row_id INTEGER NOT NULL REFERENCES context_packets(id) ON DELETE CASCADE,
	source_id TEXT NOT NULL,
	source_type TEXT NOT NULL,
	project_id TEXT NOT NULL,
	repo_id TEXT NOT NULL,
	source_snapshot_id TEXT NOT NULL,
	path TEXT NOT NULL,
	line_start INTEGER NOT NULL DEFAULT 0,
	line_end INTEGER NOT NULL DEFAULT 0,
	content_hash TEXT NOT NULL DEFAULT '',
	snippet_hash TEXT NOT NULL DEFAULT '',
	redaction_status TEXT NOT NULL,
	truncated INTEGER NOT NULL DEFAULT 0,
	generated_at TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE(context_packet_row_id, source_id)
);
`)
}

func markGooseAppliedThrough(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	execRepairSQL(t, db, `
CREATE TABLE goose_db_version (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id INTEGER NOT NULL,
	is_applied INTEGER NOT NULL,
	tstamp TIMESTAMP DEFAULT (datetime('now'))
);
INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1);
`)
	for v := 1; v <= version; v++ {
		if _, err := db.Exec("INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", v); err != nil {
			t.Fatalf("mark goose version %d applied: %v", v, err)
		}
	}
}

func assertContextPacketColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(context_packets)")
	if err != nil {
		t.Fatalf("table info context_packets: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan context_packets column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read context_packets columns: %v", err)
	}
	for _, column := range contextPacketRequiredColumns {
		if !columns[column] {
			t.Fatalf("expected context_packets column %q after repair", column)
		}
	}
}

func execRepairSQL(t *testing.T, db *sql.DB, script string) {
	t.Helper()
	for _, stmt := range strings.Split(script, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec repair setup SQL %q: %v", stmt, err)
		}
	}
}
