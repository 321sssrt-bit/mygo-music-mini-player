//go:build windows && (amd64 || arm64)

package winbridge

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func TestSettingsSnapshots(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	initial, err := b.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if initial.Host.Appearance != "system" || initial.Plugin != (bridge.PluginSettings{DisplayPolicy: "auto", SmokeOverride: "follow"}) {
		t.Fatalf("defaults = %+v", initial)
	}
	pluginDir := filepath.Join(dir, "plugin-data", "mini-player")
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "settings.json"), `{"appearance":"dark","language":"zh-CN","smoke":true,"fog":true,"playerStyle":"cassette"}`)
	pluginFile := filepath.Join(pluginDir, "settings.json")
	write(pluginFile, `{"displayPolicy":"pinned","smokeOverride":"off","guardMode":"true","hotkey":"Ctrl+Alt+M"}`)
	got, err := b.Settings()
	want := bridge.SettingsSnapshot{
		Host:   bridge.HostSettings{Appearance: "dark", Language: "zh-CN", Smoke: true, Fog: true, PlayerStyle: "cassette", ReduceMotion: got.Host.ReduceMotion},
		Plugin: bridge.PluginSettings{DisplayPolicy: "pinned", SmokeOverride: "off", GuardMode: true, Hotkey: "Ctrl+Alt+M"},
	}
	if err != nil || got != want {
		t.Fatalf("settings = %+v, %v", got, err)
	}
	t.Logf("host and plugin preferences: %+v", got)
	write(pluginFile, `{"displayPolicy":"pinned","guardMode":true}`)
	got, err = b.Settings()
	if err == nil || got.Plugin != initial.Plugin || got.Host != want.Host {
		t.Fatalf("malformed plugin settings = %+v, %v", got, err)
	}
}

func TestLoginStartupIsolation(t *testing.T) {
	subkey := `Software\MyGoMusicMiniPlayer-Test-` + filepath.Base(t.TempDir()) + "-" + time.Now().Format("20060102150405.000000000")
	path, _ := syscall.UTF16PtrFromString(subkey)
	name, _ := syscall.UTF16PtrFromString(startupEntry)
	t.Cleanup(func() {
		deleteKey := registry.NewProc("RegDeleteKeyW")
		ret, _, _ := deleteKey.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(path)))
		if ret != 0 && syscall.Errno(ret) != syscall.ERROR_FILE_NOT_FOUND {
			t.Errorf("delete isolated registry key: %v", syscall.Errno(ret))
		}
	})
	argv := []string{`C:\Program Files\Mini Player\player.exe`, "--guard", "", `C:\path with spaces\`, `a"b`}
	read := func() (string, error) {
		t.Helper()
		var key syscall.Handle
		if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, syscall.KEY_QUERY_VALUE, &key); err != nil {
			return "", err
		}
		defer syscall.RegCloseKey(key)
		var typ uint32
		var value [261]uint16
		size := uint32(unsafe.Sizeof(value))
		err := syscall.RegQueryValueEx(key, name, nil, &typ, (*byte)(unsafe.Pointer(&value[0])), &size)
		if err == nil && typ != syscall.REG_SZ {
			t.Fatalf("Run value type = %d", typ)
		}
		return syscall.UTF16ToString(value[:]), err
	}
	for range 2 {
		if err := setLoginStartup(subkey, argv); err != nil {
			t.Fatal(err)
		}
		command, err := read()
		if err != nil {
			t.Fatal(err)
		}
		want := `"C:\Program Files\Mini Player\player.exe" --guard "" "C:\path with spaces\\" a\"b`
		if command != want {
			t.Fatalf("startup command = %q, want %q", command, want)
		}
		t.Logf("isolated REG_SZ = %s", command)
	}
	for range 2 {
		if err := setLoginStartup(subkey, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := read(); !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			t.Fatalf("entry after removal = %v", err)
		}
	}
}

func TestWindowIdentity(t *testing.T) {
	for _, tc := range []struct {
		class, title string
		owner        uintptr
		want         bool
	}{
		{"MyGoWindow", "MyGo Music", 0, true},
		{"MyGoWindow", "Settings", 0, false},
		{"MyGoWindow", "设置", 0, false},
		{"MyGoApp", "MyGo Music", 0, false},
		{"MyGoWindow", "MyGo Music", 1, false},
		{"Chrome_WidgetWin_1", "MyGo Music", 0, false},
	} {
		if got := isHostMainWindow(tc.class, tc.title, tc.owner); got != tc.want {
			t.Errorf("window (%q, %q, %d) = %t", tc.class, tc.title, tc.owner, got)
		}
	}
}

func TestDWMDecorations(t *testing.T) {
	class, _ := syscall.UTF16PtrFromString("STATIC")
	create := user32.NewProc("CreateWindowExW")
	destroy := user32.NewProc("DestroyWindow")
	err := runInMTA(false, func() error {
		hwnd, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0x00cf0000, 0, 0, 64, 64, 0, 0, 0, 0)
		if hwnd == 0 {
			return err
		}
		defer destroy.Call(hwnd)
		if err := DisableWindowDecorations(hwnd); err != nil {
			return err
		}
		var corner uint32
		ret, _, _ := dwmGetAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&corner)), 4)
		if err := checkHR(ret); err != nil {
			if err == hresult(0x80070057) {
				t.Log("DWM corner attributes unavailable on this Windows version")
				return nil
			}
			return err
		}
		if corner != 1 {
			t.Errorf("DWM corner=%d, want 1", corner)
		}
		t.Logf("temporary HWND DWM corner=%d; border suppression returned S_OK", corner)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHotkeyReplacement(t *testing.T) {
	first, second := New(""), New("")
	t.Cleanup(func() {
		for _, b := range []*Bridge{first, second} {
			if err := b.SetHotkey("", nil); err != nil {
				t.Error(err)
			}
		}
	})
	a, b := "Ctrl+Shift+Alt+F23", "Ctrl+Shift+Alt+F24"
	received := make(chan string, 2)
	if err := first.SetHotkey(a, func() { received <- "first" }); err != nil {
		t.Skipf("isolated binding %s unavailable: %v", a, err)
	}
	if err := second.SetHotkey(b, func() {}); err != nil {
		t.Skipf("isolated binding %s unavailable: %v", b, err)
	}
	if err := first.SetHotkey(b, func() { received <- "unexpected" }); !errors.Is(err, syscall.Errno(1409)) {
		t.Fatalf("register conflicting key = %v", err)
	} else {
		t.Logf("conflict returns %v", err)
	}
	dispatch := func(want string) {
		t.Helper()
		ret, _, err := postThreadMsg.Call(first.hotkey.threadID, wmHotkey, bindingID, 0)
		if ret == 0 {
			t.Fatal(err)
		}
		select {
		case got := <-received:
			if got != want {
				t.Fatalf("hotkey callback = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the isolated hotkey callback")
		}
	}
	dispatch("first")
	if err := first.SetHotkey(" alt+Control+shift+f23 ", func() { received <- "updated" }); err != nil {
		t.Fatal(err)
	}
	dispatch("updated")
	if err := first.SetHotkey("", nil); err != nil {
		t.Fatal(err)
	}
	if err := second.SetHotkey(a, func() {}); err != nil {
		t.Fatalf("released binding couldn't be reused: %v", err)
	}
	t.Log("conflict preserves previous binding; normalized rebind updates callback; removal releases binding")
}

func TestInvalidInputsAndPeakDecay(t *testing.T) {
	b := New("")
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, 1.1} {
		if err := b.SetVolume(value); err == nil {
			t.Errorf("SetVolume(%v) accepted invalid value", value)
		}
	}
	for _, position := range []int64{-1, math.MaxInt64} {
		if err := b.SeekMS(position); err == nil {
			t.Errorf("SeekMS(%d) accepted invalid position", position)
		}
	}
	for _, hotkey := range []string{"F2", "ctrl+ctrl+a", "ctrl+f12", "ctrl+f25", "ctrl+"} {
		if err := b.SetHotkey(hotkey, func() {}); err == nil {
			t.Errorf("SetHotkey(%q) accepted invalid hotkey", hotkey)
		}
	}
	peak := decayPeak(0, 0.8, time.Millisecond)
	fade := decayPeak(peak, 0, 120*time.Millisecond)
	silence := decayPeak(fade, 0, time.Second)
	if peak != 0.8 || math.Abs(fade-0.2943) > 0.001 || silence != 0 {
		t.Fatalf("peak decay = %g, %g, %g", peak, fade, silence)
	}
	t.Logf("peak %.3f → %.3f → %.3f", peak, fade, silence)
}
