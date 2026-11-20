//go:build windows && (amd64 || arm64)

package winbridge

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	registerHotKey   = user32.NewProc("RegisterHotKey")
	unregisterHotKey = user32.NewProc("UnregisterHotKey")
	peekMessage      = user32.NewProc("PeekMessageW")
	getMessage       = user32.NewProc("GetMessageW")
	postThreadMsg    = user32.NewProc("PostThreadMessageW")
	currentThread    = kernel32.NewProc("GetCurrentThreadId")
)

const (
	modNoRepeat = 0x4000
	wmHotkey    = 0x0312
	wmQuit      = 0x0012
	bindingID   = 1
)

type keyCombo struct {
	mod, key uintptr
}

func parseHotkey(combo string) (keyCombo, error) {
	parts := strings.Split(strings.ToUpper(combo), "+")
	var parsed keyCombo
	invalid := fmt.Errorf("invalid hotkey %q; use a modifier and one key, for example Ctrl+Alt+M", combo)
	for _, part := range parts[:len(parts)-1] {
		mod := map[string]uintptr{"ALT": 1, "CTRL": 2, "CONTROL": 2, "SHIFT": 4, "WIN": 8}[strings.TrimSpace(part)]
		if mod == 0 || parsed.mod&mod != 0 {
			return keyCombo{}, invalid
		}
		parsed.mod |= mod
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	switch {
	case len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9'):
		parsed.key = uintptr(key[0])
	case strings.HasPrefix(key, "F") && len(key) <= 3 && len(key) > 1 && key[1] != '0':
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 24 && n != 12 {
			parsed.key = uintptr(0x6f + n)
		}
	default:
		parsed.key = map[string]uintptr{
			"SPACE": 0x20, "TAB": 0x09, "ENTER": 0x0d, "ESC": 0x1b, "ESCAPE": 0x1b,
			"HOME": 0x24, "END": 0x23, "INSERT": 0x2d, "DELETE": 0x2e,
			"PAGEUP": 0x21, "PAGEDOWN": 0x22, "LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
		}[key]
	}
	if parsed.mod == 0 || parsed.key == 0 {
		return keyCombo{}, invalid
	}
	return parsed, nil
}

type hotkeyBinding struct {
	combo    keyCombo
	threadID uintptr
	done     chan struct{}
	callback atomic.Pointer[func()]
}

type msg struct {
	hwnd   uintptr
	id     uint32
	wParam uintptr
	lParam uintptr
	time   uint32
	x, y   int32
	_      uint32
}

func startHotkey(combo keyCombo) (*hotkeyBinding, error) {
	binding := &hotkeyBinding{combo: combo, done: make(chan struct{})}
	ready := make(chan error, 1)
	go binding.run(ready)
	if err := <-ready; err != nil {
		<-binding.done
		return nil, err
	}
	return binding, nil
}

func (h *hotkeyBinding) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(h.done)
	var message msg
	// PostThreadMessage needs the target thread's queue to exist.
	peekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0x0400, 0x0400, 0)
	ret, _, err := registerHotKey.Call(0, bindingID, h.combo.mod|modNoRepeat, h.combo.key)
	if ret == 0 {
		ready <- err
		return
	}
	defer unregisterHotKey.Call(0, bindingID)
	h.threadID, _, _ = currentThread.Call()
	ready <- nil

	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		if message.id == wmHotkey && message.wParam == bindingID {
			if callback := h.callback.Load(); callback != nil {
				go (*callback)()
			}
		}
	}
}

func (h *hotkeyBinding) stop() error {
	select {
	case <-h.done:
		return nil
	default:
	}
	if ok, _, err := postThreadMsg.Call(h.threadID, wmQuit, 0, 0); ok == 0 {
		return err
	}
	<-h.done
	return nil
}

func (b *Bridge) SetHotkey(combo string, onPress func()) error {
	b.hotkeyMu.Lock()
	defer b.hotkeyMu.Unlock()

	combo = strings.TrimSpace(combo)
	old := b.hotkey
	if combo == "" {
		if old != nil {
			if err := old.stop(); err != nil {
				return fmt.Errorf("unregister hotkey: %w", err)
			}
			b.hotkey = nil
		}
		return nil
	}
	if onPress == nil {
		return errors.New("a hotkey requires a callback")
	}
	parsed, err := parseHotkey(combo)
	if err != nil {
		return err
	}
	if old != nil && old.combo == parsed {
		old.callback.Store(&onPress)
		return nil
	}
	next, err := startHotkey(parsed)
	if err != nil {
		if errors.Is(err, syscall.Errno(1409)) { // ERROR_HOTKEY_ALREADY_REGISTERED
			return fmt.Errorf("hotkey %q is already registered: %w", combo, err)
		}
		return fmt.Errorf("register hotkey %q: %w", combo, err)
	}
	if old != nil {
		if err := old.stop(); err != nil {
			return fmt.Errorf("replace hotkey: %w", errors.Join(err, next.stop()))
		}
	}
	next.callback.Store(&onPress)
	b.hotkey = next
	return nil
}
