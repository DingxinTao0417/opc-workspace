//go:build windows

package agentrunner

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
