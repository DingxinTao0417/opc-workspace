package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestProviderHealthPreservesConfigurationIdentity(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Now().UTC())
	upstream := newMockAIUpstream(func(http.ResponseWriter, *http.Request) {})
	defer upstream.Close()
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"config identity","kind":"local","protocol":"openai_chat","base_url":"`+upstream.URL+`/v1","model":"test"}`), nil)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	initial, ok := envelope.Data["config_version"].(float64)
	if !ok || initial < 1 {
		t.Fatalf("missing independent configuration identity: %s", created.Body.String())
	}
	id := envelope.Data["id"].(string)
	for _, etag := range []string{`"1"`, `"2"`} {
		checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+id+"/health", nil, map[string]string{"If-Match": etag})
		if checked.Code != http.StatusOK {
			t.Fatalf("health: %s", checked.Body.String())
		}
		if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data["config_version"] != initial {
			t.Fatalf("health changed config identity: %v", envelope.Data)
		}
	}
}

func TestProviderConfigurationIdentityChangesOnlyWithExecutionConfiguration(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Now().UTC())
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"Identity","kind":"remote","protocol":"openai_chat","base_url":"https://example.invalid/v1","model":"a"}`), nil)
	var envelope struct {
		Data aiProviderResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil || created.Code != 201 {
		t.Fatalf("create: %s, %v", created.Body.String(), err)
	}
	provider := envelope.Data
	for _, item := range []struct {
		method, suffix, body string
		config               int64
	}{
		{http.MethodPatch, "", `{"name":"Renamed"}`, 1},
		{http.MethodPatch, "", `{"model":"b"}`, 2},
		{http.MethodPatch, "", `{"base_url":"https://other.invalid/v1"}`, 3},
		{http.MethodPatch, "", `{"protocol":"anthropic_messages"}`, 4},
		{http.MethodPost, "/key", `{"api_key":"test-one"}`, 5},
		{http.MethodPost, "/key", `{"api_key":"test-one"}`, 5},
		{http.MethodPost, "/key", `{"api_key":"test-two"}`, 6},
	} {
		response := performRequest(router, item.method, "/api/v1/ai/providers/"+provider.ID+item.suffix, []byte(item.body), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, provider.Version)})
		if response.Code != http.StatusOK {
			t.Fatalf("change %s: %s", item.body, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.ConfigVersion != item.config || envelope.Data.Version != provider.Version+1 {
			t.Fatalf("change %s: %#v", item.body, envelope.Data)
		}
		provider = envelope.Data
	}
}

func TestAIEvaluationStableConfigurationGroupsAndReviews(t *testing.T) {
	router, store, provider := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	config := int64(1)
	for index := 0; index < 3; index++ {
		provider.Version = int64(index + 1)
		provider.Name = fmt.Sprintf("Display %d", index)
		seedEvaluationSummaryVersionedRun(t, store, provider, 4, 24, aieval.SuiteFull, fmt.Sprintf("2026-09-09T10:0%d:00Z", index), &config)
	}
	group := loadEvaluationReviewGroup(t, router, provider.ID, aieval.SuiteFull)
	if group.RunCount != 3 || group.ReadinessStatus != "review_candidate" || group.ProviderConfigVersion == nil || *group.ProviderConfigVersion != 1 || group.ProviderVersionMax != 3 {
		t.Fatalf("stable configuration split: %#v", group)
	}
	input := evaluationReviewInputForGroup(group, "accepted_for_local_use", "已核对同一配置的三轮本地证据。")
	response := createEvaluationReviewRequest(t, router, input, "config-review")
	if response.Code != http.StatusCreated {
		t.Fatalf("review: %s", response.Body.String())
	}
	var original []models.AIEvaluationReview
	if err := store.DB.Find(&original).Error; err != nil || len(original) != 1 || original[0].ProviderConfigVersion == nil || *original[0].ProviderConfigVersion != 1 {
		t.Fatalf("review config: %#v %v", original, err)
	}
	config = 2
	provider.Version++
	provider.Model = "different-model"
	seedEvaluationSummaryVersionedRun(t, store, provider, 4, 24, aieval.SuiteFull, "2026-09-09T10:04:00Z", &config)
	legacy := seedEvaluationSummaryVersionedRun(t, store, provider, 3, 24, aieval.SuiteFull, "2026-09-09T10:05:00Z")
	response = performRequest(router, http.MethodGet, "/api/v1/ai/evaluation-summary", nil, nil)
	var envelope struct {
		Data aiEvaluationSummaryResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Groups) != 3 {
		t.Fatalf("configuration/legacy evidence merged: %s", response.Body.String())
	}
	input.ProviderConfigVersion = &config
	if rejected := createEvaluationReviewRequest(t, router, input, "wrong-config-review"); rejected.Code != http.StatusConflict {
		t.Fatalf("stale config accepted: %s", rejected.Body.String())
	}
	var after models.AIEvaluationReview
	if err := store.DB.First(&after, "id = ?", original[0].ID).Error; err != nil || !reflect.DeepEqual(after, original[0]) {
		t.Fatalf("old review changed: %#v %v", after, err)
	}
	if err := store.DB.First(&legacy, "id = ?", legacy.ID).Error; err != nil || legacy.ProviderConfigVersion != nil {
		t.Fatalf("legacy identity guessed: %#v %v", legacy, err)
	}
}

func TestProviderHealthDoesNotBlockConfigurationEdits(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Now().UTC())
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release; w.WriteHeader(http.StatusOK) }))
	defer upstream.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	response := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"Slow probe","kind":"local","protocol":"openai_chat","base_url":"`+upstream.URL+`/v1","model":"a"}`), nil)
	var envelope struct {
		Data aiProviderResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	id := envelope.Data.ID
	probe := make(chan int, 1)
	go func() {
		probe <- performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+id+"/health", nil, map[string]string{"If-Match": `"1"`}).Code
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	patched := make(chan int, 1)
	go func() {
		patched <- performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+id, []byte(`{"model":"b"}`), map[string]string{"If-Match": `"1"`}).Code
	}()
	select {
	case status := <-patched:
		if status != 200 {
			t.Fatalf("edit=%d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("network probe holds provider lock")
	}
	unblock()
	select {
	case status := <-probe:
		if status != http.StatusConflict {
			t.Fatalf("stale health=%d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not finish")
	}
}
