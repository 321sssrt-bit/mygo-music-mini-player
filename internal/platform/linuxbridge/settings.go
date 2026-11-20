package linuxbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

const startupFile = "dev.egoist.music.miniplayer.desktop"

func desktopEntry(argv []string) (string, error) {
	if len(argv) == 0 || argv[0] == "" || strings.Contains(argv[0], "=") {
		return "", errors.New("invalid autostart executable")
	}
	// Exec quoting is applied after Desktop Entry string unescaping.
	quote := strings.NewReplacer("\\", "\\\\\\\\", "\"", "\\\\\"", "$", "\\\\$", "\x60", "\\\\\x60", "%", "%%")
	args := make([]string, 0, len(argv))
	for _, arg := range argv {
		if strings.ContainsFunc(arg, unicode.IsControl) {
			return "", errors.New("autostart argument contains a control character")
		}
		args = append(args, "\""+quote.Replace(arg)+"\"")
	}
	return "[Desktop Entry]\nType=Application\nName=MyGo Music Mini Player\nExec=" + strings.Join(args, " ") + "\n", nil
}

func setLoginStartup(configDir string, argv []string) error {
	dir := filepath.Join(configDir, "autostart")
	path := filepath.Join(dir, startupFile)
	if len(argv) == 0 {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	content, err := desktopEntry(argv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".miniplayer-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(content)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readSettings(configDir, pluginDir, language string) (bridge.SettingsSnapshot, error) {
	host, hostErr := readJSON[bridge.HostSettings](filepath.Join(configDir, hostName, "settings.json"))
	if host.Appearance != "light" && host.Appearance != "dark" {
		host.Appearance = "system"
	}
	if host.Language != "en" && host.Language != "zh-CN" {
		host.Language = language
	}
	if host.PlayerStyle != "vinyl" && host.PlayerStyle != "cassette" {
		host.PlayerStyle = ""
	}
	values, pluginErr := readJSON[map[string]string](filepath.Join(pluginDir, "settings.json"))
	plugin := bridge.PluginSettings{
		DisplayPolicy: values["displayPolicy"],
		SmokeOverride: values["smokeOverride"],
		GuardMode:     values["guardMode"] == "true",
		Hotkey:        values["hotkey"],
	}
	if plugin.DisplayPolicy != "pinned" {
		plugin.DisplayPolicy = "auto"
	}
	if plugin.SmokeOverride != "on" && plugin.SmokeOverride != "off" {
		plugin.SmokeOverride = "follow"
	}
	return bridge.SettingsSnapshot{Host: host, Plugin: plugin}, errors.Join(hostErr, pluginErr)
}

func readJSON[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		var zero T
		return zero, fmt.Errorf("read %s: %w", path, err)
	}
	return value, nil
}
