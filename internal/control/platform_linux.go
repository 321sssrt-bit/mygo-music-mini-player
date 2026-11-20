package control

import (
	"errors"
	"os"
	"syscall"
)

func tryLock(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrAlreadyRunning
	}
	return err
}

func replaceState(source, destination string) error {
	return os.Rename(source, destination)
}

func removeState(path string) error {
	return os.Remove(path)
}
