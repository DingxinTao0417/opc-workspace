package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func createClientForTest(t *testing.T, router http.Handler, body string, headers map[string]string) clientResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/clients", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create client status = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeClientResponse(t, recorder.Body.Bytes())
}

func decodeClientResponse(t *testing.T, body []byte) clientResponse {
	t.Helper()
	var envelope struct {
		Data clientResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode client response: %v", err)
	}
	return envelope.Data
}

func TestClientCreateDetailUpdateAndProjectNamePropagation(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	createdRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/clients",
		[]byte(`{
			"name":"  星河工作室  ",
			"contact_name":"  林女士  ",
			"email":"  hello@example.com  ",
			"phone":"  +86 (755) 1234-5678  ",
			"notes":"   ",
			"status":"lead"
		}`),
		nil,
	)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create client = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeClientResponse(t, createdRecorder.Body.Bytes())
	if created.Name != "星河工作室" || created.ContactName == nil || *created.ContactName != "林女士" ||
		created.Email == nil || *created.Email != "hello@example.com" ||
		created.Phone == nil || *created.Phone != "+86 (755) 1234-5678" || created.Notes != nil ||
		created.Status != "lead" || created.ProjectCount != 0 || created.Version != 1 {
		t.Fatalf("created client = %#v", created)
	}
	if createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create ETag = %q", createdRecorder.Header().Get("ETag"))
	}

	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"客户网站","client_id":%q}`, created.ID), nil)
	detailRecorder := performRequest(router, http.MethodGet, "/api/v1/clients/"+created.ID, nil, nil)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("client detail = %d: %s", detailRecorder.Code, detailRecorder.Body.String())
	}
	detail := decodeClientResponse(t, detailRecorder.Body.Bytes())
	if detail.ProjectCount != 1 || detail.Version != 2 || detailRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("detail after project attach = %#v ETag=%q", detail, detailRecorder.Header().Get("ETag"))
	}

	missingVersion := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+created.ID,
		[]byte(`{"name":"新名称"}`), nil,
	)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing If-Match = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	stale := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+created.ID,
		[]byte(`{"name":"新名称"}`), map[string]string{"If-Match": `"1"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale client update = %d: %s", stale.Code, stale.Body.String())
	}

	updatedRecorder := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/clients/"+created.ID,
		[]byte(`{
			"name":"  星河设计  ",
			"contact_name":null,
			"email":"   ",
			"phone":null,
			"notes":"  一行\n二行  ",
			"status":"inactive"
		}`),
		map[string]string{"If-Match": `"2"`},
	)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update client = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeClientResponse(t, updatedRecorder.Body.Bytes())
	if updated.Name != "星河设计" || updated.ContactName != nil || updated.Email != nil || updated.Phone != nil ||
		updated.Notes == nil || *updated.Notes != "一行\n二行" || updated.Status != "inactive" ||
		updated.ProjectCount != 1 || updated.Version != 3 || updatedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("updated client = %#v ETag=%q", updated, updatedRecorder.Header().Get("ETag"))
	}

	projectRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	if projectRecorder.Code != http.StatusOK {
		t.Fatalf("project detail after client rename = %d: %s", projectRecorder.Code, projectRecorder.Body.String())
	}
	updatedProject := decodeProjectResponse(t, projectRecorder.Body.Bytes())
	if updatedProject.ClientName == nil || *updatedProject.ClientName != "星河设计" || updatedProject.Version != project.Version+1 {
		t.Fatalf("project after client rename = %#v", updatedProject)
	}
}

func TestClientCreateIdempotencyReplaysOriginalSnapshotAfterDeletion(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	firstBody := `{"name":"  Snapshot Client  ","contact_name":"  ","status":"active"}`
	headers := map[string]string{"Idempotency-Key": "client-create-snapshot-1"}
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/clients", []byte(firstBody), headers)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create client = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeClientResponse(t, createdRecorder.Body.Bytes())
	if created.ContactName != nil || created.Status != "active" || created.Version != 1 {
		t.Fatalf("created client snapshot = %#v", created)
	}

	canonicalReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/clients",
		[]byte(`{"name":"Snapshot Client","contact_name":null}`),
		headers,
	)
	if canonicalReplay.Code != http.StatusCreated || canonicalReplay.Header().Get("Idempotency-Replayed") != "true" ||
		canonicalReplay.Header().Get("ETag") != `"1"` {
		t.Fatalf("canonical replay = %d headers=%v body=%s", canonicalReplay.Code, canonicalReplay.Header(), canonicalReplay.Body.String())
	}
	if replayed := decodeClientResponse(t, canonicalReplay.Body.Bytes()); !reflect.DeepEqual(replayed, created) {
		t.Fatalf("canonical replay = %#v, want %#v", replayed, created)
	}

	inactive := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+created.ID,
		[]byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"1"`},
	)
	if inactive.Code != http.StatusOK {
		t.Fatalf("inactivate client = %d: %s", inactive.Code, inactive.Body.String())
	}
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+created.ID+"?confirm=true",
		nil, map[string]string{"If-Match": `"2"`},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete client = %d: %s", deleted.Code, deleted.Body.String())
	}

	replayedAfterDelete := performRequest(router, http.MethodPost, "/api/v1/clients", []byte(firstBody), headers)
	if replayedAfterDelete.Code != http.StatusCreated || replayedAfterDelete.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay after delete = %d headers=%v body=%s", replayedAfterDelete.Code, replayedAfterDelete.Header(), replayedAfterDelete.Body.String())
	}
	if replayed := decodeClientResponse(t, replayedAfterDelete.Body.Bytes()); !reflect.DeepEqual(replayed, created) {
		t.Fatalf("replay after delete = %#v, want original %#v", replayed, created)
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/clients/"+created.ID, nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("replay recreated deleted client = %d: %s", missing.Code, missing.Body.String())
	}

	conflict := performRequest(
		router, http.MethodPost, "/api/v1/clients",
		[]byte(`{"name":"Different Client"}`), headers,
	)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
}

func TestClientListFilteringPagingSearchAndStableSort(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clients := []struct {
		id, name, contact, email, status, updatedAt string
	}{
		{"018f0000-0000-7000-8000-000000001101", "Twin", "Ada", "ada@example.com", "active", "2026-08-20T08:00:00Z"},
		{"018f0000-0000-7000-8000-000000001102", "Twin", "Ben", "sales@example.com", "lead", "2026-08-21T08:00:00Z"},
		{"018f0000-0000-7000-8000-000000001103", "50% Studio", "Cara", "cara@example.com", "active", "2026-08-22T08:00:00Z"},
	}
	for _, client := range clients {
		if err := store.DB.Exec(`
			INSERT INTO clients(id, name, contact_name, email, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, client.id, client.name, client.contact, client.email, client.status, client.updatedAt, client.updatedAt).Error; err != nil {
			t.Fatalf("insert client %s: %v", client.id, err)
		}
	}
	for index := 0; index < 2; index++ {
		if err := store.DB.Exec(`
			INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
			VALUES (?, ?, ?, 'planning', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`, uuid.NewString(), fmt.Sprintf("Project %d", index), clients[1].id).Error; err != nil {
			t.Fatalf("insert client project %d: %v", index, err)
		}
	}
	if err := store.DB.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, 'Single Project', ?, 'planning', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, uuid.NewString(), clients[0].id).Error; err != nil {
		t.Fatalf("insert single client project: %v", err)
	}

	lead := performRequest(router, http.MethodGet, "/api/v1/clients?status=lead&q=sales", nil, nil)
	if lead.Code != http.StatusOK {
		t.Fatalf("filtered clients = %d: %s", lead.Code, lead.Body.String())
	}
	var leadList struct {
		Data []clientResponse `json:"data"`
		Meta pageMeta         `json:"meta"`
	}
	if err := json.Unmarshal(lead.Body.Bytes(), &leadList); err != nil {
		t.Fatalf("decode filtered clients: %v", err)
	}
	if len(leadList.Data) != 1 || leadList.Meta.Total != 1 || leadList.Data[0].ID != clients[1].id || leadList.Data[0].ProjectCount != 2 {
		t.Fatalf("filtered clients = %s", lead.Body.String())
	}

	percent := performRequest(router, http.MethodGet, "/api/v1/clients?q="+url.QueryEscape("50%"), nil, nil)
	if percent.Code != http.StatusOK {
		t.Fatalf("escaped search = %d: %s", percent.Code, percent.Body.String())
	}
	var percentList struct {
		Data []clientResponse `json:"data"`
	}
	if err := json.Unmarshal(percent.Body.Bytes(), &percentList); err != nil || len(percentList.Data) != 1 || percentList.Data[0].ID != clients[2].id {
		t.Fatalf("escaped search response = %s err=%v", percent.Body.String(), err)
	}

	ordered := performRequest(router, http.MethodGet, "/api/v1/clients?sort=-project_count,name", nil, nil)
	if ordered.Code != http.StatusOK {
		t.Fatalf("sorted clients = %d: %s", ordered.Code, ordered.Body.String())
	}
	var orderedList struct {
		Data []clientResponse `json:"data"`
	}
	if err := json.Unmarshal(ordered.Body.Bytes(), &orderedList); err != nil {
		t.Fatalf("decode sorted clients: %v", err)
	}
	if len(orderedList.Data) != 3 || orderedList.Data[0].ID != clients[1].id || orderedList.Data[1].ID != clients[0].id {
		t.Fatalf("project-count sort = %s", ordered.Body.String())
	}

	firstPage := performRequest(router, http.MethodGet, "/api/v1/clients?sort=name&page=2&page_size=1", nil, nil)
	secondPage := performRequest(router, http.MethodGet, "/api/v1/clients?sort=name&page=3&page_size=1", nil, nil)
	if firstPage.Code != http.StatusOK || secondPage.Code != http.StatusOK {
		t.Fatalf("stable paging status = %d/%d", firstPage.Code, secondPage.Code)
	}
	var pageTwo, pageThree struct {
		Data []clientResponse `json:"data"`
		Meta pageMeta         `json:"meta"`
	}
	if err := json.Unmarshal(firstPage.Body.Bytes(), &pageTwo); err != nil {
		t.Fatalf("decode page two: %v", err)
	}
	if err := json.Unmarshal(secondPage.Body.Bytes(), &pageThree); err != nil {
		t.Fatalf("decode page three: %v", err)
	}
	if len(pageTwo.Data) != 1 || len(pageThree.Data) != 1 || pageTwo.Meta.Total != 3 ||
		pageTwo.Data[0].ID != clients[0].id || pageThree.Data[0].ID != clients[1].id {
		t.Fatalf("stable name pages = %s / %s", firstPage.Body.String(), secondPage.Body.String())
	}

	for _, testCase := range []struct {
		path, code string
	}{
		{"/api/v1/clients?status=unknown", "INVALID_FILTER"},
		{"/api/v1/clients?q=" + strings.Repeat("x", 201), "INVALID_FILTER"},
		{"/api/v1/clients?sort=invoice_count", "INVALID_SORT"},
		{"/api/v1/clients?page_size=101", "INVALID_PAGINATION"},
	} {
		recorder := performRequest(router, http.MethodGet, testCase.path, nil, nil)
		if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder.Body.Bytes()) != testCase.code {
			t.Fatalf("invalid list %s = %d: %s", testCase.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestClientValidationAndNullablePatchContract(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	createCases := []struct {
		name string
		body string
		code string
	}{
		{name: "blank name", body: `{"name":"   "}`, code: "VALIDATION_ERROR"},
		{name: "long name", body: `{"name":"` + strings.Repeat("界", 201) + `"}`, code: "VALIDATION_ERROR"},
		{name: "invalid email", body: `{"name":"Client","email":"not-an-email"}`, code: "VALIDATION_ERROR"},
		{name: "invalid phone control", body: `{"name":"Client","phone":"123\u0000"}`, code: "VALIDATION_ERROR"},
		{name: "invalid status", body: `{"name":"Client","status":"prospect"}`, code: "VALIDATION_ERROR"},
		{name: "null status", body: `{"name":"Client","status":null}`, code: "VALIDATION_ERROR"},
		{name: "unknown field", body: `{"name":"Client","revenue":100}`, code: "INVALID_JSON"},
	}
	for _, testCase := range createCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/clients", []byte(testCase.body), nil)
			wantStatus := http.StatusUnprocessableEntity
			if testCase.code == "INVALID_JSON" {
				wantStatus = http.StatusBadRequest
			}
			if recorder.Code != wantStatus || responseErrorCode(t, recorder.Body.Bytes()) != testCase.code {
				t.Fatalf("validation = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	client := createClientForTest(t, router, `{"name":"Nullable Client","contact_name":"Name","email":"name@example.com","phone":"555-0100","notes":"Note"}`, nil)
	patchCases := []struct {
		name, body, code string
		status           int
	}{
		{name: "missing version", body: `{"notes":null}`, code: "VERSION_REQUIRED", status: http.StatusPreconditionRequired},
		{name: "invalid version", body: `{"notes":null}`, code: "INVALID_VERSION", status: http.StatusBadRequest},
		{name: "empty patch", body: `{}`, code: "VALIDATION_ERROR", status: http.StatusUnprocessableEntity},
		{name: "null name", body: `{"name":null}`, code: "VALIDATION_ERROR", status: http.StatusUnprocessableEntity},
		{name: "null status", body: `{"status":null}`, code: "VALIDATION_ERROR", status: http.StatusUnprocessableEntity},
		{name: "unknown field", body: `{"invoice_count":0}`, code: "INVALID_JSON", status: http.StatusBadRequest},
	}
	for _, testCase := range patchCases {
		t.Run(testCase.name, func(t *testing.T) {
			headers := map[string]string{"If-Match": `"1"`}
			if testCase.name == "missing version" {
				headers = nil
			}
			if testCase.name == "invalid version" {
				headers["If-Match"] = `"bad"`
			}
			recorder := performRequest(router, http.MethodPatch, "/api/v1/clients/"+client.ID, []byte(testCase.body), headers)
			if recorder.Code != testCase.status || responseErrorCode(t, recorder.Body.Bytes()) != testCase.code {
				t.Fatalf("patch validation = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	cleared := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+client.ID,
		[]byte(`{"contact_name":"  ","email":null,"phone":" ","notes":null}`),
		map[string]string{"If-Match": `"1"`},
	)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear nullable fields = %d: %s", cleared.Code, cleared.Body.String())
	}
	response := decodeClientResponse(t, cleared.Body.Bytes())
	if response.ContactName != nil || response.Email != nil || response.Phone != nil || response.Notes != nil || response.Version != 2 {
		t.Fatalf("cleared nullable fields = %#v", response)
	}

	invalidID := performRequest(router, http.MethodGet, "/api/v1/clients/not-a-uuid", nil, nil)
	if invalidID.Code != http.StatusBadRequest || responseErrorCode(t, invalidID.Body.Bytes()) != "INVALID_CLIENT_ID" {
		t.Fatalf("invalid client id = %d: %s", invalidID.Code, invalidID.Body.String())
	}
}

func TestProjectAssociationInvalidatesClientETag(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	first := createClientForTest(t, router, `{"name":"First Client"}`, nil)
	second := createClientForTest(t, router, `{"name":"Second Client"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Linked Project","client_id":%q}`, first.ID), nil)

	firstAfterAttach := decodeClientResponse(t, performRequest(router, http.MethodGet, "/api/v1/clients/"+first.ID, nil, nil).Body.Bytes())
	if firstAfterAttach.Version != first.Version+1 || firstAfterAttach.ProjectCount != 1 {
		t.Fatalf("first client after attach = %#v", firstAfterAttach)
	}
	staleDelete := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+first.ID+"?confirm=true", nil,
		map[string]string{"If-Match": `"1"`},
	)
	if staleDelete.Code != http.StatusConflict || responseErrorCode(t, staleDelete.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale delete after attach = %d: %s", staleDelete.Code, staleDelete.Body.String())
	}

	moved := performRequest(
		router, http.MethodPatch, "/api/v1/projects/"+project.ID,
		[]byte(fmt.Sprintf(`{"client_id":%q}`, second.ID)),
		map[string]string{"If-Match": `"1"`},
	)
	if moved.Code != http.StatusOK {
		t.Fatalf("move project association = %d: %s", moved.Code, moved.Body.String())
	}
	movedProject := decodeProjectResponse(t, moved.Body.Bytes())
	firstAfterDetach := decodeClientResponse(t, performRequest(router, http.MethodGet, "/api/v1/clients/"+first.ID, nil, nil).Body.Bytes())
	secondAfterAttach := decodeClientResponse(t, performRequest(router, http.MethodGet, "/api/v1/clients/"+second.ID, nil, nil).Body.Bytes())
	if firstAfterDetach.Version != 3 || firstAfterDetach.ProjectCount != 0 || secondAfterAttach.Version != 2 || secondAfterAttach.ProjectCount != 1 {
		t.Fatalf("clients after move = first %#v second %#v", firstAfterDetach, secondAfterAttach)
	}
	staleSecondUpdate := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+second.ID,
		[]byte(`{"notes":"stale"}`), map[string]string{"If-Match": `"1"`},
	)
	if staleSecondUpdate.Code != http.StatusConflict || responseErrorCode(t, staleSecondUpdate.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale second client update = %d: %s", staleSecondUpdate.Code, staleSecondUpdate.Body.String())
	}
	detached := performRequest(
		router, http.MethodPatch, "/api/v1/projects/"+project.ID,
		[]byte(`{"client_id":null}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, movedProject.Version)},
	)
	if detached.Code != http.StatusOK {
		t.Fatalf("detach project association = %d: %s", detached.Code, detached.Body.String())
	}
	secondAfterDetach := decodeClientResponse(t, performRequest(router, http.MethodGet, "/api/v1/clients/"+second.ID, nil, nil).Body.Bytes())
	if secondAfterDetach.Version != 3 || secondAfterDetach.ProjectCount != 0 {
		t.Fatalf("second client after detach = %#v", secondAfterDetach)
	}
}

func TestClientDeleteRequiresInactiveConfirmationAndRejectsInvoices(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Deletion Client"}`, nil)

	activeDelete := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil,
		map[string]string{"If-Match": `"1"`},
	)
	if activeDelete.Code != http.StatusConflict || responseErrorCode(t, activeDelete.Body.Bytes()) != "CLIENT_NOT_INACTIVE" {
		t.Fatalf("active client delete = %d: %s", activeDelete.Code, activeDelete.Body.String())
	}
	inactiveRecorder := performRequest(
		router, http.MethodPatch, "/api/v1/clients/"+client.ID,
		[]byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"1"`},
	)
	if inactiveRecorder.Code != http.StatusOK {
		t.Fatalf("inactivate client = %d: %s", inactiveRecorder.Code, inactiveRecorder.Body.String())
	}
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Detached Project","client_id":%q}`, client.ID), nil)
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID, nil, nil)
	current := decodeClientResponse(t, currentRecorder.Body.Bytes())
	if current.Version != 3 || current.ProjectCount != 1 {
		t.Fatalf("client before constrained delete = %#v", current)
	}

	withoutConfirmation := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+client.ID, nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if withoutConfirmation.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirmation.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed client delete = %d: %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}
	stale := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil,
		map[string]string{"If-Match": `"2"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale client delete = %d: %s", stale.Code, stale.Body.String())
	}

	invoiceID := uuid.NewString()
	if err := store.DB.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, amount_minor, currency, status,
			issue_date, due_date, notes, created_at, updated_at
		) VALUES (?, ?, ?, 10000, 'CNY', 'draft', '2026-08-28', '2026-09-28', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, invoiceID, "INV-CLIENT-DELETE-001", client.ID).Error; err != nil {
		t.Fatalf("insert client invoice: %v", err)
	}
	var projectVersionBeforeBlock int64
	if err := store.SQL.QueryRow("SELECT version FROM projects WHERE id = ?", project.ID).Scan(&projectVersionBeforeBlock); err != nil {
		t.Fatalf("read project version before blocked delete: %v", err)
	}
	blocked := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != "CLIENT_HAS_INVOICES" ||
		!strings.Contains(blocked.Body.String(), "1 invoice") {
		t.Fatalf("invoice-referenced client delete = %d: %s", blocked.Code, blocked.Body.String())
	}
	var clientVersionAfterBlock, projectVersionAfterBlock int64
	if err := store.SQL.QueryRow("SELECT version FROM clients WHERE id = ?", client.ID).Scan(&clientVersionAfterBlock); err != nil {
		t.Fatalf("read client version after blocked delete: %v", err)
	}
	if err := store.SQL.QueryRow("SELECT version FROM projects WHERE id = ?", project.ID).Scan(&projectVersionAfterBlock); err != nil {
		t.Fatalf("read project version after blocked delete: %v", err)
	}
	if clientVersionAfterBlock != current.Version || projectVersionAfterBlock != projectVersionBeforeBlock {
		t.Fatalf(
			"blocked delete changed aggregate versions: client=%d want=%d project=%d want=%d",
			clientVersionAfterBlock,
			current.Version,
			projectVersionAfterBlock,
			projectVersionBeforeBlock,
		)
	}
	var stillLinked string
	if err := store.SQL.QueryRow("SELECT client_id FROM projects WHERE id = ?", project.ID).Scan(&stillLinked); err != nil || stillLinked != client.ID {
		t.Fatalf("blocked delete changed project link = %q err=%v", stillLinked, err)
	}
	if err := store.DB.Exec("DELETE FROM invoices WHERE id = ?", invoiceID).Error; err != nil {
		t.Fatalf("remove client invoice fixture: %v", err)
	}

	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete inactive client = %d: %s", deleted.Code, deleted.Body.String())
	}
	var deletion struct {
		Data deletedClientResponse `json:"data"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletion); err != nil {
		t.Fatalf("decode client deletion: %v", err)
	}
	if deletion.Data.DeletedID != client.ID || deletion.Data.DetachedProjects != 1 {
		t.Fatalf("client deletion response = %#v", deletion.Data)
	}
	var detached struct {
		ClientID *string `gorm:"column:client_id"`
		Version  int64   `gorm:"column:version"`
	}
	if err := store.DB.Table("projects").Select("client_id, version").Where("id = ?", project.ID).Take(&detached).Error; err != nil {
		t.Fatalf("read detached project: %v", err)
	}
	if detached.ClientID != nil || detached.Version != project.Version+1 {
		t.Fatalf("detached project = %#v, original version %d", detached, project.Version)
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID, nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "CLIENT_NOT_FOUND" {
		t.Fatalf("deleted client detail = %d: %s", missing.Code, missing.Body.String())
	}
}
