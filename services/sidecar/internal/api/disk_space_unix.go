//go:build !windows

package api

import "golang.org/x/sys/unix"

func diskFreeBytes(path string) (uint64, uint64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stats.Bsize)
	return stats.Bavail * blockSize, stats.Blocks * blockSize, nil
}
