//go:build windows

package api

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func storageVolumeIdentity(path string) (string, error) {
	pathPointer, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	volumePath := make([]uint16, 1024)
	if err := windows.GetVolumePathName(pathPointer, &volumePath[0], uint32(len(volumePath))); err != nil {
		return "", err
	}
	mountPoint := windows.UTF16ToString(volumePath)
	mountPointer, err := windows.UTF16PtrFromString(mountPoint)
	if err != nil {
		return "", err
	}
	volumeName := make([]uint16, 1024)
	if err := windows.GetVolumeNameForVolumeMountPoint(mountPointer, &volumeName[0], uint32(len(volumeName))); err != nil {
		return "", err
	}
	identity := strings.ToLower(strings.TrimSpace(windows.UTF16ToString(volumeName)))
	if identity == "" {
		return "", errors.New("volume identity is empty")
	}
	return identity, nil
}

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
