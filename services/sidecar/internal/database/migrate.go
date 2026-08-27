package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func applyMigrations(db *sql.DB) (int, error) {
	if _, err := db.Exec(`
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
	rows, err := db.Query("SELECT version, name FROM schema_migrations ORDER BY version")
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

		tx, err := db.Begin()
		if err != nil {
			return 0, fmt.Errorf("begin migration %d: %w", item.version, err)
		}
		if _, err := tx.Exec(item.sql); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err)
		}
		if _, err := tx.Exec(
			"INSERT INTO schema_migrations(version, name) VALUES (?, ?)",
			item.version,
			item.name,
		); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("record migration %d: %w", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit migration %d: %w", item.version, err)
		}
		latest = item.version
	}
	return latest, nil
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
		items = append(items, migration{version: version, name: entry.Name(), sql: string(contents)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}
