package api

const (
	businessImportSchema49 = 49
	businessImportSchema50 = 50
	businessImportSchema51 = 51
	businessImportSchema52 = 52
	businessImportSchema53 = 53
	businessImportSchema54 = 54
)

// Schema 50 only adds the operational import-authorization table and schema 51
// only records an immutable migration witness. Schema 52/53 add AI operational
// tables (providers/sessions/generations/messages) that are excluded from the
// portable export surface. Neither changes exported table columns. Keep the
// compatibility edges explicit instead of allowing general cross-version
// imports: v49 packages remain importable into every schema that only added
// excluded operational state.
var businessExportExcludedTablesSchema49 = []string{
	"schema_migrations",
	"workspace_identity",
	"idempotency_keys",
	"artifact_deletion_tombstones",
	"client_attachment_deletion_tombstones",
	"project_attachment_deletion_tombstones",
	"workspace_avatar_deletion_tombstones",
	"task_focus_totals",
	"storage_capacity_samples",
	"scheduled_backup_policy",
	"invoice_number_sequences",
	"automation_event_deliveries",
}

func businessImportSchemaContract(sourceSchema, targetSchema int) ([]string, bool) {
	if sourceSchema == targetSchema {
		return businessExportExcludedTables, true
	}
	if sourceSchema == businessImportSchema49 &&
		(targetSchema == businessImportSchema50 || targetSchema == businessImportSchema51 ||
			targetSchema == businessImportSchema52 || targetSchema == businessImportSchema53 ||
			targetSchema == businessImportSchema54) {
		return businessExportExcludedTablesSchema49, true
	}
	if sourceSchema == businessImportSchema50 && targetSchema == businessImportSchema51 {
		return businessExportExcludedTables, true
	}
	return nil, false
}
