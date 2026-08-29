//go:build windows

package runlease

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

func acquireFileLease(path string) (func() error, error) {
	utf16Path, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode Sidecar run lease path: %w", err)
	}
	handle, err := windows.CreateFile(
		utf16Path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		// Do not allow shared deletion: replacing the pathname while this handle
		// is locked would let a second process lock a different file.
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		if targetIsUnsafe(path) {
			return nil, ErrUnsafeTarget
		}
		return nil, fmt.Errorf("open Sidecar run lease: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = windows.CloseHandle(handle)
		}
	}()

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, fmt.Errorf("inspect Sidecar run lease target: %w", err)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nil, ErrUnsafeTarget
	}

	var overlapped windows.Overlapped
	if err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrAlreadyHeld
		}
		return nil, fmt.Errorf("lock Sidecar run lease: %w", err)
	}
	closeOnError = false

	return func() error {
		var unlockOverlapped windows.Overlapped
		unlockErr := windows.UnlockFileEx(handle, 0, 1, 0, &unlockOverlapped)
		closeErr := windows.CloseHandle(handle)
		return errors.Join(unlockErr, closeErr)
	}, nil
}
