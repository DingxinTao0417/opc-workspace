package agentexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	CapabilityReadReworkContext = "read_rework_context"
	MaxReworkArtifacts          = 4
	MaxReworkContextBytes       = 64 << 10
)

// ReworkContext is explicitly selected business input, not executable authority.
// It contains no paths, credentials, or implicitly collected prior outputs.
type ReworkContext struct {
	SubmissionID string           `json:"submission_id"`
	Sequence     int              `json:"sequence"`
	ReviewReason string           `json:"review_reason"`
	ReviewedAt   string           `json:"reviewed_at"`
	Artifacts    []ReworkArtifact `json:"artifacts"`
}

type ReworkArtifact struct {
	ID          string `json:"id"`
	StorageKind string `json:"storage_kind"`
	Name        string `json:"name"`
	Content     string `json:"content"`
	SHA256      string `json:"sha256"`
}

func ValidateReworkContext(rework ReworkContext) error {
	parsed, err := uuid.Parse(rework.SubmissionID)
	if err != nil || parsed.String() != rework.SubmissionID || rework.Sequence < 1 ||
		strings.TrimSpace(rework.ReviewReason) == "" || !utf8.ValidString(rework.ReviewReason) ||
		strings.ContainsRune(rework.ReviewReason, '\x00') || rework.Artifacts == nil || len(rework.Artifacts) > MaxReworkArtifacts {
		return errors.New("rework context identity or review is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, rework.ReviewedAt); err != nil {
		return errors.New("rework review timestamp is invalid")
	}
	seen := make(map[string]bool, len(rework.Artifacts))
	for _, artifact := range rework.Artifacts {
		id, err := uuid.Parse(artifact.ID)
		if err != nil || id.String() != artifact.ID || seen[artifact.ID] {
			return errors.New("rework artifact identity is invalid")
		}
		seen[artifact.ID] = true
		if artifact.StorageKind != "text" && artifact.StorageKind != "link" && artifact.StorageKind != "structured" {
			return errors.New("rework artifact kind is not supported")
		}
		if !utf8.ValidString(artifact.Name) || strings.ContainsRune(artifact.Name, '\x00') ||
			!utf8.ValidString(artifact.Content) || strings.ContainsRune(artifact.Content, '\x00') {
			return errors.New("rework artifact is not valid UTF-8 text")
		}
		if artifact.StorageKind == "structured" && !json.Valid([]byte(artifact.Content)) {
			return errors.New("rework structured artifact is invalid")
		}
		digest := sha256.Sum256([]byte(artifact.Content))
		if artifact.SHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("rework artifact integrity is invalid")
		}
	}
	encoded, err := json.Marshal(rework)
	if err != nil || len(encoded) > MaxReworkContextBytes {
		return errors.New("rework context exceeds the encoded size limit")
	}
	return nil
}
