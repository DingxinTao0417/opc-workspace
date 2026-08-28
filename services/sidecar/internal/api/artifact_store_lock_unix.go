//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const artifactStoreLockName = ".opc-artifact-store.lock"

type unixArtifactStoreLease struct {
	file *os.File
}

func acquireArtifactStoreLease(root string) (artifactStoreLease, error) {
	path := filepath.Join(root, artifactStoreLockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = file.Close()
		}
	}()
	lstat, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !lstat.Mode().IsRegular() || lstat.Mode()&os.ModeSymlink != 0 || !os.SameFile(lstat, stat) {
		return nil, errors.New("Artifact lock is not a regular file")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("Artifact root is already in use by another Sidecar")
		}
		return nil, fmt.Errorf("lock Artifact storage file: %w", err)
	}
	closeOnError = false
	return &unixArtifactStoreLease{file: file}, nil
}

func (l *unixArtifactStoreLease) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}
