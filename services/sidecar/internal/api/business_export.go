package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const businessExportFormatVersion = 1

type businessExportTableSpec struct {
	Name    string
	OrderBy string
}

// Keep this list explicit: adding a database table must not silently expose
// operational state or future credentials through the export surface.
var businessExportTables = []businessExportTableSpec{
	{Name: "clients", OrderBy: "id"},
	{Name: "client_activities", OrderBy: "client_id, occurred_at, id"},
	{Name: "client_attachments", OrderBy: "client_id, created_at, id"},
	{Name: "client_actor_links", OrderBy: "client_id, linked_at, id"},
	{Name: "projects", OrderBy: "id"},
	{Name: "project_notes", OrderBy: "project_id, occurred_at, id"},
	{Name: "project_attachments", OrderBy: "project_id, created_at, id"},
	{Name: "tasks", OrderBy: "id"},
	{Name: "tags", OrderBy: "id"},
	{Name: "task_tags", OrderBy: "task_id, tag_id"},
	{Name: "invoices", OrderBy: "id"},
	{Name: "actors", OrderBy: "id"},
	{Name: "task_assignments", OrderBy: "id"},
	{Name: "workflow_events", OrderBy: "id"},
	{Name: "task_submissions", OrderBy: "id"},
	{Name: "task_artifacts", OrderBy: "id"},
	{Name: "focus_sessions", OrderBy: "id"},
	{Name: "focus_session_intervals", OrderBy: "id"},
	{Name: "inbox_items", OrderBy: "id"},
	{Name: "inbox_item_tasks", OrderBy: "inbox_item_id, task_id"},
	{Name: "reminders", OrderBy: "id"},
	{Name: "automation_rules", OrderBy: "id"},
	{Name: "automation_runs", OrderBy: "attempt, started_at, id"},
	{Name: "app_settings", OrderBy: "key"},
	{Name: "workspace_avatars", OrderBy: "created_at, id"},
	{Name: "task_saved_views", OrderBy: "id"},
}

var businessExportExcludedTables = []string{
	"schema_migrations",
	"workspace_identity",
	"idempotency_keys",
	"artifact_deletion_tombstones",
	"client_attachment_deletion_tombstones",
	"project_attachment_deletion_tombstones",
	"workspace_avatar_deletion_tombstones",
	"task_focus_totals",
}

type businessExportTable struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type businessExportSource struct {
	AppVersion    string `json:"app_version"`
	Commit        string `json:"commit"`
	APIVersion    string `json:"api_version"`
	SchemaVersion int    `json:"schema_version"`
}

type businessExportArtifactFiles struct {
	Included    bool  `json:"included"`
	ActiveCount int64 `json:"active_count"`
	ActiveBytes int64 `json:"active_bytes"`
}

type businessExportPackage struct {
	FormatVersion             int                         `json:"format_version"`
	ExportedAt                string                      `json:"exported_at"`
	Source                    businessExportSource        `json:"source"`
	ArtifactFiles             businessExportArtifactFiles `json:"artifact_files"`
	ExcludedOperationalTables []string                    `json:"excluded_operational_tables"`
	Tables                    []businessExportTable       `json:"tables"`
}

func (a *API) exportBusinessData(c *gin.Context) {
	exportedAt := a.options.Now().UTC()
	packageData, err := a.buildBusinessExport(c, exportedAt)
	if err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("business export failed: %v", err)
		}
		writeError(c, http.StatusInternalServerError, "DATA_EXPORT_FAILED", "Business data could not be exported")
		return
	}
	payload, err := json.MarshalIndent(packageData, "", "  ")
	if err != nil {
		writeError(c, http.StatusInternalServerError, "DATA_EXPORT_FAILED", "Business data could not be encoded")
		return
	}
	payload = append(payload, '\n')
	filename := "opc-workspace-business-" + exportedAt.Format("20060102T150405Z") + ".json"
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Format-Version", "1")
	c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
}

func (a *API) buildBusinessExport(c *gin.Context, exportedAt time.Time) (businessExportPackage, error) {
	result := businessExportPackage{
		FormatVersion: businessExportFormatVersion,
		ExportedAt:    exportedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		Source: businessExportSource{
			AppVersion:    a.options.AppVersion,
			Commit:        a.options.Commit,
			APIVersion:    Version,
			SchemaVersion: a.options.SchemaVersion,
		},
		ArtifactFiles:             businessExportArtifactFiles{Included: false},
		ExcludedOperationalTables: append([]string(nil), businessExportExcludedTables...),
		Tables:                    make([]businessExportTable, 0, len(businessExportTables)),
	}
	err := a.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var schemaVersion int
		if err := tx.Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Row().Scan(&schemaVersion); err != nil {
			return fmt.Errorf("read export schema version: %w", err)
		}
		if schemaVersion != a.options.SchemaVersion {
			return fmt.Errorf("export schema version mismatch: database=%d runtime=%d", schemaVersion, a.options.SchemaVersion)
		}
		if err := tx.Raw(`
			SELECT COUNT(*), COALESCE(SUM(size_bytes), 0)
			FROM (
				SELECT size_bytes
				FROM task_artifacts
				WHERE storage_kind = 'file' AND deleted_at IS NULL
				UNION ALL
				SELECT size_bytes
				FROM client_attachments
				WHERE deleted_at IS NULL
				UNION ALL
				SELECT size_bytes
				FROM project_attachments
				WHERE deleted_at IS NULL
				UNION ALL
				SELECT size_bytes
				FROM workspace_avatars
				WHERE deleted_at IS NULL
			)
		`).Row().Scan(&result.ArtifactFiles.ActiveCount, &result.ArtifactFiles.ActiveBytes); err != nil {
			return fmt.Errorf("read export Artifact summary: %w", err)
		}
		for _, spec := range businessExportTables {
			table, err := readBusinessExportTable(tx, spec)
			if err != nil {
				return err
			}
			result.Tables = append(result.Tables, table)
		}
		return nil
	})
	return result, err
}

func readBusinessExportTable(tx *gorm.DB, spec businessExportTableSpec) (businessExportTable, error) {
	rows, err := tx.Raw("SELECT * FROM " + spec.Name + " ORDER BY " + spec.OrderBy).Rows()
	if err != nil {
		return businessExportTable{}, fmt.Errorf("query export table %s: %w", spec.Name, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return businessExportTable{}, fmt.Errorf("read export columns %s: %w", spec.Name, err)
	}
	result := businessExportTable{Name: spec.Name, Columns: columns, Rows: make([][]any, 0)}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return businessExportTable{}, fmt.Errorf("scan export row %s: %w", spec.Name, err)
		}
		for index, value := range values {
			if raw, ok := value.([]byte); ok {
				if !utf8.Valid(raw) {
					return businessExportTable{}, errors.New("business export contains non-UTF-8 database content")
				}
				values[index] = string(bytes.Clone(raw))
			}
		}
		result.Rows = append(result.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return businessExportTable{}, fmt.Errorf("iterate export table %s: %w", spec.Name, err)
	}
	return result, nil
}
