package runlease

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireRejectsConcurrentLeaseAndAllowsReacquireAfterClose(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workspace.db")
	first, err := Acquire(databasePath)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}

	type result struct {
		lease *Lease
		err   error
	}
	secondResult := make(chan result, 1)
	go func() {
		lease, err := Acquire(databasePath)
		secondResult <- result{lease: lease, err: err}
	}()

	select {
	case second := <-secondResult:
		if second.lease != nil {
			_ = second.lease.Close()
			t.Fatal("Acquire(second) unexpectedly obtained the held lease")
		}
		if !errors.Is(second.err, ErrAlreadyHeld) {
			t.Fatalf("Acquire(second) error = %v, want ErrAlreadyHeld", second.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire(second) blocked instead of failing immediately")
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}
	reacquired, err := Acquire(databasePath)
	if err != nil {
		t.Fatalf("Acquire(after Close) error = %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatalf("Close(reacquired) error = %v", err)
	}

	info, err := os.Lstat(filepath.Join(filepath.Dir(databasePath), lockFileName))
	if err != nil {
		t.Fatalf("Lstat(lock file) error = %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("lock target mode = %v, want regular file", info.Mode())
	}
}

func TestAcquireRejectsNonRegularTarget(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, lockFileName), 0o700); err != nil {
			t.Fatalf("Mkdir(lock target) error = %v", err)
		}
		lease, err := Acquire(filepath.Join(root, "workspace.db"))
		if lease != nil {
			_ = lease.Close()
			t.Fatal("Acquire() unexpectedly accepted a directory target")
		}
		if !errors.Is(err, ErrUnsafeTarget) {
			t.Fatalf("Acquire() error = %v, want ErrUnsafeTarget", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "ordinary-file")
		if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
			t.Fatalf("WriteFile(target) error = %v", err)
		}
		if err := os.Symlink(target, filepath.Join(root, lockFileName)); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		lease, err := Acquire(filepath.Join(root, "workspace.db"))
		if lease != nil {
			_ = lease.Close()
			t.Fatal("Acquire() unexpectedly accepted a symlink target")
		}
		if !errors.Is(err, ErrUnsafeTarget) {
			t.Fatalf("Acquire() error = %v, want ErrUnsafeTarget", err)
		}
	})
}
