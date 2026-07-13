package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const volumeLockFilename = ".xhs-downloader.lock"

var errVolumeAlreadyLocked = errors.New("volume is already locked")

func acquireVolumeLock(volumeDir string) (*os.File, error) {
	lockPath, err := filepath.Abs(filepath.Join(volumeDir, volumeLockFilename))
	if err != nil {
		return nil, fmt.Errorf("resolve volume lock path: %w", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open volume lock %s: %w", lockPath, err)
	}
	if err := tryLockVolumeFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errVolumeAlreadyLocked) {
			return nil, fmt.Errorf("volume %s is already in use by another application instance", volumeDir)
		}
		return nil, fmt.Errorf("lock volume %s: %w", volumeDir, err)
	}
	cleanup := func(cause error) (*os.File, error) {
		return nil, errors.Join(cause, releaseVolumeLock(file))
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("secure volume lock %s: %w", lockPath, err))
	}
	if err := file.Truncate(0); err != nil {
		return cleanup(fmt.Errorf("truncate volume lock %s: %w", lockPath, err))
	}
	if _, err := file.Seek(0, 0); err != nil {
		return cleanup(fmt.Errorf("seek volume lock %s: %w", lockPath, err))
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		return cleanup(fmt.Errorf("write volume lock %s: %w", lockPath, err))
	}
	return file, nil
}

func releaseVolumeLock(file *os.File) error {
	if file == nil {
		return nil
	}
	return errors.Join(unlockVolumeFile(file), file.Close())
}
