package linuxbridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func TestLanguageBeforeMyGoReady(t *testing.T) {
	env := map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}
	getenv := func(key string) string { return env[key] }
	if got := hostLanguage(environmentLocales(getenv)); got != "zh-CN" {
		t.Fatalf("LC_MESSAGES language = %s", got)
	}
	env["LANGUAGE"] = "fr_FR:en_GB:zh_CN"
	if got := hostLanguage(environmentLocales(getenv)); got != "en" {
		t.Fatalf("LANGUAGE priority = %s", got)
	}
}

func TestXDGAutostart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autostart", startupFile)
	argv := []string{"/opt/MyGo Music/mini-player", "--guard", `100% $data"\dir`, ""}
	wantExec := `Exec="/opt/MyGo Music/mini-player" "--guard" "100%% \\$data\\"\\\\dir" ""`
	for range 2 {
		if err := setLoginStartup(dir, argv); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), wantExec+"\n") {
			t.Fatalf("entry = %q, %v\nwant %s", data, err, wantExec)
		}
	}
	if err := setLoginStartup(dir, []string{"/opt/mini-player", "a\n[Desktop Entry]"}); err == nil {
		t.Fatal("accepted multiline argument")
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), wantExec) {
		t.Fatalf("invalid replacement changed entry: %q, %v", contents, err)
	}
	for range 2 {
		if err := setLoginStartup(dir, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("autostart entry still present: %v", err)
		}
	}
}

func TestSettingsSnapshots(t *testing.T) {
	config, plugin := t.TempDir(), t.TempDir()
	defaults := bridge.SettingsSnapshot{
		Host:   bridge.HostSettings{Appearance: "system", Language: "en"},
		Plugin: bridge.PluginSettings{DisplayPolicy: "auto", SmokeOverride: "follow"},
	}
	if got, err := readSettings(config, plugin, "en"); err != nil || got != defaults {
		t.Fatalf("default settings = %+v, %v", got, err)
	}
	host := filepath.Join(config, hostName)
	if err := os.Mkdir(host, 0o700); err != nil {
		t.Fatal(err)
	}
	for dir, settings := range map[string]string{
		host:   `{"appearance":"dark","language":"system","smoke":true,"fog":true,"playerStyle":"vinyl"}`,
		plugin: `{"displayPolicy":"pinned","smokeOverride":"off","guardMode":"true","hotkey":"Ctrl+M"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want := bridge.SettingsSnapshot{
		Host:   bridge.HostSettings{Appearance: "dark", Language: "zh-CN", Smoke: true, Fog: true, PlayerStyle: "vinyl"},
		Plugin: bridge.PluginSettings{DisplayPolicy: "pinned", SmokeOverride: "off", GuardMode: true, Hotkey: "Ctrl+M"},
	}
	if got, err := readSettings(config, plugin, "zh-CN"); err != nil || got != want {
		t.Fatalf("configured settings = %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(host, "settings.json"), []byte(`{"smoke":true,"fog":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want.Host = defaults.Host
	if got, err := readSettings(config, plugin, "en"); err == nil || got != want {
		t.Fatalf("malformed host settings = %+v, %v", got, err)
	}
}
