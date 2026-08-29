package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const busyTimeoutMilliseconds = 5000

type Store struct {
	DB            *gorm.DB
	SQL           *sql.DB
	SchemaVersion int
}

func Open(path string) (*Store, error) {
	store, _, err := open(path, false)
	return store, err
}

// OpenBeforeDestructiveMigrations opens the database and applies every pending
// non-destructive migration. For an existing workspace it stops before the
// first migration explicitly marked as destructive, allowing startup to create
// and verify a rollback package before any destructive SQL runs.
func OpenBeforeDestructiveMigrations(path string) (*Store, *MigrationGate, error) {
	return open(path, true)
}

func open(path string, stopBeforeDestructive bool) (*Store, *MigrationGate, error) {
	if path != ":memory:" {
		parent := filepath.Dir(path)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open SQLite database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("get SQLite connection: %w", err)
	}

	// A single desktop user does not need a wide write pool. Keeping one physical
	// connection also guarantees that connection-scoped PRAGMAs apply everywhere.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMilliseconds),
	} {
		if err := db.Exec(pragma).Error; err != nil {
			_ = sqlDB.Close()
			return nil, nil, fmt.Errorf("configure SQLite (%s): %w", pragma, err)
		}
	}

	version, gate, err := applyMigrations(sqlDB, stopBeforeDestructive)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping SQLite database: %w", err)
	}

	return &Store{DB: db, SQL: sqlDB, SchemaVersion: version}, gate, nil
}

func (s *Store) Checkpoint() error {
	if s == nil || s.SQL == nil {
		return nil
	}
	if _, err := s.SQL.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.SQL == nil {
		return nil
	}
	return s.SQL.Close()
}
