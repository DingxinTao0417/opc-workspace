//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package runlease

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func acquireFileLease(path string) (func() error, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		if targetIsUnsafe(path) {
			return nil, ErrUnsafeTarget
		}
		return nil, fmt.Errorf("open Sidecar run lease: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = file.Close()
		}
	}()

	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect Sidecar run lease target: %w", err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open Sidecar run lease: %w", err)
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 ||
		!fileInfo.Mode().IsRegular() || !os.SameFile(pathInfo, fileInfo) {
		return nil, ErrUnsafeTarget
	}

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrAlreadyHeld
		}
		return nil, fmt.Errorf("lock Sidecar run lease: %w", err)
	}
	closeOnError = false

	return func() error {
		unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
		closeErr := file.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}
