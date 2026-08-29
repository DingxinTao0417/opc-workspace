package database

import (
	"path/filepath"
	"reflect"
	"testing"

	glebarezsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrationMarkersRecognizeConsecutiveHeaderDirectives(t *testing.T) {
	markers := migrationMarkers("-- migration: foreign_keys=off\n-- migration: destructive\nCREATE TABLE example(id INTEGER);\n")
	if !markers[foreignKeysOffMigrationMarker] || !markers[destructiveMigrationMarker] {
		t.Fatalf("migration markers = %#v", markers)
	}

	markers = migrationMarkers("-- ordinary comment\n-- migration: destructive\nSELECT 1;")
	if markers[destructiveMigrationMarker] {
		t.Fatal("a directive below an ordinary comment must not be treated as a migration header")
	}
}

func TestApplyMigrationSetStopsBeforeDestructiveSQL(t *testing.T) {
	gormDB, err := gorm.Open(glebarezsqlite.Open(filepath.Join(t.TempDir(), "gate.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	db, err := gormDB.DB()
	if err != nil {
		t.Fatalf("read SQLite handle: %v", err)
	}
	defer db.Close()
	initial := migration{version: 1, name: "001_initial.sql", sql: "CREATE TABLE facts(id INTEGER PRIMARY KEY);"}
	destructive := migration{version: 2, name: "002_rebuild.sql", sql: "ALTER TABLE facts ADD COLUMN value TEXT;", destructive: true}
	if version, gate, err := applyMigrationSet(db, []migration{initial}, false); err != nil || version != 1 || gate != nil {
		t.Fatalf("initial migration version=%d gate=%#v err=%v", version, gate, err)
	}
	version, gate, err := applyMigrationSet(db, []migration{initial, destructive}, true)
	if err != nil || version != 1 || gate == nil || gate.CurrentVersion != 1 || gate.TargetVersion != 2 {
		t.Fatalf("gated migration version=%d gate=%#v err=%v", version, gate, err)
	}
	var addedColumns int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('facts') WHERE name = 'value'").Scan(&addedColumns); err != nil {
		t.Fatalf("inspect gated table: %v", err)
	}
	if addedColumns != 0 {
		t.Fatal("destructive SQL ran before the migration gate was released")
	}
	if version, gate, err = applyMigrationSet(db, []migration{initial, destructive}, false); err != nil || version != 2 || gate != nil {
		t.Fatalf("released migration version=%d gate=%#v err=%v", version, gate, err)
	}
}

func TestOpenBeforeDestructiveMigrationsReturnsCurrentStoreWithoutGate(t *testing.T) {
	store, gate, err := OpenBeforeDestructiveMigrations(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("OpenBeforeDestructiveMigrations() error = %v", err)
	}
	defer store.Close()
	if gate != nil || store.SchemaVersion != 26 {
		t.Fatalf("store schema=%d gate=%#v", store.SchemaVersion, gate)
	}
}

func TestPendingMigrationGateStopsExistingWorkspaceBeforeFirstDestructiveChange(t *testing.T) {
	migrations := []migration{
		{version: 25, name: "025_safe.sql"},
		{version: 26, name: "026_safe.sql"},
		{version: 27, name: "027_destructive.sql", destructive: true},
		{version: 28, name: "028_after.sql"},
	}
	applied := map[int]string{25: "025_safe.sql", 26: "026_safe.sql"}
	gate := pendingMigrationGate(migrations, applied, 2, 26, true, true)
	if gate == nil {
		t.Fatal("expected destructive migration gate")
	}
	if gate.CurrentVersion != 26 || gate.TargetVersion != 28 || !reflect.DeepEqual(gate.PendingVersions, []int{27, 28}) {
		t.Fatalf("migration gate = %#v", gate)
	}
}

func TestPendingMigrationGateDoesNotBackupBrandNewWorkspace(t *testing.T) {
	migrations := []migration{{version: 1, name: "001_initial.sql", destructive: true}}
	if gate := pendingMigrationGate(migrations, nil, 0, 0, true, false); gate != nil {
		t.Fatalf("new workspace gate = %#v, want nil", gate)
	}
}
