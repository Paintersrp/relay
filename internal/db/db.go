package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

var migrationMu sync.Mutex

func AutoMigrate(sqlDB *sql.DB) error {
	return migrate(sqlDB, MigrationsFS, "migrations")
}

func migrate(sqlDB *sql.DB, migrationFS fs.FS, directory string) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}
	if err := goose.Up(sqlDB, directory); err != nil {
		return fmt.Errorf("goose up %s: %w", directory, err)
	}
	return nil
}
