package api

import (
	"os"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

// TestMain lets the agent runner re-execute this test binary as the builtin
// executor subprocess: `go test` binaries receive the reserved
// `agent-executor` argument when the API tests exercise a full run. The stub
// executes the real executor contract, including the loopback model call, so
// the gold chain stays end to end.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "agent-executor" {
		os.Exit(agentexec.ExecutorMain())
	}
	os.Exit(m.Run())
}
