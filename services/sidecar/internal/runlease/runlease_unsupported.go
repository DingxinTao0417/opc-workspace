//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package runlease

import "errors"

func acquireFileLease(string) (func() error, error) {
	return nil, errors.New("Sidecar run lease is unsupported on this operating system")
}
