//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package api

import "errors"

func syncArtifactDirectory(string) error {
	return errors.New("Artifact directory synchronization is unsupported on this operating system")
}
