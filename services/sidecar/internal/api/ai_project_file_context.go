package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Explicit, transient file bytes from the trusted desktop. This request never
// gives the Sidecar a native root/snapshot handle or filesystem access. The
// desktop owns exact native revalidation; the server validates the selected
// provider/session, disclosure bounds and content digest independently.
type aiProjectFileContext struct {
	ProviderID      string               `json:"provider_id"`
	ProviderVersion int64                `json:"provider_version"`
	SessionID       string               `json:"session_id"`
	Confirmed       bool                 `json:"confirmed"`
	ExpiresAt       string               `json:"expires_at"`
	Files           []aiProjectFileInput `json:"files"`
}
type aiProjectFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

const aiProjectFileBytes = 32 * 1024

func validAIProjectFilePath(path string) bool {
	if path == "" || len(path) > 4096 || strings.ContainsAny(path, `\:<>"|?*`) {
		return false
	}
	for _, c := range path {
		if unicode.IsControl(c) {
			return false
		}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		switch stem {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		for _, prefix := range []string{"COM", "LPT"} {
			if strings.HasPrefix(stem, prefix) && strings.Contains("|1|2|3|4|5|6|7|8|9|¹|²|³|", "|"+strings.TrimPrefix(stem, prefix)+"|") {
				return false
			}
		}
	}
	return utf8.ValidString(path)
}

func projectFileContextError(code, message string) error {
	return &aiGenerationRunError{http.StatusUnprocessableEntity, code, message}
}

func prepareAIProjectFileContext(input chatAIRequest, provider models.AIProvider, now time.Time, headless bool) ([]string, error) {
	grant := input.ProjectFiles
	if grant == nil {
		return nil, nil
	}
	if headless {
		return nil, projectFileContextError("AI_PROJECT_FILES_HEADLESS", "Project files require a new explicit desktop message, not background continuation")
	}
	id, err := uuid.Parse(input.SessionID)
	if err != nil || id.String() != input.SessionID || grant.SessionID != input.SessionID || !grant.Confirmed {
		return nil, projectFileContextError("AI_PROJECT_FILES_CONSENT_INVALID", "Confirm project files for this existing conversation before sending")
	}
	if grant.ProviderID != input.ProviderID || grant.ProviderID != provider.ID || grant.ProviderVersion < 1 || grant.ProviderVersion != provider.Version {
		return nil, &aiGenerationRunError{http.StatusConflict, "AI_PROJECT_FILES_PROVIDER_CHANGED", "The model configuration changed; review file disclosure again"}
	}
	expires, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil || !expires.After(now) || expires.After(now.Add(10*time.Minute+15*time.Second)) {
		return nil, projectFileContextError("AI_PROJECT_FILES_EXPIRED", "The project file approval expired; read and confirm again")
	}
	if len(grant.Files) < 1 || len(grant.Files) > 4 {
		return nil, projectFileContextError("AI_PROJECT_FILES_INVALID", "Select between one and four project files")
	}
	seen := map[string]bool{}
	total := 0
	out := make([]string, 0, len(grant.Files))
	for _, file := range grant.Files {
		key := strings.ToLower(file.Path)
		if !validAIProjectFilePath(file.Path) || seen[key] || !utf8.ValidString(file.Content) || strings.ContainsRune(file.Content, 0) || file.SHA256 != sha256Hex([]byte(file.Content)) {
			return nil, projectFileContextError("AI_PROJECT_FILES_INVALID", "The selected file path or exact content digest is invalid; read again")
		}
		seen[key] = true
		total += len(file.Content)
		if total > aiProjectFileBytes {
			return nil, projectFileContextError("AI_PROJECT_FILES_TOO_LARGE", "Selected project files exceed 32 KiB; select a smaller complete file, not truncated content")
		}
		encoded, err := json.Marshal(file)
		if err != nil {
			return nil, projectFileContextError("AI_PROJECT_FILES_INVALID", "The selected project file cannot be encoded")
		}
		out = append(out, string(encoded))
	}
	return out, nil
}
