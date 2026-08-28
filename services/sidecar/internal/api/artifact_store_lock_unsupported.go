//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package api

import "errors"

func acquireArtifactStoreLease(string) (artifactStoreLease, error) {
	return nil, errors.New("Artifact storage locking is unsupported on this operating system")
}
