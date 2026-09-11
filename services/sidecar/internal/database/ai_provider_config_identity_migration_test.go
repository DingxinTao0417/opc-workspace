package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIConfigurationIdentityMigrationPreservesLegacyAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configuration-identity.db")
	old := openDatabaseAtVersion(t, path, 67)
	defer old.Close()
	const providerID = "018f0000-0000-7000-8000-000000009801"
	const runID = "018f0000-0000-7000-8000-000000009802"
	const reviewID = "018f0000-0000-7000-8000-000000009803"
	statements := []string{
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,last_health_at,version,created_at,updated_at) VALUES ('` + providerID + `','Legacy','local','openai_chat','http://127.0.0.1:11434/v1','model','ready','healthy','2026-09-09T08:00:00Z',7,'2026-09-09T08:00:00Z','2026-09-09T08:00:00Z')`,
		`INSERT INTO ai_evaluation_runs(id,provider_id,provider_name_snapshot,provider_model_snapshot,provider_protocol_snapshot,provider_version,dataset_version,status,total_cases,created_at,updated_at) VALUES ('` + runID + `','` + providerID + `','Legacy','model','openai_chat',7,3,'queued',24,'2026-09-09T08:00:00Z','2026-09-09T08:00:00Z')`,
		`INSERT INTO ai_evaluation_reviews(id,provider_id_snapshot,provider_name_snapshot,provider_model_snapshot,dataset_version,suite_key,provider_version_min,provider_version_max,group_last_completed_at,run_count,total_cases,passed_cases,failed_cases,overall_wilson_lower_bps,minimum_category,minimum_category_wilson_lower_bps,readiness_status,readiness_reasons,critical_failure_codes,decision,reason,reviewed_by_actor_id,reviewed_by_actor_name_snapshot,created_at) VALUES ('` + reviewID + `','` + providerID + `','Legacy','model',3,'full',5,7,'2026-09-09T08:00:00Z',3,72,72,0,9493,'grounded',8620,'insufficient_evidence','["PROVIDER_VERSION_MIXED"]','[]','needs_more_evidence','历史审计内容原样保留','00000000-0000-5000-8000-000000000001','Owner','2026-09-09T09:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// Capture every old column and compare byte/value-for-value after migration.
	snapshot := func(db *sql.DB, table string, columns []string) ([]any, []string) {
		t.Helper()
		query := "SELECT * FROM " + table
		if len(columns) > 0 {
			query = "SELECT " + strings.Join(columns, ",") + " FROM " + table
		}
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err = rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("missing old row")
		}
		values, targets := make([]any, len(columns)), make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		return values, columns
	}
	before, columns := snapshot(old, "ai_evaluation_reviews", nil)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, _ := snapshot(store.SQL, "ai_evaluation_reviews", columns)
	for index := range before {
		if before[index] != after[index] {
			t.Fatalf("audit column %s changed: %v -> %v", columns[index], before[index], after[index])
		}
	}
	copyColumns := append([]string(nil), columns...)
	for index, column := range copyColumns {
		switch column {
		case "id":
			copyColumns[index] = "'018f0000-0000-7000-8000-000000009804'"
		case "critical_failure_codes":
			copyColumns[index] = `'["CONTROL_BLOCK_LEAKED","CITATION_NOT_ALLOWED","FORBIDDEN_PHRASE_PRESENT","FACT_CONTRADICTED"]'`
		}
	}
	copyQuery := "INSERT INTO ai_evaluation_reviews(" + strings.Join(columns, ",") + ",provider_config_version) SELECT " + strings.Join(copyColumns, ",") + ",2 FROM ai_evaluation_reviews WHERE id=?"
	if _, err := store.SQL.Exec(copyQuery, reviewID); err != nil {
		t.Fatalf("four bounded critical signals rejected: %v", err)
	}
	var config int64
	if err := store.SQL.QueryRow("SELECT config_version FROM ai_providers WHERE id=?", providerID).Scan(&config); err != nil || config != 1 {
		t.Fatalf("provider baseline config=%d err=%v", config, err)
	}
	for _, table := range []string{"ai_evaluation_runs", "ai_evaluation_reviews"} {
		var identity sql.NullInt64
		if err := store.SQL.QueryRow("SELECT provider_config_version FROM " + table + " WHERE provider_config_version IS NULL").Scan(&identity); err != nil || identity.Valid {
			t.Fatalf("legacy %s identity invented: %#v err=%v", table, identity, err)
		}
	}
	for _, statement := range []string{
		"UPDATE ai_evaluation_runs SET provider_config_version=1 WHERE id='" + runID + "'",
		"UPDATE ai_evaluation_reviews SET provider_config_version=1 WHERE id='" + reviewID + "'",
		"DELETE FROM ai_evaluation_reviews WHERE id='" + reviewID + "'",
		"UPDATE ai_providers SET config_version=0 WHERE id='" + providerID + "'",
		"UPDATE ai_providers SET config_version=3 WHERE id='" + providerID + "'",
	} {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid identity/audit mutation accepted: %s", statement)
		}
	}
	var invalidFKs int
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&invalidFKs); err != nil || invalidFKs != 0 {
		t.Fatalf("foreign keys=%d %v", invalidFKs, err)
	}
}
