package api

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func createBackupForArchiveTest(t *testing.T, router http.Handler) backupSummary {
	t.Helper()
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"archive test"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup = %d: %s", created.Code, created.Body.String())
	}
	return decodeBackupSummary(t, created.Body.Bytes())
}

func archiveTemporaryEntries(t *testing.T, backupDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	var temporary []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".archive-") {
			temporary = append(temporary, entry.Name())
		}
	}
	return temporary
}

func TestBackupArchiveDownloadsExactVerifiedPackage(t *testing.T) {
	runtime, store, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		return 1 << 40, 1 << 40, nil
	})
	task, _ := setupManualReviewTask(t, runtime.Engine)
	uploaded := performMultipartRequest(
		runtime.Engine,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Archive evidence","artifacts":[{"client_ref":"archive","storage_kind":"file","name":"archive.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte("controlled archive file")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("create controlled file = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	summary := createBackupForArchiveTest(t, runtime.Engine)
	manifest, err := readBackupManifest(filepath.Join(backupDir, summary.ID))
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	response := performRequest(runtime.Engine, http.MethodGet, "/api/v1/backups/"+summary.ID+"/archive", nil, map[string]string{
		"Origin":            "tauri://localhost",
		"If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("download archive = %d: %s", response.Code, response.Body.String())
	}
	wantDisposition := fmt.Sprintf("attachment; filename=%q", "opc-workspace-backup-"+summary.ID+".zip")
	if got := response.Header().Get("Content-Type"); got != backupArchiveContentType {
		t.Errorf("Content-Type = %q, want %q", got, backupArchiveContentType)
	}
	if got := response.Header().Get("Content-Disposition"); got != wantDisposition {
		t.Errorf("Content-Disposition = %q, want %q", got, wantDisposition)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("X-Backup-Package-Format-Version"); got != "1" {
		t.Errorf("X-Backup-Package-Format-Version = %q", got)
	}
	if got := response.Header().Get("X-Backup-ID"); got != summary.ID {
		t.Errorf("X-Backup-ID = %q", got)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(response.Body.Len()) {
		t.Errorf("Content-Length = %q, want %d", got, response.Body.Len())
	}
	if got := response.Header().Get("Last-Modified"); got != "" {
		t.Errorf("Last-Modified = %q, want empty to prevent conditional 304 downloads", got)
	}
	for _, exposed := range []string{"Content-Disposition", "X-Backup-Package-Format-Version", "X-Backup-ID"} {
		if !strings.Contains(response.Header().Get("Access-Control-Expose-Headers"), exposed) {
			t.Errorf("Access-Control-Expose-Headers does not expose %s: %q", exposed, response.Header().Get("Access-Control-Expose-Headers"))
		}
	}

	reader, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatalf("open response ZIP: %v", err)
	}
	wantPaths := backupArchivePaths(manifest)
	if !strings.Contains(strings.Join(wantPaths, "\n"), artifactID) {
		t.Fatalf("archive manifest does not contain controlled Artifact %s: %v", artifactID, wantPaths)
	}
	gotPaths := make([]string, 0, len(reader.File))
	extracted := t.TempDir()
	for _, archived := range reader.File {
		gotPaths = append(gotPaths, archived.Name)
		if archived.FileInfo().IsDir() {
			t.Fatalf("archive contains directory entry %q", archived.Name)
		}
		source, err := archived.Open()
		if err != nil {
			t.Fatalf("open archive entry %q: %v", archived.Name, err)
		}
		content, err := io.ReadAll(source)
		closeErr := source.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read archive entry %q: read=%v close=%v", archived.Name, err, closeErr)
		}
		destination := filepath.Join(extracted, filepath.FromSlash(archived.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatalf("create extraction directory: %v", err)
		}
		if err := os.WriteFile(destination, content, 0o600); err != nil {
			t.Fatalf("extract %q: %v", archived.Name, err)
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("archive paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if _, err := (&backupStore{}).verifyPackage(extracted, summary.ID, store.SchemaVersion); err != nil {
		t.Fatalf("extracted archive does not re-verify: %v", err)
	}
	if temporary := archiveTemporaryEntries(t, backupDir); len(temporary) != 0 {
		t.Fatalf("download left temporary archives: %v", temporary)
	}
}

func TestBackupArchiveRejectsInvalidIdentifiersMissingAndCorruptPackages(t *testing.T) {
	router, _, _, backupDir := newBackupTestAPI(t)
	summary := createBackupForArchiveTest(t, router)

	for name, path := range map[string]string{
		"malformed":  "/api/v1/backups/not-a-uuid/archive",
		"uppercase":  "/api/v1/backups/" + strings.ToUpper(summary.ID) + "/archive",
		"whitespace": "/api/v1/backups/%20" + summary.ID + "%20/archive",
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusBadRequest || responseErrorCode(t, response.Body.Bytes()) != "INVALID_ID" {
				t.Fatalf("response = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/backups/018f0000-0000-5000-8000-000000009999/archive", nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "BACKUP_NOT_FOUND" {
		t.Fatalf("missing response = %d: %s", missing.Code, missing.Body.String())
	}

	databasePath := filepath.Join(backupDir, summary.ID, "database", "opc-workspace.db")
	database, err := os.OpenFile(databasePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open backup database for corruption: %v", err)
	}
	if _, err := database.Write([]byte("corrupt")); err != nil {
		_ = database.Close()
		t.Fatalf("corrupt backup database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close corrupt backup database: %v", err)
	}
	corrupt := performRequest(router, http.MethodGet, "/api/v1/backups/"+summary.ID+"/archive", nil, nil)
	if corrupt.Code != http.StatusConflict || responseErrorCode(t, corrupt.Body.Bytes()) != "BACKUP_INVALID" {
		t.Fatalf("corrupt response = %d: %s", corrupt.Code, corrupt.Body.String())
	}
	if temporary := archiveTemporaryEntries(t, backupDir); len(temporary) != 0 {
		t.Fatalf("invalid export left temporary archives: %v", temporary)
	}
}

func TestBackupArchiveCanceledRequestStillCleansTemporaryFile(t *testing.T) {
	runtime, _, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		return 1 << 40, 1 << 40, nil
	})
	summary := createBackupForArchiveTest(t, runtime.Engine)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/backups/"+summary.ID+"/archive", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	runtime.Engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("canceled download response = %d: %s", response.Code, response.Body.String())
	}
	if temporary := archiveTemporaryEntries(t, backupDir); len(temporary) != 0 {
		t.Fatalf("canceled request left temporary archives: %v", temporary)
	}
}

func TestBackupArchiveCapacityFailuresDoNotLeaveTemporaryFiles(t *testing.T) {
	for name, failure := range map[string]struct {
		available uint64
		total     uint64
		err       error
		status    int
		code      string
	}{
		"insufficient": {available: 1, total: 1 << 40, status: http.StatusInsufficientStorage, code: "BACKUP_EXPORT_SPACE_INSUFFICIENT"},
		"unavailable":  {err: errors.New("probe failed"), status: http.StatusServiceUnavailable, code: "BACKUP_EXPORT_CAPACITY_UNAVAILABLE"},
	} {
		t.Run(name, func(t *testing.T) {
			failCapacity := false
			runtime, _, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
				if failCapacity {
					return failure.available, failure.total, failure.err
				}
				return 1 << 40, 1 << 40, nil
			})
			summary := createBackupForArchiveTest(t, runtime.Engine)
			failCapacity = true
			response := performRequest(runtime.Engine, http.MethodGet, "/api/v1/backups/"+summary.ID+"/archive", nil, nil)
			if response.Code != failure.status || responseErrorCode(t, response.Body.Bytes()) != failure.code {
				t.Fatalf("response = %d: %s", response.Code, response.Body.String())
			}
			if temporary := archiveTemporaryEntries(t, backupDir); len(temporary) != 0 {
				t.Fatalf("capacity failure left temporary archives: %v", temporary)
			}
		})
	}
}

func TestBackupArchiveClassifiesFilesystemSpaceErrors(t *testing.T) {
	if !isBackupArchiveSpaceError(&os.PathError{Op: "write", Path: "archive.zip", Err: syscall.ENOSPC}) {
		t.Fatal("ENOSPC was not classified as insufficient export space")
	}
	if runtime.GOOS == "windows" {
		for _, errno := range []syscall.Errno{39, 112} {
			if !isBackupArchiveSpaceError(&os.PathError{Op: "write", Path: "archive.zip", Err: errno}) {
				t.Fatalf("Windows disk-full errno %d was not classified as insufficient export space", errno)
			}
		}
	}
	if isBackupArchiveSpaceError(errors.New("unrelated write failure")) {
		t.Fatal("unrelated write error was classified as insufficient export space")
	}
}

func TestBackupArchivePathsAreStableAndSorted(t *testing.T) {
	manifest := backupManifest{
		Database:       backupManifestFile{Path: "database/opc-workspace.db"},
		ArtifactMarker: backupManifestFile{Path: "artifacts/.opc-artifact-store-v1"},
		Artifacts: []backupManifestArtifact{
			{backupManifestFile: backupManifestFile{Path: "invoices/b/file.pdf"}},
			{backupManifestFile: backupManifestFile{Path: "artifacts/objects/a"}},
		},
	}
	got := backupArchivePaths(manifest)
	wantTail := append([]string(nil), got[3:]...)
	sort.Strings(wantTail)
	if !reflect.DeepEqual(got[3:], wantTail) {
		t.Fatalf("artifact paths are not sorted: %v", got)
	}
}

func TestBackupArchiveCopyRejectsSourceReplacementBetweenLstatAndOpen(t *testing.T) {
	packagePath := t.TempDir()
	relativePath := "database/opc-workspace.db"
	sourcePath := filepath.Join(packagePath, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	content := []byte("same bytes do not make a replacement the same file")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	replacementPath := filepath.Join(packagePath, "replacement.db")
	replacementContent := append([]byte(nil), content...)
	replacementContent[0] ^= 0xff
	if err := os.WriteFile(replacementPath, replacementContent, 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	expected := backupManifestFile{Path: relativePath, SizeBytes: int64(len(content)), SHA256: sha256Hex(content)}

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	err := writeVerifiedBackupArchiveFileWithOpen(zipWriter, packagePath, expected, func(path string) (*os.File, error) {
		if err := os.Rename(path, path+".replaced"); err != nil {
			return nil, err
		}
		if err := os.Rename(replacementPath, path); err != nil {
			return nil, err
		}
		return os.Open(path)
	})
	_ = zipWriter.Close()
	if !errors.Is(err, errBackupArchiveInvalid) {
		t.Fatalf("replacement error = %v, want backup archive invalid", err)
	}
}
