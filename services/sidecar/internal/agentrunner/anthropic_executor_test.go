package agentrunner

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

// Exercise the production executor, not a manifest-only child stub.
func TestAnthropicExecutorProcess(t *testing.T) {
	if os.Getenv("OPC_TEST_ANTHROPIC_EXECUTOR") == "1" {
		os.Exit(agentexec.ExecutorMain())
	}
}

func anthropicExecutorCommand() *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestAnthropicExecutorProcess$")
	command.Env = append(os.Environ(), "OPC_TEST_ANTHROPIC_EXECUTOR=1")
	return command
}

func anthropicRunnerInput(endpoint string) agentexec.InputFrame {
	input := runnerInput()
	input.ModelEndpoint = endpoint
	input.ModelProtocol = agentexec.ModelProtocolAnthropicMessages
	input.MaxOutputTokens = agentexec.AnthropicMaxOutputTokens
	input.ModelAPIKey = "private-provider-key"
	return input
}

func TestAnthropicRealExecutorReportsSafeFailureAndFullResults(t *testing.T) {
	for _, test := range []struct {
		name, body, code, wantText string
		status                     int
	}{
		{"complete", `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"完整"},{"type":"text","text":"交付"}]}`, "", "完整交付", 200},
		{"truncated", `{"type":"message","role":"assistant","stop_reason":"max_tokens","content":[{"type":"text","text":"private partial"}]}`, agentexec.ErrorCodeModelTruncated, "", 200},
		{"filtered", `{"type":"message","role":"assistant","stop_reason":"refusal","content":[{"type":"text","text":"private refusal"}]}`, agentexec.ErrorCodeModelFiltered, "", 200},
		{"invalid", `{"type":"message","role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"private tool"}]}`, agentexec.ErrorCodeModelResponseInvalid, "", 200},
		{"empty", `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":""}]}`, agentexec.ErrorCodeEmptyResult, "", 200},
		{"upstream", "private upstream error", agentexec.ErrorCodeModelFailed, "", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			outcome, code, err := Execute(context.Background(), RunRequest{Input: anthropicRunnerInput(server.URL), Timeout: 5 * time.Second, Executor: anthropicExecutorCommand})
			if code != test.code || (err != nil) != (test.code != "") || outcome.ResultText != test.wantText || requests.Load() != 1 {
				t.Fatalf("outcome=%#v code=%s err=%v requests=%d", outcome, code, err, requests.Load())
			}
			if err != nil && (outcome != (RunOutcome{}) || strings.Contains(err.Error(), "private")) {
				t.Fatal("failed result leaked output or private error")
			}
		})
	}
}

func TestAnthropicCancellationReclaimsRealExecutorWithoutRetry(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		outcome RunOutcome
		code    string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		outcome, code, err := Execute(ctx, RunRequest{Input: anthropicRunnerInput(server.URL), Timeout: 5 * time.Second, Executor: anthropicExecutorCommand})
		done <- result{outcome, code, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not send request")
	}
	cancel()
	select {
	case result := <-done:
		if result.err == nil || result.code != CodeCancelled || result.outcome != (RunOutcome{}) || requests.Load() != 1 {
			t.Fatalf("cancellation=%#v requests=%d", result, requests.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not reclaim executor")
	}
}

func TestAnthropicInvalidFrozenProtocolNeverSpawnsExecutor(t *testing.T) {
	input := anthropicRunnerInput("http://127.0.0.1:9/v1/messages")
	input.MaxOutputTokens++
	spawned := false
	outcome, code, err := Execute(context.Background(), RunRequest{Input: input, Timeout: time.Second, Executor: func() *exec.Cmd { spawned = true; return anthropicExecutorCommand() }})
	if err == nil || code != CodeProtocolInvalid || outcome != (RunOutcome{}) || spawned {
		t.Fatalf("invalid protocol ran: code=%s err=%v spawned=%t", code, err, spawned)
	}
}
