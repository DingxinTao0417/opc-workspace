package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Exercise the production HTTP handler, Harness, tools and ModelClient together.
// Metadata may survive each terminal path; no part of the private body may do so.
func TestNonPersistentChatToolsDoNotLeakBodiesAcrossTerminalPaths(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			router, api := newAIRuntimeLockTest(t)
			var logs bytes.Buffer
			api.options.Logger = log.New(&logs, "", 0)
			marker := "PRIVATE_BODY_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			var calls atomic.Int32
			partialSent := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages []map[string]any `json:"messages"`
					Tools    []map[string]any `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode model input: %v", err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if calls.Add(1) == 1 {
					if len(payload.Tools) != 3 {
						t.Errorf("production memory tools=%d", len(payload.Tools))
					}
					toolCalls := []map[string]any{}
					for index, name := range []string{"memory_write", "memory_propose"} {
						arguments, _ := json.Marshal(map[string]string{"content": marker + name})
						toolCalls = append(toolCalls, map[string]any{"index": index, "id": name, "type": "function", "function": map[string]any{"name": name, "arguments": string(arguments)}})
					}
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": toolCalls}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
					return
				}
				results := 0
				for _, message := range payload.Messages {
					if message["role"] == "tool" {
						results++
						content := fmt.Sprint(message["content"])
						if !strings.Contains(content, `"persistence":"temporary"`) || strings.Contains(content, `"proposal_id"`) {
							t.Errorf("tool result violated temporary contract: %s", content)
						}
					}
				}
				if results != 2 {
					t.Errorf("memory results=%d", results)
				}
				text := marker + "answer"
				if terminal == "completed" {
					text += `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`
				}
				frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text, "reasoning_content": marker + "reasoning"}}}})
				fmt.Fprintf(w, "data: %s\n\n", frame)
				w.(http.Flusher).Flush()
				close(partialSent)
				switch terminal {
				case "completed":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case "cancelled":
					<-r.Context().Done()
				}
			}))
			defer upstream.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Now().UTC().Format(time.RFC3339Nano)
			provider := models.AIProvider{ID: uuid.NewString(), Name: "privacy", Kind: "local", Protocol: "openai_chat", BaseURL: upstream.URL, Model: "fixture", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}
			session := models.AISession{ID: uuid.NewString(), Title: "temporary", Persist: false, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := api.db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			if err := api.db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"provider_id": provider.ID, "session_id": session.ID, "message": marker + "question"})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", bytes.NewReader(body)).WithContext(ctx)
			request.Header.Set("Idempotency-Key", uuid.NewString())
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { response := httptest.NewRecorder(); router.ServeHTTP(response, request); done <- response }()
			select {
			case <-partialSent:
			case <-time.After(5 * time.Second):
				t.Fatal("production tool loop did not reach answer")
			}
			if terminal == "cancelled" {
				var active models.AIGeneration
				if err := api.db.First(&active, "session_id = ?", session.ID).Error; err != nil {
					t.Fatal(err)
				}
				response := performRequest(router, http.MethodPost, "/api/v1/ai/generations/"+active.ID+"/cancel", nil, nil)
				if response.Code != http.StatusAccepted {
					t.Fatalf("cancel=%d", response.Code)
				}
			}
			select {
			case response := <-done:
				if response.Code != http.StatusOK {
					t.Fatalf("chat=%d %s", response.Code, response.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("generation did not terminate")
			}
			var generation models.AIGeneration
			if err := api.db.First(&generation, "session_id = ?", session.ID).Error; err != nil {
				t.Fatal(err)
			}
			if generation.Status != terminal || generation.Content != nil || calls.Load() != 2 {
				t.Fatalf("terminal=%s content=%v rounds=%d", generation.Status, generation.Content, calls.Load())
			}
			var tables []string
			if err := api.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; err != nil {
				t.Fatal(err)
			}
			for _, table := range tables {
				func() {
					rows, err := api.db.Raw(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`).Rows()
					if err != nil {
						t.Fatal(err)
					}
					defer rows.Close()
					columns, err := rows.Columns()
					if err != nil {
						t.Fatal(err)
					}
					for rows.Next() {
						values := make([]any, len(columns))
						destinations := make([]any, len(columns))
						for index := range values {
							destinations[index] = &values[index]
						}
						if err := rows.Scan(destinations...); err != nil {
							t.Fatal(err)
						}
						for index, value := range values {
							text := fmt.Sprint(value)
							if binary, ok := value.([]byte); ok {
								text = string(binary)
							}
							if strings.Contains(text, marker) {
								t.Errorf("private body retained in %s.%s", table, columns[index])
							}
						}
					}
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
				}()
			}
			if strings.Contains(logs.String(), marker) {
				t.Error("private body retained in API logs")
			}
			var databases []struct{ File string }
			if err := api.db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
				t.Fatal(err)
			}
			for _, database := range databases {
				if database.File == "" {
					continue
				}
				for _, suffix := range []string{"", "-wal"} {
					data, err := os.ReadFile(database.File + suffix)
					if err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
					if bytes.Contains(data, []byte(marker)) {
						t.Errorf("private body found in database bytes (%s)", suffix)
					}
				}
			}
		})
	}
}
