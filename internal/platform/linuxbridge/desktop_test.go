package linuxbridge

import (
	"errors"
	"testing"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func TestMainWindow(t *testing.T) {
	host := func(pid uint32) bool { return pid == 42 }
	settingsAndBrowser := "0x200  0 42 machine Settings\n0x300  0 10 machine MyGo Music\n"
	main := "0x00400  0 42 machine MyGo Music\n"
	found, _ := mainWindow(settingsAndBrowser+main, host)
	if found.id != "0x400" || found.pid != 42 {
		t.Fatalf("main window = %+v", found)
	}
	if found, state := mainWindow(settingsAndBrowser, host); found.id != "" || state != bridge.WindowHidden {
		t.Fatalf("hidden main window = %+v, %v", found, state)
	}
	for _, list := range []string{main + main, "0x400  0 0 machine MyGo Music"} {
		if found, state := mainWindow(list, host); found.id != "" || state != bridge.WindowUnknown {
			t.Fatalf("unverified window = %+v, %v", found, state)
		}
	}
	pidLine := "_NET_WM_PID(CARDINAL) = 42\n"
	for _, tt := range []struct {
		properties string
		want       bridge.WindowState
	}{
		{pidLine + "WM_STATE(WM_STATE):\n\t\twindow state: Normal", bridge.WindowVisible},
		{pidLine + "WM_STATE(WM_STATE):\n\t\twindow state: Iconic", bridge.WindowMinimized},
		{pidLine + "_NET_WM_STATE(ATOM) = _NET_WM_STATE_HIDDEN", bridge.WindowMinimized},
		{pidLine + "WM_STATE(WM_STATE):\n\t\twindow state: Withdrawn", bridge.WindowHidden},
		{pidLine + "WM_TRANSIENT_FOR(WINDOW): window id # 0x500\n\t\twindow state: Normal", bridge.WindowUnknown},
		{"_NET_WM_PID(CARDINAL) = 10\n\t\twindow state: Normal", bridge.WindowUnknown},
	} {
		if state := windowState(tt.properties, 42); state != tt.want {
			t.Fatalf("window state for %q = %v, want %v", tt.properties, state, tt.want)
		}
	}
}

func TestHotkeyReplacement(t *testing.T) {
	active := make(map[string]func())
	conflict := errors.New("shortcut already bound")
	register := func(combo string, press func()) error {
		if combo == "Ctrl+N" {
			return conflict
		}
		active[combo] = press
		return nil
	}
	unregister := func(combo string) { delete(active, combo) }
	var h hotkey
	presses := 0
	if err := h.replace("Ctrl+Shift+M", func() { presses++ }, register, unregister); err != nil {
		t.Fatal(err)
	}
	if err := h.replace("Ctrl+N", func() { presses += 100 }, register, unregister); !errors.Is(err, conflict) {
		t.Fatalf("conflicting binding = %v", err)
	}
	active["Ctrl+Shift+M"]()
	for _, alias := range []string{"ctrl+shift+m", "Shift + Control + M", "Shift+CmdOrCtrl+M"} {
		if err := h.replace(alias, func() { presses += 2 }, register, unregister); err != nil {
			t.Fatal(err)
		}
		if active["Ctrl+Shift+M"] == nil || len(active) != 1 {
			t.Fatalf("alias %q changed the native binding", alias)
		}
	}
	active["Ctrl+Shift+M"]()
	if err := h.replace("Ctrl+B", func() { presses += 4 }, register, unregister); err != nil {
		t.Fatal(err)
	}
	active["Ctrl+B"]()
	if len(active) != 1 || presses != 7 {
		t.Fatalf("active bindings = %v, presses = %d", len(active), presses)
	}
	if err := h.replace("", nil, register, unregister); err != nil || len(active) != 0 {
		t.Fatalf("failed to clear binding: %v (%d active)", err, len(active))
	}
}

func TestWakeOnlyRunningHost(t *testing.T) {
	alive := false
	opened := ""
	running := func() (bool, error) { return alive, nil }
	openURL := func(uri string) error { opened = uri; return nil }
	if err := openRunningHost(running, openURL); !errors.Is(err, bridge.ErrUnavailable) || opened != "" {
		t.Fatalf("stopped host: err=%v, URL=%q", err, opened)
	}
	alive = true
	if err := openRunningHost(running, openURL); err != nil || opened != "mygo-music://" {
		t.Fatalf("running host: err=%v, URL=%q", err, opened)
	}
}
