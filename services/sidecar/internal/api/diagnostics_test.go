package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticPackageIsVersionedAndExcludesBusinessAndRuntimeSecrets(t *testing.T) {
	router, store, artifactDir, _ := newBackupTestAPI(t)
	databasePath := filepath.Join(filepath.Dir(artifactDir), "workspace.db")
	const sensitiveName = "Private Client Diagnostic Canary"
	if err := store.DB.Exec(
		"INSERT INTO clients(id, name, notes) VALUES (?, ?, ?)",
		"018f0000-0000-7000-8000-000000009901", sensitiveName, "secret business body",
	).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(router, http.MethodGet, "/api/v1/diagnostics/package", nil, nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" ||
		response.Header().Get("X-Diagnostic-Format-Version") != "1" ||
		!strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment") ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("diagnostic response=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatalf("open diagnostic archive: %v", err)
	}
	entries := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open diagnostic entry %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read diagnostic entry %s: %v", file.Name, err)
		}
		entries[file.Name] = data
	}
	wantNames := []string{"manifest.json", "runtime.json", "database.json", "maintenance.json"}
	if len(entries) != len(wantNames) {
		t.Fatalf("diagnostic entries=%v", entries)
	}
	for _, name := range wantNames {
		if _, exists := entries[name]; !exists {
			t.Fatalf("missing diagnostic entry %s", name)
		}
	}
	var decodedContent strings.Builder
	for _, data := range entries {
		decodedContent.Write(data)
	}
	allContent := decodedContent.String()
	for _, secret := range []string{sensitiveName, "secret business body", testToken, databasePath, "127.0.0.1"} {
		if strings.Contains(allContent, secret) {
			t.Fatalf("diagnostic archive leaked %q", secret)
		}
	}
	var manifest diagnosticPackageManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil ||
		manifest.FormatVersion != 1 || len(manifest.Privacy) != 3 {
		t.Fatalf("diagnostic manifest=%#v err=%v", manifest, err)
	}
	var database diagnosticDatabaseSnapshot
	if err := json.Unmarshal(entries["database.json"], &database); err != nil ||
		!database.QuickCheckOK || !database.ForeignKeys || len(database.Migrations) == 0 {
		t.Fatalf("diagnostic database=%#v err=%v", database, err)
	}
}
