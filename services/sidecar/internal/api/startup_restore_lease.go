package api

import "github.com/opc-workspace/opc-sidecar/internal/runlease"

func defaultAcquireRunLease(databasePath string) (ioCloser, error) {
	return runlease.Acquire(databasePath)
}
