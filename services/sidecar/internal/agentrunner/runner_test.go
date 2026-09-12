package agentrunner

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

// The test binary re-executes itself as a stand-in builtin executor so the
// lifecycle matrix runs against a real child process on every platform.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_AGENT_EXECUTOR") == "1" {
		mode := os.Getenv("AGENT_TEST_MODE")
		reader := bufio.NewReader(os.Stdin)
		var input agentexec.InputFrame
		if err := agentexec.ReadFrame(reader, &input); err != nil {
			os.Exit(3)
		}
		manifest := agentexec.ManifestFrame{
			ProtocolVersion: agentexec.ProtocolVersion, RunID: input.RunID, Nonce: input.Nonce,
		}
		switch mode {
		case "succeed":
			manifest.Result = agentexec.Result{Type: "text", Text: "完成的交付文本"}
		case "error":
			manifest.Result = agentexec.Result{Type: "error", Text: "AGENT_MODEL_UNAVAILABLE"}
		case "mismatch":
			manifest.Nonce = "bogus-nonce"
		case "hang":
			os.Exit(0) // exit without a manifest
		default:
			time.Sleep(60 * time.Second) // never answers; the runner must reclaim it
		}
		_ = agentexec.WriteFrame(os.Stdout, manifest)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runnerInput() agentexec.InputFrame {
	return agentexec.InputFrame{
		ProtocolVersion: agentexec.ProtocolVersion,
		RunID:           "018f0000-0000-7000-8000-00000000a001",
		Nonce:           "nonce-1",
		Capabilities:    []string{"read_task_snapshot", "write_text_result"},
		Input:           agentexec.TaskSnapshot{TaskID: "task-1", Title: "写周报", Status: "todo", Kind: "delivery"},
		ModelEndpoint:   "http://127.0.0.1:9/v1/chat/completions",
		Model:           "local-test",
		MaxResultBytes:  agentexec.MaxResultBytes,
		Instruction:     "产出交付文本",
	}
}

func fakeExecutor(mode string) func() *exec.Cmd {
	return func() *exec.Cmd {
		command := exec.Command(os.Args[0], "-test.run=^TestMain$")
		command.Env = append(os.Environ(),
			"GO_WANT_AGENT_EXECUTOR=1", "AGENT_TEST_MODE="+mode)
		return command
	}
}

func TestExecuteReturnsTextManifest(t *testing.T) {
	outcome, code, err := Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("succeed"),
	})
	if err != nil || code != "" {
		t.Fatalf("Execute() = %q %v, want success", code, err)
	}
	if outcome.ResultText != "完成的交付文本" {
		t.Fatalf("result = %q", outcome.ResultText)
	}
}

func TestExecuteMapsExecutorErrorFrameToStableCode(t *testing.T) {
	outcome, code, err := Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("error"),
	})
	if err == nil {
		t.Fatal("expected executor error")
	}
	if code != "AGENT_MODEL_UNAVAILABLE" || outcome.ResultText != "" {
		t.Fatalf("code=%q result=%q, want stable executor code", code, outcome.ResultText)
	}
}

func TestExecuteTimesOutAndReclaimsProcess(t *testing.T) {
	started := time.Now()
	_, code, err := Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 1200 * time.Millisecond, Executor: fakeExecutor("hang-forever"),
	})
	if err == nil || code != CodeTimedOut {
		t.Fatalf("code=%q err=%v, want timeout", code, err)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("timeout reclaim took %v, grace period leaked", elapsed)
	}
}

func TestExecuteRejectsIdentityMismatch(t *testing.T) {
	input := runnerInput()
	input.Nonce = "different-from-frame"
	_, code, err := Execute(context.Background(), RunRequest{
		Input: input, Timeout: 10 * time.Second, Executor: fakeExecutor("mismatch"),
	})
	if err == nil || code != CodeProtocolInvalid {
		t.Fatalf("code=%q err=%v, want protocol invalid", code, err)
	}
}
