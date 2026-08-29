//go:build windows

package api

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diskFreeBytes(path string) (uint64, uint64, error) {
	pathPointer, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return 0, 0, err
	}
	var available uint64
	var total uint64
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(pathPointer, &available, &total, &free); err != nil {
		return 0, 0, err
	}
	return available, total, nil
}
