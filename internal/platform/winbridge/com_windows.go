//go:build windows && (amd64 || arm64)

package winbridge

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	combase  = syscall.NewLazyDLL("combase.dll")

	coInitializeEx = ole32.NewProc("CoInitializeEx")
	coUninitialize = ole32.NewProc("CoUninitialize")
	roInitialize   = combase.NewProc("RoInitialize")
	roUninitialize = combase.NewProc("RoUninitialize")
)

type comObject struct {
	vtable *[64]uintptr
}

type hresult uint32

func (hr hresult) Error() string { return fmt.Sprintf("HRESULT 0x%08X", uint32(hr)) }

func checkHR(ret uintptr) error {
	if int32(ret) < 0 {
		return hresult(ret)
	}
	return nil
}

//go:uintptrescapes
func (obj *comObject) call(slot int, args ...uintptr) uintptr {
	ret, _, _ := syscall.SyscallN(obj.vtable[slot], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return ret
}

//go:uintptrescapes
func (obj *comObject) object(slot int, args ...uintptr) (*comObject, error) {
	var out *comObject
	var pin runtime.Pinner
	pin.Pin(&out)
	defer pin.Unpin()
	ret := obj.call(slot, append(args, uintptr(unsafe.Pointer(&out)))...)
	if err := checkHR(ret); err != nil {
		return nil, err
	}
	return out, nil
}

func (obj *comObject) release() { obj.call(2) }

func (obj *comObject) query(iid *syscall.GUID) (*comObject, error) {
	return obj.object(0, uintptr(unsafe.Pointer(iid)))
}

func guid(a uint32, b, c uint16, d [8]byte) syscall.GUID {
	return syscall.GUID{Data1: a, Data2: b, Data3: c, Data4: d}
}

func inApartment(winRT bool, run func() error) error {
	result := make(chan error, 1)
	// A caller can be on MyGo's STA UI thread.
	go func() {
		result <- runInMTA(winRT, run)
	}()
	return <-result
}

func runInMTA(winRT bool, run func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var ret uintptr
	if winRT {
		ret, _, _ = roInitialize.Call(1) // RO_INIT_MULTITHREADED
	} else {
		ret, _, _ = coInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	}
	if err := checkHR(ret); err != nil {
		return err
	}
	if winRT {
		defer roUninitialize.Call()
	} else {
		defer coUninitialize.Call()
	}
	return run()
}
