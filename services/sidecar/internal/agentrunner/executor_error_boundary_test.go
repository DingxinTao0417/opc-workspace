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

// This subprocess is deliberately not ExecutorMain: it probes a compromised or
// malfunctioning child, for which a plausible safe error frame must never excuse
// a nonzero exit, extra frame, trailing bytes, or mismatched invocation identity.
func TestExecutorErrorBoundaryProcess(t *testing.T) {
	if os.Getenv("OPC_TEST_ERROR_BOUNDARY_PROCESS") != "1" {
		return
	}
	var input agentexec.InputFrame
	if err := agentexec.ReadFrame(bufio.NewReader(os.Stdin), &input); err != nil {
		os.Exit(3)
	}
	mode := os.Getenv("OPC_TEST_ERROR_BOUNDARY_MODE")
	manifest := agentexec.ManifestFrame{
		ProtocolVersion: agentexec.ProtocolVersion, RunID: input.RunID, Nonce: input.Nonce,
		Result: agentexec.Result{Type: "error", Text: os.Getenv("OPC_TEST_ERROR_BOUNDARY_CODE")},
	}
	if mode == "mismatch" {
		manifest.Nonce = "wrong-invocation"
	}
	if err := agentexec.WriteFrame(os.Stdout, manifest); err != nil {
		os.Exit(4)
	}
	switch mode {
	case "tail":
		_, _ = os.Stdout.WriteString("PRIVATE_MODEL trailing output")
	case "second_frame":
		_ = agentexec.WriteFrame(os.Stdout, manifest)
	case "nonzero":
		os.Exit(7)
	}
	os.Exit(0)
}

func TestExecutorSafeFailureStillRequiresExactProtocolAndNormalExit(t *testing.T) {
	for _, code := range []string{"AGENT_MODEL_TRUNCATED", "AGENT_MODEL_FILTERED", "AGENT_MODEL_RESPONSE_INVALID"} {
		for _, test := range []struct{ mode, wantCode string }{
			{"normal", code},
			{"tail", CodeProtocolInvalid},
			{"second_frame", CodeProtocolInvalid},
			{"nonzero", CodeExecutorFailed},
			{"mismatch", CodeProtocolInvalid},
		} {
			t.Run(code+"/"+test.mode, func(t *testing.T) {
				outcome, gotCode, err := Execute(context.Background(), RunRequest{
					Input: runnerInput(), Timeout: 5 * time.Second,
					Executor: func() *exec.Cmd {
						command := exec.Command(os.Args[0], "-test.run=^TestExecutorErrorBoundaryProcess$")
						command.Env = append(os.Environ(), "OPC_TEST_ERROR_BOUNDARY_PROCESS=1",
							"OPC_TEST_ERROR_BOUNDARY_CODE="+code, "OPC_TEST_ERROR_BOUNDARY_MODE="+test.mode)
						return command
					},
				})
				if err == nil || gotCode != test.wantCode || outcome != (RunOutcome{}) {
					t.Fatalf("outcome=%#v code=%q error=%v, want %s and no output", outcome, gotCode, err, test.wantCode)
				}
				if strings.Contains(err.Error(), "PRIVATE_MODEL") {
					t.Fatalf("untrusted child text leaked: %v", err)
				}
			})
		}
	}
}
