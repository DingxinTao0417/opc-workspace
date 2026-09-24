package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

const frozenBusinessImportSchemaV49 = 49

func TestBusinessImportSchemaContractAIOnly68To79(t *testing.T) {
	for _, target := range []int{68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79} {
		for _, source := range []int{49, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79} {
			if source > target {
				continue
			}
			expected := businessExportExcludedTables
			if source == 49 {
				expected = businessExportExcludedTablesSchema49
			} else if source >= 63 && source <= 65 {
				expected = businessExportExcludedTablesSchema65
			} else if source >= 66 && source <= 70 {
				expected = businessExportExcludedTablesSchema67
			} else if source == 71 {
				expected = businessExportExcludedTablesSchema71
			} else if source >= 72 && source <= 75 {
				expected = businessExportExcludedTablesSchema75
			} else if source == 76 {
				expected = businessExportExcludedTablesSchema76
			} else if source == 77 {
				expected = businessExportExcludedTablesSchema77
			}
			actual, ok := businessImportSchemaContract(source, target)
			if !ok || !equalStrings(actual, expected) {
				t.Fatalf("schema %d -> %d contract invalid: %#v %v", source, target, actual, ok)
			}
		}
	}
	if _, ok := businessImportSchemaContract(72, 71); ok {
		t.Fatal("schema downgrade accepted")
	}
	if _, ok := businessImportSchemaContract(76, 75); ok {
		t.Fatal("plan schema downgrade accepted")
	}
	if _, ok := businessImportSchemaContract(77, 76); ok {
		t.Fatal("continuation schema downgrade accepted")
	}
	if _, ok := businessImportSchemaContract(78, 77); ok {
		t.Fatal("access request schema downgrade accepted")
	}
	if _, ok := businessImportSchemaContract(79, 78); ok {
		t.Fatal("future schema accepted")
	}
}

func TestBusinessImportSchema77PackageUsesFrozenExclusionsAt78(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	current := emptyBusinessExportFixture(t, router)
	current.Source.SchemaVersion = 77
	current.ExcludedOperationalTables = append([]string(nil), businessExportExcludedTablesSchema77...)
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", encodeBusinessImportJSON(t, current), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("schema77 preview=%d %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || !envelope.Data.CanApply || envelope.Data.SchemaVersion != 77 || envelope.Data.TargetSchemaVersion != 79 {
		t.Fatalf("schema77 compatibility=%+v err=%v", envelope.Data, err)
	}
}

func TestBusinessImportSchemaContractUsesFrozenHistoricalExcludedTables(t *testing.T) {
	base := []string{
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
		"business_import_project_completion_authorizations",
		"ai_providers",
		"ai_sessions",
		"ai_generations",
		"ai_messages",
		"ai_memories",
		"ai_memory_entries",
		"ai_run_steps",
	}
	knowledge := []string{
		"knowledge_sources",
		"knowledge_documents",
		"knowledge_chunks",
		"knowledge_index_jobs",
		"knowledge_chunks_fts",
		"knowledge_chunks_fts_data",
		"knowledge_chunks_fts_idx",
		"knowledge_chunks_fts_content",
		"knowledge_chunks_fts_docsize",
		"knowledge_chunks_fts_config",
	}
	schema65 := append(append(append([]string{}, base...),
		"ai_evaluation_runs", "ai_evaluation_results"), knowledge...)
	schema67 := append(append(append([]string{}, base...),
		"ai_evaluation_runs", "ai_evaluation_results", "ai_evaluation_reviews"), knowledge...)
	schema71 := append(append([]string{}, schema67...), "agent_runs")
	schema72 := append(append(append([]string{}, base...),
		"ai_action_proposals", "ai_evaluation_runs", "ai_evaluation_results", "ai_evaluation_reviews"), knowledge...)
	schema72 = append(schema72, "agent_runs")

	for _, test := range []struct {
		name     string
		source   int
		expected []string
	}{
		{name: "schema65 has no future reviews runs or proposals", source: 65, expected: schema65},
		{name: "schema67 adds evaluation reviews", source: 67, expected: schema67},
		{name: "schema71 adds agent runs", source: 71, expected: schema71},
		{name: "schema72 adds action proposals", source: 72, expected: schema72},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := businessImportSchemaContract(test.source, businessImportSchema75)
			if !ok || !equalStrings(actual, test.expected) {
				t.Fatalf("schema %d -> 75 excluded tables = %#v, ok=%v; want %#v", test.source, actual, ok, test.expected)
			}
		})
	}
}

func TestBusinessImportSchema76KeepsContinuationAuthorizationNonportable(t *testing.T) {
	source, sourceStore, _, _ := newBackupTestAPI(t)
	for _, statement := range []string{
		`INSERT INTO clients(id,name) VALUES ('018f0000-0000-7000-8000-000000007701','Portable client')`,
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('continuation-private-session','Private authorization',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('continuation-private-provider','Private authorization provider','local','openai_chat','http://127.0.0.1:1/v1','test','ready','healthy',0,1,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('continuation-private-generation','continuation-private-session','continuation-private-provider','streaming','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_work_plan_revisions(session_id,version,generation_id,plan_json,created_at) VALUES ('continuation-private-session',1,'continuation-private-generation','{"title":"Private continuation plan","steps":[]}','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_continuations(id,session_id,provider_id,provider_version,provider_config_version,provider_name,provider_kind,provider_protocol,provider_model,workspace_json,initial_plan_version,current_plan_version,max_turns,turns_started,status,reason,version,created_at,updated_at,expires_at) VALUES ('continuation-private-lease','continuation-private-session','continuation-private-provider',1,1,'Private authorization provider','local','openai_chat','test','{"provider_version":1,"scopes":["work","outputs","actions"]}',1,1,2,1,'running','generating',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z','2026-09-21T12:30:00Z')`,
		`INSERT INTO ai_continuation_turns(continuation_id,turn_index,generation_id,plan_version,observation_hash,created_at) VALUES ('continuation-private-lease',1,'continuation-private-generation',1,'` + strings.Repeat("a", 64) + `','2026-09-21T12:00:00Z')`,
	} {
		if err := sourceStore.DB.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	exported := performRequest(source, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export=%d %s", exported.Code, exported.Body.String())
	}
	if strings.Contains(exported.Body.String(), "continuation-private-") || strings.Contains(exported.Body.String(), "Private authorization") {
		t.Fatal("portable export contains authorization or turn data")
	}
	current := decodeBusinessImportJSON(t, exported.Body.Bytes())
	if current.Source.SchemaVersion != 79 || !equalStrings(current.ExcludedOperationalTables, businessExportExcludedTables) {
		t.Fatalf("current export contract=%+v", current.Source)
	}
	for _, name := range []string{"ai_continuations", "ai_continuation_turns"} {
		found := false
		for _, excluded := range current.ExcludedOperationalTables {
			found = found || excluded == name
		}
		if !found {
			t.Fatalf("missing exclusion %s", name)
		}
		for _, table := range current.Tables {
			if table.Name == name {
				t.Fatalf("runtime table exported: %s", name)
			}
		}
	}
	// Schema 077 changes no portable columns. Freeze the exact v76 envelope,
	// including its old exclusion list, rather than rewriting the old contract.
	frozen := cloneBusinessExportPackage(t, current)
	frozen.Source.SchemaVersion = 76
	frozen.ExcludedOperationalTables = append([]string(nil), businessExportExcludedTablesSchema76...)
	for _, excluded := range frozen.ExcludedOperationalTables {
		if excluded == "ai_continuations" || excluded == "ai_continuation_turns" {
			t.Fatalf("v76 manifest was retroactively changed: %s", excluded)
		}
	}
	target, targetStore, _, backupDir := newBackupTestAPI(t)
	for _, changed := range []businessExportPackage{
		func() businessExportPackage {
			copy := cloneBusinessExportPackage(t, frozen)
			copy.ExcludedOperationalTables = append(copy.ExcludedOperationalTables, "ai_continuations")
			return copy
		}(),
		func() businessExportPackage {
			copy := cloneBusinessExportPackage(t, frozen)
			copy.Tables = append(copy.Tables, businessExportTable{Name: "ai_continuations", Columns: []string{"id"}, Rows: [][]any{{"smuggled-authorization"}}})
			return copy
		}(),
	} {
		bad := performRequest(target, http.MethodPost, "/api/v1/imports/business-data/preview", encodeBusinessImportJSON(t, changed), nil)
		if bad.Code != http.StatusUnprocessableEntity || responseErrorCode(t, bad.Body.Bytes()) != "IMPORT_MANIFEST_INVALID" {
			t.Fatalf("smuggled runtime contract=%d %s", bad.Code, bad.Body.String())
		}
	}
	body := encodeBusinessImportJSON(t, frozen)
	preview := performRequest(target, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("v76 preview=%d %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || !envelope.Data.CanApply || envelope.Data.SchemaVersion != 76 || envelope.Data.TargetSchemaVersion != 79 {
		t.Fatalf("v76 preview=%+v %v", envelope.Data, err)
	}
	assertDatabaseCount(t, targetStore, `SELECT COUNT(*) FROM clients`, 0)
	if len(backupPackageDirectories(t, backupDir)) != 0 {
		t.Fatal("read-only or rejected preview created backup")
	}
	apply := performRequest(target, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if apply.Code != http.StatusOK {
		t.Fatalf("v76 apply=%d %s", apply.Code, apply.Body.String())
	}
	assertDatabaseCount(t, targetStore, `SELECT COUNT(*) FROM clients WHERE id='018f0000-0000-7000-8000-000000007701'`, 1)
	for _, table := range []string{"ai_continuations", "ai_continuation_turns", "ai_sessions", "ai_generations"} {
		assertDatabaseCount(t, targetStore, `SELECT COUNT(*) FROM `+table, 0)
	}
	if len(backupPackageDirectories(t, backupDir)) != 1 {
		t.Fatal("approved import did not create its single rollback backup")
	}
	assertDatabaseCount(t, sourceStore, `SELECT COUNT(*) FROM ai_continuations WHERE turns_started=1`, 1)
	assertDatabaseCount(t, sourceStore, `SELECT COUNT(*) FROM ai_continuation_turns`, 1)
}

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

func TestBusinessImportSchemaContractAllowsSchema63Into64(t *testing.T) {
	for _, target := range []int{businessImportSchema64, businessImportSchema65, businessImportSchema66, businessImportSchema67} {
		excluded, ok := businessImportSchemaContract(businessImportSchema63, target)
		if !ok || !equalStrings(excluded, businessExportExcludedTablesSchema65) {
			t.Fatalf("schema 63 to %d contract = %#v, ok=%v", target, excluded, ok)
		}
	}
}

func TestBusinessImportSchemaContractAllowsSchema64Into65(t *testing.T) {
	for _, target := range []int{businessImportSchema65, businessImportSchema66, businessImportSchema67} {
		excluded, ok := businessImportSchemaContract(businessImportSchema64, target)
		if !ok || !equalStrings(excluded, businessExportExcludedTablesSchema65) {
			t.Fatalf("schema 64 to %d contract = %#v, ok=%v", target, excluded, ok)
		}
	}
}

func TestBusinessImportSchemaContractAllowsSchema65IntoCurrent(t *testing.T) {
	for _, target := range []int{businessImportSchema66, businessImportSchema67} {
		excluded, ok := businessImportSchemaContract(businessImportSchema65, target)
		if !ok || !equalStrings(excluded, businessExportExcludedTablesSchema65) {
			t.Fatalf("schema 65 to %d contract = %#v, ok=%v", target, excluded, ok)
		}
	}
}

func TestBusinessImportSchemaContractAllowsSchema66Into67(t *testing.T) {
	excluded, ok := businessImportSchemaContract(businessImportSchema66, businessImportSchema67)
	if !ok || !equalStrings(excluded, businessExportExcludedTablesSchema67) {
		t.Fatalf("schema 66 to 67 contract = %#v, ok=%v", excluded, ok)
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
		{version: 80, blocker: "source_schema_newer"},
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
