//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package filestore

import "errors"

func AcquireArtifactStoreLease(string) (ArtifactStoreLease, error) {
	return nil, errors.New("Artifact storage locking is unsupported on this operating system")
}
