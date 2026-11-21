//go:build darwin || linux

package updater

import (
	"errors"
	"golang.org/x/sys/unix"
)

func processAlive(pid int) (bool, bool) {
	e := unix.Kill(pid, 0)
	if e == nil || errors.Is(e, unix.EPERM) {
		return true, true
	}
	if errors.Is(e, unix.ESRCH) {
		return false, true
	}
	return false, false
}
