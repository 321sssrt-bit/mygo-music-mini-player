//go:build windows && (amd64 || arm64)

package winbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

var (
	sysParameters          = user32.NewProc("SystemParametersInfoW")
	userPreferredLanguages = kernel32.NewProc("GetUserPreferredUILanguages")
	registry               = syscall.NewLazyDLL("advapi32.dll")
	regCreateKey           = registry.NewProc("RegCreateKeyExW")
	regSetValue            = registry.NewProc("RegSetValueExW")
	regDeleteValue         = registry.NewProc("RegDeleteValueW")
)

func (b *Bridge) Settings() (bridge.SettingsSnapshot, error) {
	dir := b.dataDir
	if dir == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return bridge.SettingsSnapshot{}, err
		}
		dir = filepath.Join(config, hostTitle)
	}
	snapshot, err := readSettings(dir, preferredLanguage)
	var animate int32
	ok, _, spiErr := sysParameters.Call(0x1042, 0, uintptr(unsafe.Pointer(&animate)), 0) // SPI_GETCLIENTAREAANIMATION
	if ok != 0 {
		snapshot.Host.ReduceMotion = animate == 0
	} else {
		err = errors.Join(err, fmt.Errorf("read system animation preference: %w", spiErr))
	}
	return snapshot, err
}

func preferredLanguage() string {
	var count, size uint32
	const muiLanguageName = 8
	ret, _, _ := userPreferredLanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&size)))
	if ret == 0 || size == 0 {
		return "en"
	}
	buf := make([]uint16, size)
	ret, _, _ = userPreferredLanguages.Call(muiLanguageName, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret != 0 {
		for _, tag := range strings.Split(string(utf16.Decode(buf[:size])), "\x00") {
			base, _, _ := strings.Cut(tag, "-")
			switch strings.ToLower(base) {
			case "zh":
				return "zh-CN"
			case "en":
				return "en"
			}
		}
	}
	return "en"
}

func readSettings(dir string, language func() string) (bridge.SettingsSnapshot, error) {
	var snapshot bridge.SettingsSnapshot
	hostPath := filepath.Join(dir, "settings.json")
	hostErr := readJSON(hostPath, &snapshot.Host)
	if hostErr != nil {
		snapshot.Host = bridge.HostSettings{}
	}
	switch snapshot.Host.Appearance {
	case "light", "dark":
	default:
		snapshot.Host.Appearance = "system"
	}
	switch snapshot.Host.Language {
	case "en", "zh-CN":
	default:
		snapshot.Host.Language = language()
	}
	switch snapshot.Host.PlayerStyle {
	case "vinyl", "cassette":
	default:
		snapshot.Host.PlayerStyle = ""
	}

	var plugin map[string]string
	pluginPath := filepath.Join(dir, "plugin-data", "mini-player", "settings.json")
	pluginErr := readJSON(pluginPath, &plugin)
	if pluginErr != nil {
		plugin = nil
	}
	snapshot.Plugin = bridge.PluginSettings{
		DisplayPolicy: "auto",
		SmokeOverride: "follow",
		GuardMode:     plugin["guardMode"] == "true",
		Hotkey:        plugin["hotkey"],
	}
	if plugin["displayPolicy"] == "pinned" {
		snapshot.Plugin.DisplayPolicy = "pinned"
	}
	if v := plugin["smokeOverride"]; v == "on" || v == "off" {
		snapshot.Plugin.SmokeOverride = v
	}
	return snapshot, errors.Join(hostErr, pluginErr)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupEntry = "MyGoMusicMiniPlayer"
)

func (*Bridge) SetLoginStartup(argv []string) error {
	return setLoginStartup(runKey, argv)
}

func startupCommand(argv []string) ([]uint16, error) {
	if !filepath.IsAbs(argv[0]) || strings.ContainsRune(argv[0], '"') {
		return nil, errors.New("login startup requires an absolute executable path")
	}
	args := make([]string, len(argv))
	args[0] = `"` + argv[0] + `"`
	for i, arg := range argv[1:] {
		args[i+1] = syscall.EscapeArg(arg)
	}
	command, err := syscall.UTF16FromString(strings.Join(args, " "))
	if err != nil {
		return nil, fmt.Errorf("invalid login startup arguments: %w", err)
	}
	if len(command)-1 > 260 {
		return nil, errors.New("login startup command exceeds the Windows Run limit of 260 characters")
	}
	return command, nil
}

func setLoginStartup(path string, argv []string) error {
	subkey, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	name, _ := syscall.UTF16PtrFromString(startupEntry)
	var key syscall.Handle
	if len(argv) == 0 {
		err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, subkey, 0, syscall.KEY_SET_VALUE, &key)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open login startup key: %w", err)
		}
		defer syscall.RegCloseKey(key)
		ret, _, _ := regDeleteValue.Call(uintptr(key), uintptr(unsafe.Pointer(name)))
		if ret != 0 && syscall.Errno(ret) != syscall.ERROR_FILE_NOT_FOUND {
			return fmt.Errorf("delete login startup entry: %w", syscall.Errno(ret))
		}
		return nil
	}
	command, err := startupCommand(argv)
	if err != nil {
		return err
	}
	ret, _, _ := regCreateKey.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(subkey)), 0, 0, 0,
		syscall.KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&key)), 0)
	if ret != 0 {
		return fmt.Errorf("open or create login startup key: %w", syscall.Errno(ret))
	}
	defer syscall.RegCloseKey(key)
	ret, _, _ = regSetValue.Call(uintptr(key), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ,
		uintptr(unsafe.Pointer(&command[0])), uintptr(len(command)*2))
	if ret != 0 {
		return fmt.Errorf("write login startup entry: %w", syscall.Errno(ret))
	}
	return nil
}
