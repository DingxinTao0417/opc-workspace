//go:build !windows

package api

import "os"

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
