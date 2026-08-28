//go:build windows

package api

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const artifactStoreLockName = ".opc-artifact-store.lock"

type windowsArtifactStoreLease struct {
	handle windows.Handle
}

func acquireArtifactStoreLease(root string) (artifactStoreLease, error) {
	path, err := windows.UTF16PtrFromString(filepath.Join(root, artifactStoreLockName))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		// Keep the lock pathname anchored to this handle for the lease lifetime;
		// allowing shared delete would let another process replace the file and
		// acquire a logically separate byte-range lock at the same path.
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = windows.CloseHandle(handle)
		}
	}()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nil, errors.New("Artifact lock is not a regular file")
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
			return nil, errors.New("Artifact root is already in use by another Sidecar")
		}
		return nil, fmt.Errorf("lock Artifact storage file: %w", err)
	}
	closeOnError = false
	return &windowsArtifactStoreLease{handle: handle}, nil
}

func (l *windowsArtifactStoreLease) Close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	handle := l.handle
	l.handle = 0
	var overlapped windows.Overlapped
	unlockErr := windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
	closeErr := windows.CloseHandle(handle)
	return errors.Join(unlockErr, closeErr)
}
