package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessPackageExportIncludesVerifiedControlledFiles(t *testing.T) {
	router, store, artifactDir, _ := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	fileBody := []byte("portable controlled file")
	uploaded := performMultipartRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Package export","artifacts":[{"client_ref":"file","storage_kind":"file","name":"portable.txt","file_field":"file"}]}`,
		map[string][]byte{"file": fileBody},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit package fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID

	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export package = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/zip" ||
		response.Header().Get("X-Business-Package-Format-Version") != "1" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(response.Header().Get("Content-Disposition"), "opc-workspace-business-files-") {
		t.Fatalf("package headers = %v", response.Header())
	}
	entries := readBusinessPackageEntries(t, response.Body.Bytes())
	wantFilePath := "files/objects/" + artifactID
	if len(entries) != 3 || entries["manifest.json"] == nil || entries["business-data.json"] == nil || entries[wantFilePath] == nil {
		t.Fatalf("package entries = %v", mapKeys(entries))
	}
	if !bytes.Equal(entries[wantFilePath], fileBody) {
		t.Fatalf("controlled file body = %q", entries[wantFilePath])
	}

	var manifest businessPackageManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode package manifest: %v", err)
	}
	businessHash := sha256.Sum256(entries["business-data.json"])
	fileHash := sha256.Sum256(fileBody)
	if manifest.FormatVersion != businessPackageFormatVersion || manifest.Source.SchemaVersion != store.SchemaVersion ||
		manifest.BusinessData.Path != "business-data.json" || manifest.BusinessData.SizeBytes != int64(len(entries["business-data.json"])) ||
		manifest.BusinessData.SHA256 != hex.EncodeToString(businessHash[:]) || manifest.FileCount != 1 ||
		manifest.FileBytes != int64(len(fileBody)) || manifest.TotalBytes != int64(len(entries["business-data.json"])+len(fileBody)) ||
		len(manifest.Files) != 1 || manifest.Files[0].ID != artifactID || manifest.Files[0].Path != wantFilePath ||
		manifest.Files[0].SizeBytes != int64(len(fileBody)) || manifest.Files[0].SHA256 != hex.EncodeToString(fileHash[:]) {
		t.Fatalf("package manifest = %#v", manifest)
	}
	var exported businessExportPackage
	if err := json.Unmarshal(entries["business-data.json"], &exported); err != nil {
		t.Fatalf("decode packaged business data: %v", err)
	}
	if !exported.ArtifactFiles.Included || exported.ArtifactFiles.ActiveCount != 1 || exported.ArtifactFiles.ActiveBytes != int64(len(fileBody)) {
		t.Fatalf("packaged Artifact scope = %#v", exported.ArtifactFiles)
	}
	metadata := string(entries["manifest.json"]) + string(entries["business-data.json"])
	if strings.Contains(metadata, testToken) || strings.Contains(metadata, artifactDir) || strings.Contains(metadata, `:\\`) || strings.Contains(metadata, "/tmp/") || strings.Contains(metadata, artifactStoreMarkerName) {
		t.Fatal("package metadata leaked runtime secrets or local absolute paths")
	}
}

func TestBusinessPackageExportContainsOnlyMetadataFilesWhenWorkspaceHasNoControlledFiles(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("empty export package = %d: %s", response.Code, response.Body.String())
	}
	entries := readBusinessPackageEntries(t, response.Body.Bytes())
	if len(entries) != 2 || entries["manifest.json"] == nil || entries["business-data.json"] == nil {
		t.Fatalf("empty package entries = %v", mapKeys(entries))
	}
}

func TestBusinessPackageExportFailsClosedAndCleansStagingFileWhenControlledFileChanges(t *testing.T) {
	router, _, artifactDir, backupDir := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	uploaded := performMultipartRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Tamper fixture","artifacts":[{"client_ref":"file","storage_kind":"file","name":"tamper.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte("original")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit tamper fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	if err := os.WriteFile(filepath.Join(artifactDir, "objects", artifactID), []byte("modified"), 0o600); err != nil {
		t.Fatalf("tamper controlled file: %v", err)
	}

	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if response.Code != http.StatusInternalServerError || responseErrorCode(t, response.Body.Bytes()) != "DATA_PACKAGE_EXPORT_FAILED" {
		t.Fatalf("tampered package export = %d: %s", response.Code, response.Body.String())
	}
	staging, err := filepath.Glob(filepath.Join(backupDir, ".business-export-*.zip"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("package staging files = %v err=%v", staging, err)
	}
}

func readBusinessPackageEntries(t *testing.T, payload []byte) map[string][]byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("open package zip: %v", err)
	}
	entries := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		if _, exists := entries[file.Name]; exists {
			t.Fatalf("duplicate package entry %q", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open package entry %q: %v", file.Name, err)
		}
		body, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read package entry %q: read=%v close=%v", file.Name, readErr, closeErr)
		}
		entries[file.Name] = body
	}
	return entries
}

func mapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
