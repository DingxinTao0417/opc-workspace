//go:build !windows

package agentrunner

import (
	"os"
	"os/exec"
	"syscall"
)

// Non-Windows process groups do not retain a separate parent-side handle.
func releaseProcessTree(_ *exec.Cmd) {}

// bindProcessTree and terminate dispatch through runtime.GOOS in runner.go.
// Keep these Windows-named helpers available to the compiler on other targets;
// the guarded branches never call them at runtime.
func bindWindowsJob(_ *exec.Cmd) error { return nil }

func terminateWindowsJob(_ *exec.Cmd) {}

// signalProcessGroup is the non-Windows fallback until the macOS/Linux
// lifecycle matrices are verified (ADR-027); builtin execution stays
// disabled there, so this path only exists to keep the package portable.
func signalProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// ExecutorCommand resolves the builtin executor re-exec command. It lives in
// the runner so tests can inject a fake executor binary instead.
func ExecutorCommand() (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := executorCommand(executable)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command, nil
}
