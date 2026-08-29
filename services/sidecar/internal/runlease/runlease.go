package runlease

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const lockFileName = ".opc-sidecar-run.lock"

var (
	// ErrAlreadyHeld means another Sidecar owns the run lease for the same
	// database directory.
	ErrAlreadyHeld = errors.New("database directory is already in use by another Sidecar")
	// ErrUnsafeTarget means the fixed lock path is not an ordinary file.
	ErrUnsafeTarget = errors.New("Sidecar run lease target is not a regular file")
)

// Lease prevents another Sidecar from opening or restoring a database in the
// same directory. The lock file is intentionally retained after Close; only
// the operating-system lock represents ownership.
type Lease struct {
	mu      sync.Mutex
	closeFn func() error
}

// Acquire takes a non-blocking process-level lease in databasePath's parent
// directory. In-memory databases have no shared on-disk state and therefore
// return a no-op lease.
func Acquire(databasePath string) (*Lease, error) {
	if strings.TrimSpace(databasePath) == "" {
		return nil, errors.New("database path is required for Sidecar run lease")
	}
	if databasePath == ":memory:" {
		return &Lease{}, nil
	}

	absoluteDatabase, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path for Sidecar run lease: %w", err)
	}
	directory := filepath.Dir(absoluteDatabase)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create database directory for Sidecar run lease: %w", err)
	}

	lockPath := filepath.Join(directory, lockFileName)
	if targetIsUnsafe(lockPath) {
		return nil, ErrUnsafeTarget
	}
	closeFn, err := acquireFileLease(lockPath)
	if err != nil {
		return nil, err
	}
	return &Lease{closeFn: closeFn}, nil
}

// Close releases the operating-system lock. It is safe to call more than once.
func (lease *Lease) Close() error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closeFn == nil {
		return nil
	}
	closeFn := lease.closeFn
	lease.closeFn = nil
	return closeFn()
}

func targetIsUnsafe(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0)
}
