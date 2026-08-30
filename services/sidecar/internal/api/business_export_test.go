package api

import (
	"encoding/json"
	"fmt"
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
	if err := store.DB.Exec(`
		INSERT INTO actors(id, type, display_name, status, is_builtin, notes, metadata_json, version, created_at, updated_at)
		VALUES (
			'018f0000-0000-7000-8000-000000001704', 'person', 'Exported Contact', 'active', 0,
			'', '{}', 1, '2026-08-28T08:01:00Z', '2026-08-28T08:01:00Z'
		);
		INSERT INTO client_actor_links(id, client_id, actor_id, role, linked_by_actor_id, linked_at)
		VALUES (
			'018f0000-0000-7000-8000-000000001705',
			'018f0000-0000-7000-8000-000000001701',
			'018f0000-0000-7000-8000-000000001704', 'contact',
			'00000000-0000-5000-8000-000000000001', '2026-08-28T08:02:00Z'
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000001706', 'Exported Project', 'in_progress', 1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z');
		INSERT INTO roadmap_milestones(
			id, title, year, quarter, target_date, status, manual_order,
			version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000001709', 'Exported milestone', 2026, 3,
			'2026-08-28', 'active', 10, 1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z'
		);
		INSERT INTO roadmap_milestone_projects(milestone_id, project_id, linked_at)
		VALUES (
			'018f0000-0000-7000-8000-000000001709',
			'018f0000-0000-7000-8000-000000001706', '2026-08-28T08:00:00Z'
		);
		INSERT INTO project_notes(
			id, project_id, title, body, occurred_at, created_by_actor_id,
			version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000001707',
			'018f0000-0000-7000-8000-000000001706',
			'Exported project note', 'Local project note body',
			'2026-08-28T08:03:00Z', '00000000-0000-5000-8000-000000000001',
			1, '2026-08-28T08:03:00Z', '2026-08-28T08:03:00Z'
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
	task, _ := setupManualReviewTask(t, router)
	if err := store.DB.Exec(`
		INSERT INTO content_items(
			id, title, platform, status, scheduled_at, scheduled_timezone, project_id,
			manual_order, version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000001710', 'Exported content item', 'blog',
			'scheduled', '2026-09-01T08:00:00Z', 'UTC', '018f0000-0000-7000-8000-000000001706',
			10, 1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z'
		);
		INSERT INTO content_item_tasks(content_item_id, task_id, is_required, linked_at)
		VALUES ('018f0000-0000-7000-8000-000000001710', ?, 1, '2026-08-28T08:00:00Z')
	`, task.ID).Error; err != nil {
		t.Fatal(err)
	}
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
	clientAttachmentBody := []byte("client attachment bytes stay outside JSON")
	clientAttachment := performClientAttachmentUpload(
		t, router, "/api/v1/clients/018f0000-0000-7000-8000-000000001702/attachments",
		`{"name":"client-export.txt"}`, "client-export.txt", clientAttachmentBody,
		map[string]string{"If-Match": `"1"`},
	)
	if clientAttachment.Code != http.StatusCreated {
		t.Fatalf("create client attachment export fixture = %d: %s", clientAttachment.Code, clientAttachment.Body.String())
	}
	var projectVersion int64
	if err := store.DB.Table("projects").Where("id = ?", "018f0000-0000-7000-8000-000000001706").Pluck("version", &projectVersion).Error; err != nil {
		t.Fatalf("read project version: %v", err)
	}
	projectAttachmentBody := []byte("project attachment bytes stay outside JSON")
	projectAttachment := performClientAttachmentUpload(
		t, router, "/api/v1/projects/018f0000-0000-7000-8000-000000001706/attachments",
		`{"name":"project-export.txt"}`, "project-export.txt", projectAttachmentBody,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, projectVersion)},
	)
	if projectAttachment.Code != http.StatusCreated {
		t.Fatalf("create project attachment export fixture = %d: %s", projectAttachment.Code, projectAttachment.Body.String())
	}
	if err := store.DB.Exec(`
		INSERT INTO workspace_avatars(
			id, relative_path, extension, mime_type, size_bytes, sha256,
			integrity_status, integrity_checked_at, created_at
		) VALUES (
			'018f0000-0000-7000-8000-000000001708',
			'avatars/018f0000-0000-7000-8000-000000001708.png',
			'png', 'image/png', 4,
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'verified', '2026-08-28T08:04:00Z', '2026-08-28T08:04:00Z'
		)
	`).Error; err != nil {
		t.Fatal(err)
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
	if exported.ArtifactFiles.Included || exported.ArtifactFiles.ActiveCount != 4 ||
		exported.ArtifactFiles.ActiveBytes != int64(len("file bytes stay outside JSON")+len(clientAttachmentBody)+len(projectAttachmentBody)+4) ||
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
	if attachments := tables["client_attachments"]; len(attachments.Rows) != 1 {
		t.Fatalf("Client attachment metadata was not exported: %#v", attachments)
	}
	if attachments := tables["project_attachments"]; len(attachments.Rows) != 1 {
		t.Fatalf("Project attachment metadata was not exported: %#v", attachments)
	}
	if links := tables["client_actor_links"]; len(links.Rows) != 1 {
		t.Fatalf("Client actor links were not exported: %#v", links)
	}
	if notes := tables["project_notes"]; len(notes.Rows) != 1 {
		t.Fatalf("Project notes were not exported: %#v", notes)
	}
	if items := tables["content_items"]; len(items.Rows) != 1 {
		t.Fatalf("Content item was not exported: %#v", items)
	}
	if links := tables["content_item_tasks"]; len(links.Rows) != 1 {
		t.Fatalf("Content item task link was not exported: %#v", links)
	}
	if milestones := tables["roadmap_milestones"]; len(milestones.Rows) != 1 {
		t.Fatalf("Roadmap milestone was not exported: %#v", milestones)
	}
	if links := tables["roadmap_milestone_projects"]; len(links.Rows) != 1 {
		t.Fatalf("Roadmap milestone project link was not exported: %#v", links)
	}
	if avatars := tables["workspace_avatars"]; len(avatars.Rows) != 1 {
		t.Fatalf("Workspace avatar metadata was not exported: %#v", avatars)
	}
	for _, excluded := range businessExportExcludedTables {
		if _, leaked := tables[excluded]; leaked {
			t.Fatalf("operational table %q leaked into export", excluded)
		}
	}
	body := response.Body.String()
	if strings.Contains(body, "file bytes stay outside JSON") || strings.Contains(body, "client attachment bytes stay outside JSON") || strings.Contains(body, "project attachment bytes stay outside JSON") || strings.Contains(body, testToken) || strings.Contains(body, `:\\`) || strings.Contains(body, "/tmp/") {
		t.Fatalf("export leaked runtime token or absolute database path")
	}
}

func TestBusinessExportClassifiesEveryApplicationTableExactlyOnce(t *testing.T) {
	_, store, _, _ := newBackupTestAPI(t)

	var applicationTables []string
	if err := store.DB.Raw(`
		SELECT name
		FROM sqlite_schema
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`).Scan(&applicationTables).Error; err != nil {
		t.Fatalf("list application tables: %v", err)
	}

	classifications := make(map[string]int, len(businessExportTables)+len(businessExportExcludedTables))
	exported := make(map[string]struct{}, len(businessExportTables))
	excluded := make(map[string]struct{}, len(businessExportExcludedTables))
	for _, spec := range businessExportTables {
		classifications[spec.Name]++
		exported[spec.Name] = struct{}{}
	}
	for _, table := range businessExportExcludedTables {
		classifications[table]++
		excluded[table] = struct{}{}
	}

	actual := make(map[string]struct{}, len(applicationTables))
	for _, table := range applicationTables {
		actual[table] = struct{}{}
		if count := classifications[table]; count != 1 {
			t.Errorf("application table %q has %d business export classifications, want exactly 1", table, count)
		}
	}
	for table, count := range classifications {
		if _, exists := actual[table]; !exists {
			t.Errorf("business export classification references missing application table %q", table)
		}
		if count != 1 {
			t.Errorf("business export table %q is classified %d times, want exactly 1", table, count)
		}
	}

	if _, ok := exported["automation_event_deliveries"]; ok {
		t.Error("pending automation event deliveries must not be included in portable business exports")
	}
	if _, ok := excluded["automation_event_deliveries"]; !ok {
		t.Error("pending automation event deliveries must be explicitly classified as operational state")
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
