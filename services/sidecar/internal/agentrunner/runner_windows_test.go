//go:build windows

package agentrunner

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func trackedWindowsJobCount() int {
	jobHandlesMu.Lock()
	defer jobHandlesMu.Unlock()
	return len(jobHandles)
}

func TestExecuteReleasesWindowsJobHandleOnEveryExit(t *testing.T) {
	baseline := trackedWindowsJobCount()
	tests := []struct {
		name    string
		mode    string
		timeout time.Duration
	}{
		{name: "success", mode: "succeed", timeout: 10 * time.Second},
		{name: "normal nonzero failure", mode: "succeed-nonzero", timeout: 10 * time.Second},
		{name: "protocol failure", mode: "succeed-tail", timeout: 10 * time.Second},
		{name: "timeout termination", mode: "hang-forever", timeout: 300 * time.Millisecond},
		{name: "blocked input termination", mode: "never-read", timeout: 300 * time.Millisecond},
		{name: "partial input termination", mode: "partial-read", timeout: 300 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := runnerInput()
			if test.mode == "never-read" || test.mode == "partial-read" {
				input = largeRunnerInput()
			}
			_, _, _ = Execute(context.Background(), RunRequest{
				Input: input, Timeout: test.timeout, Executor: fakeExecutor(test.mode),
			})
			if current := trackedWindowsJobCount(); current != baseline {
				t.Fatalf("tracked Windows Jobs = %d, want baseline %d", current, baseline)
			}
		})
	}
}

func TestTakeWindowsJobTransfersCloseOwnershipOnce(t *testing.T) {
	command := &exec.Cmd{}
	baseline := trackedWindowsJobCount()
	jobHandlesMu.Lock()
	jobHandles[command] = windows.Handle(1) // ownership token only; never closed by this test
	jobHandlesMu.Unlock()

	var owners atomic.Int32
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, tracked := takeWindowsJob(command); tracked {
				owners.Add(1)
			}
		}()
	}
	wait.Wait()

	if got := owners.Load(); got != 1 {
		t.Fatalf("Windows Job close owners = %d, want exactly 1", got)
	}
	if current := trackedWindowsJobCount(); current != baseline {
		t.Fatalf("tracked Windows Jobs = %d after ownership transfer, want baseline %d", current, baseline)
	}
}

const nestedJobInnerEnv = "AGENT_NESTED_JOB_INNER"

// The desktop puts the whole Sidecar into a kill-on-close Job Object. The
// runner's own Job for an executor then has to nest inside that one. The
// parent test below reproduces the arrangement: it runs this package's test
// binary inside an outer job and lets it execute the inner test.
func TestExecuteWorksInsideAnOuterKillOnCloseJob(t *testing.T) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatalf("create outer job: %v", err)
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		t.Fatalf("configure outer job: %v", err)
	}

	inner := exec.Command(os.Args[0], "-test.run=^TestExecuteInsideOuterJobInner$", "-test.v")
	inner.Env = append(os.Environ(), nestedJobInnerEnv+"=1")
	stdin, err := inner.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	inner.Stdout = &output
	inner.Stderr = &output
	if err := inner.Start(); err != nil {
		t.Fatalf("start inner test process: %v", err)
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(inner.Process.Pid))
	if err != nil {
		_ = inner.Process.Kill()
		t.Fatalf("open inner test process: %v", err)
	}
	err = windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		_ = inner.Process.Kill()
		t.Fatalf("put inner test process into the outer job: %v", err)
	}
	// The inner test starts no executor before it is told that it is inside
	// the outer job.
	if _, err := io.WriteString(stdin, "go\n"); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := inner.Wait(); err != nil || !strings.Contains(output.String(), "--- PASS: TestExecuteInsideOuterJobInner") {
		t.Fatalf("inner test inside the outer job: err=%v output:\n%s", err, output.String())
	}
}

func TestExecuteInsideOuterJobInner(t *testing.T) {
	if os.Getenv(nestedJobInnerEnv) != "1" {
		t.Skip("runs only as the child of TestExecuteWorksInsideAnOuterKillOnCloseJob")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "go" {
		t.Fatalf("start signal = %q, %v", line, err)
	}

	outcome, code, err := Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 10 * time.Second, Executor: fakeExecutor("succeed"),
	})
	if err != nil || code != "" || outcome.ResultText != "完成的交付文本" {
		t.Fatalf("Execute() inside an outer job = %#v %q %v, want success", outcome, code, err)
	}

	// A hung executor is still reclaimed through the runner's nested job.
	_, code, err = Execute(context.Background(), RunRequest{
		Input: runnerInput(), Timeout: 1200 * time.Millisecond, Executor: fakeExecutor("hang-forever"),
	})
	if err == nil || code != CodeTimedOut {
		t.Fatalf("hung executor inside an outer job: code=%q err=%v, want timeout", code, err)
	}
	if tracked := trackedWindowsJobCount(); tracked != 0 {
		t.Fatalf("tracked Windows Jobs = %d after both runs, want 0", tracked)
	}
}
