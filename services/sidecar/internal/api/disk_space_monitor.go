package api

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultLowDiskThresholdBytes uint64 = 1 << 30

func storageProbePaths(options Options) []string {
	candidates := []string{options.ArtifactDir, options.BackupDir}
	if strings.TrimSpace(options.DatabasePath) != "" {
		candidates = append(candidates, filepath.Dir(options.DatabasePath))
	}
	paths := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		canonical := filepath.Clean(absolute)
		key := canonical
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, canonical)
	}
	return paths
}

func (a *API) scanDiskSpace() error {
	checker := a.options.DiskSpaceCheck
	if checker == nil {
		checker = diskFreeBytes
	}
	low := false
	probeFailed := false
	for _, path := range storageProbePaths(a.options) {
		available, _, err := checker(path)
		if err != nil {
			probeFailed = true
			continue
		}
		if available < defaultLowDiskThresholdBytes {
			low = true
		}
	}
	if !low {
		if probeFailed {
			return errors.New("storage capacity probe failed")
		}
		a.lowDiskActive.Store(false)
		return nil
	}
	if !a.lowDiskActive.CompareAndSwap(false, true) {
		return nil
	}
	if err := a.recordStorageLowSpace(); err != nil {
		a.lowDiskActive.Store(false)
		return err
	}
	return nil
}
