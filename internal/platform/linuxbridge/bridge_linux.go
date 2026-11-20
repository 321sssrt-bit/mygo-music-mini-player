//go:build linux

// Package linuxbridge connects the companion to MyGo Music on Linux.
package linuxbridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/egoist/mygo"
	"github.com/godbus/dbus/v5"
)

const operationTimeout = 2 * time.Second

// Adapter implements bridge.HostBridge for the current Linux desktop session.
type Adapter struct {
	configDir, pluginDir string
	art                  artworkCache
	shortcut             hotkey
}

var _ bridge.HostBridge = (*Adapter)(nil)

// New uses the host's standard XDG configuration directory and the plugin data
// directory supplied by the companion's launcher.
func New(pluginDataDir string) (*Adapter, error) {
	if !filepath.IsAbs(pluginDataDir) {
		return nil, errors.New("an absolute MyGo Music plugin data directory is required")
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configDir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".config")
	}
	return &Adapter{configDir: configDir, pluginDir: pluginDataDir}, nil
}

func (a *Adapter) ObserveHost() (bridge.HostState, error) {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return bridge.HostState{}, err
	}
	for _, proc := range procs {
		pid, err := strconv.ParseUint(proc.Name(), 10, 32)
		if err == nil && isHostProcess(uint32(pid)) {
			_, state := inspectWindow()
			return bridge.HostState{Running: true, Window: state}, nil
		}
	}
	return bridge.HostState{}, nil
}

func isHostProcess(pid uint32) bool {
	if pid == 0 {
		return false
	}
	proc := "/proc/" + strconv.FormatUint(uint64(pid), 10)
	exe, err := os.Readlink(proc + "/exe")
	if err != nil || filepath.Base(strings.TrimSuffix(exe, " (deleted)")) != hostSlug {
		return false
	}
	info, err := os.Stat(proc)
	return err == nil && info.Sys().(*syscall.Stat_t).Uid == uint32(os.Geteuid())
}

func inspectWindow() (xWindow, bridge.WindowState) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" ||
		strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		return xWindow{}, bridge.WindowUnknown
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	list, err := command(ctx, "wmctrl", "-lp")
	if err != nil {
		return xWindow{}, bridge.WindowUnknown
	}
	window, state := mainWindow(string(list), isHostProcess)
	if window.id == "" {
		return window, state
	}
	data, err := command(ctx, "xprop", "-id", window.id, "_NET_WM_PID", "WM_STATE", "_NET_WM_STATE", "WM_TRANSIENT_FOR")
	if err != nil {
		return xWindow{}, bridge.WindowUnknown
	}
	state = windowState(string(data), window.pid)
	if state == bridge.WindowUnknown {
		return xWindow{}, state
	}
	return window, state
}

func (a *Adapter) WakeMainWindow() error {
	var activateErr error
	if window, _ := inspectWindow(); window.id != "" {
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		_, activateErr = command(ctx, "wmctrl", "-ia", window.id)
		cancel()
		if activateErr == nil {
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	bus, owner, raiseErr := hostMedia(ctx)
	if raiseErr == nil {
		var canRaise dbus.Variant
		raiseErr = bus(owner, props+".Get", mprisRoot, "CanRaise").Store(&canRaise)
		if raiseErr == nil && canRaise.Value() == true {
			raiseErr = bus(owner, mprisRoot+".Raise").Err
			if raiseErr == nil {
				cancel()
				return nil
			}
		}
	}
	cancel()
	if mygo.App.IsReady() {
		if err := mygo.Shell.OpenExternal(hostSlug + "://"); err != nil {
			return errors.Join(activateErr, raiseErr, err)
		}
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := command(ctx, "gio", "open", hostSlug+"://"); err != nil {
		return errors.Join(activateErr, raiseErr, err)
	}
	return nil
}

func hostMedia(ctx context.Context) (mediaBus, string, error) {
	// The companion can use an existing session but must not launch a D-Bus daemon.
	if address := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); address == "" || address == "autolaunch:" {
		return nil, "", bridge.ErrUnavailable
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, "", err
	}
	bus := func(destination, method string, args ...any) *dbus.Call {
		path := dbus.ObjectPath(mprisPath)
		if destination == busName {
			path = "/org/freedesktop/DBus"
		}
		return conn.Object(destination, path).CallWithContext(ctx, method, 0, args...)
	}
	owner, err := mediaOwner(bus, isHostProcess)
	return bus, owner, err
}

func (a *Adapter) Media() (bridge.MediaSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	bus, owner, err := hostMedia(ctx)
	if errors.Is(err, bridge.ErrUnavailable) {
		a.art.clear()
		return bridge.MediaSnapshot{}, nil
	}
	if err != nil {
		return bridge.MediaSnapshot{}, err
	}
	var properties map[string]dbus.Variant
	if err := bus(owner, props+".GetAll", player).Store(&properties); err != nil {
		if gone(err) {
			a.art.clear()
			return bridge.MediaSnapshot{}, nil
		}
		return bridge.MediaSnapshot{}, err
	}
	snapshot, artworkURL := mediaSnapshot(properties)
	snapshot.Artwork = a.art.load(ctx, owner, artworkURL)
	return snapshot, nil
}

func (a *Adapter) ControlPlayback(action bridge.PlaybackAction) error {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	bus, owner, err := hostMedia(ctx)
	if err != nil {
		return err
	}
	return controlPlayback(bus, owner, action)
}

func (a *Adapter) SeekMS(positionMS int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	bus, owner, err := hostMedia(ctx)
	if err != nil {
		return err
	}
	return seekMS(bus, owner, positionMS)
}

func hostStream(ctx context.Context) (pulseStream, error) {
	data, err := command(ctx, "pactl", "--format=json", "list", "sink-inputs")
	if err != nil {
		return pulseStream{}, err
	}
	return matchStream(data, isHostProcess)
}

func (a *Adapter) Volume() (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	stream, err := hostStream(ctx)
	if err != nil {
		return 0, err
	}
	return streamLevel(stream)
}

func (a *Adapter) SetVolume(level float64) error {
	volume, err := pulseVolume(level)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	stream, err := hostStream(ctx)
	if err != nil {
		return err
	}
	_, err = command(ctx, "pactl", "set-sink-input-volume", strconv.FormatUint(uint64(*stream.Index), 10), volume)
	return err
}

// PeakLevel leaves the timing fallback to the companion since pactl exposes no per-stream meter.
func (a *Adapter) PeakLevel() (float64, bool, error) { return 0, false, nil }

func (a *Adapter) SetLoginStartup(argv []string) error {
	return setLoginStartup(a.configDir, argv)
}

func (a *Adapter) Settings() (bridge.SettingsSnapshot, error) {
	language := "en"
	for _, locale := range mygo.App.PreferredLanguages() {
		base, _, _ := strings.Cut(locale, "-")
		if base == "zh" {
			language = "zh-CN"
			break
		}
		if base == "en" {
			break
		}
	}
	snapshot, err := readSettings(a.configDir, a.pluginDir, language)
	if mygo.App.IsReady() {
		snapshot.Host.ReduceMotion = mygo.Theme.Preferences().ReduceMotion
	}
	return snapshot, err
}

func (a *Adapter) SetHotkey(combo string, onPress func()) error {
	register := func(keys string, fn func()) error {
		if !mygo.App.IsReady() {
			return fmt.Errorf("global shortcut %q requires a ready MyGo app: %w", keys, bridge.ErrUnavailable)
		}
		return mygo.GlobalShortcut.Register(keys, fn)
	}
	return a.shortcut.replace(combo, onPress, register, mygo.GlobalShortcut.Unregister)
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	data, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) != 0 {
			return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return data, nil
}
