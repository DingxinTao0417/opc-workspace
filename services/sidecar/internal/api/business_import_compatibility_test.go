package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

const frozenBusinessImportSchemaV49 = 49

var frozenBusinessExportV49ExcludedTables = []string{
	"schema_migrations",
	"workspace_identity",
	"idempotency_keys",
	"artifact_deletion_tombstones",
	"client_attachment_deletion_tombstones",
	"project_attachment_deletion_tombstones",
	"workspace_avatar_deletion_tombstones",
	"task_focus_totals",
	"storage_capacity_samples",
	"scheduled_backup_policy",
	"invoice_number_sequences",
	"automation_event_deliveries",
}

func TestBusinessImportSchemaContractAllowsSchema50Into51(t *testing.T) {
	excluded, ok := businessImportSchemaContract(businessImportSchema50, businessImportSchema51)
	if !ok || !equalStrings(excluded, businessExportExcludedTables) {
		t.Fatalf("schema 50 to 51 contract = %#v, ok=%v", excluded, ok)
	}
}

func TestBusinessImportAcceptsFrozenV49DeletedProjectCompletionHistory(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	jsonPackage := frozenBusinessExportV49(t, fixture.jsonBody)
	zipPackage := frozenBusinessPackageV49(t, fixture.zipBody)
	jsonBody := encodeBusinessImportJSON(t, jsonPackage)
	zipBody := encodeBusinessImportPackage(t, fixture.zipBody, zipPackage)

	for _, test := range []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
		applyCode    string
	}{
		{
			name: "JSON", body: jsonBody,
			previewPath: "/api/v1/imports/business-data/preview", applyPath: "/api/v1/imports/business-data",
			confirmation: importAppendConfirmation, applyCode: "IMPORT_APPLY_FAILED",
		},
		{
			name: "ZIP", body: zipBody,
			previewPath: "/api/v1/imports/business-package/preview", applyPath: "/api/v1/imports/business-package",
			confirmation: packageImportAppendConfirmation, applyCode: "IMPORT_PACKAGE_APPLY_FAILED",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertHistoricalProjectAutomationImport(
				t, fixture, test.body, test.previewPath, test.applyPath, test.confirmation, test.applyCode,
			)
		})
	}
}

func TestBusinessImportAcceptsFrozenV49IntoEmptyWorkspace(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	jsonBody := encodeBusinessImportJSON(t, frozenBusinessExportV49(t, fixture.jsonBody))
	zipBody := encodeBusinessImportPackage(t, fixture.zipBody, frozenBusinessPackageV49(t, fixture.zipBody))

	for _, test := range []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
	}{
		{
			name: "JSON", body: jsonBody,
			previewPath: "/api/v1/imports/business-data/preview", applyPath: "/api/v1/imports/business-data",
			confirmation: importReplaceConfirmation,
		},
		{
			name: "ZIP", body: zipBody,
			previewPath: "/api/v1/imports/business-package/preview", applyPath: "/api/v1/imports/business-package",
			confirmation: packageImportReplaceConfirmation,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, store, _, backupDir := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, test.previewPath, test.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("empty v49 preview = %d: %s", preview.Code, preview.Body.String())
			}
			var envelope struct {
				Data businessImportPreview `json:"data"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil ||
				!envelope.Data.CanApply || envelope.Data.ApplyMode != importModeReplaceEmpty ||
				envelope.Data.SchemaVersion != frozenBusinessImportSchemaV49 || envelope.Data.TargetSchemaVersion != store.SchemaVersion {
				t.Fatalf("empty v49 preview = %#v err=%v", envelope.Data, err)
			}
			apply := performRequest(
				router, http.MethodPost, test.applyPath, test.body,
				map[string]string{"X-Import-Confirmation": test.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("empty v49 apply = %d: %s", apply.Code, apply.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id = ?", 0, fixture.projectID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND status = 'succeeded'", 1, fixture.runID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL", 1, fixture.completionSourceInboxID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM business_import_project_completion_authorizations", 0)
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
				t.Fatalf("empty v49 apply backups = %v, want exactly one", backups)
			}
		})
	}
}

func TestBusinessImportKeepsSchemasOutsideV49CompatibilityBlocked(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	jsonBase := frozenBusinessExportV49(t, fixture.jsonBody)
	zipBase := frozenBusinessPackageV49(t, fixture.zipBody)

	for _, schema := range []struct {
		version int
		blocker string
	}{
		{version: 48, blocker: "source_schema_older"},
		{version: 57, blocker: "source_schema_newer"},
	} {
		for _, format := range []struct {
			name         string
			base         businessExportPackage
			previewPath  string
			applyPath    string
			confirmation string
			encode       func(*testing.T, businessExportPackage) []byte
		}{
			{
				name: "JSON", base: jsonBase, previewPath: "/api/v1/imports/business-data/preview", applyPath: "/api/v1/imports/business-data",
				confirmation: importAppendConfirmation,
				encode: func(t *testing.T, packageData businessExportPackage) []byte {
					return encodeBusinessImportJSON(t, packageData)
				},
			},
			{
				name: "ZIP", base: zipBase, previewPath: "/api/v1/imports/business-package/preview", applyPath: "/api/v1/imports/business-package",
				confirmation: packageImportAppendConfirmation,
				encode: func(t *testing.T, packageData businessExportPackage) []byte {
					return encodeBusinessImportPackage(t, fixture.zipBody, packageData)
				},
			},
		} {
			t.Run(fmt.Sprintf("%s/schema_%d", format.name, schema.version), func(t *testing.T) {
				packageData := cloneBusinessExportPackage(t, format.base)
				packageData.Source.SchemaVersion = schema.version
				assertBusinessImportSchemaBlockedWithoutSideEffects(
					t, format.encode(t, packageData), format.previewPath, format.applyPath,
					format.confirmation, schema.version, schema.blocker, fixture.runID,
				)
			})
		}
	}
}

func TestBusinessImportRejectsTamperedFrozenV49ManifestAndColumnsWithoutSideEffects(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	jsonBase := frozenBusinessExportV49(t, fixture.jsonBody)
	zipBase := frozenBusinessPackageV49(t, fixture.zipBody)

	for _, mutation := range []struct {
		name string
		code string
		run  func(*testing.T, *businessExportPackage)
	}{
		{
			name: "v50 operational exclusion smuggled into v49 manifest",
			code: "IMPORT_MANIFEST_INVALID",
			run: func(_ *testing.T, packageData *businessExportPackage) {
				packageData.ExcludedOperationalTables = append(
					packageData.ExcludedOperationalTables,
					"business_import_project_completion_authorizations",
				)
			},
		},
		{
			name: "same-width table column replacement",
			code: "IMPORT_SCHEMA_MISMATCH",
			run: func(t *testing.T, packageData *businessExportPackage) {
				table := automationImportTable(t, packageData, "clients")
				if len(table.Columns) == 0 {
					t.Fatal("v49 clients manifest has no columns")
				}
				table.Columns[len(table.Columns)-1] = "unknown_v49_column"
			},
		},
	} {
		for _, format := range []struct {
			name         string
			base         businessExportPackage
			previewPath  string
			applyPath    string
			confirmation string
			encode       func(*testing.T, businessExportPackage) []byte
		}{
			{
				name: "JSON", base: jsonBase, previewPath: "/api/v1/imports/business-data/preview", applyPath: "/api/v1/imports/business-data",
				confirmation: importAppendConfirmation,
				encode: func(t *testing.T, packageData businessExportPackage) []byte {
					return encodeBusinessImportJSON(t, packageData)
				},
			},
			{
				name: "ZIP", base: zipBase, previewPath: "/api/v1/imports/business-package/preview", applyPath: "/api/v1/imports/business-package",
				confirmation: packageImportAppendConfirmation,
				encode: func(t *testing.T, packageData businessExportPackage) []byte {
					return encodeBusinessImportPackage(t, fixture.zipBody, packageData)
				},
			},
		} {
			t.Run(format.name+"/"+mutation.name, func(t *testing.T) {
				packageData := cloneBusinessExportPackage(t, format.base)
				mutation.run(t, &packageData)
				assertBusinessImportRejectedWithoutSideEffects(
					t, format.encode(t, packageData), format.previewPath, format.applyPath,
					format.confirmation, mutation.code, fixture.runID,
				)
			})
		}
	}
}

func frozenBusinessExportV49(t *testing.T, source []byte) businessExportPackage {
	t.Helper()
	var packageData businessExportPackage
	if err := json.Unmarshal(source, &packageData); err != nil {
		t.Fatalf("decode real v51 business export: %v", err)
	}
	latestSchema, err := database.LatestSchemaVersion()
	if err != nil {
		t.Fatalf("latest schema version: %v", err)
	}
	if packageData.Source.SchemaVersion != latestSchema {
		t.Fatalf("compatibility fixture source schema = %d, want real current v%d export", packageData.Source.SchemaVersion, latestSchema)
	}
	packageData.Source.SchemaVersion = frozenBusinessImportSchemaV49
	packageData.ExcludedOperationalTables = append([]string(nil), frozenBusinessExportV49ExcludedTables...)
	return packageData
}

func frozenBusinessPackageV49(t *testing.T, sourceZIP []byte) businessExportPackage {
	t.Helper()
	entries := readBusinessPackageEntries(t, sourceZIP)
	return frozenBusinessExportV49(t, entries["business-data.json"])
}

func cloneBusinessExportPackage(t *testing.T, source businessExportPackage) businessExportPackage {
	t.Helper()
	return decodeBusinessImportJSON(t, encodeBusinessImportJSON(t, source))
}

func decodeBusinessImportJSON(t *testing.T, body []byte) businessExportPackage {
	t.Helper()
	var packageData businessExportPackage
	if err := json.Unmarshal(body, &packageData); err != nil {
		t.Fatalf("decode business import fixture: %v", err)
	}
	return packageData
}

func encodeBusinessImportJSON(t *testing.T, packageData businessExportPackage) []byte {
	t.Helper()
	body, err := json.MarshalIndent(packageData, "", "  ")
	if err != nil {
		t.Fatalf("encode business import fixture: %v", err)
	}
	return append(body, '\n')
}

func encodeBusinessImportPackage(t *testing.T, sourceZIP []byte, packageData businessExportPackage) []byte {
	t.Helper()
	entries := readBusinessPackageEntries(t, sourceZIP)
	businessRaw := encodeBusinessImportJSON(t, packageData)
	entries["business-data.json"] = businessRaw

	var manifest businessPackageManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode business package manifest: %v", err)
	}
	manifest.ExportedAt = packageData.ExportedAt
	manifest.Source = packageData.Source
	hash := sha256.Sum256(businessRaw)
	manifest.BusinessData.SizeBytes = int64(len(businessRaw))
	manifest.BusinessData.SHA256 = hex.EncodeToString(hash[:])
	manifest.TotalBytes = manifest.FileBytes + int64(len(businessRaw))
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode business package manifest: %v", err)
	}
	entries["manifest.json"] = append(manifestRaw, '\n')
	return writeBusinessPackageTestZIP(t, entries)
}

func assertBusinessImportSchemaBlockedWithoutSideEffects(
	t *testing.T,
	body []byte,
	previewPath, applyPath, confirmation string,
	schemaVersion int,
	blocker, incomingRunID string,
) {
	t.Helper()
	router, store, artifactDir, backupDir := newBusinessCompatibilityTarget(t)

	preview := performRequest(router, http.MethodPost, previewPath, body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("schema %d preview = %d: %s", schemaVersion, preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil ||
		envelope.Data.CanApply || envelope.Data.Blocker != blocker ||
		envelope.Data.SchemaVersion != schemaVersion || envelope.Data.TargetSchemaVersion != store.SchemaVersion ||
		envelope.Data.ApplyMode != "" {
		t.Fatalf("schema %d preview = %#v err=%v", schemaVersion, envelope.Data, err)
	}
	apply := performRequest(
		router, http.MethodPost, applyPath, body,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_VERSION_UNSUPPORTED" {
		t.Fatalf("schema %d apply = %d: %s", schemaVersion, apply.Code, apply.Body.String())
	}
	assertBusinessCompatibilityTargetUnchanged(t, store, artifactDir, backupDir, incomingRunID)
}

func assertBusinessImportRejectedWithoutSideEffects(
	t *testing.T,
	body []byte,
	previewPath, applyPath, confirmation, errorCode, incomingRunID string,
) {
	t.Helper()
	router, store, artifactDir, backupDir := newBusinessCompatibilityTarget(t)
	preview := performRequest(router, http.MethodPost, previewPath, body, nil)
	if preview.Code != http.StatusUnprocessableEntity || responseErrorCode(t, preview.Body.Bytes()) != errorCode {
		t.Fatalf("invalid v49 preview = %d: %s", preview.Code, preview.Body.String())
	}
	apply := performRequest(
		router, http.MethodPost, applyPath, body,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != errorCode {
		t.Fatalf("invalid v49 apply = %d: %s", apply.Code, apply.Body.String())
	}
	assertBusinessCompatibilityTargetUnchanged(t, store, artifactDir, backupDir, incomingRunID)
}

func newBusinessCompatibilityTarget(t *testing.T) (http.Handler, *database.Store, string, string) {
	t.Helper()
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000009949', 'Retained F23 client', 'active',
		        '2026-09-04T08:00:00Z', '2026-09-04T08:00:00Z')
	`).Error; err != nil {
		t.Fatalf("seed F23 target client: %v", err)
	}
	marker := filepath.Join(artifactDir, "f23-import-marker")
	if err := os.WriteFile(marker, []byte("retained F23 target file"), 0o600); err != nil {
		t.Fatalf("seed F23 target file: %v", err)
	}
	return router, store, artifactDir, backupDir
}

func assertBusinessCompatibilityTargetUnchanged(
	t *testing.T,
	store *database.Store,
	artifactDir, backupDir, incomingRunID string,
) {
	t.Helper()
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = '018f0000-0000-7000-8000-000000009949' AND name = 'Retained F23 client'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ?", 0, incomingRunID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM business_import_project_completion_authorizations", 0)
	marker, err := os.ReadFile(filepath.Join(artifactDir, "f23-import-marker"))
	if err != nil || string(marker) != "retained F23 target file" {
		t.Fatalf("rejected compatibility import changed target file body=%q err=%v", marker, err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("rejected compatibility import created backups: %v", backups)
	}
	if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
		t.Fatalf("rejected compatibility import retained staging: %v", staging)
	}
}
