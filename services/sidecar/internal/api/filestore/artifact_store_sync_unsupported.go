//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package filestore

import "errors"

func SyncArtifactDirectory(string) error {
	return errors.New("Artifact directory synchronization is unsupported on this operating system")
}
