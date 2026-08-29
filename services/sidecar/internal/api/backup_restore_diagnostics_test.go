package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRestoreDiagnosticsEndpointReportsIdleWithoutMachinePaths(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	response := performRequest(router, http.MethodGet, "/api/v1/backups/restore-diagnostics", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("restore diagnostics = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data restoreDiagnostics `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if envelope.Data.Status != "idle" || envelope.Data.RestartRequired || envelope.Data.CleanupRequired || envelope.Data.AttentionRequired {
		t.Fatalf("idle diagnostics = %#v", envelope.Data)
	}
}

func TestInspectRestoreDiagnosticsReportsPendingAppliedFailedAndInvalidFacts(t *testing.T) {
	root := t.TempDir()
	pending := restoreDiagnosticTestPlan(time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC))
	writeRestoreDiagnosticDirectory(t, root, pendingRestoreDirectory, pending)

	applied := restoreDiagnosticTestPlan(time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))
	writeRestoreDiagnosticDirectory(t, root, appliedRestorePrefix+applied.OperationID, applied)
	failed := restoreDiagnosticTestPlan(time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC))
	writeRestoreDiagnosticDirectory(t, root, ".restore-failed-"+failed.OperationID, failed)
	if err := os.WriteFile(filepath.Join(root, appliedRestorePrefix+"not-a-uuid"), []byte("unsafe"), 0o600); err != nil {
		t.Fatal(err)
	}

	diagnostics, err := inspectRestoreDiagnostics(root, StartupRestoreResult{})
	if err != nil {
		t.Fatalf("inspectRestoreDiagnostics() error = %v", err)
	}
	if diagnostics.Status != "attention_required" || !diagnostics.RestartRequired || !diagnostics.CleanupRequired || !diagnostics.AttentionRequired {
		t.Fatalf("diagnostic state = %#v", diagnostics)
	}
	if diagnostics.ResidualAppliedCount != 1 || diagnostics.FailedAttemptCount != 1 || diagnostics.InvalidEntryCount != 1 {
		t.Fatalf("diagnostic counts = %#v", diagnostics)
	}
	if diagnostics.BackupID == nil || *diagnostics.BackupID != pending.BackupID || diagnostics.RequestedAt == nil || *diagnostics.RequestedAt != pending.RequestedAt {
		t.Fatalf("pending details = %#v", diagnostics)
	}
}

func TestInspectRestoreDiagnosticsReportsSanitizedStartupCleanupWarning(t *testing.T) {
	root := t.TempDir()
	result := StartupRestoreResult{
		Applied: true, BackupID: uuid.NewString(), RollbackBackupID: uuid.NewString(),
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano), CleanupWarning: `C:\private\workspace\secret-token`,
	}
	diagnostics, err := inspectRestoreDiagnostics(root, result)
	if err != nil {
		t.Fatalf("inspectRestoreDiagnostics() error = %v", err)
	}
	if diagnostics.Status != "cleanup_required" || !diagnostics.AppliedThisStartup || !diagnostics.CleanupRequired || diagnostics.AttentionRequired {
		t.Fatalf("startup cleanup diagnostics = %#v", diagnostics)
	}
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret-token") || strings.Contains(string(encoded), `C:\\`) {
		t.Fatalf("diagnostics leaked raw cleanup warning: %s", encoded)
	}
}

func restoreDiagnosticTestPlan(requestedAt time.Time) pendingRestorePlan {
	return pendingRestorePlan{
		FormatVersion: pendingRestoreVersion, OperationID: uuid.NewString(), BackupID: uuid.NewString(),
		RollbackBackupID: uuid.NewString(), RequestedAt: requestedAt.Format(time.RFC3339Nano),
		DatabaseID: uuid.NewString(), ArtifactStoreID: uuid.NewString(), SourceSchema: 28,
	}
}

func writeRestoreDiagnosticDirectory(t *testing.T, root, name string, plan pendingRestorePlan) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeStrictJSONFile(filepath.Join(path, pendingRestorePlanName), plan, maxRestorePlanBytes); err != nil {
		t.Fatal(err)
	}
}
