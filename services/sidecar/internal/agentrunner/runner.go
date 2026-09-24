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
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

const executorExitGracePeriod = 500 * time.Millisecond

var errUnexpectedExecutorOutput = errors.New("executor stdout contains data after the manifest frame")

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
	ResultType string
	ResultText string
	Duration   time.Duration
}

type manifestReadResult struct {
	manifest agentexec.ManifestFrame
	err      error
}

// Execute spawns the executor, sends the input frame, requires exactly one
// manifest plus a clean executor exit, and always reclaims the process tree
// before returning. The second return value is a stable run error code
// ("" on success).
func Execute(ctx context.Context, request RunRequest) (RunOutcome, string, error) {
	started := time.Now()
	if request.Executor == nil {
		return RunOutcome{}, CodeExecutorUnavailable, errors.New("executor command factory missing")
	}
	if err := agentexec.ValidateInputFrame(request.Input); err != nil {
		return RunOutcome{}, CodeProtocolInvalid, err
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

	manifestCh := make(chan manifestReadResult, 1)
	streamCh := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		reader := bufio.NewReader(stdout)
		var manifest agentexec.ManifestFrame
		if err := agentexec.ReadFrame(reader, &manifest); err != nil {
			manifestCh <- manifestReadResult{err: err}
			return
		}
		manifestCh <- manifestReadResult{manifest: manifest}
		if _, err := reader.ReadByte(); err == nil {
			streamCh <- errUnexpectedExecutorOutput
		} else if errors.Is(err, io.EOF) {
			streamCh <- nil
		} else {
			streamCh <- fmt.Errorf("read executor stdout after manifest: %w", err)
		}
	}()

	// A full H5-C input frame can exceed an anonymous pipe's buffer. Write it
	// off the control goroutine so a child that never reads (or only partially
	// reads) cannot prevent cancellation or the run deadline from reclaiming
	// the process tree. Closing stdin from the control path unblocks a pending
	// WriteFrame after terminate has revoked the child.
	writeCh := make(chan error, 1)
	go func() {
		writeErr := agentexec.WriteFrame(stdin, request.Input)
		if closeErr := stdin.Close(); writeErr == nil {
			writeErr = closeErr
		}
		writeCh <- writeErr
	}()
	select {
	case writeErr := <-writeCh:
		if writeErr != nil {
			terminate(command)
			_ = stdin.Close()
			<-readerDone
			_ = command.Wait()
			if runContext.Err() != nil {
				cause := cancelCause(runContext)
				return RunOutcome{}, cause, errors.New(cause)
			}
			return RunOutcome{}, CodeProtocolInvalid, writeErr
		}
	case <-runContext.Done():
		terminate(command)
		_ = stdin.Close()
		<-writeCh
		<-readerDone
		_ = command.Wait()
		cause := cancelCause(runContext)
		return RunOutcome{}, cause, errors.New(cause)
	}

	var frameResult manifestReadResult
	select {
	case frameResult = <-manifestCh:
	case <-runContext.Done():
		terminate(command)
		<-readerDone
		_ = command.Wait()
		cause := cancelCause(runContext)
		return RunOutcome{}, cause, errors.New(cause)
	}
	if frameResult.err != nil {
		terminate(command)
		<-readerDone
		_ = command.Wait()
		if errors.Is(frameResult.err, agentexec.ErrFrameTooLarge) {
			return RunOutcome{}, CodeResultTooLarge, frameResult.err
		}
		if runContext.Err() != nil {
			cause := cancelCause(runContext)
			return RunOutcome{}, cause, errors.New(cause)
		}
		return RunOutcome{}, CodeProtocolInvalid, frameResult.err
	}

	// A valid response is exactly one frame followed by EOF, and the executor
	// must then exit successfully. Start one bounded grace window as soon as
	// the frame arrives; a child cannot make a result authoritative by writing
	// a valid prefix and then emitting more bytes, failing, or lingering.
	exitTimer := time.NewTimer(executorExitGracePeriod)
	defer exitTimer.Stop()
	var streamErr error
	select {
	case streamErr = <-streamCh:
	case <-exitTimer.C:
		terminate(command)
		<-readerDone
		_ = command.Wait()
		return RunOutcome{}, CodeExecutorFailed, errors.New("executor did not finish its output stream after the manifest")
	case <-runContext.Done():
		terminate(command)
		<-readerDone
		_ = command.Wait()
		cause := cancelCause(runContext)
		return RunOutcome{}, cause, errors.New(cause)
	}
	if streamErr != nil {
		terminate(command)
		<-readerDone
		_ = command.Wait()
		return RunOutcome{}, CodeProtocolInvalid, streamErr
	}
	<-readerDone

	waitCh := make(chan error, 1)
	go func() { waitCh <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-exitTimer.C:
		select {
		case waitErr = <-waitCh:
		default:
			terminate(command)
			<-waitCh
			return RunOutcome{}, CodeExecutorFailed, errors.New("executor did not exit after closing its output stream")
		}
	case <-runContext.Done():
		select {
		case waitErr = <-waitCh:
		default:
			terminate(command)
			<-waitCh
			cause := cancelCause(runContext)
			return RunOutcome{}, cause, errors.New(cause)
		}
	}
	if waitErr != nil {
		return RunOutcome{}, CodeExecutorFailed, fmt.Errorf("executor exited unsuccessfully: %w", waitErr)
	}

	manifest := frameResult.manifest
	if manifest.ProtocolVersion != agentexec.ProtocolVersion || manifest.RunID != request.Input.RunID ||
		manifest.Nonce != request.Input.Nonce {
		return RunOutcome{}, CodeProtocolInvalid, errors.New("manifest identity mismatch")
	}
	if manifest.Result.Type == "error" {
		if code, ok := stableExecutorErrorCode(manifest.Result.Text); ok {
			return RunOutcome{}, code, errors.New("executor reported a stable failure")
		}
		return RunOutcome{}, CodeProtocolInvalid, errors.New("executor reported an invalid failure code")
	}
	resultPayload, payloadErr := agentexec.ResultPayload(
		manifest.Result,
		agentexec.EffectiveOutputContract(request.Input),
		agentexec.EffectiveMaxResultBytes(request.Input),
	)
	if payloadErr != nil {
		if errors.Is(payloadErr, agentexec.ErrResultTooLarge) {
			return RunOutcome{}, CodeResultTooLarge, payloadErr
		}
		return RunOutcome{}, CodeProtocolInvalid, payloadErr
	}
	return RunOutcome{ResultType: manifest.Result.Type, ResultText: resultPayload, Duration: time.Since(started)}, "", nil
}

func stableExecutorErrorCode(value string) (string, bool) {
	switch value {
	case agentexec.ErrorCodeInvalidInput,
		agentexec.ErrorCodeModelEndpoint,
		agentexec.ErrorCodeModelUnavailable,
		agentexec.ErrorCodeModelFailed,
		agentexec.ErrorCodeModelTruncated,
		agentexec.ErrorCodeModelFiltered,
		agentexec.ErrorCodeModelResponseInvalid,
		agentexec.ErrorCodeEmptyResult,
		agentexec.ErrorCodeInvalidResult,
		agentexec.ErrorCodeResultTooLarge:
		return value, true
	default:
		return "", false
	}
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
	// A successful Wait sets ProcessState before this deferred cleanup runs.
	// The OS-specific release must still happen: on Windows it closes and
	// forgets the kill-on-close Job handle. releaseProcessTree is idempotent,
	// because error/cancellation paths may already have consumed that handle
	// while terminating the job.
	defer releaseProcessTree(command)
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	terminate(command)
	_ = command.Wait()
}

func terminate(command *exec.Cmd) {
	if command.Process == nil {
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
