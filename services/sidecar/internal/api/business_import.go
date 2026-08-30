package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxBusinessImportBytes       = 16 * 1024 * 1024
	importReplaceConfirmation    = "replace-empty-workspace"
	importAppendConfirmation     = "append-to-existing-workspace"
	importModeReplaceEmpty       = "replace_empty"
	importModeAppend             = "append"
	importBlockerTargetConflicts = "target_key_conflicts"
)

type businessImportPreview struct {
	FormatVersion       int                           `json:"format_version"`
	SchemaVersion       int                           `json:"schema_version"`
	TargetSchemaVersion int                           `json:"target_schema_version"`
	ExportedAt          string                        `json:"exported_at"`
	TableCounts         map[string]int                `json:"table_counts"`
	TotalRows           int                           `json:"total_rows"`
	TargetRows          int                           `json:"target_rows"`
	KeyConflicts        int                           `json:"key_conflicts"`
	ConflictTables      []businessImportTableConflict `json:"conflict_tables"`
	CanApply            bool                          `json:"can_apply"`
	ApplyMode           string                        `json:"apply_mode,omitempty"`
	Blocker             string                        `json:"blocker,omitempty"`
}

type businessImportTableConflict struct {
	Table        string `json:"table"`
	IncomingRows int    `json:"incoming_rows"`
	TargetRows   int    `json:"target_rows"`
	KeyConflicts int    `json:"key_conflicts"`
}

type businessImportResult struct {
	ImportedRows int    `json:"imported_rows"`
	BackupID     string `json:"backup_id"`
	ApplyMode    string `json:"apply_mode"`
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
	confirmation := strings.TrimSpace(c.GetHeader("X-Import-Confirmation"))
	if confirmation != importReplaceConfirmation && confirmation != importAppendConfirmation {
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
	unlockInvoicePDFs := a.lockInvoicePDFStore()
	defer unlockInvoicePDFs()
	preview, err := a.validateBusinessImport(c, packageData)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	if !preview.CanApply {
		writeBusinessImportBlocker(c, preview.Blocker)
		return
	}
	if !validBusinessImportConfirmation(confirmation, preview.ApplyMode, false) {
		writeError(c, http.StatusPreconditionRequired, "IMPORT_CONFIRMATION_REQUIRED", "The confirmation does not match the current import mode")
		return
	}
	if err := a.backupStore.requireCreateCapacity(a.db.WithContext(c.Request.Context()), a.options, 0); err != nil {
		writeImportRollbackCapacityError(c, err)
		return
	}

	note := "自动导入前回滚备份"
	if preview.ApplyMode == importModeAppend {
		note = "自动追加导入前回滚备份"
	}
	backup, err := a.backupStore.create(
		a.db.WithContext(c.Request.Context()), a.options, note, "", sha256Hex([]byte(note)),
	)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "IMPORT_BACKUP_FAILED", "A verified rollback backup could not be created; existing data was not changed")
		return
	}
	if err := a.applyBusinessTables(c, packageData, preview.ApplyMode, nil); err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("business import failed after rollback backup backup_id=%s: %v", backup.ID, err)
		}
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_APPLY_FAILED", "Business data failed integrity validation and was not imported")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": businessImportResult{ImportedRows: preview.TotalRows, BackupID: backup.ID, ApplyMode: preview.ApplyMode}})
}

func validBusinessImportConfirmation(confirmation, applyMode string, withFiles bool) bool {
	if withFiles {
		return applyMode == importModeReplaceEmpty && confirmation == packageImportReplaceConfirmation ||
			applyMode == importModeAppend && confirmation == packageImportAppendConfirmation
	}
	return applyMode == importModeReplaceEmpty && confirmation == importReplaceConfirmation ||
		applyMode == importModeAppend && confirmation == importAppendConfirmation
}

func writeImportRollbackCapacityError(c *gin.Context, err error) {
	if errors.Is(err, errBackupSpaceInsufficient) {
		writeError(c, http.StatusInsufficientStorage, "IMPORT_BACKUP_SPACE_INSUFFICIENT", "There is not enough storage space to create the required rollback backup; business data was not changed")
		return
	}
	writeError(c, http.StatusServiceUnavailable, "IMPORT_BACKUP_CAPACITY_UNAVAILABLE", "Rollback backup storage capacity could not be confirmed; business data was not changed")
}

func writeBusinessImportBlocker(c *gin.Context, blocker string) {
	switch blocker {
	case "source_schema_older", "source_schema_newer":
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_VERSION_UNSUPPORTED", "The source schema cannot be applied by this version")
	case importBlockerTargetConflicts:
		writeError(c, http.StatusConflict, "IMPORT_TARGET_CONFLICT", "Incoming business keys conflict with the current workspace")
	case "target_file_conflicts":
		writeError(c, http.StatusConflict, "IMPORT_TARGET_FILE_CONFLICT", "Incoming controlled files conflict with the current workspace")
	default:
		writeError(c, http.StatusConflict, "IMPORT_TARGET_CONFLICT", "The business import cannot be safely appended to the current workspace")
	}
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
	if packageData.FormatVersion != businessExportFormatVersion || packageData.Source.APIVersion != Version || packageData.Source.SchemaVersion < 1 {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_VERSION_UNSUPPORTED", "The export format, API, or schema version is not compatible"}
	}
	if !allowControlledFiles && (packageData.ArtifactFiles.Included || packageData.ArtifactFiles.ActiveCount != 0 || packageData.ArtifactFiles.ActiveBytes != 0) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_FILES_UNSUPPORTED", "JSON import does not support workspaces with controlled files"}
	}
	if allowControlledFiles && (!packageData.ArtifactFiles.Included || packageData.ArtifactFiles.ActiveCount < 0 || packageData.ArtifactFiles.ActiveBytes < 0) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The controlled-file package summary is invalid"}
	}
	counts, total, err := inspectBusinessImportEnvelope(packageData)
	if err != nil {
		return businessImportPreview{}, err
	}
	if packageData.Source.SchemaVersion != a.options.SchemaVersion {
		blocker := "source_schema_older"
		if packageData.Source.SchemaVersion > a.options.SchemaVersion {
			blocker = "source_schema_newer"
		}
		return businessImportPreview{
			FormatVersion: packageData.FormatVersion, SchemaVersion: packageData.Source.SchemaVersion,
			TargetSchemaVersion: a.options.SchemaVersion, ExportedAt: packageData.ExportedAt,
			TableCounts: counts, TotalRows: total, ConflictTables: []businessImportTableConflict{},
			CanApply: false, Blocker: blocker,
		}, nil
	}
	if !equalStrings(packageData.ExcludedOperationalTables, businessExportExcludedTables) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The operational-table exclusion manifest is invalid"}
	}
	if len(packageData.Tables) != len(businessExportTables) {
		return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The import table manifest is incomplete"}
	}

	for index, spec := range businessExportTables {
		table := packageData.Tables[index]
		if table.Name != spec.Name {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The import table order or name is invalid"}
		}
		columns, err := tableColumns(a.db.WithContext(c), spec.Name)
		if err != nil || !equalStrings(table.Columns, columns) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "The import table columns do not match the current schema"}
		}
		if !allowControlledFiles && tableContainsControlledFiles(table) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_FILES_UNSUPPORTED", "JSON import cannot restore controlled-file metadata"}
		}
		if table.Name == "focus_sessions" && tableHasValuesOutside(table, "status", "completed", "cancelled", "interrupted") {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ACTIVE_FOCUS_UNSUPPORTED", "Stop or cancel the active Focus Session before exporting"}
		}
		if table.Name == "reminders" && tableHasInvalidReminderRecurrence(table) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "A Reminder recurrence rule is invalid"}
		}
		if table.Name == "automation_rules" && tableHasInvalidAutomationRules(table) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An Automation preset configuration is invalid"}
		}
		if table.Name == "agent_adapters" && tableHasInvalidAgentAdapters(table) {
			return businessImportPreview{}, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An Agent Adapter manifest or diagnostic state is invalid"}
		}
	}

	report, err := buildBusinessImportConflictReport(a.db.WithContext(c), packageData)
	if err != nil {
		var importErr *businessImportError
		if errors.As(err, &importErr) {
			return businessImportPreview{}, importErr
		}
		return businessImportPreview{}, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The target workspace could not be checked"}
	}
	preview := businessImportPreview{
		FormatVersion: packageData.FormatVersion, SchemaVersion: packageData.Source.SchemaVersion,
		TargetSchemaVersion: a.options.SchemaVersion, ExportedAt: packageData.ExportedAt,
		TableCounts: counts, TotalRows: total, TargetRows: report.TargetRows,
		KeyConflicts: report.KeyConflicts, ConflictTables: report.Tables,
	}
	if report.KeyConflicts != 0 {
		preview.Blocker = importBlockerTargetConflicts
		return preview, nil
	}
	preview.CanApply = true
	preview.ApplyMode = importModeReplaceEmpty
	if report.TargetRows != 0 {
		preview.ApplyMode = importModeAppend
	}
	return preview, nil
}

func inspectBusinessImportEnvelope(packageData businessExportPackage) (map[string]int, int, error) {
	if _, err := time.Parse(time.RFC3339Nano, packageData.ExportedAt); err != nil {
		return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "The export time is invalid"}
	}
	counts := make(map[string]int, len(packageData.Tables))
	total := 0
	for _, table := range packageData.Tables {
		if strings.TrimSpace(table.Name) == "" {
			return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "An import table name is invalid"}
		}
		if _, duplicate := counts[table.Name]; duplicate {
			return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID", "Import table names must be unique"}
		}
		seenColumns := make(map[string]struct{}, len(table.Columns))
		for _, column := range table.Columns {
			if strings.TrimSpace(column) == "" {
				return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "An import table column is invalid"}
			}
			if _, duplicate := seenColumns[column]; duplicate {
				return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_SCHEMA_MISMATCH", "Import table columns must be unique"}
			}
			seenColumns[column] = struct{}{}
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "An import row does not match its table columns"}
			}
			for _, value := range row {
				switch value.(type) {
				case nil, string, bool, json.Number:
				default:
					return nil, 0, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "Import rows may only contain scalar JSON values"}
				}
			}
		}
		counts[table.Name] = len(table.Rows)
		total += len(table.Rows)
	}
	return counts, total, nil
}

type businessImportConflictReport struct {
	TargetRows   int
	KeyConflicts int
	Tables       []businessImportTableConflict
}

func buildBusinessImportConflictReport(db *gorm.DB, packageData businessExportPackage) (businessImportConflictReport, error) {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	report := businessImportConflictReport{Tables: make([]businessImportTableConflict, 0)}
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, spec := range businessExportTables {
			primaryKeys, err := tablePrimaryKeyColumns(tx, spec.Name)
			if err != nil || len(primaryKeys) == 0 {
				return fmt.Errorf("read import conflict key %s: %w", spec.Name, err)
			}
			incoming := tables[spec.Name]
			keyIndexes := make([]int, len(primaryKeys))
			for index, column := range primaryKeys {
				keyIndexes[index] = columnIndex(incoming.Columns, column)
				if keyIndexes[index] < 0 {
					return fmt.Errorf("import conflict key %s.%s is missing", spec.Name, column)
				}
			}
			incomingKeys := make(map[string]struct{}, len(incoming.Rows))
			for _, row := range incoming.Rows {
				values := make([]any, len(keyIndexes))
				for index, rowIndex := range keyIndexes {
					values[index] = row[rowIndex]
				}
				key, err := canonicalBusinessImportKey(values)
				if err != nil {
					return err
				}
				if _, duplicate := incomingKeys[key]; duplicate {
					return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "Import rows contain a duplicate primary key"}
				}
				incomingKeys[key] = struct{}{}
			}
			targetRows, conflicts, err := scanBusinessImportTargetKeys(tx, spec.Name, primaryKeys, incomingKeys)
			if err != nil {
				return err
			}
			if targetRows == 0 {
				continue
			}
			report.TargetRows += targetRows
			report.KeyConflicts += conflicts
			report.Tables = append(report.Tables, businessImportTableConflict{
				Table: spec.Name, IncomingRows: len(incoming.Rows), TargetRows: targetRows, KeyConflicts: conflicts,
			})
		}
		return nil
	})
	return report, err
}

func tablePrimaryKeyColumns(db *gorm.DB, table string) ([]string, error) {
	rows, err := db.Raw("PRAGMA table_info(" + quoteIdentifier(table) + ")").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type keyColumn struct {
		name  string
		order int
	}
	keys := make([]keyColumn, 0)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var column, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		if primaryKey > 0 {
			keys = append(keys, keyColumn{name: column, order: primaryKey})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left].order < keys[right].order })
	result := make([]string, len(keys))
	for index, key := range keys {
		result[index] = key.name
	}
	return result, nil
}

func scanBusinessImportTargetKeys(db *gorm.DB, table string, primaryKeys []string, incomingKeys map[string]struct{}) (int, int, error) {
	columns := make([]string, len(primaryKeys))
	for index, column := range primaryKeys {
		columns[index] = quoteIdentifier(column)
	}
	query := "SELECT " + strings.Join(columns, ",") + " FROM " + quoteIdentifier(table)
	if filter := businessImportTargetFilter(table); filter != "" {
		query += " WHERE " + filter
	}
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	targetRows := 0
	conflicts := 0
	for rows.Next() {
		values := make([]any, len(primaryKeys))
		destinations := make([]any, len(values))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return 0, 0, err
		}
		key, err := canonicalBusinessImportKey(values)
		if err != nil {
			return 0, 0, err
		}
		targetRows++
		if _, exists := incomingKeys[key]; exists {
			conflicts++
		}
	}
	return targetRows, conflicts, rows.Err()
}

func canonicalBusinessImportKey(values []any) (string, error) {
	for index, value := range values {
		if raw, ok := value.([]byte); ok {
			values[index] = string(raw)
		}
	}
	payload, err := json.Marshal(normalizeImportValues(values))
	return string(payload), err
}

func businessImportTargetFilter(table string) string {
	switch table {
	case "actors":
		return "is_builtin = 0"
	case "automation_rules":
		return "enabled = 1 OR version <> 1"
	default:
		return ""
	}
}

func (a *API) replaceBusinessTables(c *gin.Context, packageData businessExportPackage) error {
	return a.applyBusinessTables(c, packageData, importModeReplaceEmpty, nil)
}

func (a *API) replaceBusinessTablesWithValidation(c *gin.Context, packageData businessExportPackage, validate func(*gorm.DB) error) error {
	return a.applyBusinessTables(c, packageData, importModeReplaceEmpty, validate)
}

func (a *API) applyBusinessTables(c *gin.Context, packageData businessExportPackage, applyMode string, validate func(*gorm.DB) error) error {
	if applyMode != importModeReplaceEmpty && applyMode != importModeAppend {
		return errors.New("business import apply mode is invalid")
	}
	return a.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("PRAGMA defer_foreign_keys = ON").Error; err != nil {
			return err
		}
		tables := make(map[string]businessExportTable, len(packageData.Tables))
		for _, table := range packageData.Tables {
			tables[table.Name] = table
		}
		if err := importActorRows(tx, tables["actors"], applyMode == importModeAppend); err != nil {
			return err
		}
		if applyMode == importModeReplaceEmpty {
			if err := tx.Exec("DELETE FROM automation_rules").Error; err != nil {
				return err
			}
		} else if err := mergeAutomationImportRows(tx, tables["automation_rules"]); err != nil {
			return err
		}
		order := []string{
			"clients", "projects", "roadmap_milestones", "roadmap_milestone_projects", "task_submissions", "tasks", "content_items", "content_item_tasks", "tags", "task_tags", "invoices", "invoice_pdf_assets", "financial_entries",
			"task_assignments", "task_artifacts", "workflow_events",
			"client_activities", "client_attachments", "client_actor_links", "client_followups", "project_notes", "project_attachments",
			"focus_sessions", "focus_session_intervals", "inbox_items", "inbox_item_tasks",
			"reminders", "automation_runs", "agent_adapters",
			"workspace_avatars", "app_settings", "task_saved_views",
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
		if applyMode == importModeReplaceEmpty {
			if err := insertBusinessImportRows(tx, tables["automation_rules"]); err != nil {
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
		if err := restoreImportedProjectVersions(tx, tables["projects"]); err != nil {
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

func restoreImportedProjectVersions(tx *gorm.DB, table businessExportTable) error {
	if len(table.Rows) == 0 {
		return nil
	}
	idIndex := columnIndex(table.Columns, "id")
	versionIndex := columnIndex(table.Columns, "version")
	updatedAtIndex := columnIndex(table.Columns, "updated_at")
	if idIndex < 0 || versionIndex < 0 || updatedAtIndex < 0 {
		return errors.New("project import aggregate columns are incomplete")
	}
	for _, row := range table.Rows {
		id, ok := row[idIndex].(string)
		version, versionOK := importInteger(row[versionIndex])
		updatedAt, updatedAtOK := row[updatedAtIndex].(string)
		if !ok || id == "" || !versionOK || version < 1 || !updatedAtOK || updatedAt == "" {
			return errors.New("project import aggregate values are invalid")
		}
		result := tx.Exec("UPDATE projects SET version = ?, updated_at = ? WHERE id = ?", version, updatedAt, id)
		if result.Error != nil {
			return fmt.Errorf("restore imported project aggregate: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return errors.New("restore imported project aggregate did not match its project")
		}
	}
	return nil
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

func importActorRows(tx *gorm.DB, table businessExportTable, preserveTargetOwner bool) error {
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
	if preserveTargetOwner {
		return nil
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

func mergeAutomationImportRows(tx *gorm.DB, table businessExportTable) error {
	if len(table.Rows) == 0 {
		return nil
	}
	idIndex := columnIndex(table.Columns, "id")
	if idIndex < 0 {
		return errors.New("automation rule import columns are incomplete")
	}
	assignments := make([]string, 0, len(table.Columns)-1)
	for _, column := range table.Columns {
		if column != "id" && column != "preset_key" && column != "created_at" {
			quoted := quoteIdentifier(column)
			assignments = append(assignments, quoted+" = excluded."+quoted)
		}
	}
	columns := make([]string, len(table.Columns))
	placeholders := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		columns[index] = quoteIdentifier(column)
		placeholders[index] = "?"
	}
	statement := "INSERT INTO " + quoteIdentifier(table.Name) + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ") " +
		"ON CONFLICT(id) DO UPDATE SET " + strings.Join(assignments, ",") + " WHERE automation_rules.enabled = 0 AND automation_rules.version = 1"
	for _, row := range table.Rows {
		if _, ok := row[idIndex].(string); !ok {
			return errors.New("automation rule id is invalid")
		}
		result := tx.Exec(statement, normalizeImportValues(row)...)
		if result.Error != nil {
			return fmt.Errorf("merge import table %s: %w", table.Name, result.Error)
		}
		if result.RowsAffected != 1 {
			return errors.New("automation rule changed after import preflight")
		}
	}
	return nil
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
		} else if spec.Name == "automation_rules" {
			query += " WHERE enabled = 1 OR version <> 1"
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
	if table.Name == "client_attachments" || table.Name == "project_attachments" || table.Name == "workspace_avatars" || table.Name == "invoice_pdf_assets" {
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

func tableHasInvalidReminderRecurrence(table businessExportTable) bool {
	indexes := make(map[string]int, len(table.Columns))
	for index, column := range table.Columns {
		indexes[column] = index
	}
	required := []string{"series_id", "recurrence_type", "recurrence_interval", "recurrence_timezone", "occurrence_number", "recurrence_anchor_day"}
	for _, column := range required {
		if _, ok := indexes[column]; !ok {
			return len(table.Rows) != 0
		}
	}
	for _, row := range table.Rows {
		seriesID, seriesOK := row[indexes["series_id"]].(string)
		recurrenceType, typeOK := row[indexes["recurrence_type"]].(string)
		timezone, timezoneOK := row[indexes["recurrence_timezone"]].(string)
		interval, intervalOK := businessImportInt64(row[indexes["recurrence_interval"]])
		occurrence, occurrenceOK := businessImportInt64(row[indexes["occurrence_number"]])
		anchorDay, anchorOK := businessImportInt64(row[indexes["recurrence_anchor_day"]])
		if !seriesOK || strings.TrimSpace(seriesID) == "" || !typeOK || !timezoneOK ||
			!intervalOK || interval > 365 || !occurrenceOK || occurrence < 1 || !anchorOK ||
			validateReminderRecurrence(recurrenceType, int(interval), timezone, int(anchorDay)) != nil {
			return true
		}
	}
	return false
}

func tableHasInvalidAutomationRules(table businessExportTable) bool {
	indexes := make(map[string]int, len(table.Columns))
	for index, column := range table.Columns {
		indexes[column] = index
	}
	required := []string{"id", "preset_key", "enabled", "config_json", "next_run_at", "version"}
	for _, column := range required {
		if _, ok := indexes[column]; !ok {
			return true
		}
	}
	if len(table.Rows) != len(automationPresets) {
		return true
	}
	seen := make(map[string]struct{}, len(table.Rows))
	for _, row := range table.Rows {
		id, idOK := row[indexes["id"]].(string)
		presetKey, keyOK := row[indexes["preset_key"]].(string)
		configJSON, configOK := row[indexes["config_json"]].(string)
		enabled, enabledOK := businessImportInt64(row[indexes["enabled"]])
		version, versionOK := businessImportInt64(row[indexes["version"]])
		preset, exists := automationPresetByKey(presetKey)
		if !idOK || !keyOK || !configOK || !enabledOK || (enabled != 0 && enabled != 1) ||
			!versionOK || version < 1 || !exists || preset.ID != id {
			return true
		}
		if _, duplicate := seen[presetKey]; duplicate {
			return true
		}
		seen[presetKey] = struct{}{}
		if _, err := decodeAutomationConfig(presetKey, configJSON); err != nil {
			return true
		}
		nextValue := row[indexes["next_run_at"]]
		if enabled == 0 || preset.TriggerType == "event" {
			if nextValue != nil {
				return true
			}
		} else {
			next, ok := nextValue.(string)
			if !ok {
				return true
			}
			if _, err := time.Parse(time.RFC3339Nano, next); err != nil {
				return true
			}
		}
		if enabled == 1 && !preset.Available {
			return true
		}
	}
	return len(seen) != len(automationPresets)
}

func tableHasInvalidAgentAdapters(table businessExportTable) bool {
	indexes := make(map[string]int, len(table.Columns))
	for index, column := range table.Columns {
		indexes[column] = index
	}
	required := []string{
		"id", "adapter_key", "kind", "display_name", "executable_ref", "manifest_json", "protocol_version",
		"status", "health_status", "health_error_code", "isolation_status", "execution_ready", "last_health_at", "version",
	}
	for _, column := range required {
		if _, ok := indexes[column]; !ok {
			return true
		}
	}
	if len(table.Rows) > len(agentAdapterPresets) {
		return true
	}
	seen := make(map[string]struct{}, len(table.Rows))
	for _, row := range table.Rows {
		key, keyOK := row[indexes["adapter_key"]].(string)
		preset, exists := agentAdapterPresetByKey(key)
		manifest, err := json.Marshal(preset.Manifest)
		status, statusOK := row[indexes["status"]].(string)
		health, healthOK := row[indexes["health_status"]].(string)
		isolation, isolationOK := row[indexes["isolation_status"]].(string)
		ready, readyOK := businessImportInt64(row[indexes["execution_ready"]])
		version, versionOK := businessImportInt64(row[indexes["version"]])
		id, idOK := row[indexes["id"]].(string)
		kind, kindOK := row[indexes["kind"]].(string)
		displayName, displayOK := row[indexes["display_name"]].(string)
		executableRef, executableOK := row[indexes["executable_ref"]].(string)
		manifestJSON, manifestOK := row[indexes["manifest_json"]].(string)
		protocol, protocolOK := row[indexes["protocol_version"]].(string)
		if !keyOK || !exists || err != nil || !statusOK || status != "disabled" || !healthOK ||
			!isolationOK || isolation != "unverified" || !readyOK || ready != 0 || !versionOK || version != 1 ||
			!idOK || id != preset.ID || !kindOK || kind != "builtin" || !displayOK || displayName != preset.DisplayName ||
			!executableOK || executableRef != preset.ExecutableRef || !manifestOK || manifestJSON != string(manifest) ||
			!protocolOK || protocol != agentAdapterProtocolVersion {
			return true
		}
		if _, duplicate := seen[key]; duplicate {
			return true
		}
		seen[key] = struct{}{}
		healthError := row[indexes["health_error_code"]]
		lastHealth := row[indexes["last_health_at"]]
		switch health {
		case "unknown":
			if healthError != nil || lastHealth != nil {
				return true
			}
		case "blocked":
			code, codeOK := healthError.(string)
			timestamp, timestampOK := lastHealth.(string)
			if !codeOK || code != agentAdapterIsolationBlockedCode || !timestampOK {
				return true
			}
			if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func businessImportInt64(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	return parsed, err == nil
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
