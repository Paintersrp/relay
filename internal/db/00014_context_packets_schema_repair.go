package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationNoTxContext(
		"00014_context_packets_schema_repair.go",
		upContextPacketsSchemaRepair,
		downContextPacketsSchemaRepair,
	)
}

func upContextPacketsSchemaRepair(ctx context.Context, db *sql.DB) error {
	exists, err := schemaRepairTableExists(ctx, db, "context_packets")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	columns, err := schemaRepairColumns(ctx, db, "context_packets")
	if err != nil {
		return err
	}
	if schemaRepairHasColumns(columns, contextPacketRequiredColumns...) {
		return schemaRepairEnsureIndexes(ctx, db)
	}

	if err := schemaRepairRequireColumns(columns, contextPacketLegacyColumns...); err != nil {
		return err
	}
	if err := schemaRepairPreflightBackfill(ctx, db); err != nil {
		return err
	}

	sourcesExist, err := schemaRepairTableExists(ctx, db, "context_packet_sources")
	if err != nil {
		return err
	}
	sourceColumns := map[string]bool{}
	if sourcesExist {
		sourceColumns, err = schemaRepairColumns(ctx, db, "context_packet_sources")
		if err != nil {
			return err
		}
		if err := schemaRepairPreflightSources(ctx, db); err != nil {
			return err
		}
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("context packet schema repair: get connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("context packet schema repair: disable foreign keys: %w", err)
	}
	foreignKeysOff := true
	defer func() {
		if foreignKeysOff {
			_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("context packet schema repair: begin transaction: %w", err)
	}
	if err := schemaRepairRebuildTables(ctx, tx, columns, sourceColumns, sourcesExist); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("context packet schema repair: commit rebuild: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("context packet schema repair: enable foreign keys: %w", err)
	}
	foreignKeysOff = false

	if err := schemaRepairForeignKeyCheck(ctx, conn); err != nil {
		return err
	}
	return nil
}

func downContextPacketsSchemaRepair(ctx context.Context, db *sql.DB) error {
	return fmt.Errorf("context packet schema repair is forward-only and cannot be downgraded")
}

var contextPacketRequiredColumns = []string{
	"id",
	"context_packet_id",
	"project_row_id",
	"project_id",
	"plan_id",
	"pass_id",
	"task_slug",
	"source_snapshot_row_id",
	"source_snapshot_id",
	"status",
	"packet_json_path",
	"packet_markdown_path",
	"coverage_report_path",
	"source_count",
	"covered_seed_count",
	"blocked_seed_count",
	"missing_seed_count",
	"truncated",
	"blockers_json",
	"summary_json",
	"created_at",
	"completed_at",
}

var contextPacketLegacyColumns = []string{
	"id",
	"context_packet_id",
	"project_id",
	"plan_id",
	"pass_id",
	"task_slug",
	"source_snapshot_id",
	"status",
	"packet_json_path",
	"packet_markdown_path",
	"coverage_report_path",
	"source_count",
	"covered_seed_count",
	"blocked_seed_count",
	"missing_seed_count",
	"truncated",
	"blockers_json",
	"created_at",
}

func schemaRepairTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
		return false, fmt.Errorf("context packet schema repair: check table %s: %w", table, err)
	}
	return count > 0, nil
}

func schemaRepairColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, fmt.Errorf("context packet schema repair: inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("context packet schema repair: scan %s column: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("context packet schema repair: read %s columns: %w", table, err)
	}
	return columns, nil
}

func schemaRepairHasColumns(columns map[string]bool, required ...string) bool {
	for _, column := range required {
		if !columns[column] {
			return false
		}
	}
	return true
}

func schemaRepairRequireColumns(columns map[string]bool, required ...string) error {
	var missing []string
	for _, column := range required {
		if !columns[column] {
			missing = append(missing, column)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("context packet schema repair: stale context_packets table is missing required legacy columns: %s", strings.Join(missing, ", "))
	}
	return nil
}

func schemaRepairPreflightBackfill(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM context_packets cp
LEFT JOIN projects p ON p.project_id = cp.project_id
LEFT JOIN source_snapshots ss ON ss.source_snapshot_id = cp.source_snapshot_id
WHERE p.id IS NULL OR ss.id IS NULL
`).Scan(&count)
	if err != nil {
		return fmt.Errorf("context packet schema repair: preflight project/source snapshot backfill: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("context packet schema repair: %d context_packets rows cannot be backfilled to project/source snapshot row IDs", count)
	}
	return nil
}

func schemaRepairPreflightSources(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM context_packet_sources cps
LEFT JOIN context_packets cp ON cp.id = cps.context_packet_row_id
WHERE cp.id IS NULL
`).Scan(&count)
	if err != nil {
		return fmt.Errorf("context packet schema repair: preflight context packet source references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("context packet schema repair: %d context_packet_sources rows reference missing context_packets rows", count)
	}
	return nil
}

func schemaRepairRebuildTables(ctx context.Context, tx *sql.Tx, packetColumns, sourceColumns map[string]bool, sourcesExist bool) error {
	statements := []string{
		"DROP TABLE IF EXISTS context_packets_repair_new",
		"DROP TABLE IF EXISTS context_packet_sources_repair_new",
		createContextPacketsRepairTableSQL,
		createContextPacketSourcesRepairTableSQL,
		schemaRepairCopyContextPacketsSQL(packetColumns),
	}
	if sourcesExist {
		statements = append(statements, schemaRepairCopyContextPacketSourcesSQL(sourceColumns))
	}
	statements = append(statements,
		"DROP TABLE IF EXISTS context_packet_sources",
		"DROP TABLE IF EXISTS context_packets",
		"ALTER TABLE context_packets_repair_new RENAME TO context_packets",
		"ALTER TABLE context_packet_sources_repair_new RENAME TO context_packet_sources",
	)
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("context packet schema repair: execute %q: %w", firstLine(stmt), err)
		}
	}
	if err := schemaRepairEnsureIndexes(ctx, tx); err != nil {
		return err
	}
	return nil
}

const createContextPacketsRepairTableSQL = `
CREATE TABLE context_packets_repair_new (
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
)`

const createContextPacketSourcesRepairTableSQL = `
CREATE TABLE context_packet_sources_repair_new (
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
)`

func schemaRepairCopyContextPacketsSQL(columns map[string]bool) string {
	return fmt.Sprintf(`
INSERT INTO context_packets_repair_new (
    id, context_packet_id, project_row_id, project_id, plan_id, pass_id, task_slug,
    source_snapshot_row_id, source_snapshot_id, status, packet_json_path, packet_markdown_path,
    coverage_report_path, source_count, covered_seed_count, blocked_seed_count,
    missing_seed_count, truncated, blockers_json, summary_json, created_at, completed_at
)
SELECT
    cp.id,
    cp.context_packet_id,
    p.id,
    cp.project_id,
    COALESCE(cp.plan_id, ''),
    COALESCE(cp.pass_id, ''),
    cp.task_slug,
    ss.id,
    cp.source_snapshot_id,
    cp.status,
    cp.packet_json_path,
    cp.packet_markdown_path,
    cp.coverage_report_path,
    COALESCE(cp.source_count, 0),
    COALESCE(cp.covered_seed_count, 0),
    COALESCE(cp.blocked_seed_count, 0),
    COALESCE(cp.missing_seed_count, 0),
    COALESCE(cp.truncated, 0),
    COALESCE(cp.blockers_json, '[]'),
    %s,
    cp.created_at,
    %s
FROM context_packets cp
JOIN projects p ON p.project_id = cp.project_id
JOIN source_snapshots ss ON ss.source_snapshot_id = cp.source_snapshot_id`,
		schemaRepairColumnOrDefault(columns, "summary_json", "'{}'"),
		schemaRepairColumnOrDefault(columns, "completed_at", "''"),
	)
}

func schemaRepairCopyContextPacketSourcesSQL(columns map[string]bool) string {
	return fmt.Sprintf(`
INSERT INTO context_packet_sources_repair_new (
    id, context_packet_row_id, source_id, source_type, project_id, repo_id,
    source_snapshot_id, path, line_start, line_end, content_hash, snippet_hash,
    redaction_status, truncated, generated_at, reason, created_at
)
SELECT
    cps.id,
    cps.context_packet_row_id,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s,
    %s
FROM context_packet_sources cps`,
		schemaRepairSourceColumnOrDefault(columns, "source_id", "''"),
		schemaRepairSourceColumnOrDefault(columns, "source_type", "''"),
		schemaRepairSourceColumnOrDefault(columns, "project_id", "''"),
		schemaRepairSourceColumnOrDefault(columns, "repo_id", "''"),
		schemaRepairSourceColumnOrDefault(columns, "source_snapshot_id", "''"),
		schemaRepairSourceColumnOrDefault(columns, "path", "''"),
		schemaRepairSourceColumnOrDefault(columns, "line_start", "0"),
		schemaRepairSourceColumnOrDefault(columns, "line_end", "0"),
		schemaRepairSourceColumnOrDefault(columns, "content_hash", "''"),
		schemaRepairSourceColumnOrDefault(columns, "snippet_hash", "''"),
		schemaRepairSourceColumnOrDefault(columns, "redaction_status", "''"),
		schemaRepairSourceColumnOrDefault(columns, "truncated", "0"),
		schemaRepairSourceColumnOrDefault(columns, "generated_at", "datetime('now')"),
		schemaRepairSourceColumnOrDefault(columns, "reason", "''"),
		schemaRepairSourceColumnOrDefault(columns, "created_at", "datetime('now')"),
	)
}

func schemaRepairColumnOrDefault(columns map[string]bool, column, defaultValue string) string {
	if columns[column] {
		return "COALESCE(cp." + column + ", " + defaultValue + ")"
	}
	return defaultValue
}

func schemaRepairSourceColumnOrDefault(columns map[string]bool, column, defaultValue string) string {
	if columns[column] {
		return "COALESCE(cps." + column + ", " + defaultValue + ")"
	}
	return defaultValue
}

type schemaRepairExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func schemaRepairEnsureIndexes(ctx context.Context, execer schemaRepairExecer) error {
	statements := []string{
		"CREATE INDEX IF NOT EXISTS idx_context_packets_context_packet_id ON context_packets(context_packet_id)",
		"CREATE INDEX IF NOT EXISTS idx_context_packets_project_id ON context_packets(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_context_packets_source_snapshot_id ON context_packets(source_snapshot_id)",
		"CREATE INDEX IF NOT EXISTS idx_context_packet_sources_packet_row_id ON context_packet_sources(context_packet_row_id)",
		"CREATE INDEX IF NOT EXISTS idx_context_packet_sources_path ON context_packet_sources(path)",
	}
	for _, stmt := range statements {
		if _, err := execer.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("context packet schema repair: ensure index: %w", err)
		}
	}
	return nil
}

func schemaRepairForeignKeyCheck(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("context packet schema repair: foreign key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("context packet schema repair: foreign key check failed after rebuild")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("context packet schema repair: foreign key check rows: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
