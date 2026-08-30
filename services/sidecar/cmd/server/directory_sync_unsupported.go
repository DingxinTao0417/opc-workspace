//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package main

import "errors"

func syncStartupDirectory(string) error {
	return errors.New("directory synchronization is unsupported on this operating system")
}
