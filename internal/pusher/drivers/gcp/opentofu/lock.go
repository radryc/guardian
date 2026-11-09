package opentofu

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// acquireLock takes a non-blocking exclusive flock on path. Two workspaces for
// the same partition/intent cannot run concurrently.
func acquireLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opentofu: open lock %q: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("opentofu: lock workspace: %w", err)
	}
	return file, nil
}

func releaseLock(file *os.File) error {
	if file == nil {
		return nil
	}
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("opentofu: unlock workspace: %w", err)
	}
	return closeErr
}
