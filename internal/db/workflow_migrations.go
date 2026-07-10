package db

import (
	"database/sql"
	"embed"
)

//go:embed workflow_migrations/*.sql
var WorkflowMigrationsFS embed.FS

func AutoMigrateWorkflow(sqlDB *sql.DB) error {
	return migrate(sqlDB, WorkflowMigrationsFS, "workflow_migrations")
}
