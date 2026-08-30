package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestClientHardDeleteRejectsHistoricalFinanceAndFollowupReferencesWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name            string
		expectedCode    string
		expectedMessage string
		seedReference   func(t *testing.T, router http.Handler, store *database.Store, clientID string) func()
	}{
		{
			name:            "active and voided financial entries",
			expectedCode:    "CLIENT_HAS_FINANCIAL_ENTRIES",
			expectedMessage: "2 financial entry",
			seedReference: func(t *testing.T, router http.Handler, store *database.Store, clientID string) func() {
				voidedEntry := createFinancialEntryForTest(t, router, fmt.Sprintf(`{
					"type":"income","amount_minor":6400,"currency":"CNY","occurred_on":"2026-08-20",
					"status":"confirmed","category":"历史收入","client_id":%q
				}`, clientID), nil)
				voided := performRequest(
					router, http.MethodDelete, "/api/v1/financial-entries/"+voidedEntry.ID+"?confirm=true",
					[]byte(`{"reason":"保留作废历史"}`), map[string]string{"If-Match": `"1"`},
				)
				if voided.Code != http.StatusOK || decodeFinancialEntryResponse(t, voided.Body.Bytes()).Status != "voided" {
					t.Fatalf("void client financial reference = %d: %s", voided.Code, voided.Body.String())
				}
				activeEntry := createFinancialEntryForTest(t, router, fmt.Sprintf(`{
					"type":"income","amount_minor":6500,"currency":"CNY","occurred_on":"2026-08-21",
					"status":"pending","category":"待确认收入","client_id":%q
				}`, clientID), nil)
				var voidedBefore, activeBefore models.FinancialEntry
				if err := store.DB.First(&voidedBefore, "id = ?", voidedEntry.ID).Error; err != nil {
					t.Fatalf("load voided client financial reference: %v", err)
				}
				if err := store.DB.First(&activeBefore, "id = ?", activeEntry.ID).Error; err != nil {
					t.Fatalf("load active client financial reference: %v", err)
				}
				return func() {
					var voidedAfter, activeAfter models.FinancialEntry
					if err := store.DB.First(&voidedAfter, "id = ?", voidedEntry.ID).Error; err != nil {
						t.Fatalf("reload voided client financial reference: %v", err)
					}
					if err := store.DB.First(&activeAfter, "id = ?", activeEntry.ID).Error; err != nil {
						t.Fatalf("reload active client financial reference: %v", err)
					}
					if !reflect.DeepEqual(voidedAfter, voidedBefore) || !reflect.DeepEqual(activeAfter, activeBefore) {
						t.Fatalf(
							"blocked client delete changed financial references: voided before=%#v after=%#v active before=%#v after=%#v",
							voidedBefore, voidedAfter, activeBefore, activeAfter,
						)
					}
				}
			},
		},
		{
			name:            "planned and cancelled followups",
			expectedCode:    "CLIENT_HAS_FOLLOWUPS",
			expectedMessage: "2 follow-up",
			seedReference: func(t *testing.T, router http.Handler, store *database.Store, clientID string) func() {
				plannedRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(fmt.Sprintf(`{
					"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-01T09:00:00Z",
					"timezone":"UTC","channel":"phone","purpose":"保留计划回访"
				}`, clientID, models.BuiltinOwnerActorID)), nil)
				if plannedRecorder.Code != http.StatusCreated {
					t.Fatalf("create planned client followup reference = %d: %s", plannedRecorder.Code, plannedRecorder.Body.String())
				}
				planned := decodeClientFollowupResponse(t, plannedRecorder.Body.Bytes())
				cancelCandidateRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(fmt.Sprintf(`{
					"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-02T09:00:00Z",
					"timezone":"UTC","channel":"phone","purpose":"保留取消回访"
				}`, clientID, models.BuiltinOwnerActorID)), nil)
				if cancelCandidateRecorder.Code != http.StatusCreated {
					t.Fatalf("create cancellable client followup reference = %d: %s", cancelCandidateRecorder.Code, cancelCandidateRecorder.Body.String())
				}
				cancelCandidate := decodeClientFollowupResponse(t, cancelCandidateRecorder.Body.Bytes())
				cancelled := performRequest(
					router, http.MethodDelete, "/api/v1/client-followups/"+cancelCandidate.ID+"?confirm=true",
					[]byte(`{"reason":"保留取消历史"}`), map[string]string{"If-Match": `"1"`},
				)
				if cancelled.Code != http.StatusOK || decodeClientFollowupResponse(t, cancelled.Body.Bytes()).Status != "cancelled" {
					t.Fatalf("cancel client followup reference = %d: %s", cancelled.Code, cancelled.Body.String())
				}
				var plannedBefore, cancelledBefore models.ClientFollowup
				if err := store.DB.First(&plannedBefore, "id = ?", planned.ID).Error; err != nil {
					t.Fatalf("load planned client followup reference: %v", err)
				}
				if err := store.DB.First(&cancelledBefore, "id = ?", cancelCandidate.ID).Error; err != nil {
					t.Fatalf("load cancelled client followup reference: %v", err)
				}
				return func() {
					var plannedAfter, cancelledAfter models.ClientFollowup
					if err := store.DB.First(&plannedAfter, "id = ?", planned.ID).Error; err != nil {
						t.Fatalf("reload planned client followup reference: %v", err)
					}
					if err := store.DB.First(&cancelledAfter, "id = ?", cancelCandidate.ID).Error; err != nil {
						t.Fatalf("reload cancelled client followup reference: %v", err)
					}
					if !reflect.DeepEqual(plannedAfter, plannedBefore) || !reflect.DeepEqual(cancelledAfter, cancelledBefore) {
						t.Fatalf(
							"blocked client delete changed followup references: planned before=%#v after=%#v cancelled before=%#v after=%#v",
							plannedBefore, plannedAfter, cancelledBefore, cancelledAfter,
						)
					}
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, store, artifactRoot := newClientAttachmentTestAPI(t)
			client := createClientForTest(t, router.Engine, `{"name":"Client delete guard"}`, nil)
			project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"Preserved client project","client_id":%q}`, client.ID), nil)
			current := decodeClientResponse(t, performRequest(router.Engine, http.MethodGet, "/api/v1/clients/"+client.ID, nil, nil).Body.Bytes())
			upload := performClientAttachmentUpload(
				t, router.Engine, "/api/v1/clients/"+client.ID+"/attachments", `{"name":"guard.bin"}`,
				"guard.bin", []byte("client guard attachment"),
				map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
			)
			if upload.Code != http.StatusCreated {
				t.Fatalf("upload guarded client attachment = %d: %s", upload.Code, upload.Body.String())
			}
			attachmentID := decodeClientAttachmentResponse(t, upload.Body.Bytes()).ID
			assertReferenceUnchanged := test.seedReference(t, router.Engine, store, client.ID)

			current = decodeClientResponse(t, performRequest(router.Engine, http.MethodGet, "/api/v1/clients/"+client.ID, nil, nil).Body.Bytes())
			inactiveRecorder := performRequest(
				router.Engine, http.MethodPatch, "/api/v1/clients/"+client.ID,
				[]byte(`{"status":"inactive"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
			)
			if inactiveRecorder.Code != http.StatusOK {
				t.Fatalf("inactivate guarded client = %d: %s", inactiveRecorder.Code, inactiveRecorder.Body.String())
			}
			inactive := decodeClientResponse(t, inactiveRecorder.Body.Bytes())

			var clientBefore models.Client
			if err := store.DB.First(&clientBefore, "id = ?", client.ID).Error; err != nil {
				t.Fatalf("load guarded client: %v", err)
			}
			var projectBefore models.Project
			if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
				t.Fatalf("load guarded client project: %v", err)
			}
			var attachmentBefore models.ClientAttachment
			if err := store.DB.First(&attachmentBefore, "id = ?", attachmentID).Error; err != nil {
				t.Fatalf("load guarded client attachment: %v", err)
			}
			attachmentPath := filepath.Join(artifactRoot, filepath.FromSlash(attachmentBefore.RelativePath))
			fileBefore, err := os.ReadFile(attachmentPath)
			if err != nil {
				t.Fatalf("read guarded client attachment: %v", err)
			}
			var eventCountBefore, tombstoneCountBefore int64
			if err := store.DB.Model(&models.WorkflowEvent{}).Count(&eventCountBefore).Error; err != nil {
				t.Fatalf("count workflow events before guarded client delete: %v", err)
			}
			if err := store.DB.Model(&models.ClientAttachmentDeletionTombstone{}).Count(&tombstoneCountBefore).Error; err != nil {
				t.Fatalf("count client attachment tombstones before guarded delete: %v", err)
			}

			blocked := performRequest(
				router.Engine, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil,
				map[string]string{"If-Match": fmt.Sprintf(`"%d"`, inactive.Version)},
			)
			if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != test.expectedCode ||
				!strings.Contains(blocked.Body.String(), test.expectedMessage) {
				t.Fatalf("guarded client delete = %d: %s", blocked.Code, blocked.Body.String())
			}

			var clientAfter models.Client
			if err := store.DB.First(&clientAfter, "id = ?", client.ID).Error; err != nil || !reflect.DeepEqual(clientAfter, clientBefore) {
				t.Fatalf("blocked delete changed Client: before=%#v after=%#v err=%v", clientBefore, clientAfter, err)
			}
			var projectAfter models.Project
			if err := store.DB.First(&projectAfter, "id = ?", project.ID).Error; err != nil || !reflect.DeepEqual(projectAfter, projectBefore) {
				t.Fatalf("blocked delete changed linked Project: before=%#v after=%#v err=%v", projectBefore, projectAfter, err)
			}
			var attachmentAfter models.ClientAttachment
			if err := store.DB.First(&attachmentAfter, "id = ?", attachmentID).Error; err != nil || !reflect.DeepEqual(attachmentAfter, attachmentBefore) {
				t.Fatalf("blocked delete changed client attachment: before=%#v after=%#v err=%v", attachmentBefore, attachmentAfter, err)
			}
			fileAfter, err := os.ReadFile(attachmentPath)
			if err != nil || !reflect.DeepEqual(fileAfter, fileBefore) {
				t.Fatalf("blocked delete changed client attachment file: before=%q after=%q err=%v", fileBefore, fileAfter, err)
			}
			var eventCountAfter, tombstoneCountAfter int64
			if err := store.DB.Model(&models.WorkflowEvent{}).Count(&eventCountAfter).Error; err != nil || eventCountAfter != eventCountBefore {
				t.Fatalf("blocked client delete changed workflow events: before=%d after=%d err=%v", eventCountBefore, eventCountAfter, err)
			}
			if err := store.DB.Model(&models.ClientAttachmentDeletionTombstone{}).Count(&tombstoneCountAfter).Error; err != nil || tombstoneCountAfter != tombstoneCountBefore {
				t.Fatalf("blocked client delete changed attachment tombstones: before=%d after=%d err=%v", tombstoneCountBefore, tombstoneCountAfter, err)
			}
			assertReferenceUnchanged()
		})
	}
}

func TestProjectHardDeleteRejectsInvoiceAndFinanceReferencesWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name            string
		expectedCode    string
		expectedMessage string
		seedReference   func(t *testing.T, router http.Handler, store *database.Store, clientID, projectID string) func()
	}{
		{
			name:            "sent invoice",
			expectedCode:    "PROJECT_HAS_INVOICES",
			expectedMessage: "1 invoice",
			seedReference: func(t *testing.T, router http.Handler, store *database.Store, clientID, projectID string) func() {
				invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{
					"client_id":%q,"project_id":%q,"amount_minor":7300,"currency":"CNY",
					"issue_date":"2026-08-01","due_date":"2026-09-01"
				}`, clientID, projectID), nil)
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
				return assertInvoiceUnchangedAfterBlockedProjectDelete(t, store, invoice.ID)
			},
		},
		{
			name:            "paid invoice with linked financial entry",
			expectedCode:    "PROJECT_HAS_INVOICES",
			expectedMessage: "1 invoice",
			seedReference: func(t *testing.T, router http.Handler, store *database.Store, clientID, projectID string) func() {
				invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{
					"client_id":%q,"project_id":%q,"amount_minor":7400,"currency":"CNY",
					"issue_date":"2026-08-01","due_date":"2026-09-01"
				}`, clientID, projectID), nil)
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_paid","paid_date":"2026-08-20"}`, "")
				if invoice.FinancialEntryID == nil {
					t.Fatalf("paid Invoice has no linked Financial Entry: %#v", invoice)
				}
				assertInvoice := assertInvoiceUnchangedAfterBlockedProjectDelete(t, store, invoice.ID)
				assertEntry := assertFinancialEntryUnchangedAfterBlockedProjectDelete(t, store, *invoice.FinancialEntryID)
				return func() {
					assertInvoice()
					assertEntry()
				}
			},
		},
		{
			name:            "voided manual financial entry",
			expectedCode:    "PROJECT_HAS_FINANCIAL_ENTRIES",
			expectedMessage: "1 financial entry",
			seedReference: func(t *testing.T, router http.Handler, store *database.Store, _, projectID string) func() {
				entry := createFinancialEntryForTest(t, router, fmt.Sprintf(`{
					"type":"expense","amount_minor":7500,"currency":"CNY","occurred_on":"2026-08-20",
					"status":"confirmed","category":"历史项目支出","project_id":%q
				}`, projectID), nil)
				voided := performRequest(
					router, http.MethodDelete, "/api/v1/financial-entries/"+entry.ID+"?confirm=true",
					[]byte(`{"reason":"保留项目作废历史"}`), map[string]string{"If-Match": `"1"`},
				)
				if voided.Code != http.StatusOK || decodeFinancialEntryResponse(t, voided.Body.Bytes()).Status != "voided" {
					t.Fatalf("void project financial reference = %d: %s", voided.Code, voided.Body.String())
				}
				return assertFinancialEntryUnchangedAfterBlockedProjectDelete(t, store, entry.ID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, store, artifactRoot := newClientAttachmentTestAPI(t)
			client := createClientForTest(t, router.Engine, `{"name":"Project delete guard client"}`, nil)
			project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"Project delete guard","client_id":%q}`, client.ID), nil)
			upload := performClientAttachmentUpload(
				t, router.Engine, "/api/v1/projects/"+project.ID+"/attachments", `{"name":"guard.bin"}`,
				"guard.bin", []byte("project guard attachment"), map[string]string{"If-Match": `"1"`},
			)
			if upload.Code != http.StatusCreated {
				t.Fatalf("upload guarded project attachment = %d: %s", upload.Code, upload.Body.String())
			}
			attachmentID := decodeProjectAttachmentResponse(t, upload.Body.Bytes()).ID
			taskRecorder := performRequest(
				router.Engine, http.MethodPost, "/api/v1/tasks",
				[]byte(fmt.Sprintf(`{"title":"Preserved project task","project_id":%q}`, project.ID)), nil,
			)
			if taskRecorder.Code != http.StatusCreated {
				t.Fatalf("create guarded project Task = %d: %s", taskRecorder.Code, taskRecorder.Body.String())
			}
			var taskEnvelope struct {
				Data models.Task `json:"data"`
			}
			if err := json.Unmarshal(taskRecorder.Body.Bytes(), &taskEnvelope); err != nil {
				t.Fatalf("decode guarded project Task: %v", err)
			}
			assertReferenceUnchanged := test.seedReference(t, router.Engine, store, client.ID, project.ID)

			current := decodeProjectResponse(t, performRequest(router.Engine, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil).Body.Bytes())
			archived := transitionProjectForTest(t, router.Engine, project.ID, current.Version, `{"action":"archive"}`)
			var projectBefore models.Project
			if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
				t.Fatalf("load guarded Project: %v", err)
			}
			var taskBefore models.Task
			if err := store.DB.First(&taskBefore, "id = ?", taskEnvelope.Data.ID).Error; err != nil {
				t.Fatalf("load guarded Project Task: %v", err)
			}
			var attachmentBefore models.ProjectAttachment
			if err := store.DB.First(&attachmentBefore, "id = ?", attachmentID).Error; err != nil {
				t.Fatalf("load guarded Project attachment: %v", err)
			}
			attachmentPath := filepath.Join(artifactRoot, filepath.FromSlash(attachmentBefore.RelativePath))
			fileBefore, err := os.ReadFile(attachmentPath)
			if err != nil {
				t.Fatalf("read guarded Project attachment: %v", err)
			}
			var eventCountBefore, tombstoneCountBefore int64
			if err := store.DB.Model(&models.WorkflowEvent{}).Count(&eventCountBefore).Error; err != nil {
				t.Fatalf("count workflow events before guarded Project delete: %v", err)
			}
			if err := store.DB.Model(&models.ProjectAttachmentDeletionTombstone{}).Count(&tombstoneCountBefore).Error; err != nil {
				t.Fatalf("count Project attachment tombstones before guarded delete: %v", err)
			}

			blocked := performRequest(
				router.Engine, http.MethodDelete, "/api/v1/projects/"+project.ID+"?confirm=true", nil,
				map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archived.Version)},
			)
			if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != test.expectedCode ||
				!strings.Contains(blocked.Body.String(), test.expectedMessage) {
				t.Fatalf("guarded Project delete = %d: %s", blocked.Code, blocked.Body.String())
			}

			var projectAfter models.Project
			if err := store.DB.First(&projectAfter, "id = ?", project.ID).Error; err != nil || !reflect.DeepEqual(projectAfter, projectBefore) {
				t.Fatalf("blocked delete changed Project: before=%#v after=%#v err=%v", projectBefore, projectAfter, err)
			}
			var taskAfter models.Task
			if err := store.DB.First(&taskAfter, "id = ?", taskBefore.ID).Error; err != nil || !reflect.DeepEqual(taskAfter, taskBefore) {
				t.Fatalf("blocked delete changed Project Task: before=%#v after=%#v err=%v", taskBefore, taskAfter, err)
			}
			var attachmentAfter models.ProjectAttachment
			if err := store.DB.First(&attachmentAfter, "id = ?", attachmentID).Error; err != nil || !reflect.DeepEqual(attachmentAfter, attachmentBefore) {
				t.Fatalf("blocked delete changed Project attachment: before=%#v after=%#v err=%v", attachmentBefore, attachmentAfter, err)
			}
			fileAfter, err := os.ReadFile(attachmentPath)
			if err != nil || !reflect.DeepEqual(fileAfter, fileBefore) {
				t.Fatalf("blocked delete changed Project attachment file: before=%q after=%q err=%v", fileBefore, fileAfter, err)
			}
			var eventCountAfter, tombstoneCountAfter int64
			if err := store.DB.Model(&models.WorkflowEvent{}).Count(&eventCountAfter).Error; err != nil || eventCountAfter != eventCountBefore {
				t.Fatalf("blocked Project delete changed workflow events: before=%d after=%d err=%v", eventCountBefore, eventCountAfter, err)
			}
			if err := store.DB.Model(&models.ProjectAttachmentDeletionTombstone{}).Count(&tombstoneCountAfter).Error; err != nil || tombstoneCountAfter != tombstoneCountBefore {
				t.Fatalf("blocked Project delete changed attachment tombstones: before=%d after=%d err=%v", tombstoneCountBefore, tombstoneCountAfter, err)
			}
			assertReferenceUnchanged()
		})
	}
}

func assertInvoiceUnchangedAfterBlockedProjectDelete(t *testing.T, store *database.Store, invoiceID string) func() {
	t.Helper()
	var before models.Invoice
	if err := store.DB.First(&before, "id = ?", invoiceID).Error; err != nil {
		t.Fatalf("load guarded Invoice: %v", err)
	}
	return func() {
		var after models.Invoice
		if err := store.DB.First(&after, "id = ?", invoiceID).Error; err != nil {
			t.Fatalf("reload guarded Invoice: %v", err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("blocked Project delete changed Invoice: before=%#v after=%#v", before, after)
		}
	}
}

func assertFinancialEntryUnchangedAfterBlockedProjectDelete(t *testing.T, store *database.Store, entryID string) func() {
	t.Helper()
	var before models.FinancialEntry
	if err := store.DB.First(&before, "id = ?", entryID).Error; err != nil {
		t.Fatalf("load guarded Financial Entry: %v", err)
	}
	return func() {
		var after models.FinancialEntry
		if err := store.DB.First(&after, "id = ?", entryID).Error; err != nil {
			t.Fatalf("reload guarded Financial Entry: %v", err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("blocked Project delete changed Financial Entry: before=%#v after=%#v", before, after)
		}
	}
}
