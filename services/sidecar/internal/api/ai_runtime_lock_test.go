package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
)

func newAIRuntimeLockTest(t *testing.T) (*gin.Engine, *API) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a := &API{db: store.DB, maintenance: &sync.RWMutex{}, aiGenerations: newAIGenerationRegistry(), keyStore: keystore.NewMemoryStore(), options: Options{Now: time.Now, Logger: log.New(io.Discard, "", 0)}}
	r := gin.New()
	r.Use(a.maintenanceReadMiddleware())
	r.POST("/api/v1/ai/chat", a.chatAI)
	r.POST("/api/v1/ai/generations/:id/cancel", a.cancelAIGeneration)
	r.GET("/api/v1/ai/generations/:id", a.getAIGeneration)
	r.GET("/api/v1/ai/generations/by-request/:key", a.getAIGenerationByRequest)
	r.GET("/api/v1/ai/active-generations", a.listActiveAIGenerations)
	r.GET("/api/v1/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r, a
}

func TestSlowChatDoesNotHoldMaintenanceOrProviderLock(t *testing.T) {
	r, a := newAIRuntimeLockTest(t)
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.db.Exec("INSERT INTO ai_providers(id,name,protocol,base_url,model,kind,status,health_status,last_health_at,has_key,version,created_at,updated_at) VALUES(?,?,'openai_chat',?,'test','local','ready','healthy',?,0,1,?,?)", id, "slow", server.URL, now, now, now).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(fmt.Sprintf(`{"provider_id":%q,"message":"hello"}`, id))).WithContext(ctx)
	done := make(chan struct{})
	go func() { r.ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model never started")
	}
	locked := make(chan struct{})
	go func() {
		a.maintenance.Lock()
		a.maintenance.Unlock()
		a.aiProviderMu.Lock()
		a.aiProviderMu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(250 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("model wait holds global lock")
	}
	response := performRequest(r, http.MethodGet, "/api/v1/probe", nil, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("probe=%d", response.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not finish")
	}
}

func TestCancelRegistryBypassesMaintenanceWriter(t *testing.T) {
	r, a := newAIRuntimeLockTest(t)
	id := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.aiGenerations.register(id, uuid.NewString(), uuid.NewString(), cancel)
	a.maintenance.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- performRequest(r, http.MethodPost, "/api/v1/ai/generations/"+id+"/cancel", nil, nil) }()
	select {
	case response := <-done:
		a.maintenance.Unlock()
		if response.Code != http.StatusAccepted || ctx.Err() == nil {
			t.Fatalf("cancel=%d err=%v", response.Code, ctx.Err())
		}
	case <-time.After(250 * time.Millisecond):
		a.maintenance.Unlock()
		<-done
		t.Fatal("cancel waited for maintenance writer")
	}
}

func TestDisabledScheduledBackupDoesNotRequestExclusiveMaintenance(t *testing.T) {
	_, a := newAIRuntimeLockTest(t)
	a.backupStore = &backupStore{}
	a.maintenance.RLock()
	done := make(chan error, 1)
	go func() { done <- a.runDueScheduledBackup(context.Background()) }()
	select {
	case err := <-done:
		a.maintenance.RUnlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		a.maintenance.RUnlock()
		<-done
		t.Fatal("disabled scan attempted exclusive maintenance lock")
	}
}

func TestRestoreCancelsActiveRunWithoutWritingAfterBarrier(t *testing.T) {
	r, a := newAIRuntimeLockTest(t)
	id := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.aiGenerations.register(id, uuid.NewString(), uuid.NewString(), cancel)
	a.maintenance.Lock()
	a.restorePending.Store(true)
	a.cancelAIRunsForRestore()
	if ctx.Err() == nil {
		t.Fatal("restore did not cancel active generation")
	}
	response := performRequest(r, http.MethodPost, "/api/v1/ai/generations/"+id+"/cancel", nil, nil)
	a.maintenance.Unlock()
	if response.Code != http.StatusAccepted {
		t.Fatalf("cancel blocked during restore: %d %s", response.Code, response.Body.String())
	}
}
