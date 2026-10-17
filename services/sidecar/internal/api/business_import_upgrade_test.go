package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"gorm.io/gorm"
)

const (
	releasedBusinessImportSchema = 51
	upgradeFixturePersonID       = "018f0000-0000-7000-8000-00000000d051"
)

func portableTablesFrom(t *testing.T, db *gorm.DB) []businessExportTable {
	t.Helper()
	tables := make([]businessExportTable, 0, len(businessExportTables))
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, spec := range businessExportTables {
			table, err := readBusinessExportTable(tx, spec)
			if err != nil {
				return err
			}
			tables = append(tables, table)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read portable tables: %v", err)
	}
	return tables
}

// historicalWorkspaceExport builds a workspace with the real migrator stopped
// at the given schema, adds one person, and returns the package that schema's
// export would have produced together with the database path.
func historicalWorkspaceExport(t *testing.T, schema int) (businessExportPackage, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "historical.db")
	store, err := database.OpenAtSchemaVersion(path, schema)
	if err != nil {
		t.Fatalf("open schema %d workspace: %v", schema, err)
	}
	defer store.Close()
	if err := store.DB.Exec(`
		INSERT INTO actors (id, type, display_name, status, is_builtin, notes, metadata_json, version, created_at, updated_at)
		VALUES (?, 'person', '旧版同事', 'active', 0, '', '{}', 1, '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z')
	`, upgradeFixturePersonID).Error; err != nil {
		t.Fatalf("insert schema %d person: %v", schema, err)
	}
	// The Sidecar seeds the preset rules on every start, so a workspace that
	// was ever opened by the application has them; the released version had
	// the same five presets as today.
	for _, preset := range automationPresets {
		configJSON, err := encodeAutomationConfig(preset.DefaultConfig)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.DB.Exec(`
			INSERT INTO automation_rules(id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at)
			VALUES (?, ?, 0, ?, NULL, 1, '2026-01-02T03:04:05Z', '2026-01-02T03:04:05Z')
		`, preset.ID, preset.PresetKey, configJSON).Error; err != nil {
			t.Fatalf("seed schema %d automation preset %s: %v", schema, preset.PresetKey, err)
		}
	}
	excluded, ok := businessImportSchemaContract(schema, businessImportSchema80)
	if !ok {
		t.Fatalf("schema %d has no import contract", schema)
	}
	return businessExportPackage{
		FormatVersion: businessExportFormatVersion,
		ExportedAt:    "2026-01-02T03:04:05.000000000Z",
		Source: businessExportSource{
			AppVersion: "historical", Commit: "historical", APIVersion: Version, SchemaVersion: schema,
		},
		ArtifactFiles:             businessExportArtifactFiles{Included: false},
		ExcludedOperationalTables: append([]string(nil), excluded...),
		Tables:                    portableTablesFrom(t, store.DB),
	}, path
}

func TestBusinessImportPortableSurfaceChangesAreRegistered(t *testing.T) {
	latest, err := database.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	surface := func(version int) map[string][]string {
		store, err := database.OpenAtSchemaVersion(filepath.Join(t.TempDir(), "surface.db"), version)
		if err != nil {
			t.Fatalf("open schema %d: %v", version, err)
		}
		defer store.Close()
		result := make(map[string][]string, len(businessExportTables))
		for _, spec := range businessExportTables {
			columns, err := tableColumns(store.DB, spec.Name)
			if err != nil {
				t.Fatalf("schema %d has no portable table %s: %v", version, spec.Name, err)
			}
			result[spec.Name] = columns
		}
		return result
	}

	observed := make([]businessImportUpgradeStep, 0)
	previous := surface(businessImportSchema49)
	for version := businessImportSchema49 + 1; version <= latest; version++ {
		current := surface(version)
		for _, spec := range businessExportTables {
			before, after := previous[spec.Name], current[spec.Name]
			if equalStrings(before, after) {
				continue
			}
			// The only shape a step can express today is a column appended at
			// the end. Anything else needs a new kind of step first.
			if len(after) != len(before)+1 || !equalStrings(after[:len(before)], before) {
				t.Fatalf("schema %d changed %s in a way no upgrade step can express:\n before %v\n after  %v", version, spec.Name, before, after)
			}
			observed = append(observed, businessImportUpgradeStep{
				SchemaVersion: version, Table: spec.Name, Change: "add_column", Column: after[len(after)-1],
			})
		}
		previous = current
	}

	registered := make([]businessImportUpgradeStep, 0, len(businessImportSurfaceChanges))
	for _, change := range businessImportSurfaceChanges {
		registered = append(registered, change.step)
	}
	if !reflect.DeepEqual(observed, registered) {
		t.Fatalf("portable surface changes since schema 49:\n observed   %+v\n registered %+v", observed, registered)
	}
}

func TestBusinessImportUpgradeMatchesMigratedExport(t *testing.T) {
	for _, schema := range []int{businessImportSchema49, releasedBusinessImportSchema, businessImportSchema63, businessImportSchema70} {
		t.Run(fmt.Sprintf("schema %d", schema), func(t *testing.T) {
			historical, path := historicalWorkspaceExport(t, schema)
			migrated, err := database.Open(path)
			if err != nil {
				t.Fatalf("migrate schema %d workspace: %v", schema, err)
			}
			defer migrated.Close()
			expected := portableTablesFrom(t, migrated.DB)

			upgraded, steps, err := upgradeBusinessImportPackage(historical, migrated.SchemaVersion)
			if err != nil {
				t.Fatalf("upgrade schema %d package: %v", schema, err)
			}
			if len(steps) != 1 || steps[0].SchemaVersion != businessImportSchema71 || steps[0].Table != "actors" {
				t.Fatalf("schema %d upgrade steps = %+v", schema, steps)
			}
			// The input stays as it was exported.
			if columnIndex(automationImportTable(t, &historical, "actors").Columns, "agent_adapter_id") >= 0 {
				t.Fatal("upgrade modified its input package")
			}
			for index, table := range upgraded.Tables {
				want := expected[index]
				if table.Name != want.Name || !equalStrings(table.Columns, want.Columns) {
					t.Fatalf("schema %d %s columns = %v, migrated workspace exports %v", schema, table.Name, table.Columns, want.Columns)
				}
				keys, err := tablePrimaryKeyColumns(migrated.DB, table.Name)
				if err != nil || len(keys) == 0 {
					t.Fatalf("primary key of %s: %v", table.Name, err)
				}
				keyOf := func(columns []string, row []any) string {
					values := make([]any, len(keys))
					for position, key := range keys {
						values[position] = row[columnIndex(columns, key)]
					}
					key, err := canonicalBusinessImportKey(values)
					if err != nil {
						t.Fatal(err)
					}
					return key
				}
				migratedRows := make(map[string][]any, len(want.Rows))
				for _, row := range want.Rows {
					migratedRows[keyOf(want.Columns, row)] = row
				}
				// Later migrations may add rows of their own; every row the
				// old schema exported must come out exactly as migration
				// leaves it.
				for _, row := range table.Rows {
					migratedRow, ok := migratedRows[keyOf(table.Columns, row)]
					if !ok {
						t.Fatalf("schema %d %s row %v is missing after migration", schema, table.Name, row)
					}
					left, _ := json.Marshal(normalizeImportValues(append([]any(nil), row...)))
					right, _ := json.Marshal(normalizeImportValues(append([]any(nil), migratedRow...)))
					if string(left) != string(right) {
						t.Fatalf("schema %d %s row differs:\n upgraded %s\n migrated %s", schema, table.Name, left, right)
					}
				}
			}
		})
	}
}

func TestBusinessImportSchemaContractAcceptsOnlyKnownHistoricalSources(t *testing.T) {
	excluded, ok := businessImportSchemaContract(releasedBusinessImportSchema, businessImportSchema80)
	if !ok || !equalStrings(excluded, businessExportExcludedTablesSchema51) {
		t.Fatalf("released schema 51 contract = %v %v", excluded, ok)
	}
	if len(businessExportExcludedTablesSchema51) != len(businessExportExcludedTablesSchema49)+1 ||
		businessExportExcludedTablesSchema51[len(businessExportExcludedTablesSchema51)-1] != "business_import_project_completion_authorizations" {
		t.Fatalf("schema 51 exclusion manifest = %v", businessExportExcludedTablesSchema51)
	}
	for _, schema := range []int{48, 50, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 81} {
		if _, ok := businessImportSchemaContract(schema, businessImportSchema80); ok {
			t.Fatalf("schema %d has no frozen manifest and must stay unsupported", schema)
		}
	}
}

func TestBusinessImportAppliesReleasedSchema51Export(t *testing.T) {
	historical, _ := historicalWorkspaceExport(t, releasedBusinessImportSchema)
	body := encodeBusinessImportJSON(t, historical)
	router, store, _, backupDir := newBackupTestAPI(t)

	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("schema 51 preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	wantSteps := []businessImportUpgradeStep{{SchemaVersion: businessImportSchema71, Table: "actors", Change: "add_column", Column: "agent_adapter_id"}}
	if !envelope.Data.CanApply || envelope.Data.ApplyMode != importModeReplaceEmpty ||
		envelope.Data.SchemaVersion != releasedBusinessImportSchema || envelope.Data.TargetSchemaVersion != store.SchemaVersion ||
		!reflect.DeepEqual(envelope.Data.UpgradeSteps, wantSteps) {
		t.Fatalf("schema 51 preview = %+v", envelope.Data)
	}

	refused := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if refused.Code != http.StatusPreconditionRequired || responseErrorCode(t, refused.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("schema 51 apply with the same-schema phrase = %d: %s", refused.Code, refused.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("refused confirmation created backups: %v", backups)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id = ?", 0, upgradeFixturePersonID)

	apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importUpgradeReplaceConfirmation})
	if apply.Code != http.StatusOK {
		t.Fatalf("schema 51 apply = %d: %s", apply.Code, apply.Body.String())
	}
	assertDatabaseCount(t, store,
		"SELECT COUNT(*) FROM actors WHERE id = ? AND type = 'person' AND display_name = '旧版同事' AND agent_adapter_id IS NULL",
		1, upgradeFixturePersonID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("schema 51 apply backups = %v, want exactly one", backups)
	}
}

func TestBusinessImportRejectsPackagesThatDoNotMatchTheirDeclaredSchema(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
		code   string
	}{
		{
			name: "column from a later schema",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table := automationImportTable(t, packageData, "actors")
				table.Columns = append(table.Columns, "agent_adapter_id")
				for row := range table.Rows {
					table.Rows[row] = append(table.Rows[row], nil)
				}
			},
			code: "IMPORT_SCHEMA_MISMATCH",
		},
		{
			name: "actor type that did not exist yet",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table := automationImportTable(t, packageData, "actors")
				typeIndex := columnIndex(table.Columns, "type")
				for row := range table.Rows {
					if table.Rows[row][columnIndex(table.Columns, "id")] == upgradeFixturePersonID {
						table.Rows[row][typeIndex] = "agent"
					}
				}
			},
			code: "IMPORT_ROW_INVALID",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			historical, _ := historicalWorkspaceExport(t, releasedBusinessImportSchema)
			test.mutate(t, &historical)
			body := encodeBusinessImportJSON(t, historical)
			router, store, _, backupDir := newBackupTestAPI(t)
			for _, request := range []struct {
				path    string
				headers map[string]string
			}{
				{path: "/api/v1/imports/business-data/preview"},
				{path: "/api/v1/imports/business-data", headers: map[string]string{"X-Import-Confirmation": importUpgradeReplaceConfirmation}},
			} {
				response := performRequest(router, http.MethodPost, request.path, body, request.headers)
				if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != test.code {
					t.Fatalf("%s = %d: %s, want %s", request.path, response.Code, response.Body.String(), test.code)
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id = ?", 0, upgradeFixturePersonID)
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
				t.Fatalf("rejected package created backups: %v", backups)
			}
		})
	}
}
