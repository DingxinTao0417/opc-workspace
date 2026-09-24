package api

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

func TestAIContextWindowNoticeAndHandoffShareFinalResponseBudget(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		result := harness.Result{Text: "verified output", Reasoning: "private reasoning", ContextTrimmedTurns: 2, BudgetHandoff: handoff}
		text, err := aiCompletedRunText(result)
		if err != nil || !strings.HasPrefix(text, aiContextWindowNotice+"\n\n") || !strings.HasSuffix(text, result.Text) {
			t.Fatalf("completed text = %q, %v", text, err)
		}
		if strings.Contains(text, aiBudgetHandoffNotice) != handoff || strings.Contains(text, result.Reasoning) {
			t.Fatalf("unexpected notice/reasoning in %q", text)
		}
		prefixSize := len(text) - len(result.Text)
		result.Text = strings.Repeat("x", modelclient.MaxResponseBytes-prefixSize-len(result.Reasoning))
		text, err = aiCompletedRunText(result)
		if err != nil || len(text)+len(result.Reasoning) != modelclient.MaxResponseBytes {
			t.Fatalf("exact limit rejected: %v", err)
		}
		result.Text += "x"
		text, err = aiCompletedRunText(result)
		if !errors.Is(err, modelclient.ErrResponseBudget) || text != "" {
			t.Fatalf("overflow must not truncate or claim success: %v, %d", err, len(text))
		}
	}
	text, err := aiCompletedRunText(harness.Result{Text: "unchanged"})
	if err != nil || text != "unchanged" {
		t.Fatalf("ordinary text changed: %q, %v", text, err)
	}
}

func TestAIToolResultCompactionNoticeIsPersistentAndBudgeted(t *testing.T) {
	for _, historyTrimmed := range []bool{false, true} {
		result := harness.Result{Text: "verified answer", Reasoning: "private reasoning", CompactedToolResults: 2, BudgetHandoff: true}
		if historyTrimmed {
			result.ContextTrimmedTurns = 1
		}
		prefix := aiToolResultCompactionNotice + "\n\n" + aiBudgetHandoffNotice + "\n\n"
		if historyTrimmed {
			prefix = aiContextWindowNotice + "\n\n" + prefix
		}
		text, err := aiCompletedRunText(result)
		if err != nil || text != prefix+result.Text || strings.Contains(text, result.Reasoning) {
			t.Fatalf("persistent notice = %q, %v", text, err)
		}
		result.Text = strings.Repeat("x", modelclient.MaxResponseBytes-len(prefix)-len(result.Reasoning))
		if _, err := aiCompletedRunText(result); err != nil {
			t.Fatalf("exact budget rejected: %v", err)
		}
		result.Text += "x"
		if text, err := aiCompletedRunText(result); !errors.Is(err, modelclient.ErrResponseBudget) || text != "" {
			t.Fatalf("notice overflow truncated text: %v %q", err, text)
		}
	}
}

func TestAIContextWindowLiveProgressOnlyDisclosesBoundedCounts(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []string{"model_turn", "self_check", "tool_call", "citation_validation"} {
		step := harness.RunStep{Kind: kind, Status: "succeeded", TurnIndex: 2, StartedAt: now, CompletedAt: now, TrimmedHistoryTurns: 3, CompactedToolResults: 2}
		progress := aiProgressFromStep(2, step)
		encoded, err := json.Marshal(progress)
		if err != nil {
			t.Fatal(err)
		}
		isModel := kind == "model_turn" || kind == "self_check"
		if strings.Contains(string(encoded), `"trimmed_history_turns":3`) != isModel {
			t.Fatalf("unexpected count for %s: %s", kind, encoded)
		}
		if strings.Contains(string(encoded), `"compacted_tool_results":2`) != isModel {
			t.Fatalf("unexpected compacted count for %s: %s", kind, encoded)
		}
	}
	for _, value := range []int{-1, 0, 201} {
		progress := aiProgressFromStep(2, harness.RunStep{Kind: "model_turn", Status: "running", StartedAt: now, TrimmedHistoryTurns: value})
		if progress.TrimmedHistoryTurns != min(200, max(0, value)) {
			t.Fatalf("unbounded count: %+v", progress)
		}
	}
	for _, value := range []int{-1, 0, harness.DefaultMaxToolCalls + 1} {
		progress := aiProgressFromStep(2, harness.RunStep{Kind: "model_turn", Status: "running", StartedAt: now, CompactedToolResults: value})
		if progress.CompactedToolResults != min(harness.DefaultMaxToolCalls, max(0, value)) {
			t.Fatalf("unbounded compacted count: %+v", progress)
		}
	}
}
