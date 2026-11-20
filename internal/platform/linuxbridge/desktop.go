package linuxbridge

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

var (
	errMissingCallback = errors.New("global shortcut requires a callback")
	errHotkeyBusy      = errors.New("global shortcut update already in progress")
)

type xWindow struct {
	id  string
	pid uint32
}

func mainWindow(list string, isHost func(uint32) bool) (xWindow, bridge.WindowState) {
	var candidate xWindow
	uncertain := false
	for line := range strings.Lines(list) {
		fields := strings.Fields(line)
		if len(fields) < 5 || strings.Join(fields[4:], " ") != hostName {
			continue
		}
		pid, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || pid == 0 {
			uncertain = true
			continue
		}
		if !isHost(uint32(pid)) {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(fields[0], "0x"), 16, 32)
		if err != nil || id == 0 || candidate.id != "" {
			return xWindow{}, bridge.WindowUnknown
		}
		candidate = xWindow{id: "0x" + strconv.FormatUint(id, 16), pid: uint32(pid)}
	}
	if candidate.id == "" && !uncertain {
		return candidate, bridge.WindowHidden
	}
	return candidate, bridge.WindowUnknown
}

func windowState(properties string, pid uint32) bridge.WindowState {
	var owned, transient, hidden bool
	var state bridge.WindowState
	for line := range strings.Lines(properties) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "_NET_WM_PID(CARDINAL) = "):
			owned = line == "_NET_WM_PID(CARDINAL) = "+strconv.FormatUint(uint64(pid), 10)
		case strings.HasPrefix(line, "WM_TRANSIENT_FOR(WINDOW):"):
			transient = true
		case strings.HasPrefix(line, "_NET_WM_STATE(ATOM) = "):
			hidden = strings.Contains(line, "_NET_WM_STATE_HIDDEN")
		case line == "window state: Normal":
			state = bridge.WindowVisible
		case line == "window state: Iconic":
			state = bridge.WindowMinimized
		case line == "window state: Withdrawn":
			state = bridge.WindowHidden
		}
	}
	if !owned || transient {
		return bridge.WindowUnknown
	}
	if hidden {
		return bridge.WindowMinimized
	}
	return state
}

type hotkey struct {
	mu      sync.Mutex
	combo   string
	onPress *atomic.Pointer[func()]
}

func (h *hotkey) replace(combo string, onPress func(), register func(string, func()) error, unregister func(string)) error {
	combo = strings.TrimSpace(combo)
	if combo != "" && onPress == nil {
		return errMissingCallback
	}
	// MyGo dispatches to the main thread, which can itself be a caller.
	if !h.mu.TryLock() {
		return errHotkeyBusy
	}
	defer h.mu.Unlock()
	if strings.EqualFold(h.combo, combo) {
		if combo != "" {
			h.onPress.Store(&onPress)
		}
		return nil
	}
	var callback *atomic.Pointer[func()]
	if combo != "" {
		callback = new(atomic.Pointer[func()])
		callback.Store(&onPress)
		if err := register(combo, func() { (*callback.Load())() }); err != nil {
			return err
		}
	}
	if h.combo != "" {
		unregister(h.combo)
	}
	h.combo, h.onPress = combo, callback
	return nil
}
