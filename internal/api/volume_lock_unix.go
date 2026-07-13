//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package api

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockVolumeFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errVolumeAlreadyLocked
	}
	return err
}

func unlockVolumeFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
