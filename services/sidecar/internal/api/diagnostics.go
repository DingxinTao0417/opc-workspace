package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"runtime"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const diagnosticPackageFormatVersion = 1

type diagnosticPackageManifest struct {
	FormatVersion int      `json:"format_version"`
	GeneratedAt   string   `json:"generated_at"`
	Files         []string `json:"files"`
	Privacy       []string `json:"privacy"`
}

type diagnosticRuntimeSnapshot struct {
	AppVersion    string `json:"app_version"`
	Commit        string `json:"commit"`
	APIVersion    string `json:"api_version"`
	SchemaVersion int    `json:"schema_version"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	GoVersion     string `json:"go_version"`
}

type diagnosticMigration struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	AppliedAt string `json:"applied_at"`
}

type diagnosticDatabaseSnapshot struct {
	QuickCheckOK bool                  `json:"quick_check_ok"`
	ForeignKeys  bool                  `json:"foreign_keys"`
	JournalMode  string                `json:"journal_mode"`
	PageCount    int64                 `json:"page_count"`
	FreePages    int64                 `json:"free_pages"`
	Migrations   []diagnosticMigration `json:"migrations"`
}

type diagnosticMaintenanceSummary struct {
	FailureCode    string `json:"failure_code"`
	Status         string `json:"status"`
	Count          int64  `json:"count"`
	LastOccurredAt string `json:"last_occurred_at"`
}

func (a *API) downloadDiagnosticPackage(c *gin.Context) {
	generatedAt := a.options.Now().UTC()
	payload, err := a.buildDiagnosticPackage(c.Request.Context(), generatedAt)
	if err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("diagnostic package failed: %v", err)
		}
		writeError(c, http.StatusInternalServerError, "DIAGNOSTIC_PACKAGE_FAILED", "Diagnostic package could not be generated")
		return
	}
	filename := "opc-workspace-diagnostics-" + generatedAt.Format("20060102T150405Z") + ".zip"
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Diagnostic-Format-Version", "1")
	c.Data(http.StatusOK, "application/zip", payload)
}

func (a *API) buildDiagnosticPackage(ctx context.Context, generatedAt time.Time) ([]byte, error) {
	db := a.db.WithContext(ctx)
	database, err := readDiagnosticDatabase(db)
	if err != nil {
		return nil, err
	}
	maintenance, err := readDiagnosticMaintenance(db)
	if err != nil {
		return nil, err
	}
	files := []string{"manifest.json", "runtime.json", "database.json", "maintenance.json"}
	manifest := diagnosticPackageManifest{
		FormatVersion: diagnosticPackageFormatVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339Nano),
		Files:         files,
		Privacy: []string{
			"No session tokens, listener addresses, or absolute local paths are included.",
			"No client, project, task, artifact, invoice, or message content is included.",
			"Raw application logs are not included in diagnostic package format v1.",
		},
	}
	runtimeSnapshot := diagnosticRuntimeSnapshot{
		AppVersion: a.options.AppVersion, Commit: a.options.Commit, APIVersion: Version,
		SchemaVersion: a.options.SchemaVersion, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		GoVersion: runtime.Version(),
	}
	contents := map[string]any{
		"manifest.json": manifest, "runtime.json": runtimeSnapshot,
		"database.json": database, "maintenance.json": maintenance,
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, name := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(generatedAt.UTC())
		entry, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("create diagnostic entry: %w", err)
		}
		encoded, err := json.MarshalIndent(contents[name], "", "  ")
		if err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("encode diagnostic entry: %w", err)
		}
		encoded = append(encoded, '\n')
		if _, err := entry.Write(encoded); err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("write diagnostic entry: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close diagnostic archive: %w", err)
	}
	return buffer.Bytes(), nil
}

func readDiagnosticDatabase(db *gorm.DB) (diagnosticDatabaseSnapshot, error) {
	result := diagnosticDatabaseSnapshot{Migrations: make([]diagnosticMigration, 0)}
	var quickCheck string
	if err := db.Raw("PRAGMA quick_check").Row().Scan(&quickCheck); err != nil {
		return result, fmt.Errorf("run database quick check: %w", err)
	}
	result.QuickCheckOK = quickCheck == "ok"
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Row().Scan(&foreignKeys); err != nil {
		return result, fmt.Errorf("read foreign key mode: %w", err)
	}
	result.ForeignKeys = foreignKeys == 1
	if err := db.Raw("PRAGMA journal_mode").Row().Scan(&result.JournalMode); err != nil {
		return result, fmt.Errorf("read journal mode: %w", err)
	}
	if err := db.Raw("PRAGMA page_count").Row().Scan(&result.PageCount); err != nil {
		return result, fmt.Errorf("read page count: %w", err)
	}
	if err := db.Raw("PRAGMA freelist_count").Row().Scan(&result.FreePages); err != nil {
		return result, fmt.Errorf("read free page count: %w", err)
	}
	if err := db.Raw("SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC").
		Scan(&result.Migrations).Error; err != nil {
		return result, fmt.Errorf("read migrations: %w", err)
	}
	return result, nil
}

func readDiagnosticMaintenance(db *gorm.DB) ([]diagnosticMaintenanceSummary, error) {
	rows := make([]diagnosticMaintenanceSummary, 0)
	if err := db.Raw(`
		SELECT json_extract(payload_json, '$.failure_code') AS failure_code,
		       status, COUNT(*) AS count,
		       MAX(json_extract(payload_json, '$.occurred_at')) AS last_occurred_at
		FROM inbox_items
		WHERE source_entity_type = 'system_maintenance'
		GROUP BY failure_code, status
	`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read maintenance summary: %w", err)
	}
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].FailureCode != rows[right].FailureCode {
			return rows[left].FailureCode < rows[right].FailureCode
		}
		return rows[left].Status < rows[right].Status
	})
	return rows, nil
}
