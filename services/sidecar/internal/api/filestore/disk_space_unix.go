//go:build !windows

package filestore

import (
	"strconv"

	"golang.org/x/sys/unix"
)

func StorageVolumeIdentity(path string) (string, error) {
	var stats unix.Stat_t
	if err := unix.Stat(path, &stats); err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(stats.Dev), 10), nil
}

func DiskFreeBytes(path string) (uint64, uint64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stats.Bsize)
	return stats.Bavail * blockSize, stats.Blocks * blockSize, nil
}
