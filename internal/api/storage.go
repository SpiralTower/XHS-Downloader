package api

import (
	"errors"
	"fmt"
	"os"
)

func verifyWritableDirectory(directory string) error {
	file, err := os.CreateTemp(directory, ".xhs-write-check-*")
	if err != nil {
		return fmt.Errorf("%s is not writable: %w", directory, err)
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return errors.Join(closeErr, removeErr)
}
