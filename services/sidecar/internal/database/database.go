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

// OpenAtSchemaVersion opens a database migrated only up to the given schema
// version. The Sidecar itself always migrates to the latest schema; this
// exists for compatibility tests that need a workspace exactly as an older
// release left it, for example to produce a genuine historical export.
func OpenAtSchemaVersion(path string, version int) (*Store, error) {
	store, _, err := openWith(path, func(db *sql.DB) (int, *MigrationGate, error) {
		migrations, err := loadMigrations()
		if err != nil {
			return 0, nil, err
		}
		limited := make([]migration, 0, len(migrations))
		for _, item := range migrations {
			if item.version <= version {
				limited = append(limited, item)
			}
		}
		if len(limited) == 0 || limited[len(limited)-1].version != version {
			return 0, nil, fmt.Errorf("no embedded migration reaches schema version %d", version)
		}
		return applyMigrationSet(db, limited, false)
	})
	return store, err
}

func open(path string, stopBeforeDestructive bool) (*Store, *MigrationGate, error) {
	return openWith(path, func(db *sql.DB) (int, *MigrationGate, error) {
		return applyMigrations(db, stopBeforeDestructive)
	})
}

func openWith(path string, migrate func(*sql.DB) (int, *MigrationGate, error)) (*Store, *MigrationGate, error) {
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

	version, gate, err := migrate(sqlDB)
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
