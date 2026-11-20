package control

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx                 = kernel32.NewProc("LockFileEx")
	unlockFileEx               = kernel32.NewProc("UnlockFileEx")
	setFileInformationByHandle = kernel32.NewProc("SetFileInformationByHandle")
)

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

func releaseLock(file *os.File) error {
	var offset syscall.Overlapped
	ok, _, err := unlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&offset)))
	if ok == 0 {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}

func replaceState(source, destination string) error {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16FromString(absolute)
	if err != nil {
		return err
	}
	path, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	const deleteAccess = 0x10000
	handle, err := syscall.CreateFile(path, deleteAccess,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return &os.LinkError{Op: "replace", Old: source, New: destination, Err: err}
	}
	defer syscall.CloseHandle(handle)

	// A single rename with POSIX semantics keeps existing readers on the old file.
	const fileRenameInfoEx = 22
	const replaceWithPOSIXSemantics = 0x1 | 0x2
	type fileRenameInfo struct {
		Flags    uint32
		Root     uintptr
		NameSize uint32
		Name     uint16
	}
	var layout fileRenameInfo
	nameOffset := unsafe.Offsetof(layout.Name)
	buffer := make([]byte, max(unsafe.Sizeof(layout), nameOffset+uintptr(len(target)*2)))
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.Flags = replaceWithPOSIXSemantics
	info.NameSize = uint32((len(target) - 1) * 2)
	copy(unsafe.Slice(&info.Name, len(target)), target)
	err = retrySharingViolation(func() error {
		ok, _, err := setFileInformationByHandle.Call(uintptr(handle), fileRenameInfoEx,
			uintptr(unsafe.Pointer(info)), uintptr(len(buffer)))
		if ok == 0 {
			return err
		}
		return nil
	})
	if err != nil {
		return &os.LinkError{Op: "replace", Old: source, New: destination, Err: err}
	}
	return nil
}

func removeState(path string) error {
	return retrySharingViolation(func() error { return os.Remove(path) })
}

func retrySharingViolation(operation func() error) error {
	const sharingViolation = syscall.Errno(32)
	err := operation()
	// Readers without FILE_SHARE_DELETE can briefly block replacement or removal.
	for range 20 {
		if !errors.Is(err, sharingViolation) {
			break
		}
		time.Sleep(10 * time.Millisecond)
		err = operation()
	}
	return err
}
