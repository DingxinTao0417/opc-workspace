package agentrunner

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

// The test binary re-executes itself as a stand-in builtin executor so the
// lifecycle matrix runs against a real child process on every platform.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_AGENT_EXECUTOR") == "1" {
		mode := os.Getenv("AGENT_TEST_MODE")
		switch mode {
		case "never-read":
			time.Sleep(5 * time.Second)
			os.Exit(0)
		case "partial-read":
			_, _ = os.Stdin.Read(make([]byte, 8))
			time.Sleep(5 * time.Second)
			os.Exit(0)
		}
		reader := bufio.NewReader(os.Stdin)
		var input agentexec.InputFrame
		if err := agentexec.ReadFrame(reader, &input); err != nil {
			os.Exit(3)
		}
		manifest := agentexec.ManifestFrame{
			ProtocolVersion: agentexec.ProtocolVersion, RunID: input.RunID, Nonce: input.Nonce,
		}
		switch mode {
		case "succeed", "succeed-tail", "succeed-second-frame", "succeed-nonzero", "succeed-linger":
			manifest.Result = agentexec.Result{Type: "text", Text: "完成的交付文本"}
		case "succeed-file":
			manifest.Result = agentexec.Result{Type: "file", Text: "# 完成的文件\n"}
		case "error", "error-nonzero":
			manifest.Result = agentexec.Result{Type: "error", Text: "AGENT_MODEL_UNAVAILABLE"}
		case "error-unknown":
			manifest.Result = agentexec.Result{Type: "error", Text: "AGENT_PRIVATE_CHILD_FAILURE"}
		case "error-overlong":
			manifest.Result = agentexec.Result{Type: "error", Text: strings.Repeat("A", 4096)}
		case "error-malicious":
			manifest.Result = agentexec.Result{Type: "error", Text: "AGENT_MODEL_UNAVAILABLE\nPRIVATE model detail"}
		case "mismatch":
			manifest.Nonce = "bogus-nonce"
		case "hang":
			os.Exit(0) // exit without a manifest
		default:
			time.Sleep(60 * time.Second) // never answers; the runner must reclaim it
		}
		_ = agentexec.WriteFrame(os.Stdout, manifest)
		switch mode {
		case "succeed-tail":
			_, _ = os.Stdout.Write([]byte("unexpected-tail"))
		case "succeed-second-frame":
			_ = agentexec.WriteFrame(os.Stdout, manifest)
		case "succeed-nonzero", "error-nonzero":
			os.Exit(7)
		case "succeed-linger":
			// Closing stdout proves that the runner separately requires a
			// normal process exit after it has verified the stream EOF.
			_ = os.Stdout.Close()
			time.Sleep(60 * time.Second)
		}
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

func largeRunnerInput() agentexec.InputFrame {
	input := runnerInput()
	// Keep the frame below MaxFrameBytes while making it much larger than the
	// default anonymous-pipe buffer on supported development platforms.
	input.Instruction = strings.Repeat("x", 900<<10)
	return input
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
	if outcome.ResultType != agentexec.ResultTypeText || outcome.ResultText != "完成的交付文本" {
		t.Fatalf("result = %#v", outcome)
	}
}

func TestExecuteRejectsTrailingExecutorOutput(t *testing.T) {
	for _, mode := range []string{"succeed-tail", "succeed-second-frame"} {
		t.Run(mode, func(t *testing.T) {
			outcome, code, err := Execute(context.Background(), RunRequest{
				Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor(mode),
			})
			if err == nil || code != CodeProtocolInvalid || outcome != (RunOutcome{}) {
				t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want protocol invalid", outcome, code, err)
			}
		})
	}
}

func TestExecuteRequiresNormalExecutorExit(t *testing.T) {
	for _, mode := range []string{"succeed-nonzero", "error-nonzero"} {
		t.Run(mode, func(t *testing.T) {
			outcome, code, err := Execute(context.Background(), RunRequest{
				Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor(mode),
			})
			if err == nil || code != CodeExecutorFailed || outcome != (RunOutcome{}) {
				t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want executor failure", outcome, code, err)
			}
		})
	}
}

func TestExecuteRejectsExecutorThatLingersAfterManifest(t *testing.T) {
	started := time.Now()
	outcome, code, err := Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("succeed-linger"),
	})
	if err == nil || code != CodeExecutorFailed || outcome != (RunOutcome{}) {
		t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want executor failure", outcome, code, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("lingering executor was not reclaimed promptly: %v", elapsed)
	}
}

func TestExecuteReturnsFileManifestOnlyForFrozenFileContract(t *testing.T) {
	input := runnerInput()
	input.OutputContract = &agentexec.OutputContract{
		Type: agentexec.ResultTypeFile, Name: "answer.md", MIME: "text/markdown",
	}
	input.Capabilities = []string{agentexec.CapabilityReadTaskSnapshot, agentexec.CapabilityWriteFileResult}
	outcome, code, err := Execute(context.Background(), RunRequest{
		Input: input, Timeout: 10 * time.Second, Executor: fakeExecutor("succeed-file"),
	})
	if err != nil || code != "" {
		t.Fatalf("Execute() = %q %v, want success", code, err)
	}
	if outcome.ResultType != agentexec.ResultTypeFile || outcome.ResultText != "# 完成的文件\n" {
		t.Fatalf("result = %#v", outcome)
	}
}

func TestExecuteRejectsResultTypeMismatch(t *testing.T) {
	fileInput := runnerInput()
	fileInput.OutputContract = &agentexec.OutputContract{
		Type: agentexec.ResultTypeFile, Name: "answer.md", MIME: "text/markdown",
	}
	fileInput.Capabilities = []string{agentexec.CapabilityReadTaskSnapshot, agentexec.CapabilityWriteFileResult}
	tests := []struct {
		name  string
		input agentexec.InputFrame
		mode  string
	}{
		{name: "legacy text cannot receive file", input: runnerInput(), mode: "succeed-file"},
		{name: "file cannot receive text", input: fileInput, mode: "succeed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome, code, err := Execute(context.Background(), RunRequest{
				Input: test.input, Timeout: 10 * time.Second, Executor: fakeExecutor(test.mode),
			})
			if err == nil || code != CodeProtocolInvalid || outcome.ResultText != "" || outcome.ResultType != "" {
				t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want protocol invalid", outcome, code, err)
			}
		})
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

func TestExecuteMapsFastExecutorErrorFrameDeterministically(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		outcome, code, err := Execute(context.Background(), RunRequest{
			Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("error"),
		})
		if err == nil || code != agentexec.ErrorCodeModelUnavailable || outcome != (RunOutcome{}) {
			t.Fatalf("attempt %d: Execute() = outcome=%#v code=%q err=%v, want stable executor error", attempt+1, outcome, code, err)
		}
	}
}

func TestExecuteRejectsUntrustedExecutorErrorCodes(t *testing.T) {
	for _, mode := range []string{"error-unknown", "error-overlong", "error-malicious"} {
		t.Run(mode, func(t *testing.T) {
			outcome, code, err := Execute(context.Background(), RunRequest{
				Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor(mode),
			})
			if err == nil || code != CodeProtocolInvalid || outcome.ResultText != "" {
				t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want protocol invalid", outcome, code, err)
			}
			if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "AGENT_PRIVATE_CHILD_FAILURE") {
				t.Fatalf("untrusted executor error leaked through runner: %q", err)
			}
		})
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

func TestExecuteCanStopWhileExecutorDoesNotConsumeInput(t *testing.T) {
	t.Run("deadline while child never reads", func(t *testing.T) {
		started := time.Now()
		outcome, code, err := Execute(context.Background(), RunRequest{
			Input: largeRunnerInput(), Timeout: 300 * time.Millisecond, Executor: fakeExecutor("never-read"),
		})
		if err == nil || code != CodeTimedOut || outcome != (RunOutcome{}) {
			t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want timeout", outcome, code, err)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("blocked stdin write ignored the deadline for %v", elapsed)
		}
	})

	t.Run("cancellation while child reads only a prefix", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(300*time.Millisecond, cancel)
		defer timer.Stop()
		started := time.Now()
		outcome, code, err := Execute(ctx, RunRequest{
			Input: largeRunnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("partial-read"),
		})
		if err == nil || code != CodeCancelled || outcome != (RunOutcome{}) {
			t.Fatalf("Execute() = outcome=%#v code=%q err=%v, want cancellation", outcome, code, err)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("partially consumed stdin write ignored cancellation for %v", elapsed)
		}
	})
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
