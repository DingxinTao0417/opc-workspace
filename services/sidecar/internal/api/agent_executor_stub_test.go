package api

import (
	"fmt"
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
	cleanup, err := prepareAPITestDatabaseTemplate()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare API test database template: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if err := cleanup(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "clean API test database template: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
