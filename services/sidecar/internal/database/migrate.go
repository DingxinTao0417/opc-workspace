package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const foreignKeysOffMigrationMarker = "-- migration: foreign_keys=off"

type migration struct {
	version        int
	name           string
	sql            string
	foreignKeysOff bool
}

func applyMigrations(db *sql.DB) (int, error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("reserve SQLite migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return 0, err
	}
	applied := make(map[int]string)
	available := make(map[int]string, len(migrations))
	for _, item := range migrations {
		available[item.version] = item.name
	}
	rows, err := conn.QueryContext(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return 0, fmt.Errorf("read schema migrations: %w", err)
	}
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan schema migration: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close schema migration rows: %w", err)
	}
	for version, name := range applied {
		migrationName, exists := available[version]
		if !exists {
			return 0, fmt.Errorf("database schema version %d (%s) is newer than or unknown to this Sidecar", version, name)
		}
		if migrationName != name {
			return 0, fmt.Errorf("migration %d name mismatch: database=%q binary=%q", version, name, migrationName)
		}
	}

	latest := 0
	for _, item := range migrations {
		if existing, ok := applied[item.version]; ok {
			if existing != item.name {
				return 0, fmt.Errorf("migration %d name mismatch: database=%q binary=%q", item.version, existing, item.name)
			}
			latest = item.version
			continue
		}

		if err := applyMigration(ctx, conn, item); err != nil {
			return 0, err
		}
		latest = item.version
	}
	return latest, nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, item migration) (err error) {
	if item.foreignKeysOff {
		defer func() {
			if restoreErr := setForeignKeys(ctx, conn, true); restoreErr != nil {
				restoreErr = fmt.Errorf("restore foreign keys after migration %d: %w", item.version, restoreErr)
				if err == nil {
					err = restoreErr
				} else {
					err = errors.Join(err, restoreErr)
				}
			}
		}()
		if err := setForeignKeys(ctx, conn, false); err != nil {
			return fmt.Errorf("disable foreign keys for migration %d: %w", item.version, err)
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", item.version, err)
	}
	rollback := func(migrationErr error) error {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(migrationErr, fmt.Errorf("rollback migration %d: %w", item.version, rollbackErr))
		}
		return migrationErr
	}

	if _, err := tx.ExecContext(ctx, item.sql); err != nil {
		return rollback(fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err))
	}
	if item.foreignKeysOff {
		if err := checkForeignKeys(ctx, tx); err != nil {
			return rollback(fmt.Errorf("validate migration %d foreign keys: %w", item.version, err))
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO schema_migrations(version, name) VALUES (?, ?)",
		item.version,
		item.name,
	); err != nil {
		return rollback(fmt.Errorf("record migration %d: %w", item.version, err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", item.version, err)
	}
	return nil
}

func setForeignKeys(ctx context.Context, conn *sql.Conn, enabled bool) error {
	value := 0
	pragma := "PRAGMA foreign_keys = OFF"
	if enabled {
		value = 1
		pragma = "PRAGMA foreign_keys = ON"
	}
	if _, err := conn.ExecContext(ctx, pragma); err != nil {
		return err
	}
	var got int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&got); err != nil {
		return err
	}
	if got != value {
		return fmt.Errorf("PRAGMA foreign_keys = %d, want %d", got, value)
	}
	return nil
}

func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var constraintID int
		if err := rows.Scan(&table, &rowID, &parent, &constraintID); err != nil {
			return err
		}
		return fmt.Errorf(
			"foreign key violation: table=%s rowid=%v parent=%s constraint=%d",
			table,
			rowID,
			parent,
			constraintID,
		)
	}
	return rows.Err()
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	items := make([]migration, 0, len(entries))
	seen := make(map[int]struct{})
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if _, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		seen[version] = struct{}{}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		migrationSQL := string(contents)
		firstLine, _, _ := strings.Cut(migrationSQL, "\n")
		items = append(items, migration{
			version:        version,
			name:           entry.Name(),
			sql:            migrationSQL,
			foreignKeysOff: strings.TrimSpace(firstLine) == foreignKeysOffMigrationMarker,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}
