package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sync"

	"github.com/pressly/goose/v3"
)

var migrationMu sync.Mutex

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
