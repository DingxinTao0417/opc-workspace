package api

const (
	businessImportSchema49 = 49
	businessImportSchema50 = 50
	businessImportSchema51 = 51
	businessImportSchema52 = 52
	businessImportSchema53 = 53
	businessImportSchema54 = 54
	businessImportSchema55 = 55
	businessImportSchema56 = 56
	businessImportSchema57 = 57
	businessImportSchema58 = 58
	businessImportSchema59 = 59
	businessImportSchema60 = 60
	businessImportSchema61 = 61
	businessImportSchema62 = 62
	businessImportSchema63 = 63
	businessImportSchema64 = 64
	businessImportSchema65 = 65
	businessImportSchema66 = 66
	businessImportSchema67 = 67
	businessImportSchema68 = 68
	businessImportSchema69 = 69
	businessImportSchema70 = 70
	businessImportSchema71 = 71
	businessImportSchema72 = 72
	businessImportSchema73 = 73
	businessImportSchema74 = 74
	businessImportSchema75 = 75
	businessImportSchema76 = 76
	businessImportSchema77 = 77
	businessImportSchema78 = 78
	businessImportSchema79 = 79
)

// Schema 50 only adds the operational import-authorization table and schema 51
// only records an immutable migration witness. Schema 52/53 add AI operational
// tables (providers/sessions/generations/messages) that are excluded from the
// portable export surface, schema 54 adds an excluded AI message column,
// schema 55 adds an excluded AI provider column, schema 56 adds the excluded
// ai_memories table, schema 57 adds excluded AI context memory, and schema 58
// adds an excluded AI message context column. Schema 59 adds the excluded local
// knowledge-base tables and FTS shadow tables. Schema 60 adds an excluded AI
// message citation column, and schema 61 adds excluded AI run steps plus an
// excluded AI message generation link. Schema 62 adds excluded Provider token
// usage columns. Schema 63 adds excluded local AI evaluation runs/results, and
// schema 64 only widens their excluded dataset-version constraint, and schema
// 65 adds an excluded evaluation-suite column, and schema 66 adds excluded
// append-only local evaluation review records. Schema 67 only widens the
// excluded evaluation/review suite constraint.
// None changes exported table columns. Keep the
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

var businessExportExcludedTablesSchema65 = []string{
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
	"business_import_project_completion_authorizations",
	"ai_providers",
	"ai_sessions",
	"ai_generations",
	"ai_messages",
	"ai_memories",
	"ai_memory_entries",
	"ai_run_steps",
	"ai_evaluation_runs",
	"ai_evaluation_results",
	"knowledge_sources",
	"knowledge_documents",
	"knowledge_chunks",
	"knowledge_index_jobs",
	"knowledge_chunks_fts",
	"knowledge_chunks_fts_data",
	"knowledge_chunks_fts_idx",
	"knowledge_chunks_fts_content",
	"knowledge_chunks_fts_docsize",
	"knowledge_chunks_fts_config",
}

var businessExportExcludedTablesSchema67 = []string{
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
	"business_import_project_completion_authorizations",
	"ai_providers",
	"ai_sessions",
	"ai_generations",
	"ai_messages",
	"ai_memories",
	"ai_memory_entries",
	"ai_run_steps",
	"ai_evaluation_runs",
	"ai_evaluation_results",
	"ai_evaluation_reviews",
	"knowledge_sources",
	"knowledge_documents",
	"knowledge_chunks",
	"knowledge_index_jobs",
	"knowledge_chunks_fts",
	"knowledge_chunks_fts_data",
	"knowledge_chunks_fts_idx",
	"knowledge_chunks_fts_content",
	"knowledge_chunks_fts_docsize",
	"knowledge_chunks_fts_config",
}

var businessExportExcludedTablesSchema71 = []string{
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
	"business_import_project_completion_authorizations",
	"ai_providers",
	"ai_sessions",
	"ai_generations",
	"ai_messages",
	"ai_memories",
	"ai_memory_entries",
	"ai_run_steps",
	"ai_evaluation_runs",
	"ai_evaluation_results",
	"ai_evaluation_reviews",
	"knowledge_sources",
	"knowledge_documents",
	"knowledge_chunks",
	"knowledge_index_jobs",
	"knowledge_chunks_fts",
	"knowledge_chunks_fts_data",
	"knowledge_chunks_fts_idx",
	"knowledge_chunks_fts_content",
	"knowledge_chunks_fts_docsize",
	"knowledge_chunks_fts_config",
	"agent_runs",
}

// Frozen schema 072–075 list; newer conversation tables must not rewrite it.
var businessExportExcludedTablesSchema75 = []string{
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
	"business_import_project_completion_authorizations",
	"ai_providers",
	"ai_sessions",
	"ai_generations",
	"ai_messages",
	"ai_memories",
	"ai_memory_entries",
	"ai_run_steps",
	"ai_action_proposals",
	"ai_evaluation_runs",
	"ai_evaluation_results",
	"ai_evaluation_reviews",
	"knowledge_sources",
	"knowledge_documents",
	"knowledge_chunks",
	"knowledge_index_jobs",
	"knowledge_chunks_fts",
	"knowledge_chunks_fts_data",
	"knowledge_chunks_fts_idx",
	"knowledge_chunks_fts_content",
	"knowledge_chunks_fts_docsize",
	"knowledge_chunks_fts_config", "agent_runs",
}

// Frozen schema 076 list: authorizations and turn ledgers were not yet present.
var businessExportExcludedTablesSchema76 = []string{
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
	"business_import_project_completion_authorizations",
	"ai_providers",
	"ai_sessions",
	"ai_generations",
	"ai_messages",
	"ai_memories",
	"ai_memory_entries",
	"ai_run_steps",
	"ai_action_proposals",
	"ai_work_plan_revisions",
	"ai_evaluation_runs",
	"ai_evaluation_results",
	"ai_evaluation_reviews",
	"knowledge_sources",
	"knowledge_documents",
	"knowledge_chunks",
	"knowledge_index_jobs",
	"knowledge_chunks_fts",
	"knowledge_chunks_fts_data",
	"knowledge_chunks_fts_idx",
	"knowledge_chunks_fts_content",
	"knowledge_chunks_fts_docsize",
	"knowledge_chunks_fts_config", "agent_runs",
}

// Frozen schema 077 list: saved access-request recommendations did not exist.
var businessExportExcludedTablesSchema77 = func() []string {
	result := make([]string, 0, len(businessExportExcludedTablesSchema76)+2)
	for _, table := range businessExportExcludedTablesSchema76 {
		result = append(result, table)
		if table == "ai_work_plan_revisions" {
			result = append(result, "ai_continuations", "ai_continuation_turns")
		}
	}
	return result
}()

func businessImportSchemaContract(sourceSchema, targetSchema int) ([]string, bool) {
	// 79 only widens the excluded permission recommendation ledger.
	if targetSchema == businessImportSchema79 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema78 {
			return businessExportExcludedTables, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema78)
	}
	// 78 adds only a saved-conversation permission-request recommendation.
	// It is operational state, never a portable business export or grant.
	if targetSchema == businessImportSchema78 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema77 {
			return businessExportExcludedTablesSchema77, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema77)
	}
	// 77 adds only excluded continuation authorization/turn state and changes
	// an excluded message index. Existing portable business rows are unchanged.
	if targetSchema == businessImportSchema77 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema76 {
			return businessExportExcludedTablesSchema76, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema76)
	}
	if targetSchema == businessImportSchema76 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema75 {
			return businessExportExcludedTablesSchema75, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema75)
	}
	// 75 only adds output-delivery state and recovery staging to the excluded
	// Agent Run ledger. The portable business surface is unchanged.
	if targetSchema == businessImportSchema75 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema74 {
			return businessExportExcludedTablesSchema75, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema74)
	}
	// 74 only freezes excluded Agent Run execution identities and adds
	// operational uniqueness guards. The portable business surface is unchanged.
	if targetSchema == businessImportSchema74 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema73 {
			return businessExportExcludedTablesSchema75, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema73)
	}
	// 73 only widens excluded approval JSON capacity, retaining all identities.
	if targetSchema == businessImportSchema73 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema72 {
			return businessExportExcludedTablesSchema75, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema72)
	}
	// 72 adds only excluded AI approval state; business data is unchanged.
	if targetSchema == businessImportSchema72 && sourceSchema != targetSchema {
		if sourceSchema == businessImportSchema71 {
			return businessExportExcludedTablesSchema71, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema71)
	}
	if sourceSchema == targetSchema {
		if sourceSchema == businessImportSchema77 {
			return businessExportExcludedTablesSchema77, true
		}
		if sourceSchema == businessImportSchema76 {
			return businessExportExcludedTablesSchema76, true
		}
		if sourceSchema >= businessImportSchema72 && sourceSchema <= businessImportSchema75 {
			return businessExportExcludedTablesSchema75, true
		}
		if sourceSchema >= businessImportSchema63 && sourceSchema <= businessImportSchema65 {
			return businessExportExcludedTablesSchema65, true
		}
		if sourceSchema >= businessImportSchema66 && sourceSchema <= businessImportSchema70 {
			return businessExportExcludedTablesSchema67, true
		}
		if sourceSchema == businessImportSchema71 {
			return businessExportExcludedTablesSchema71, true
		}
		return businessExportExcludedTables, true
	}
	// 68/69 change only excluded AI operational columns/triggers. Reuse the
	// explicit pre-67 compatibility graph, without admitting unknown schemas.
	if targetSchema == businessImportSchema68 || targetSchema == businessImportSchema69 {
		if sourceSchema == businessImportSchema67 || sourceSchema == businessImportSchema68 {
			return businessExportExcludedTablesSchema67, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema67)
	}
	// 70/71 only widen excluded knowledge-base schema (PDF source type and
	// chunk page columns) and add the excluded agent_runs execution ledger;
	// the portable export surface is unchanged.
	if targetSchema == businessImportSchema70 || targetSchema == businessImportSchema71 {
		if sourceSchema >= businessImportSchema67 && sourceSchema <= businessImportSchema70 {
			return businessExportExcludedTablesSchema67, true
		}
		return businessImportSchemaContract(sourceSchema, businessImportSchema67)
	}
	if sourceSchema == businessImportSchema49 &&
		(targetSchema == businessImportSchema50 || targetSchema == businessImportSchema51 ||
			targetSchema == businessImportSchema52 || targetSchema == businessImportSchema53 ||
			targetSchema == businessImportSchema54 || targetSchema == businessImportSchema55 ||
			targetSchema == businessImportSchema56 || targetSchema == businessImportSchema57 ||
			targetSchema == businessImportSchema58 || targetSchema == businessImportSchema59 ||
			targetSchema == businessImportSchema60 || targetSchema == businessImportSchema61 ||
			targetSchema == businessImportSchema62 || targetSchema == businessImportSchema63 ||
			targetSchema == businessImportSchema64 || targetSchema == businessImportSchema65 ||
			targetSchema == businessImportSchema66 || targetSchema == businessImportSchema67) {
		return businessExportExcludedTablesSchema49, true
	}
	if sourceSchema == businessImportSchema50 && targetSchema == businessImportSchema51 {
		return businessExportExcludedTables, true
	}
	if sourceSchema == businessImportSchema63 &&
		(targetSchema == businessImportSchema64 || targetSchema == businessImportSchema65 ||
			targetSchema == businessImportSchema66 || targetSchema == businessImportSchema67) {
		return businessExportExcludedTablesSchema65, true
	}
	if sourceSchema == businessImportSchema64 &&
		(targetSchema == businessImportSchema65 || targetSchema == businessImportSchema66 ||
			targetSchema == businessImportSchema67) {
		return businessExportExcludedTablesSchema65, true
	}
	if sourceSchema == businessImportSchema65 &&
		(targetSchema == businessImportSchema66 || targetSchema == businessImportSchema67) {
		return businessExportExcludedTablesSchema65, true
	}
	if sourceSchema == businessImportSchema66 && targetSchema == businessImportSchema67 {
		return businessExportExcludedTablesSchema67, true
	}
	return nil, false
}
