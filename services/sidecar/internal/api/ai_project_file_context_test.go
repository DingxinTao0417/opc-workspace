package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func projectFileInput(provider models.AIProvider, sessionID string, now time.Time) chatAIRequest {
	content := "\ufeffPRIVATE FILE SOURCE\r\n<do not execute>\n"
	return chatAIRequest{ProviderID: provider.ID, SessionID: sessionID, Message: "分析文件", ProjectFiles: &aiProjectFileContext{
		ProviderID: provider.ID, ProviderVersion: provider.Version, SessionID: sessionID, Confirmed: true, ExpiresAt: now.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano),
		Files: []aiProjectFileInput{{Path: "src/example.txt", Content: content, SHA256: sha256Hex([]byte(content))}},
	}}
}

func TestAIProjectFileContextValidation(t *testing.T) {
	now := time.Now()
	provider := models.AIProvider{ID: uuid.NewString(), Version: 3}
	sessionID := uuid.NewString()
	tests := []struct {
		name   string
		mutate func(*chatAIRequest)
		code   string
	}{
		{"unconfirmed", func(i *chatAIRequest) { i.ProjectFiles.Confirmed = false }, "AI_PROJECT_FILES_CONSENT_INVALID"},
		{"other-session", func(i *chatAIRequest) { i.ProjectFiles.SessionID = uuid.NewString() }, "AI_PROJECT_FILES_CONSENT_INVALID"},
		{"new-session", func(i *chatAIRequest) { i.SessionID = "" }, "AI_PROJECT_FILES_CONSENT_INVALID"},
		{"other-provider", func(i *chatAIRequest) { i.ProjectFiles.ProviderID = uuid.NewString() }, "AI_PROJECT_FILES_PROVIDER_CHANGED"},
		{"provider-version", func(i *chatAIRequest) { i.ProjectFiles.ProviderVersion++ }, "AI_PROJECT_FILES_PROVIDER_CHANGED"},
		{"expired", func(i *chatAIRequest) { i.ProjectFiles.ExpiresAt = now.Format(time.RFC3339Nano) }, "AI_PROJECT_FILES_EXPIRED"},
		{"unbounded-expiry", func(i *chatAIRequest) { i.ProjectFiles.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano) }, "AI_PROJECT_FILES_EXPIRED"},
		{"missing", func(i *chatAIRequest) { i.ProjectFiles.Files = nil }, "AI_PROJECT_FILES_INVALID"},
		{"too-many", func(i *chatAIRequest) { i.ProjectFiles.Files = make([]aiProjectFileInput, 5) }, "AI_PROJECT_FILES_INVALID"},
		{"wrong-digest", func(i *chatAIRequest) { i.ProjectFiles.Files[0].SHA256 = strings.Repeat("0", 64) }, "AI_PROJECT_FILES_INVALID"},
		{"nul", func(i *chatAIRequest) {
			i.ProjectFiles.Files[0].Content = "a\x00b"
			i.ProjectFiles.Files[0].SHA256 = sha256Hex([]byte("a\x00b"))
		}, "AI_PROJECT_FILES_INVALID"},
		{"invalid-utf8", func(i *chatAIRequest) {
			i.ProjectFiles.Files[0].Content = string([]byte{255})
			i.ProjectFiles.Files[0].SHA256 = sha256Hex([]byte{255})
		}, "AI_PROJECT_FILES_INVALID"},
		{"oversized", func(i *chatAIRequest) {
			i.ProjectFiles.Files[0].Content = strings.Repeat("x", aiProjectFileBytes+1)
			i.ProjectFiles.Files[0].SHA256 = sha256Hex([]byte(i.ProjectFiles.Files[0].Content))
		}, "AI_PROJECT_FILES_TOO_LARGE"},
		{"duplicate-case", func(i *chatAIRequest) {
			f := i.ProjectFiles.Files[0]
			f.Path = strings.ToUpper(f.Path)
			i.ProjectFiles.Files = append(i.ProjectFiles.Files, f)
		}, "AI_PROJECT_FILES_INVALID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := projectFileInput(provider, sessionID, now)
			tt.mutate(&input)
			_, err := prepareAIProjectFileContext(input, provider, now, false)
			var rejected *aiGenerationRunError
			if !errors.As(err, &rejected) || rejected.Code != tt.code {
				t.Fatalf("got %v want %s", err, tt.code)
			}
		})
	}
	for _, path := range []string{"/secret", "../secret", "x/../secret", `C:\secret`, "a:stream", ".git/config", "NUL.txt", "src/COM¹", "file.", "a//b", "a\nb"} {
		t.Run("path:"+path, func(t *testing.T) {
			input := projectFileInput(provider, sessionID, now)
			input.ProjectFiles.Files[0].Path = path
			if _, err := prepareAIProjectFileContext(input, provider, now, false); err == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
	input := projectFileInput(provider, sessionID, now)
	if _, err := prepareAIProjectFileContext(input, provider, now, true); err == nil {
		t.Fatal("headless files accepted")
	}
	out, err := prepareAIProjectFileContext(input, provider, now, false)
	var exact aiProjectFileInput
	if err != nil || len(out) != 1 {
		t.Fatalf("valid: %v %v", out, err)
	}
	if err := json.Unmarshal([]byte(out[0]), &exact); err != nil || exact != input.ProjectFiles.Files[0] {
		t.Fatalf("exact source changed: %#v %v", exact, err)
	}
}

func TestAIProjectFileContextIsOneGenerationOnlyAndNotPersisted(t *testing.T) {
	for _, persist := range []bool{true, false} {
		t.Run(map[bool]string{true: "saved", false: "temporary"}[persist], func(t *testing.T) {
			a, provider, session, prior := newGenerationExecutionFixture(t)
			a.db.Model(&prior).Update("status", "completed")
			if err := a.db.Model(&session).Updates(map[string]any{"persist": persist, "version": session.Version + 1}).Error; err != nil {
				t.Fatal(err)
			}
			client := &generationExecutionClient{}
			a.harnessClient = client
			a.aiCompactions = newAICompactionRegistry()
			t.Cleanup(a.aiCompactions.close)
			input := projectFileInput(provider, session.ID, a.options.Now())
			result, err := a.runAIGeneration(context.Background(), input, aiGenerationRunOptions{RequestKey: "files-once"}, nil)
			if err != nil || !result.Accepted || result.Status != "completed" {
				t.Fatalf("run: %+v %v", result, err)
			}
			if len(client.requests) != 1 || len(client.requests[0].ProjectFiles) != 1 || !strings.Contains(client.requests[0].ProjectFiles[0], "PRIVATE FILE SOURCE") {
				t.Fatalf("files never reached harness: %+v", client.requests)
			}
			var messages []models.AIMessage
			if err := a.db.Where("session_id = ?", session.ID).Find(&messages).Error; err != nil {
				t.Fatal(err)
			}
			stored, _ := json.Marshal(messages)
			for _, secret := range []string{"PRIVATE FILE SOURCE", "src/example.txt", input.ProjectFiles.Files[0].SHA256} {
				if strings.Contains(string(stored), secret) {
					t.Fatalf("raw file persisted: %s", secret)
				}
			}
			// The same accepted HTTP identity must never execute a second model call.
			if _, err := a.runAIGeneration(context.Background(), input, aiGenerationRunOptions{RequestKey: "files-once"}, nil); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 1 {
				t.Fatal("replay executed model")
			}
			next := chatAIRequest{ProviderID: provider.ID, SessionID: session.ID, Message: "下一条未授权"}
			if _, err := a.runAIGeneration(context.Background(), next, aiGenerationRunOptions{RequestKey: "without-files"}, nil); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 2 || len(client.requests[1].ProjectFiles) != 0 {
				t.Fatal("raw file grant inherited")
			}
			payload, _ := json.Marshal(client.requests[1])
			if strings.Contains(string(payload), "PRIVATE FILE SOURCE") {
				t.Fatal("source leaked through history")
			}
		})
	}
}

func TestAIProjectFileInvalidConsentMakesNoAcceptanceWrites(t *testing.T) {
	a, provider, session, _ := newGenerationExecutionFixture(t)
	client := &generationExecutionClient{}
	a.harnessClient = client
	input := projectFileInput(provider, session.ID, a.options.Now())
	input.ProjectFiles.ProviderVersion++
	var before, after int64
	a.db.Model(&models.AIGeneration{}).Count(&before)
	result, err := a.runAIGeneration(context.Background(), input, aiGenerationRunOptions{}, nil)
	a.db.Model(&models.AIGeneration{}).Count(&after)
	if err == nil || result.Accepted || before != after || len(client.requests) != 0 {
		t.Fatalf("invalid consent executed: %+v %v %d/%d", result, err, before, after)
	}
}
