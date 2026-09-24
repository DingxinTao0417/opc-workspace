package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func recoveryCitationFixture() aiGenerationCitations {
	return aiGenerationCitations{CitationStatus: "validated", Citations: []aiCitationItem{{
		ChunkID: uuid.NewString(), SourceID: uuid.NewString(), DocumentID: uuid.NewString(),
		SourceName: "private-guide.pdf", DocumentTitle: "private-guide.pdf", SourceType: "pdf",
		SourceVersion: 2, DocumentVersion: 3, EndChar: 80, StartLine: 1, EndLine: 2, StartPage: 3, EndPage: 3,
	}}}
}

func TestAIGenerationCitationCacheBoundsAndLifecycle(t *testing.T) {
	r := newAIGenerationRegistry()
	now := time.Now()
	evidence := recoveryCitationFixture()
	r.rememberCitations("g", "s", evidence, now)
	evidence.Citations[0].SourceName = "mutated"
	r.release("g") // Finishing must erase bodies, but not the short-lived metadata.
	got := r.citationSnapshot("g", "s", now)
	if got == nil || got.Citations[0].SourceName != "private-guide.pdf" || r.citationSnapshot("g", "other", now) != nil {
		t.Fatalf("cache identity/copy: %+v", got)
	}
	got.Citations[0].SourceName = "read mutation"
	if r.citationSnapshot("g", "s", now).Citations[0].SourceName != "private-guide.pdf" {
		t.Fatal("reader mutated shared evidence")
	}
	if r.citationSnapshot("g", "s", now.Add(aiCitationRecoveryTTL)) != nil || len(r.citations) != 0 {
		t.Fatal("expired citation retained")
	}
	for i := 0; i <= aiCitationRecoveryLimit; i++ {
		r.rememberCitations(fmt.Sprint(i), "s", evidence, now.Add(time.Duration(i)*time.Second))
	}
	if len(r.citations) != aiCitationRecoveryLimit || r.citationSnapshot("0", "s", now) != nil {
		t.Fatal("cache not bounded / oldest not evicted")
	}
	r.forgetSessionCitations("s")
	if len(r.citations) != 0 {
		t.Fatal("deleted session metadata retained")
	}
	evidence.Citations[0].SourceName = strings.Repeat("密", 6000)
	r.rememberCitations("oversized", "s", evidence, now)
	if len(r.citations) != 0 {
		t.Fatal("oversized metadata cached")
	}
	r.rememberCitations("restore", "s", recoveryCitationFixture(), now)
	ctx, cancel := context.WithCancel(context.Background())
	r.register("active", "p", "s", cancel)
	r.cancelAll()
	if len(r.citations) != 0 || ctx.Err() == nil {
		t.Fatal("restore did not clear metadata and cancel active work")
	}
}

func TestAIGenerationCitationRecoveryPersistenceAndDeletion(t *testing.T) {
	for _, persistent := range []bool{true, false} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			r, a := newAIRuntimeLockTest(t)
			r.DELETE("/api/v1/ai/sessions/:id", a.deleteAISession)
			now := nowStamp(a)
			provider := models.AIProvider{ID: uuid.NewString(), Name: "recovery", Kind: "local", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:1", Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}
			session := models.AISession{ID: uuid.NewString(), Title: "temporary", Persist: persistent, Version: 1, CreatedAt: now, UpdatedAt: now}
			generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: now, UpdatedAt: now, RequestKey: aiStringPtr("recover-key"), RequestHash: aiStringPtr(strings.Repeat("a", 64))}
			for _, row := range []any{&provider, &session, &generation} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			evidence := recoveryCitationFixture()
			encoded, err := encodeAICitationSnapshot(aiCitationSnapshot{Version: 1, Status: evidence.CitationStatus, Items: evidence.Citations})
			if err != nil {
				t.Fatal(err)
			}
			// Missing run root makes persistence fail: neither the message nor
			// a citation result may escape the rolled-back completion.
			if err := a.finalizeCompletedGeneration(generation, &session, "private answer", "private reasoning", provider, encoded, nil, now); err == nil {
				t.Fatal("missing root unexpectedly completed")
			}
			var messageCount int64
			a.db.Model(&models.AIMessage{}).Count(&messageCount)
			if messageCount != 0 || len(a.aiGenerations.citations) != 0 {
				t.Fatal("rollback leaked result")
			}
			if err := createAIRunRootStep(a.db, generation); err != nil {
				t.Fatal(err)
			}
			if err := a.finalizeCompletedGeneration(generation, &session, "private answer", "private reasoning", provider, encoded, nil, now); err != nil {
				t.Fatal(err)
			}
			a.aiGenerations.release(generation.ID)
			for _, path := range []string{generation.ID, "by-request/recover-key"} {
				response := performRequest(r, http.MethodGet, "/api/v1/ai/generations/"+path, nil, nil)
				var body struct {
					Data aiGenerationResponse `json:"data"`
				}
				body.Data.aiGenerationCitations = &aiGenerationCitations{}
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Data.aiGenerationCitations == nil || body.Data.CitationStatus != "validated" || body.Data.Citations[0].StartPage != 3 || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(response.Code, response.Body.String())
				}
				if persistent != (body.Data.Content == "private answer") || persistent != (body.Data.Reasoning == "private reasoning") {
					t.Fatal("wrong body lifetime", response.Body.String())
				}
			}
			if !persistent {
				var count int64
				a.db.Model(&models.AIMessage{}).Count(&count)
				if count != 0 {
					t.Fatal("temporary message persisted")
				}
				a.db.First(&generation, "id = ?", generation.ID)
				if generation.Content != nil {
					t.Fatal("temporary generation persisted")
				}
				// Lost cache is unknown, never relabelled as not_requested.
				a.aiGenerations.citationSnapshot(generation.ID, session.ID, time.Now().Add(aiCitationRecoveryTTL))
				response := performRequest(r, http.MethodGet, "/api/v1/ai/generations/"+generation.ID, nil, nil)
				if strings.Contains(response.Body.String(), "citation_status") || strings.Contains(response.Body.String(), "private") {
					t.Fatal(response.Body.String())
				}
				a.aiGenerations.rememberCitations(generation.ID, session.ID, evidence, time.Now())
			}
			deleted := performRequest(r, http.MethodDelete, "/api/v1/ai/sessions/"+session.ID, nil, map[string]string{"If-Match": `"2"`})
			if deleted.Code != 200 || len(a.aiGenerations.citations) != 0 {
				t.Fatal("delete did not clear cache", deleted.Code, deleted.Body.String())
			}
			if response := performRequest(r, http.MethodGet, "/api/v1/ai/generations/"+generation.ID, nil, nil); response.Code != 404 {
				t.Fatal("deleted metadata accessible")
			}
		})
	}
}
