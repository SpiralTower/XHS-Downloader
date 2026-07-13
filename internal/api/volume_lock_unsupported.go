//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package api

import (
	"errors"
	"os"
)

var errVolumeLockUnsupported = errors.New("single-instance volume locking is unsupported on this platform")

func tryLockVolumeFile(_ *os.File) error {
	return errVolumeLockUnsupported
}

func unlockVolumeFile(_ *os.File) error {
	return nil
}
