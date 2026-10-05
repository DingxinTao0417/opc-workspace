package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The two files under testdata/released-v0.1.0-* were written by the released
// v0.1.0 Sidecar itself, not by the current exporter. The v0.1.0 tag was built
// unchanged and run on an empty data directory; a client, a project, two
// tasks, a tag, a saved view, an Inbox item and a workspace name were created
// through its API, and GET /api/v1/exports/business-data was saved. A client
// attachment was then uploaded and GET /api/v1/exports/business-package saved.
//
// They pin what a real schema 51 package looks like: the envelope, the
// exclusion manifest, the column lists and the ZIP layout. The tests that
// build a historical workspace with the current code cannot show that.
const (
	releasedV010ExportFixture  = "released-v0.1.0-business-export.json"
	releasedV010PackageFixture = "released-v0.1.0-business-package.zip"
	releasedV010TotalRows      = 19
	releasedV010AttachmentBody = "v0.1.0 controlled file\n"
)

func releasedV010Fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read released fixture: %v", err)
	}
	return body
}

func assertReleasedV010BusinessRows(t *testing.T, query func(string, int64, ...any)) {
	t.Helper()
	query("SELECT COUNT(*) FROM clients WHERE name = '星河设计工作室' AND status = 'active'", 1)
	query(`SELECT COUNT(*) FROM projects
		JOIN clients ON clients.id = projects.client_id
		WHERE projects.name = '官网改版' AND clients.name = '星河设计工作室'`, 1)
	query(`SELECT COUNT(*) FROM tasks
		JOIN projects ON projects.id = tasks.project_id
		WHERE tasks.title = '整理首页文案' AND tasks.priority = 'P1' AND projects.name = '官网改版'`, 1)
	query("SELECT COUNT(*) FROM tasks WHERE title = '预约拍摄' AND priority = 'P2' AND project_id IS NULL", 1)
	query(`SELECT COUNT(*) FROM task_tags
		JOIN tags ON tags.id = task_tags.tag_id
		JOIN tasks ON tasks.id = task_tags.task_id
		WHERE tags.name = '重要客户' AND tasks.title = '整理首页文案'`, 1)
	query("SELECT COUNT(*) FROM task_saved_views WHERE name = '本周重点'", 1)
	query("SELECT COUNT(*) FROM inbox_items WHERE title = '回电给星河设计'", 1)
	query("SELECT COUNT(*) FROM app_settings WHERE key = 'workspace' AND value_json LIKE '%星河旧工作区%'", 1)
	// Schema 71 added the column; nobody had an Agent identity before it.
	query("SELECT COUNT(*) FROM actors WHERE is_builtin = 1 AND agent_adapter_id IS NOT NULL", 0)
	query("SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
}

func TestBusinessImportAppliesAnExportWrittenByReleasedV010(t *testing.T) {
	body := releasedV010Fixture(t, releasedV010ExportFixture)
	router, store, _, backupDir := newBackupTestAPI(t)
	count := func(query string, want int64, values ...any) {
		t.Helper()
		assertDatabaseCount(t, store, query, want, values...)
	}

	preview := previewBusinessImportForTest(t, router, body)
	wantSteps := []businessImportUpgradeStep{{SchemaVersion: businessImportSchema71, Table: "actors", Change: "add_column", Column: "agent_adapter_id"}}
	if !preview.CanApply || preview.ApplyMode != importModeReplaceEmpty ||
		preview.SchemaVersion != releasedBusinessImportSchema || preview.TargetSchemaVersion != store.SchemaVersion ||
		preview.TotalRows != releasedV010TotalRows || preview.KeyConflicts != 0 || preview.SemanticConflicts != 0 ||
		len(preview.Resolutions) != 0 || preview.PlanDigest != "" || !reflect.DeepEqual(preview.UpgradeSteps, wantSteps) {
		t.Fatalf("released v0.1.0 preview = %+v", preview)
	}

	refused := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if refused.Code != http.StatusPreconditionRequired || responseErrorCode(t, refused.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("released v0.1.0 apply with the same-schema phrase = %d: %s", refused.Code, refused.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("refused confirmation created backups: %v", backups)
	}

	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importUpgradeReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("released v0.1.0 apply = %d: %s", applied.Code, applied.Body.String())
	}
	var result struct {
		Data businessImportResult `json:"data"`
	}
	if err := json.Unmarshal(applied.Body.Bytes(), &result); err != nil ||
		result.Data.ImportedRows != releasedV010TotalRows || result.Data.ApplyMode != importModeReplaceEmpty {
		t.Fatalf("released v0.1.0 result = %+v err=%v", result.Data, err)
	}
	assertReleasedV010BusinessRows(t, count)
	count("SELECT COUNT(*) FROM workflow_events WHERE aggregate_type IN ('setting', 'inbox_item', 'project')", 3)

	// The same package once more: what the user created is already there and
	// the workspace keeps its own settings, so nothing is duplicated.
	again := previewBusinessImportForTest(t, router, body)
	if !again.CanApply || again.ApplyMode != importModeAppend || again.KeyConflicts != 0 || again.SemanticConflicts != 0 ||
		again.PlanDigest == "" || !reflect.DeepEqual(again.UpgradeSteps, wantSteps) {
		t.Fatalf("repeated released v0.1.0 preview = %+v", again)
	}
	for table, rows := range map[string]int{
		"clients": 1, "projects": 1, "tasks": 2, "tags": 1, "task_tags": 1, "inbox_items": 1, "task_saved_views": 1,
	} {
		if skipped := resolutionFor(again, table, importResolutionSkipIdentical); skipped != rows {
			t.Fatalf("repeated import skips %d rows of %s, want %d: %+v", skipped, table, rows, again.Resolutions)
		}
	}
	if kept := resolutionFor(again, "app_settings", importResolutionKeepTarget); kept != 1 {
		t.Fatalf("repeated import keeps %d settings rows, want 1: %+v", kept, again.Resolutions)
	}
	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{
		"X-Import-Confirmation": importUpgradeAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeated released v0.1.0 apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	assertReleasedV010BusinessRows(t, count)
	count("SELECT COUNT(*) FROM clients", 1)
	count("SELECT COUNT(*) FROM tasks", 2)
	count("SELECT COUNT(*) FROM workflow_events WHERE aggregate_type IN ('setting', 'inbox_item', 'project')", 3)
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 2 {
		t.Fatalf("rollback backups = %v, want one per apply", backups)
	}
}

func TestBusinessPackageImportAppliesAPackageWrittenByReleasedV010(t *testing.T) {
	payload := releasedV010Fixture(t, releasedV010PackageFixture)
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	count := func(query string, want int64, values ...any) {
		t.Helper()
		assertDatabaseCount(t, store, query, want, values...)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/imports/business-package/preview", payload, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("released v0.1.0 package preview = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Data
	if !preview.CanApply || preview.ApplyMode != importModeReplaceEmpty ||
		preview.SchemaVersion != releasedBusinessImportSchema || preview.TargetSchemaVersion != store.SchemaVersion ||
		preview.FileCount != 1 || preview.FileBytes != int64(len(releasedV010AttachmentBody)) ||
		preview.FileConflicts != 0 || preview.SkippedFiles != 0 || len(preview.UpgradeSteps) != 1 || preview.PlanDigest != "" {
		t.Fatalf("released v0.1.0 package preview = %+v", preview)
	}

	refused := performRequest(router, http.MethodPost, "/api/v1/imports/business-package", payload,
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation})
	if refused.Code != http.StatusPreconditionRequired || responseErrorCode(t, refused.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("released v0.1.0 package apply with the same-schema phrase = %d: %s", refused.Code, refused.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("refused confirmation created backups: %v", backups)
	}

	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-package", payload,
		map[string]string{"X-Import-Confirmation": packageImportUpgradeReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("released v0.1.0 package apply = %d: %s", applied.Code, applied.Body.String())
	}
	var result struct {
		Data businessPackageImportResult `json:"data"`
	}
	if err := json.Unmarshal(applied.Body.Bytes(), &result); err != nil || result.Data.ImportedFiles != 1 {
		t.Fatalf("released v0.1.0 package result = %+v err=%v", result.Data, err)
	}
	assertReleasedV010BusinessRows(t, count)

	var relativePath string
	if err := store.DB.Raw(`
		SELECT relative_path FROM client_attachments
		WHERE name = '合同要点.txt' AND size_bytes = ? AND deleted_at IS NULL
	`, len(releasedV010AttachmentBody)).Row().Scan(&relativePath); err != nil {
		t.Fatalf("imported client attachment: %v", err)
	}
	published, err := os.ReadFile(filepath.Join(artifactDir, filepath.FromSlash(relativePath)))
	if err != nil || string(published) != releasedV010AttachmentBody {
		t.Fatalf("published controlled file = %q err=%v", published, err)
	}

	// Importing the same package again publishes nothing a second time.
	response = performRequest(router, http.MethodPost, "/api/v1/imports/business-package/preview", payload, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("repeated released v0.1.0 package preview = %d: %s", response.Code, response.Body.String())
	}
	envelope.Data = businessPackageImportPreview{}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	again := envelope.Data
	if !again.CanApply || again.ApplyMode != importModeAppend || again.FileConflicts != 0 ||
		again.SkippedFiles != 1 || again.PlanDigest == "" {
		t.Fatalf("repeated released v0.1.0 package preview = %+v", again)
	}
	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-package", payload, map[string]string{
		"X-Import-Confirmation": packageImportUpgradeAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeated released v0.1.0 package apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	result.Data = businessPackageImportResult{}
	if err := json.Unmarshal(repeated.Body.Bytes(), &result); err != nil || result.Data.ImportedFiles != 0 {
		t.Fatalf("repeated released v0.1.0 package result = %+v err=%v", result.Data, err)
	}
	count("SELECT COUNT(*) FROM client_attachments", 1)
	assertReleasedV010BusinessRows(t, count)
}
