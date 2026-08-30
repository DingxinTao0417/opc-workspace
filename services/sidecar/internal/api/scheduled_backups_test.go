package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func TestScheduledBackupPolicyAPIValidatesAndUsesOptimisticVersion(t *testing.T) {
	router, _, _, _, _ := newBackupCapacityTestRuntime(t, nil)
	get := performRequest(router, http.MethodGet, "/api/v1/backups/policy", nil, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get policy=%d: %s", get.Code, get.Body.String())
	}
	var initial struct {
		Data scheduledBackupPolicy `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Data.Enabled || initial.Data.LocalTime != "02:00" || initial.Data.Timezone != "UTC" || initial.Data.RetentionCount != 30 || initial.Data.Version != 1 || initial.Data.NextRunAt != nil {
		t.Fatalf("initial policy=%#v", initial.Data)
	}

	invalid := performRequest(router, http.MethodPatch, "/api/v1/backups/policy", []byte(`{"enabled":true,"local_time":"25:00","timezone":"UTC","retention_count":30}`), map[string]string{"If-Match": `"1"`})
	if invalid.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalid.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid policy=%d: %s", invalid.Code, invalid.Body.String())
	}
	updated := performRequest(router, http.MethodPatch, "/api/v1/backups/policy", []byte(`{"enabled":true,"local_time":"03:15","timezone":"Asia/Shanghai","retention_count":7}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("update policy=%d: %s", updated.Code, updated.Body.String())
	}
	var saved struct {
		Data scheduledBackupPolicy `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Data.Enabled || saved.Data.LocalTime != "03:15" || saved.Data.Timezone != "Asia/Shanghai" || saved.Data.RetentionCount != 7 || saved.Data.Version != 2 || saved.Data.NextRunAt == nil {
		t.Fatalf("saved policy=%#v", saved.Data)
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/backups/policy", []byte(`{"enabled":false,"local_time":"03:15","timezone":"Asia/Shanghai","retention_count":7}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale policy=%d: %s", stale.Code, stale.Body.String())
	}
}

func TestScheduledBackupStartupCompensatesOncePerLocalDayAndPrunesOnlyScheduledPackages(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	artifactDir := filepath.Join(root, "artifacts")
	backupDir := filepath.Join(root, "backups")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 3, 0, 0, 0, time.UTC)
	openRouter := func() *Router {
		router, err := NewRouter(store.DB, Options{
			AppVersion: "0.1.0-test", Commit: "scheduled-test", SchemaVersion: store.SchemaVersion,
			SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
			Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactDir,
			DatabasePath: databasePath, BackupDir: backupDir, Now: func() time.Time { return now },
			FocusHeartbeatInterval: -1, ReminderScanInterval: -1, DiskSpaceScanInterval: -1,
			ScheduledBackupScanInterval: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return router
	}

	router := openRouter()
	manual := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"manual checkpoint"}`), map[string]string{"Idempotency-Key": "scheduled-retention-manual"})
	if manual.Code != http.StatusCreated {
		t.Fatalf("manual backup=%d: %s", manual.Code, manual.Body.String())
	}
	enable := performRequest(router, http.MethodPatch, "/api/v1/backups/policy", []byte(`{"enabled":true,"local_time":"02:00","timezone":"UTC","retention_count":1}`), map[string]string{"If-Match": `"1"`})
	if enable.Code != http.StatusOK {
		t.Fatalf("enable policy=%d: %s", enable.Code, enable.Body.String())
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	router = openRouter()
	first := performRequest(router, http.MethodGet, "/api/v1/backups", nil, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first list=%d: %s", first.Code, first.Body.String())
	}
	assertBackupKinds(t, first.Body.Bytes(), 1, 1)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	router = openRouter()
	sameDay := performRequest(router, http.MethodGet, "/api/v1/backups", nil, nil)
	assertBackupKinds(t, sameDay.Body.Bytes(), 1, 1)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	now = now.Add(24 * time.Hour)
	router = openRouter()
	defer router.Close()
	nextDay := performRequest(router, http.MethodGet, "/api/v1/backups", nil, nil)
	assertBackupKinds(t, nextDay.Body.Bytes(), 1, 1)
	policyResponse := performRequest(router, http.MethodGet, "/api/v1/backups/policy", nil, nil)
	var policy struct {
		Data scheduledBackupPolicy `json:"data"`
	}
	if err := json.Unmarshal(policyResponse.Body.Bytes(), &policy); err != nil || policy.Data.LastStatus != "succeeded" || policy.Data.LastBackupID == nil || policy.Data.LastSuccessAt == nil {
		t.Fatalf("successful policy=%#v err=%v", policy.Data, err)
	}
}

func assertBackupKinds(t *testing.T, body []byte, manualWant, scheduledWant int) {
	t.Helper()
	var envelope struct {
		Data []backupSummary `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	manual, scheduled := 0, 0
	for _, item := range envelope.Data {
		switch item.Kind {
		case "manual":
			manual++
		case "scheduled":
			scheduled++
		}
	}
	if manual != manualWant || scheduled != scheduledWant {
		t.Fatalf("backup kinds manual=%d scheduled=%d; want %d/%d, data=%#v", manual, scheduled, manualWant, scheduledWant, envelope.Data)
	}
}
