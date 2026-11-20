//go:build windows && (amd64 || arm64)

package winbridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

const (
	hostTitle = "MyGo Music"
	hostExe   = hostTitle + ".exe"
	hostClass = "MyGoWindow"
)

var (
	processIDToSessionID  = kernel32.NewProc("ProcessIdToSessionId")
	queryProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	enumWindows           = user32.NewProc("EnumWindows")
	windowPID             = user32.NewProc("GetWindowThreadProcessId")
	windowClass           = user32.NewProc("GetClassNameW")
	windowTitle           = user32.NewProc("GetWindowTextW")
	getWindow             = user32.NewProc("GetWindow")
	isWindow              = user32.NewProc("IsWindow")
	isWindowVisible       = user32.NewProc("IsWindowVisible")
	isIconic              = user32.NewProc("IsIconic")
	showWindowAsync       = user32.NewProc("ShowWindowAsync")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	shellExecute          = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	dwmapi                = syscall.NewLazyDLL("dwmapi.dll")
	dwmGetAttribute       = dwmapi.NewProc("DwmGetWindowAttribute")
	dwmSetAttribute       = dwmapi.NewProc("DwmSetWindowAttribute")
	rtlGetVersion         = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
)

func processSession(pid uint32) (uint32, error) {
	var session uint32
	ret, _, err := processIDToSessionID.Call(uintptr(pid), uintptr(unsafe.Pointer(&session)))
	if ret == 0 {
		return 0, err
	}
	return session, nil
}

func hostImage(pid, localSession uint32) string {
	if session, err := processSession(pid); err != nil || session != localSession {
		return ""
	}
	process, err := syscall.OpenProcess(0x1000, false, pid) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(process)
	var buf [1024]uint16
	n := uint32(len(buf))
	short := buf[:]
	ret, _, winErr := queryProcessImageName.Call(uintptr(process), 0, uintptr(unsafe.Pointer(&short[0])), uintptr(unsafe.Pointer(&n)))
	if ret == 0 && errors.Is(winErr, syscall.ERROR_INSUFFICIENT_BUFFER) {
		short = make([]uint16, 32768)
		n = uint32(len(short))
		ret, _, _ = queryProcessImageName.Call(uintptr(process), 0, uintptr(unsafe.Pointer(&short[0])), uintptr(unsafe.Pointer(&n)))
	}
	if ret != 0 {
		path := syscall.UTF16ToString(short[:n])
		if strings.EqualFold(filepath.Base(path), hostExe) {
			return path
		}
	}
	return ""
}

func hostProcesses() (map[uint32]string, error) {
	session, err := processSession(uint32(os.Getpid()))
	if err != nil {
		return nil, err
	}
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snapshot)
	pids := make(map[uint32]string)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), hostExe) {
			if path := hostImage(entry.ProcessID, session); path != "" {
				pids[entry.ProcessID] = path
			}
		}
	}
	if !errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return pids, nil
}

func windowString(proc *syscall.LazyProc, hwnd uintptr) string {
	var buf [128]uint16
	n, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func isHostMainWindow(class, title string, owner uintptr) bool {
	return class == hostClass && title == hostTitle && owner == 0
}

type windowSearch struct {
	pids  map[uint32]string
	hwnd  uintptr
	state bridge.WindowState
}

var enumHostWindow = syscall.NewCallback(func(hwnd uintptr, search *windowSearch) uintptr {
	var pid uint32
	windowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if search.pids[pid] == "" {
		return 1
	}
	owner, _, _ := getWindow.Call(hwnd, 4) // GW_OWNER
	if !isHostMainWindow(windowString(windowClass, hwnd), windowString(windowTitle, hwnd), owner) {
		return 1
	}

	state := bridge.WindowHidden
	if iconic, _, _ := isIconic.Call(hwnd); iconic != 0 {
		state = bridge.WindowMinimized
	} else if visible, _, _ := isWindowVisible.Call(hwnd); visible != 0 {
		var cloaked uint32
		dwmGetAttribute.Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), 4) // DWMWA_CLOAKED
		if cloaked == 0 {
			state = bridge.WindowVisible
		}
	}
	if search.hwnd == 0 || state == bridge.WindowVisible || state == bridge.WindowMinimized && search.state == bridge.WindowHidden {
		search.hwnd, search.state = hwnd, state
	}
	return 1
})

func findHostWindow() (bridge.HostState, uintptr, error) {
	pids, err := hostProcesses()
	if err != nil || len(pids) == 0 {
		return bridge.HostState{}, 0, err
	}
	search := windowSearch{pids: pids}
	ret, _, err := enumWindows.Call(enumHostWindow, uintptr(unsafe.Pointer(&search)))
	if ret == 0 {
		return bridge.HostState{Running: true}, 0, err
	}
	return bridge.HostState{Running: true, Window: search.state}, search.hwnd, nil
}

func (*Bridge) ObserveHost() (bridge.HostState, error) {
	state, _, err := findHostWindow()
	return state, err
}

func (*Bridge) WakeMainWindow() error {
	state, hwnd, _ := findHostWindow()
	if hwnd != 0 {
		switch state.Window {
		case bridge.WindowMinimized:
			showWindowAsync.Call(hwnd, 9) // SW_RESTORE
		case bridge.WindowHidden:
			showWindowAsync.Call(hwnd, 5) // SW_SHOW
		}
		if ok, _, _ := setForegroundWindow.Call(hwnd); ok != 0 {
			return nil
		}
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	uri, _ := syscall.UTF16PtrFromString("mygo-music://")
	ret, _, _ := shellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(uri)), 0, 0, 1)
	if ret <= 32 {
		return fmt.Errorf("open mygo-music://: ShellExecuteW code %d", ret)
	}
	return nil
}

// DisableWindowDecorations turns off the Windows 11 DWM corner rounding and
// outline for hwnd. Earlier versions of Windows do not apply these attributes.
func DisableWindowDecorations(hwnd uintptr) error {
	if valid, _, _ := isWindow.Call(hwnd); valid == 0 {
		return fmt.Errorf("DWM decorations: invalid window handle")
	}
	var version struct {
		size, major, minor, build, platform uint32
		servicePack                         [128]uint16
	}
	version.size = uint32(unsafe.Sizeof(version))
	if ret, _, _ := rtlGetVersion.Call(uintptr(unsafe.Pointer(&version))); ret != 0 {
		return fmt.Errorf("get Windows version: NTSTATUS %#x", uint32(ret))
	}
	if version.build < 22000 {
		return nil
	}
	for _, attribute := range []struct{ id, value uint32 }{
		{33, 1},          // DWMWA_WINDOW_CORNER_PREFERENCE, DWMWCP_DONOTROUND
		{34, 0xfffffffe}, // DWMWA_BORDER_COLOR, DWMWA_COLOR_NONE
	} {
		ret, _, _ := dwmSetAttribute.Call(hwnd, uintptr(attribute.id), uintptr(unsafe.Pointer(&attribute.value)), 4)
		if err := checkHR(ret); err != nil {
			return fmt.Errorf("set DWM attribute %d: %w", attribute.id, err)
		}
	}
	return nil
}
