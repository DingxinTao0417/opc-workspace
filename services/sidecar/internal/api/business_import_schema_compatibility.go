package api

const (
	businessImportSchema49 = 49
	businessImportSchema50 = 50
)

// Schema 50 only adds the operational import-authorization table and replaces
// a guard trigger. It does not change the exported table list or any exported
// table column. Keeping the schema-49 exclusion manifest explicit makes this a
// single, reviewable compatibility edge instead of a general cross-version
// import bypass.
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
	if sourceSchema == businessImportSchema49 && targetSchema == businessImportSchema50 {
		return businessExportExcludedTablesSchema49, true
	}
	return nil, false
}
