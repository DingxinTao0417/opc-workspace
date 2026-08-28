package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBusinessExportReturnsVersionedDeterministicAllowlistedSnapshot(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	for _, fixture := range []struct {
		id   string
		name string
	}{
		{id: "018f0000-0000-7000-8000-000000001702", name: "后创建但靠后"},
		{id: "018f0000-0000-7000-8000-000000001701", name: "先排序"},
	} {
		if err := store.DB.Exec("INSERT INTO clients(id, name) VALUES (?, ?)", fixture.id, fixture.name).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DB.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000001703',
			'018f0000-0000-7000-8000-000000001701',
			'note', 'Exported client note', 'Local activity body',
			'2026-08-28T08:00:00Z', '00000000-0000-5000-8000-000000000001',
			1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z'
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	task, _ := setupManualReviewTask(t, router)
	uploaded := performMultipartRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Export metadata","artifacts":[{"client_ref":"file","storage_kind":"file","name":"export.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte("file bytes stay outside JSON")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit export fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}

	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("X-Export-Format-Version") != "1" ||
		!strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export headers = %v", response.Header())
	}
	var exported businessExportPackage
	if err := json.Unmarshal(response.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if exported.FormatVersion != 1 || exported.Source.SchemaVersion != store.SchemaVersion || exported.Source.APIVersion != Version {
		t.Fatalf("export metadata = %#v", exported)
	}
	if exported.ArtifactFiles.Included || exported.ArtifactFiles.ActiveCount != 1 ||
		exported.ArtifactFiles.ActiveBytes != int64(len("file bytes stay outside JSON")) ||
		len(exported.Tables) != len(businessExportTables) {
		t.Fatalf("export scope = %#v tables=%d", exported.ArtifactFiles, len(exported.Tables))
	}
	tables := make(map[string]businessExportTable, len(exported.Tables))
	for _, table := range exported.Tables {
		tables[table.Name] = table
	}
	clients, ok := tables["clients"]
	if !ok || len(clients.Rows) != 2 || len(clients.Columns) == 0 {
		t.Fatalf("clients export = %#v", clients)
	}
	idColumn := -1
	for index, column := range clients.Columns {
		if column == "id" {
			idColumn = index
			break
		}
	}
	if idColumn < 0 || clients.Rows[0][idColumn] != "018f0000-0000-7000-8000-000000001701" || clients.Rows[1][idColumn] != "018f0000-0000-7000-8000-000000001702" {
		t.Fatalf("clients are not deterministic: %#v", clients.Rows)
	}
	if artifacts := tables["task_artifacts"]; len(artifacts.Rows) != 1 {
		t.Fatalf("Artifact metadata was not exported: %#v", artifacts)
	}
	if activities := tables["client_activities"]; len(activities.Rows) != 1 {
		t.Fatalf("Client activities were not exported: %#v", activities)
	}
	for _, excluded := range businessExportExcludedTables {
		if _, leaked := tables[excluded]; leaked {
			t.Fatalf("operational table %q leaked into export", excluded)
		}
	}
	body := response.Body.String()
	if strings.Contains(body, "file bytes stay outside JSON") || strings.Contains(body, testToken) || strings.Contains(body, `:\\`) || strings.Contains(body, "/tmp/") {
		t.Fatalf("export leaked runtime token or absolute database path")
	}
}

func TestBusinessExportFailsClosedWhenAnAllowlistedTableIsUnavailable(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	if err := store.DB.Exec("DROP TABLE invoices").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if response.Code != http.StatusInternalServerError || responseErrorCode(t, response.Body.Bytes()) != "DATA_EXPORT_FAILED" {
		t.Fatalf("broken export = %d: %s", response.Code, response.Body.String())
	}
}
