//go:build windows

package agentrunner

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobHandles   = make(map[*exec.Cmd]windows.Handle)
	jobHandlesMu sync.Mutex
)

// bindWindowsJob assigns the child to a kill-on-close Job Object. The handle
// is intentionally leaked until reclaimProcessTree terminates the job: closing
// it early would defeat the kill-on-close guarantee if the sidecar dies
// before cancellation runs.
func bindWindowsJob(command *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	limitInfo := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
				windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY,
		},
		ProcessMemoryLimit: 1 << 30,
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limitInfo)), uint32(unsafe.Sizeof(limitInfo))); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	childHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(childHandle)
	if err := windows.AssignProcessToJobObject(job, childHandle); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	jobHandlesMu.Lock()
	jobHandles[command] = job
	jobHandlesMu.Unlock()
	return nil
}

func terminateWindowsJob(command *exec.Cmd) {
	jobHandlesMu.Lock()
	job, tracked := jobHandles[command]
	delete(jobHandles, command)
	jobHandlesMu.Unlock()
	if !tracked {
		_ = command.Process.Kill()
		return
	}
	_ = windows.TerminateJobObject(job, 1)
	_ = windows.CloseHandle(job)
}

// signalProcessGroup has no meaning on Windows; the Job Object owns tree
// reclamation. Kept as a no-op so the portable runner compiles.
func signalProcessGroup(pid int) {}

// ExecutorCommand resolves the builtin executor re-exec command. It lives in
// the runner so tests can inject a fake executor binary instead.
func ExecutorCommand() (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return executorCommand(executable), nil
}
