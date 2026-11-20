package control

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func tryLock(file *os.File) error {
	const exclusiveNonblocking = 0x2 | 0x1
	const lockViolation = syscall.Errno(33)
	var offset syscall.Overlapped
	ok, _, err := lockFileEx.Call(file.Fd(), exclusiveNonblocking, 0, 1, 0, uintptr(unsafe.Pointer(&offset)))
	if ok != 0 {
		return nil
	}
	if errors.Is(err, lockViolation) {
		return ErrAlreadyRunning
	}
	return err
}
