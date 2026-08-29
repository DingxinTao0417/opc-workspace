package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxBusinessImportBytes = 16 * 1024 * 1024
	importConfirmation     = "replace-empty-workspace"
)

type businessImportPreview struct {
	FormatVersion int            `json:"format_version"`
	SchemaVersion int            `json:"schema_version"`
	ExportedAt    string         `json:"exported_at"`
	TableCounts   map[string]int `json:"table_counts"`
	TotalRows     int            `json:"total_rows"`
	CanApply      bool           `json:"can_apply"`
	Blocker       string         `json:"blocker,omitempty"`
}

type businessImportResult struct {
	ImportedRows int    `json:"imported_rows"`
	BackupID     string `json:"backup_id"`
}

type businessImportError struct {
	status  int
	code    string
	message string
}

func (e *businessImportError) Error() string { return e.message }

func (a *API) previewBusinessImport(c *gin.Context) {
	packageData, err := decodeBusinessImport(c)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	preview, err := a.validateBusinessImport(c, packageData)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

func (a *API) applyBusinessImport(c *gin.Context) {
	if strings.TrimSpace(c.GetHeader("X-Import-Confirmation")) != importConfirmation {
		writeError(c, http.StatusPreconditionRequired, "IMPORT_CONFIRMATION_REQUIRED", "Explicit business import confirmation is required")
		return
	}
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are required before import")
		return
	}
	packageData, err := decodeBusinessImport(c)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}

	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	preview, err := a.validateBusinessImport(c, packageData)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	if !preview.CanApply {
		writeError(c, http.StatusConflict, "IMPORT_TARGET_NOT_EMPTY", "Business data can only be imported into an empty workspace")
		return
	}

	note := "自动导入前回滚备份"
	backup, err := a.backupStore.create(
		a.db.WithContext(c.Request.Context()), a.options, note, "", sha256Hex([]byte(note)),
	)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "IMPORT_BACKUP_FAILED", "A verified rollback backup could not be created; existing data was not changed")
		return
	}
	if err := a.replaceBusinessTables(c, packageData); err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("business import failed after rollback backup backup_id=%s: %v", backup.ID, err)
		}
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_APPLY_FAILED", "Business data failed integrity validation and was not imported")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": businessImportResult{ImportedRows: preview.TotalRows, BackupID: backup.ID}})
}

func decodeBusinessImport(c *gin.Context) (businessExportPackage, error) {
	limited := http.MaxBytesReader(c.Writer, c.Request.Body, maxBusinessImportBytes)
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var packageData businessExportPackage
	if err := decoder.Decode(&packageData); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			return businessExportPackage{}, &businessImportError{http.StatusRequestEntityTooLarge, "IMPORT_TOO_LARGE", "Business import must not exceed 16 MiB"}
		}
		return businessExportPackage{}, &businessImportError{http.StatusBadRequest, "INVALID_IMPORT_JSON", "The import file is not valid business export JSON"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return businessExportPackage{}, &businessImportError{http.StatusBadRequest, "INVALID_IMPORT_JSON", "The import file must contain exactly one JSON document"}
	}
	return packageData, nil
}

func (a *API) validateBusinessImport(c *gin.Context, packageData businessExportPackage) (businessImportPreview, error) {
	return a.validateBusinessImportData(c, packageData, false)
}

func (a *API) validateBusinessImportWithFiles(c *gin.Context, packageData businessExportPackage) (businessImportPreview, error) {
	return a.validateBusinessImportData(c, packageData, true)
}

func (a *API) validateBusinessImportData(c *gin.Context, packageData businessExportPackage, allowControlledFiles bool) (businessImportPreview, error) {
	if packageData.FormatVersion != businessExportFormatVersion || packageData.Source.APIVersion != Version || packageData.Source.SchemaVersion != a.options.SchemaVersion {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_VERSION_UNSUPPORTED", "The export format, API, or schema version is not compatible"}
	}
	if !allowControlledFiles && (packageData.ArtifactFiles.Included || packageData.ArtifactFiles.ActiveCount != 0 || packageData.ArtifactFiles.ActiveBytes != 0) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_FILES_UNSUPPORTED", "JSON import does not support workspaces with controlled files"}
	}
	if allowControlledFiles && (!packageData.ArtifactFiles.Included || packageData.ArtifactFiles.ActiveCount < 0 || packageData.ArtifactFiles.ActiveBytes < 0) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The controlled-file package summary is invalid"}
	}
	if !equalStrings(packageData.ExcludedOperationalTables, businessExportExcludedTables) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The operational-table exclusion manifest is invalid"}
	}
	if len(packageData.Tables) != len(businessExportTables) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The import table manifest is incomplete"}
	}

	counts := make(map[string]int, len(packageData.Tables))
	total := 0
	for index, spec := range businessExportTables {
		table := packageData.Tables[index]
		if table.Name != spec.Name {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The import table order or name is invalid"}
		}
		columns, err := tableColumns(a.db.WithContext(c), spec.Name)
		if err != nil || !equalStrings(table.Columns, columns) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "The import table columns do not match the current schema"}
		}
		for _, row := range table.Rows {
			if len(row) != len(columns) {
				return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An import row does not match its table columns"}
			}
			for _, value := range row {
				switch value.(type) {
				case nil, string, bool, json.Number:
				default:
					return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "Import rows may only contain scalar JSON values"}
				}
			}
		}
		if !allowControlledFiles && tableContainsControlledFiles(table) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_FILES_UNSUPPORTED", "JSON import cannot restore controlled-file metadata"}
		}
		if table.Name == "focus_sessions" && tableHasValuesOutside(table, "status", "completed", "cancelled", "interrupted") {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ACTIVE_FOCUS_UNSUPPORTED", "Stop or cancel the active Focus Session before exporting"}
		}
		counts[table.Name] = len(table.Rows)
		total += len(table.Rows)
	}

	empty, err := businessTargetEmpty(a.db.WithContext(c))
	if err != nil {
		return businessImportPreview{}, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The target workspace could not be checked"}
	}
	preview := businessImportPreview{
		FormatVersion: packageData.FormatVersion, SchemaVersion: packageData.Source.SchemaVersion,
		ExportedAt: packageData.ExportedAt, TableCounts: counts, TotalRows: total, CanApply: empty,
	}
	if !empty {
		preview.Blocker = "target_not_empty"
	}
	return preview, nil
}

func (a *API) replaceBusinessTables(c *gin.Context, packageData businessExportPackage) error {
	return a.replaceBusinessTablesWithValidation(c, packageData, nil)
}

func (a *API) replaceBusinessTablesWithValidation(c *gin.Context, packageData businessExportPackage, validate func(*gorm.DB) error) error {
	return a.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("PRAGMA defer_foreign_keys = ON").Error; err != nil {
			return err
		}
		tables := make(map[string]businessExportTable, len(packageData.Tables))
		for _, table := range packageData.Tables {
			tables[table.Name] = table
		}
		if err := importActorRows(tx, tables["actors"]); err != nil {
			return err
		}
		order := []string{
			"clients", "projects", "task_submissions", "tasks", "tags", "task_tags", "invoices",
			"task_assignments", "task_artifacts", "workflow_events",
			"client_activities", "client_attachments", "client_actor_links", "project_notes", "project_attachments",
			"focus_sessions", "focus_session_intervals", "inbox_items", "inbox_item_tasks",
			"reminders", "workspace_avatars", "app_settings", "task_saved_views",
		}
		for _, name := range order {
			var err error
			if name == "tasks" {
				err = insertTaskImportRows(tx, tables[name])
			} else {
				err = insertBusinessImportRows(tx, tables[name])
			}
			if err != nil {
				return err
			}
		}
		if err := tx.Exec("DELETE FROM task_focus_totals").Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO task_focus_totals(task_id, exact_seconds, applied_minutes, updated_at)
			SELECT task_id, SUM(accumulated_seconds), SUM(accumulated_seconds) / 60, MAX(updated_at)
			FROM focus_sessions
			WHERE status = 'completed' AND task_id IS NOT NULL
			GROUP BY task_id
		`).Error; err != nil {
			return err
		}
		var foreignKeyFailures int
		if err := tx.Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Row().Scan(&foreignKeyFailures); err != nil || foreignKeyFailures != 0 {
			return fmt.Errorf("foreign key validation failed: count=%d err=%w", foreignKeyFailures, err)
		}
		var quickCheck string
		if err := tx.Raw("PRAGMA quick_check").Row().Scan(&quickCheck); err != nil || quickCheck != "ok" {
			return fmt.Errorf("database quick check failed: result=%s err=%w", quickCheck, err)
		}
		if validate != nil {
			if err := validate(tx); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertTaskImportRows(tx *gorm.DB, table businessExportTable) error {
	parentIndex := columnIndex(table.Columns, "parent_task_id")
	idIndex := columnIndex(table.Columns, "id")
	if parentIndex < 0 || idIndex < 0 {
		return errors.New("task import columns are incomplete")
	}
	pending := append([][]any(nil), table.Rows...)
	inserted := make(map[string]struct{}, len(pending))
	for len(pending) > 0 {
		next := make([][]any, 0, len(pending))
		progress := false
		for _, row := range pending {
			parent, hasParent := row[parentIndex].(string)
			if hasParent && parent != "" {
				if _, ok := inserted[parent]; !ok {
					next = append(next, row)
					continue
				}
			}
			if err := insertBusinessImportRows(tx, businessExportTable{Name: table.Name, Columns: table.Columns, Rows: [][]any{row}}); err != nil {
				return err
			}
			id, ok := row[idIndex].(string)
			if !ok || id == "" {
				return errors.New("task id is invalid")
			}
			inserted[id] = struct{}{}
			progress = true
		}
		if !progress {
			return errors.New("task parent hierarchy is invalid")
		}
		pending = next
	}
	return nil
}

func importActorRows(tx *gorm.DB, table businessExportTable) error {
	isBuiltinIndex := columnIndex(table.Columns, "is_builtin")
	idIndex := columnIndex(table.Columns, "id")
	typeIndex := columnIndex(table.Columns, "type")
	if isBuiltinIndex < 0 || idIndex < 0 || typeIndex < 0 {
		return errors.New("actor import columns are incomplete")
	}
	personRows := make([][]any, 0)
	builtinCount := 0
	var ownerRow []any
	for _, row := range table.Rows {
		builtin, ok := importInteger(row[isBuiltinIndex])
		if !ok {
			return errors.New("actor is_builtin is invalid")
		}
		if builtin == 0 {
			personRows = append(personRows, row)
			continue
		}
		id, idOK := row[idIndex].(string)
		actorType, typeOK := row[typeIndex].(string)
		if !idOK || !typeOK || (id != "00000000-0000-5000-8000-000000000001" && id != "00000000-0000-5000-8000-000000000002") ||
			(id == "00000000-0000-5000-8000-000000000001" && actorType != "owner") ||
			(id == "00000000-0000-5000-8000-000000000002" && actorType != "system") {
			return errors.New("builtin actor identity is invalid")
		}
		builtinCount++
		if actorType == "owner" {
			ownerRow = row
		}
	}
	if builtinCount != 2 {
		return errors.New("builtin actor manifest is incomplete")
	}
	table.Rows = personRows
	if err := insertBusinessImportRows(tx, table); err != nil {
		return err
	}
	if ownerRow == nil {
		return errors.New("owner actor manifest is missing")
	}
	displayName := ownerRow[columnIndex(table.Columns, "display_name")]
	version := normalizeImportValues([]any{ownerRow[columnIndex(table.Columns, "version")]})[0]
	updatedAt := ownerRow[columnIndex(table.Columns, "updated_at")]
	return tx.Exec(`
		UPDATE actors SET display_name = ?, version = ?, updated_at = ?
		WHERE id = '00000000-0000-5000-8000-000000000001'
	`, displayName, version, updatedAt).Error
}

func columnIndex(columns []string, name string) int {
	for index, column := range columns {
		if column == name {
			return index
		}
	}
	return -1
}

func importInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case int64:
		return typed, true
	default:
		return 0, false
	}
}

func insertBusinessImportRows(tx *gorm.DB, table businessExportTable) error {
	if len(table.Rows) == 0 {
		return nil
	}
	columns := make([]string, len(table.Columns))
	placeholders := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		columns[index] = quoteIdentifier(column)
		placeholders[index] = "?"
	}
	statement := "INSERT INTO " + quoteIdentifier(table.Name) + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	for _, row := range table.Rows {
		if err := tx.Exec(statement, normalizeImportValues(row)...).Error; err != nil {
			return fmt.Errorf("insert import table %s: %w", table.Name, err)
		}
	}
	return nil
}

func normalizeImportValues(row []any) []any {
	values := make([]any, len(row))
	for index, value := range row {
		if number, ok := value.(json.Number); ok {
			if integer, err := number.Int64(); err == nil {
				values[index] = integer
			} else if decimal, err := number.Float64(); err == nil {
				values[index] = decimal
			} else {
				values[index] = number.String()
			}
		} else {
			values[index] = value
		}
	}
	return values
}

func tableColumns(db *gorm.DB, name string) ([]string, error) {
	rows, err := db.Raw("PRAGMA table_info(" + quoteIdentifier(name) + ")").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make([]string, 0)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var column, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func businessTargetEmpty(db *gorm.DB) (bool, error) {
	for _, spec := range businessExportTables {
		query := "SELECT COUNT(*) FROM " + quoteIdentifier(spec.Name)
		if spec.Name == "actors" {
			query += " WHERE is_builtin = 0"
		}
		var count int64
		if err := db.Raw(query).Row().Scan(&count); err != nil {
			return false, err
		}
		if count != 0 {
			return false, nil
		}
	}
	return true, nil
}

func tableContainsControlledFiles(table businessExportTable) bool {
	if table.Name == "client_attachments" || table.Name == "project_attachments" || table.Name == "workspace_avatars" {
		return len(table.Rows) != 0
	}
	if table.Name != "task_artifacts" {
		return false
	}
	return tableHasValuesOutside(table, "storage_kind", "text", "link", "structured")
}

func tableHasValuesOutside(table businessExportTable, column string, allowed ...string) bool {
	columnIndex := -1
	for index, candidate := range table.Columns {
		if candidate == column {
			columnIndex = index
			break
		}
	}
	if columnIndex < 0 {
		return len(table.Rows) != 0
	}
	for _, row := range table.Rows {
		value, ok := row[columnIndex].(string)
		if !ok {
			return true
		}
		matched := false
		for _, candidate := range allowed {
			if value == candidate {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func writeBusinessImportError(c *gin.Context, err error) {
	var importErr *businessImportError
	if errors.As(err, &importErr) {
		writeError(c, importErr.status, importErr.code, importErr.message)
		return
	}
	writeError(c, http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The business import could not be checked")
}
