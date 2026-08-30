package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessPackageImportPreviewsAndAtomicallyAppliesControlledFiles(t *testing.T) {
	sourceRouter, _, _, _ := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, sourceRouter)
	fileBody := []byte("imported controlled file body")
	uploaded := performMultipartRequest(
		sourceRouter,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Portable output","artifacts":[{"client_ref":"file","storage_kind":"file","name":"portable.txt","file_field":"file"}]}`,
		map[string][]byte{"file": fileBody},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit source file = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	client := createClientForTest(t, sourceRouter, `{"name":"Portable Client"}`, nil)
	clientFileBody := []byte("imported client attachment")
	clientUpload := performClientAttachmentUpload(
		t, sourceRouter, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"client.txt"}`, "client.txt", clientFileBody, map[string]string{"If-Match": `"1"`},
	)
	if clientUpload.Code != http.StatusCreated {
		t.Fatalf("submit source client attachment = %d: %s", clientUpload.Code, clientUpload.Body.String())
	}
	clientAttachmentID := decodeClientAttachmentResponse(t, clientUpload.Body.Bytes()).ID
	project := createProjectForTest(t, sourceRouter, `{"name":"Portable Project"}`, nil)
	projectFileBody := []byte("imported project attachment")
	projectUpload := performClientAttachmentUpload(
		t, sourceRouter, "/api/v1/projects/"+project.ID+"/attachments",
		`{"name":"project.txt"}`, "project.txt", projectFileBody, map[string]string{"If-Match": `"1"`},
	)
	if projectUpload.Code != http.StatusCreated {
		t.Fatalf("submit source project attachment = %d: %s", projectUpload.Code, projectUpload.Body.String())
	}
	projectAttachmentID := decodeProjectAttachmentResponse(t, projectUpload.Body.Bytes()).ID
	_, avatarRef := replaceWorkspaceAvatarForTest(t, sourceRouter, 0, nil, testPNGAvatar)
	exported := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export source package = %d: %s", exported.Code, exported.Body.String())
	}

	targetRouter, targetStore, artifactDir, backupDir := newBackupTestAPI(t)
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", exported.Body.Bytes(), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview package = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode package preview: %v", err)
	}
	if !previewEnvelope.Data.CanApply || previewEnvelope.Data.FileCount != 4 || previewEnvelope.Data.FileBytes != int64(len(fileBody)+len(clientFileBody)+len(projectFileBody)+len(testPNGAvatar)) || previewEnvelope.Data.TotalRows == 0 {
		t.Fatalf("package preview = %#v", previewEnvelope.Data)
	}
	if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
		t.Fatalf("preview retained import staging = %v", staging)
	}

	missingConfirmation := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package", exported.Body.Bytes(), nil)
	if missingConfirmation.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingConfirmation.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("missing package confirmation = %d: %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", exported.Body.Bytes(),
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation},
	)
	if apply.Code != http.StatusOK {
		t.Fatalf("apply package = %d: %s", apply.Code, apply.Body.String())
	}
	var resultEnvelope struct {
		Data businessPackageImportResult `json:"data"`
	}
	if err := json.Unmarshal(apply.Body.Bytes(), &resultEnvelope); err != nil {
		t.Fatalf("decode package result: %v", err)
	}
	if resultEnvelope.Data.ImportedFiles != 4 || resultEnvelope.Data.ImportedRows != previewEnvelope.Data.TotalRows || resultEnvelope.Data.BackupID == "" {
		t.Fatalf("package result = %#v", resultEnvelope.Data)
	}
	if body, err := os.ReadFile(filepath.Join(artifactDir, "objects", artifactID)); err != nil || !bytes.Equal(body, fileBody) {
		t.Fatalf("imported controlled file = %q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(artifactDir, "objects", clientAttachmentID)); err != nil || !bytes.Equal(body, clientFileBody) {
		t.Fatalf("imported client attachment = %q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(artifactDir, "objects", projectAttachmentID)); err != nil || !bytes.Equal(body, projectFileBody) {
		t.Fatalf("imported project attachment = %q err=%v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(artifactDir, filepath.FromSlash(avatarRef))); err != nil || !bytes.Equal(body, testPNGAvatar) {
		t.Fatalf("imported avatar = %x err=%v", body, err)
	}
	avatarContent := performRequest(targetRouter, http.MethodGet, "/api/v1/settings/avatar/content", nil, nil)
	if avatarContent.Code != http.StatusOK || !bytes.Equal(avatarContent.Body.Bytes(), testPNGAvatar) {
		t.Fatalf("imported avatar API = %d: %x", avatarContent.Code, avatarContent.Body.Bytes())
	}
	var artifactCount int64
	if err := targetStore.DB.Table("task_artifacts").Where("id = ? AND deleted_at IS NULL", artifactID).Count(&artifactCount).Error; err != nil || artifactCount != 1 {
		t.Fatalf("imported Artifact count=%d err=%v", artifactCount, err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, resultEnvelope.Data.BackupID, backupManifestName)); err != nil {
		t.Fatalf("rollback backup missing: %v", err)
	}
	if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
		t.Fatalf("apply retained import staging = %v", staging)
	}
}

func TestBusinessPackageImportRejectsTamperedFileAndExtraEntry(t *testing.T) {
	payload, artifactPath := businessPackageFixture(t)
	entries := readBusinessPackageEntries(t, payload)
	entries[artifactPath] = []byte("tampered package file")
	tampered := writeBusinessPackageTestZIP(t, entries)
	targetRouter, _, _, backupDir := newBackupTestAPI(t)
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", tampered, nil)
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_PACKAGE_FILE_INVALID" {
		t.Fatalf("tampered package = %d: %s", response.Code, response.Body.String())
	}

	entries = readBusinessPackageEntries(t, payload)
	entries["unexpected.txt"] = []byte("not allowlisted")
	extra := writeBusinessPackageTestZIP(t, entries)
	response = performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", extra, nil)
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_PACKAGE_MANIFEST_INVALID" {
		t.Fatalf("extra package entry = %d: %s", response.Code, response.Body.String())
	}
	if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
		t.Fatalf("rejected package retained staging = %v", staging)
	}
}

func TestBusinessPackageImportAppendsToNonEmptyTargetWithoutOverwriting(t *testing.T) {
	payload, artifactPath := businessPackageFixture(t)
	targetRouter, targetStore, artifactDir, backupDir := newBackupTestAPI(t)
	if err := targetStore.DB.Exec("INSERT INTO clients(id, name) VALUES ('018f0000-0000-7000-8000-000000008001', 'Existing')").Error; err != nil {
		t.Fatal(err)
	}
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", payload, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("non-empty package preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || !envelope.Data.CanApply || envelope.Data.ApplyMode != importModeAppend || envelope.Data.Blocker != "" ||
		envelope.Data.TargetSchemaVersion != 46 || envelope.Data.TargetRows != 1 || envelope.Data.KeyConflicts != 0 || len(envelope.Data.ConflictTables) != 1 ||
		envelope.Data.ConflictTables[0] != (businessImportTableConflict{Table: "clients", IncomingRows: 0, TargetRows: 1, KeyConflicts: 0}) {
		t.Fatalf("non-empty package preview = %#v err=%v", envelope.Data, err)
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", payload,
		map[string]string{"X-Import-Confirmation": packageImportAppendConfirmation},
	)
	if apply.Code != http.StatusOK {
		t.Fatalf("non-empty package apply = %d: %s", apply.Code, apply.Body.String())
	}
	if _, err := os.Stat(filepath.Join(artifactDir, filepath.FromSlash(stringsTrimFileRoot(artifactPath)))); err != nil {
		t.Fatalf("appended package file missing: %v", err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("append rollback backups=%v", backups)
	}
	var existing int64
	if err := targetStore.DB.Table("clients").Where("name = ?", "Existing").Count(&existing).Error; err != nil || existing != 1 {
		t.Fatalf("existing target changed count=%d err=%v", existing, err)
	}
}

func TestBusinessPackageImportBlocksExistingControlledFileBeforeBackup(t *testing.T) {
	payload, artifactPath := businessPackageFixture(t)
	targetRouter, _, artifactDir, backupDir := newBackupTestAPI(t)
	targetPath := filepath.Join(artifactDir, filepath.FromSlash(stringsTrimFileRoot(artifactPath)))
	if err := os.WriteFile(targetPath, []byte("orphan target object"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", payload, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("file-conflict preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || envelope.Data.CanApply || envelope.Data.Blocker != "target_file_conflicts" || envelope.Data.FileConflicts != 1 || envelope.Data.ApplyMode != "" {
		t.Fatalf("file-conflict preview = %#v err=%v", envelope.Data, err)
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", payload,
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation},
	)
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_FILE_CONFLICT" {
		t.Fatalf("file-conflict apply = %d: %s", apply.Code, apply.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("file conflict created rollback backup: %v", backups)
	}
	body, err := os.ReadFile(targetPath)
	if err != nil || string(body) != "orphan target object" {
		t.Fatalf("existing object changed body=%q err=%v", body, err)
	}
}

func TestBusinessPackageImportCompensatesCommittedFilesWhenDatabaseApplyFails(t *testing.T) {
	payload, artifactPath := businessPackageFixture(t)
	entries := readBusinessPackageEntries(t, payload)
	var business businessExportPackage
	if err := json.Unmarshal(entries["business-data.json"], &business); err != nil {
		t.Fatal(err)
	}
	for tableIndex := range business.Tables {
		table := &business.Tables[tableIndex]
		if table.Name != "tasks" || len(table.Rows) == 0 {
			continue
		}
		projectIndex := columnIndex(table.Columns, "project_id")
		if projectIndex >= 0 {
			table.Rows[0][projectIndex] = "018f0000-0000-7000-8000-000000009999"
		}
	}
	businessRaw, err := json.MarshalIndent(business, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	businessRaw = append(businessRaw, '\n')
	entries["business-data.json"] = businessRaw
	var manifest businessPackageManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(businessRaw)
	manifest.BusinessData.SizeBytes = int64(len(businessRaw))
	manifest.BusinessData.SHA256 = hex.EncodeToString(hash[:])
	manifest.TotalBytes = manifest.FileBytes + int64(len(businessRaw))
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	entries["manifest.json"] = append(manifestRaw, '\n')
	tampered := writeBusinessPackageTestZIP(t, entries)

	targetRouter, targetStore, artifactDir, backupDir := newBackupTestAPI(t)
	response := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", tampered,
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation},
	)
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_PACKAGE_APPLY_FAILED" {
		t.Fatalf("invalid database package = %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(artifactDir, filepath.FromSlash(stringsTrimFileRoot(artifactPath)))); !os.IsNotExist(err) {
		t.Fatalf("failed import retained committed file: %v", err)
	}
	var tasks int64
	if err := targetStore.DB.Table("tasks").Count(&tasks).Error; err != nil || tasks != 0 {
		t.Fatalf("failed package changed tasks=%d err=%v", tasks, err)
	}
	backups, err := os.ReadDir(backupDir)
	if err != nil || len(backups) != 1 || !backups[0].IsDir() {
		t.Fatalf("failed apply rollback backups=%v err=%v", backups, err)
	}
}

func businessPackageFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	router, _, _, _ := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	uploaded := performMultipartRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Package fixture","artifacts":[{"client_ref":"file","storage_kind":"file","name":"fixture.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte("original package file")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit package fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	exported := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export package fixture = %d: %s", exported.Code, exported.Body.String())
	}
	return bytes.Clone(exported.Body.Bytes()), "files/objects/" + artifactID
}

func writeBusinessPackageTestZIP(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	for _, name := range []string{"manifest.json", "business-data.json"} {
		if body, ok := entries[name]; ok {
			if err := writeBusinessPackageBytes(archive, name, body); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, body := range entries {
		if name == "manifest.json" || name == "business-data.json" {
			continue
		}
		if err := writeBusinessPackageBytes(archive, name, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func importPackageStagingFiles(t *testing.T, backupDir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(backupDir, ".business-import-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func stringsTrimFileRoot(value string) string {
	return strings.TrimPrefix(value, "files/")
}
