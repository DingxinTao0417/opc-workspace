package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

const (
	aiCompactedEvidenceMaxFields = 18
	aiCompactedEvidenceMaxDepth  = 6
	aiCompactedEvidenceMaxNodes  = 512
	aiCompactedEvidenceMaxRunes  = 160
)

// CompactResult creates a deterministic, bounded identity capsule for an
// older read-only workspace result. It never queries the database, calls a
// provider, or changes the original result. Full bodies, descriptions, notes,
// free-form summaries and file content are deliberately omitted.
func (t *aiWorkspaceTool) CompactResult(arguments json.RawMessage, result string) (string, bool) {
	if !t.RequeryableResult() || result == "" {
		return "", false
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(result))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return "", false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", false
	}
	for _, maxFacts := range []int{8, 4, 2, 1} {
		collector := aiCompactedEvidenceCollector{maxFacts: maxFacts}
		collector.visit(decoded, 0)
		if collector.total == 0 || len(collector.facts) == 0 {
			return "", false
		}
		evidence := map[string]any{"facts": collector.facts}
		if omitted := collector.total - len(collector.facts); omitted > 0 {
			evidence["facts_omitted"] = omitted
		}
		if collector.scanLimited {
			evidence["scan_limited"] = true
		}
		capsule := map[string]any{
			"arguments_sha256": aiCompactionDigest(arguments),
			"complete":         false,
			"evidence":         evidence,
			"instruction":      "仅为较早只读结果的有界身份胶囊；未列字段未知，当前事实需要重新查询",
			"kind":             "compacted_read_evidence",
			"original_bytes":   len(result),
			"result_sha256":    aiCompactionDigest([]byte(result)),
			"stale":            true,
			"tool":             t.name,
			"version":          1,
		}
		encoded, err := json.Marshal(capsule)
		if err == nil && len(encoded) <= 4<<10 {
			return string(encoded), true
		}
	}
	return "", false
}

func aiCompactionDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

type aiCompactedEvidenceCollector struct {
	facts       []map[string]any
	maxFacts    int
	total       int
	nodes       int
	scanLimited bool
}

func (c *aiCompactedEvidenceCollector) visit(value any, depth int) {
	if c.scanLimited || depth > aiCompactedEvidenceMaxDepth {
		return
	}
	c.nodes++
	if c.nodes > aiCompactedEvidenceMaxNodes {
		c.scanLimited = true
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if fact := aiCompactedEvidenceFact(typed); len(fact) > 0 {
			c.total++
			if len(c.facts) < c.maxFacts {
				c.facts = append(c.facts, fact)
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			switch typed[key].(type) {
			case map[string]any, []any:
				c.visit(typed[key], depth+1)
			}
		}
	case []any:
		for _, item := range typed {
			c.visit(item, depth+1)
			if c.scanLimited {
				return
			}
		}
	}
}

func aiCompactedEvidenceFact(value map[string]any) map[string]any {
	keys := make([]string, 0, len(value))
	for key, field := range value {
		if aiCompactedEvidenceField(key, field) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := aiCompactedEvidencePriority(keys[i]), aiCompactedEvidencePriority(keys[j])
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	omitted := 0
	if len(keys) > aiCompactedEvidenceMaxFields {
		omitted = len(keys) - aiCompactedEvidenceMaxFields
		keys = keys[:aiCompactedEvidenceMaxFields]
	}
	result := make(map[string]any, len(keys)+1)
	for _, key := range keys {
		result[key] = aiCompactedEvidenceValue(value[key])
	}
	if omitted > 0 {
		result["_fields_omitted"] = omitted
	}
	return result
}

func aiCompactedEvidenceField(key string, value any) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, char := range key {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	allowed := key == "id" || key == "type" || key == "kind" || key == "mode" ||
		key == "status" || key == "state" || key == "version" || key == "attempt" ||
		key == "title" || key == "name" || key == "label" || key == "subject" ||
		key == "offset" || key == "limit" || key == "total" || key == "next_offset" ||
		key == "has_more" || key == "window_limited" || key == "required" ||
		key == "active" || key == "enabled" || key == "accepted" || key == "deleted" ||
		key == "archived" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids") ||
		strings.HasSuffix(key, "_version") || strings.HasSuffix(key, "_status") ||
		strings.HasSuffix(key, "_state") || strings.HasSuffix(key, "_count") ||
		strings.HasSuffix(key, "_total") || strings.HasSuffix(key, "_at") ||
		strings.HasSuffix(key, "_on") || strings.HasSuffix(key, "_date") ||
		strings.HasSuffix(key, "_sha256")
	if !allowed {
		return false
	}
	switch typed := value.(type) {
	case nil, bool, json.Number, string:
		return true
	case []any:
		if !strings.HasSuffix(key, "_ids") || len(typed) > 16 {
			return false
		}
		for _, item := range typed {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func aiCompactedEvidencePriority(key string) int {
	switch {
	case key == "id", strings.HasSuffix(key, "_id"), strings.HasSuffix(key, "_ids"):
		return 0
	case key == "version", strings.HasSuffix(key, "_version"), key == "attempt":
		return 1
	case key == "status", key == "state", strings.HasSuffix(key, "_status"), strings.HasSuffix(key, "_state"):
		return 2
	case key == "type", key == "kind", key == "mode":
		return 3
	case key == "title", key == "name", key == "label", key == "subject":
		return 4
	case key == "offset", key == "limit", key == "total", key == "next_offset", key == "has_more", key == "window_limited", strings.HasSuffix(key, "_count"), strings.HasSuffix(key, "_total"):
		return 5
	case strings.HasSuffix(key, "_sha256"):
		return 6
	default:
		return 7
	}
}

func aiCompactedEvidenceValue(value any) any {
	switch typed := value.(type) {
	case string:
		runes := []rune(typed)
		if len(runes) > aiCompactedEvidenceMaxRunes {
			return string(runes[:aiCompactedEvidenceMaxRunes]) + "…"
		}
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, aiCompactedEvidenceValue(item).(string))
		}
		return result
	default:
		return value
	}
}
