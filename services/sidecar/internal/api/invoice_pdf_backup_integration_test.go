package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInvoicePDFBackupSyncDirectoriesIncludeEveryParentDeepestFirst(t *testing.T) {
	root := filepath.Join(t.TempDir(), "package")
	ownerID := "018f0000-0000-7000-8000-000000009101"
	assetID := "018f0000-0000-7000-8000-000000009102"
	manifest := backupManifest{Artifacts: []backupManifestArtifact{{
		ID:                 assetID,
		backupManifestFile: backupManifestFile{Path: invoicePDFControlledPrefix + ownerID + "/" + assetID + ".pdf"},
	}}}
	directories := backupPackageSyncDirectories(root, manifest)
	ownerDirectory := filepath.Join(root, "invoices", ownerID)
	invoiceDirectory := filepath.Join(root, "invoices")
	indexOf := func(expected string) int {
		for index, directory := range directories {
			if directory == expected {
				return index
			}
		}
		return -1
	}
	ownerIndex := indexOf(ownerDirectory)
	invoiceIndex := indexOf(invoiceDirectory)
	rootIndex := indexOf(root)
	if ownerIndex < 0 || invoiceIndex < 0 || rootIndex < 0 {
		t.Fatalf("backup sync directories = %v", directories)
	}
	if ownerIndex >= invoiceIndex || invoiceIndex >= rootIndex {
		t.Fatalf("backup sync order = %v", directories)
	}
}

func TestRollbackRestoreSwapRestoresPreviousInvoicePDFDirectory(t *testing.T) {
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(root, "invoices")
	previous := filepath.Join(root, ".restore-old-invoices-test")
	if err := os.MkdirAll(live, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "new.pdf"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "original.pdf"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := restoreSwapPaths{
		database: filepath.Join(root, "workspace.db"),
		objects:  filepath.Join(artifactRoot, "objects"), avatars: filepath.Join(artifactRoot, "avatars"),
		invoicePDFs: live, invoicePDFsOld: previous,
	}
	if err := rollbackRestoreSwap(paths); err != nil {
		t.Fatalf("rollback invoice PDF swap: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(live, "original.pdf"))
	if err != nil || string(contents) != "original" {
		t.Fatalf("restored invoice PDF = %q err=%v", contents, err)
	}
	if _, err := os.Lstat(filepath.Join(live, "new.pdf")); !os.IsNotExist(err) {
		t.Fatalf("failed invoice PDF restore contents survived rollback: %v", err)
	}
	if _, err := os.Lstat(previous); !os.IsNotExist(err) {
		t.Fatalf("previous invoice PDF directory was not consumed: %v", err)
	}
}

func TestInvoicePDFVerifiedBackupDrillAndStartupRestoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, root)
	invoicePDFDir := filepath.Join(root, "invoices")
	client := createClientForTest(t, router.Engine, `{"name":"PDF Backup Client"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":18800,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29","notes":"backup target"}`,
		client.ID,
	), nil)
	originalPDF := generateInvoicePDFForTest(t, router.Engine, invoice, "invoice-pdf-backup-target")
	var originalAsset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&originalAsset).Error; err != nil {
		t.Fatalf("load original invoice PDF asset: %v", err)
	}
	originalBytes, err := os.ReadFile(filepath.Join(invoicePDFDir, filepath.FromSlash(originalAsset.RelativePath)))
	if err != nil || int64(len(originalBytes)) != originalPDF.SizeBytes {
		t.Fatalf("read original invoice PDF bytes=%d err=%v", len(originalBytes), err)
	}
	if required, err := estimateBackupCreateCapacityWithInvoicePDFs(
		store.DB, databasePath, router.artifactStore, invoicePDFDir, store.SchemaVersion, 0,
	); err != nil || required == 0 {
		t.Fatalf("estimate invoice PDF backup capacity=%d err=%v", required, err)
	}
	if _, err := estimateBackupCreateCapacityWithInvoicePDFs(
		store.DB, databasePath, router.artifactStore, "", store.SchemaVersion, 0,
	); !errors.Is(err, errBackupCapacityUnavailable) {
		t.Fatalf("invoice PDF backup capacity without controlled root error=%v", err)
	}

	created := performRequest(router.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"invoice pdf restore target"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invoice PDF backup = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	if target.ArtifactCount != 1 || target.ArtifactBytes != originalPDF.SizeBytes {
		t.Fatalf("invoice PDF backup summary = %#v", target)
	}
	packagePDFPath := filepath.Join(backupDir, target.ID, filepath.FromSlash(invoicePDFControlledPrefix+originalAsset.RelativePath))
	packagedBytes, err := os.ReadFile(packagePDFPath)
	if err != nil || !bytes.Equal(packagedBytes, originalBytes) {
		t.Fatalf("packaged invoice PDF bytes=%d err=%v", len(packagedBytes), err)
	}
	drilled := performRequest(router.Engine, http.MethodPost, "/api/v1/backups/"+target.ID+"/drill", []byte(`{}`), nil)
	if drilled.Code != http.StatusOK {
		t.Fatalf("drill invoice PDF backup = %d: %s", drilled.Code, drilled.Body.String())
	}

	updatedRecorder := performRequest(
		router.Engine, http.MethodPatch, "/api/v1/invoices/"+invoice.ID,
		[]byte(`{"notes":"changed after backup"}`), map[string]string{"If-Match": `"1"`},
	)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update invoice after backup = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeInvoiceResponse(t, updatedRecorder.Body.Bytes())
	_ = generateInvoicePDFForTest(t, router.Engine, updated, "invoice-pdf-backup-late")
	var lateAsset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&lateAsset).Error; err != nil {
		t.Fatalf("load late invoice PDF asset: %v", err)
	}
	if lateAsset.ID == originalAsset.ID {
		t.Fatal("invoice PDF regeneration did not replace the asset")
	}

	scheduled := performRequest(router.Engine, http.MethodPost, "/api/v1/backups/"+target.ID+"/restore", []byte(`{"confirm":true}`), nil)
	if scheduled.Code != http.StatusAccepted {
		t.Fatalf("schedule invoice PDF restore = %d: %s", scheduled.Code, scheduled.Body.String())
	}
	var scheduledEnvelope struct {
		Data scheduledRestoreResult `json:"data"`
	}
	if err := json.Unmarshal(scheduled.Body.Bytes(), &scheduledEnvelope); err != nil {
		t.Fatalf("decode invoice PDF restore schedule: %v", err)
	}
	if err := router.Close(); err != nil {
		t.Fatalf("close router before invoice PDF restore: %v", err)
	}
	if err := store.Checkpoint(); err != nil {
		t.Fatalf("checkpoint before invoice PDF restore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close database before invoice PDF restore: %v", err)
	}

	latestSchema, err := database.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyPendingRestoreWithInvoicePDFs(backupDir, databasePath, artifactDir, invoicePDFDir, latestSchema)
	if err != nil {
		t.Fatalf("apply invoice PDF restore: %v", err)
	}
	if !result.Applied || result.BackupID != target.ID || result.RollbackBackupID != scheduledEnvelope.Data.RollbackBackupID {
		t.Fatalf("invoice PDF restore result = %#v", result)
	}
	restoredBytes, err := os.ReadFile(filepath.Join(invoicePDFDir, filepath.FromSlash(originalAsset.RelativePath)))
	if err != nil || !bytes.Equal(restoredBytes, originalBytes) {
		t.Fatalf("restored invoice PDF bytes=%d err=%v", len(restoredBytes), err)
	}
	if _, err := os.Lstat(filepath.Join(invoicePDFDir, filepath.FromSlash(lateAsset.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("late invoice PDF survived restore: %v", err)
	}
	if rollbackBytes, err := os.ReadFile(filepath.Join(backupDir, result.RollbackBackupID, filepath.FromSlash(invoicePDFControlledPrefix+lateAsset.RelativePath))); err != nil || len(rollbackBytes) == 0 {
		t.Fatalf("rollback backup omitted late invoice PDF bytes=%d err=%v", len(rollbackBytes), err)
	}

	reopened, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("open invoice PDF restored database: %v", err)
	}
	restoredRouter, err := NewRouter(reopened.DB, Options{
		AppVersion: "0.1.0-test", Commit: "restore-test", SchemaVersion: reopened.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"}, Logger: log.New(io.Discard, "", 0),
		ArtifactDir: artifactDir, InvoicePDFDir: invoicePDFDir, DatabasePath: databasePath, BackupDir: backupDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		_ = reopened.Close()
		t.Fatalf("open invoice PDF restored router: %v", err)
	}
	defer restoredRouter.Close()
	defer reopened.Close()
	download := performRequest(restoredRouter.Engine, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), originalBytes) {
		t.Fatalf("download restored invoice PDF = %d bytes=%d", download.Code, download.Body.Len())
	}
}

func TestInvoicePDFBusinessPackageRoundTripIncludesMetadataAndBytes(t *testing.T) {
	sourceRouter, sourceStore, sourceArtifactDir, _ := newBackupTestAPI(t)
	sourceInvoicePDFDir := filepath.Join(filepath.Dir(sourceArtifactDir), "invoices")
	client := createClientForTest(t, sourceRouter, `{"name":"Portable PDF Client"}`, nil)
	invoice := createInvoiceForTest(t, sourceRouter, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":9900,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`,
		client.ID,
	), nil)
	generated := generateInvoicePDFForTest(t, sourceRouter, invoice, "invoice-pdf-package-source")
	var sourceAsset models.InvoicePDFAsset
	if err := sourceStore.DB.Where("invoice_id = ?", invoice.ID).Take(&sourceAsset).Error; err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(filepath.Join(sourceInvoicePDFDir, filepath.FromSlash(sourceAsset.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}

	exported := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export invoice PDF business package = %d: %s", exported.Code, exported.Body.String())
	}
	entries := readBusinessPackageEntries(t, exported.Body.Bytes())
	packagePath := "files/" + invoicePDFControlledPrefix + sourceAsset.RelativePath
	if !bytes.Equal(entries[packagePath], sourceBytes) {
		t.Fatalf("packaged invoice PDF bytes=%d want=%d", len(entries[packagePath]), len(sourceBytes))
	}
	var business businessExportPackage
	if err := json.Unmarshal(entries["business-data.json"], &business); err != nil {
		t.Fatal(err)
	}
	if business.ArtifactFiles.ActiveCount != 1 || business.ArtifactFiles.ActiveBytes != generated.SizeBytes {
		t.Fatalf("invoice PDF business file summary = %#v", business.ArtifactFiles)
	}
	foundMetadata := false
	for _, table := range business.Tables {
		if table.Name == "invoice_pdf_assets" {
			foundMetadata = len(table.Rows) == 1
		}
	}
	if !foundMetadata {
		t.Fatal("business package omitted invoice_pdf_assets metadata")
	}

	targetRouter, targetStore, targetArtifactDir, _ := newBackupTestAPI(t)
	targetInvoicePDFDir := filepath.Join(filepath.Dir(targetArtifactDir), "invoices")
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", exported.Body.Bytes(), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview invoice PDF package = %d: %s", preview.Code, preview.Body.String())
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", exported.Body.Bytes(),
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation},
	)
	if apply.Code != http.StatusOK {
		t.Fatalf("apply invoice PDF package = %d: %s", apply.Code, apply.Body.String())
	}
	var targetAsset models.InvoicePDFAsset
	if err := targetStore.DB.Where("invoice_id = ?", invoice.ID).Take(&targetAsset).Error; err != nil {
		t.Fatalf("load imported invoice PDF metadata: %v", err)
	}
	if targetAsset.ID != sourceAsset.ID || targetAsset.SHA256 != sourceAsset.SHA256 || targetAsset.SizeBytes != sourceAsset.SizeBytes {
		t.Fatalf("imported invoice PDF metadata=%#v source=%#v", targetAsset, sourceAsset)
	}
	targetBytes, err := os.ReadFile(filepath.Join(targetInvoicePDFDir, filepath.FromSlash(targetAsset.RelativePath)))
	if err != nil || !bytes.Equal(targetBytes, sourceBytes) {
		t.Fatalf("imported invoice PDF bytes=%d err=%v", len(targetBytes), err)
	}
	download := performRequest(targetRouter, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), sourceBytes) {
		t.Fatalf("download imported invoice PDF = %d bytes=%d", download.Code, download.Body.Len())
	}
}

func TestInvoicePDFBusinessPackageBlocksCrossStoreControlledFileIDConflict(t *testing.T) {
	sourceRouter, sourceStore, sourceArtifactDir, _ := newBackupTestAPI(t)
	client := createClientForTest(t, sourceRouter, `{"name":"Cross-store PDF Client"}`, nil)
	invoice := createInvoiceForTest(t, sourceRouter, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":6600,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`,
		client.ID,
	), nil)
	_ = generateInvoicePDFForTest(t, sourceRouter, invoice, "invoice-pdf-cross-store-source")
	var sourceAsset models.InvoicePDFAsset
	if err := sourceStore.DB.Where("invoice_id = ?", invoice.ID).Take(&sourceAsset).Error; err != nil {
		t.Fatal(err)
	}
	targetRouter, _, artifactDir, backupDir := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, targetRouter)
	existingBody := []byte("existing object with the incoming invoice PDF id")
	uploaded := performMultipartRequest(
		targetRouter,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Existing output","artifacts":[{"client_ref":"file","storage_kind":"file","name":"existing.txt","file_field":"file"}]}`,
		map[string][]byte{"file": existingBody},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("create target object = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	existingID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	objectsDir := filepath.Join(artifactDir, "objects")
	sourceInvoicePDFDir := filepath.Join(filepath.Dir(sourceArtifactDir), "invoices")
	updatedRelativePath := invoice.ID + "/" + existingID + ".pdf"
	if err := os.Rename(
		filepath.Join(sourceInvoicePDFDir, filepath.FromSlash(sourceAsset.RelativePath)),
		filepath.Join(sourceInvoicePDFDir, filepath.FromSlash(updatedRelativePath)),
	); err != nil {
		t.Fatalf("align source invoice PDF file id fixture: %v", err)
	}
	if err := sourceStore.DB.Exec(
		"UPDATE invoice_pdf_assets SET id = ?, relative_path = ? WHERE id = ?",
		existingID, updatedRelativePath, sourceAsset.ID,
	).Error; err != nil {
		t.Fatalf("align source invoice PDF metadata id fixture: %v", err)
	}
	sourceAsset.ID = existingID
	sourceAsset.RelativePath = updatedRelativePath
	exported := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export invoice PDF business package = %d: %s", exported.Code, exported.Body.String())
	}

	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-package/preview", exported.Body.Bytes(), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("cross-store conflict preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || envelope.Data.CanApply || envelope.Data.Blocker != "target_file_conflicts" || envelope.Data.FileConflicts != 1 || envelope.Data.ApplyMode != "" {
		t.Fatalf("cross-store conflict preview = %#v err=%v", envelope.Data, err)
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-package", exported.Body.Bytes(),
		map[string]string{"X-Import-Confirmation": packageImportAppendConfirmation},
	)
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_FILE_CONFLICT" {
		t.Fatalf("cross-store conflict apply = %d: %s", apply.Code, apply.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("cross-store conflict created rollback backup: %v", backups)
	}
	body, err := os.ReadFile(filepath.Join(objectsDir, sourceAsset.ID))
	if err != nil || !bytes.Equal(body, existingBody) {
		t.Fatalf("existing cross-store object changed body=%q err=%v", body, err)
	}
}
