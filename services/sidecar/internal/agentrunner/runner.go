// Package agentrunner launches the builtin executor as a short-lived child
// process, speaks opc-agent-pipe-v1 over anonymous pipes, and guarantees
// process-tree cleanup. On Windows the child is bound to a kill-on-close Job
// Object so cancellation and sidecar death reclaim the whole subtree
// (verified 2026-09-12, ADR-027); other platforms fall back to process-group
// termination until their matrices are verified.
package agentrunner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

const defaultGracePeriod = 5 * time.Second

// Stable agent run error codes surfaced by the runner.
const (
	CodeExecutorUnavailable = "AGENT_EXECUTOR_UNAVAILABLE"
	CodeExecutorFailed      = "AGENT_EXECUTOR_FAILED"
	CodeProtocolInvalid     = "AGENT_PROTOCOL_INVALID"
	CodeResultTooLarge      = "AGENT_RESULT_TOO_LARGE"
	CodeCancelled           = "AGENT_RUN_CANCELLED"
	CodeTimedOut            = "AGENT_RUN_TIMED_OUT"
)

type RunRequest struct {
	Input    agentexec.InputFrame
	Timeout  time.Duration
	Executor func() *exec.Cmd
}

type RunOutcome struct {
	ResultText string
	Duration   time.Duration
}

// Execute spawns the executor, sends the input frame, waits for the manifest,
// and always reclaims the process tree before returning. The second return
// value is a stable run error code ("" on success).
func Execute(ctx context.Context, request RunRequest) (RunOutcome, string, error) {
	started := time.Now()
	if request.Executor == nil {
		return RunOutcome{}, CodeExecutorUnavailable, errors.New("executor command factory missing")
	}
	command := request.Executor()
	stdin, err := command.StdinPipe()
	if err != nil {
		return RunOutcome{}, CodeExecutorFailed, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return RunOutcome{}, CodeExecutorFailed, err
	}
	if err := command.Start(); err != nil {
		return RunOutcome{}, CodeExecutorUnavailable, err
	}

	if err := bindProcessTree(command); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return RunOutcome{}, CodeExecutorFailed, err
	}
	defer reclaimProcessTree(command)

	runContext, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

	if err := agentexec.WriteFrame(stdin, request.Input); err != nil {
		terminate(command)
		_ = command.Wait()
		return RunOutcome{}, CodeProtocolInvalid, err
	}
	// The executor reads exactly one frame; close stdin so it cannot block.
	_ = stdin.Close()

	resultCh := make(chan error, 1)
	manifestCh := make(chan agentexec.ManifestFrame, 1)
	go func() {
		var manifest agentexec.ManifestFrame
		if err := agentexec.ReadFrame(bufio.NewReader(stdout), &manifest); err != nil {
			resultCh <- err
			return
		}
		manifestCh <- manifest
		resultCh <- nil
	}()

	monitorCh := make(chan error, 1)
	go func() { monitorCh <- command.Wait() }()

	var manifest agentexec.ManifestFrame
	var frameErr error
	timedOut := false
	select {
	case <-monitorCh:
		if runContext.Err() != nil {
			cause := cancelCause(runContext)
			return RunOutcome{}, cause, errors.New(cause)
		}
		return RunOutcome{}, CodeExecutorFailed, fmt.Errorf("executor exited before responding")
	case frameErr = <-resultCh:
		if frameErr == nil {
			manifest = <-manifestCh
		}
	case <-runContext.Done():
		timedOut = true
	}

	if timedOut || frameErr != nil {
		// Grace: the executor sees stdin EOF and may exit on its own; only a
		// still-running tree gets terminated through the Job Object.
		select {
		case <-monitorCh:
		case <-time.After(defaultGracePeriod):
			terminate(command)
			<-monitorCh
		}
		if timedOut {
			cause := cancelCause(runContext)
			return RunOutcome{}, cause, errors.New(cause)
		}
		if errors.Is(frameErr, agentexec.ErrFrameTooLarge) {
			return RunOutcome{}, CodeResultTooLarge, frameErr
		}
		if runContext.Err() != nil {
			cause := cancelCause(runContext)
			return RunOutcome{}, cause, errors.New(cause)
		}
		return RunOutcome{}, CodeProtocolInvalid, frameErr
	}
	// Manifest received; the executor should exit on its own. Reclaim it if
	// it lingers so no run can leave a live process behind.
	select {
	case <-monitorCh:
	case <-time.After(500 * time.Millisecond):
		terminate(command)
		<-monitorCh
	}

	if manifest.ProtocolVersion != agentexec.ProtocolVersion || manifest.RunID != request.Input.RunID ||
		manifest.Nonce != request.Input.Nonce {
		return RunOutcome{}, CodeProtocolInvalid, errors.New("manifest identity mismatch")
	}
	if manifest.Result.Type == "error" {
		return RunOutcome{}, manifest.Result.Text, errors.New("executor reported a stable failure")
	}
	if manifest.Result.Type != "text" || manifest.Result.Text == "" {
		return RunOutcome{}, CodeProtocolInvalid, errors.New("manifest result is not inline text")
	}
	if len(manifest.Result.Text) > request.Input.MaxResultBytes {
		return RunOutcome{}, CodeResultTooLarge, errors.New("executor result exceeds the byte budget")
	}
	return RunOutcome{ResultText: manifest.Result.Text, Duration: time.Since(started)}, "", nil
}

func cancelCause(runContext context.Context) string {
	if errors.Is(runContext.Err(), context.DeadlineExceeded) {
		return CodeTimedOut
	}
	return CodeCancelled
}

// bindProcessTree pins the child into a kill-on-close Job Object on Windows.
// The runner never proceeds without it: an unbound child could leak its own
// children past cancellation or sidecar death.
func bindProcessTree(command *exec.Cmd) error {
	if runtime.GOOS != "windows" {
		// POSIX platforms use process-group kill in reclaimProcessTree; their
		// full matrices are unverified and builtin execution stays off.
		return nil
	}
	return bindWindowsJob(command)
}

func reclaimProcessTree(command *exec.Cmd) {
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	terminate(command)
	_ = command.Wait()
}

func terminate(command *exec.Cmd) {
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	if runtime.GOOS == "windows" {
		terminateWindowsJob(command)
		return
	}
	if command.Process.Pid > 0 {
		signalProcessGroup(command.Process.Pid)
	}
	_ = command.Process.Kill()
}

// executorCommand resolves the builtin executor re-exec command. It lives in
// the runner so tests can inject a fake executor binary instead.
func executorCommand(executablePath string) *exec.Cmd {
	command := exec.Command(executablePath, "agent-executor")
	command.Env = append(command.Environ(), "OPC_AGENT_EXECUTOR_MODE=1")
	return command
}
