// Package bridge defines the companion's boundary with MyGo Music and the desktop.
package bridge

import "errors"

// ErrUnavailable means the host resource needed for an operation is absent.
var ErrUnavailable = errors.New("host resource unavailable")

// WindowState describes the host's main window independently of process liveness.
type WindowState uint8

const (
	WindowUnknown WindowState = iota // The desktop cannot report the main window state.
	WindowHidden
	WindowVisible
	WindowMinimized
)

// HostState describes the host process and its main window. Window is unknown when Running is false.
type HostState struct {
	Running bool
	Window  WindowState
}

// MediaSnapshot contains the host's media session. Available is false when no
// host session exists. PositionMS and DurationMS are in milliseconds, and a
// zero duration can indicate an unknown length. The caller owns Artwork.
type MediaSnapshot struct {
	Available              bool
	Title, Artist, Album   string
	Artwork                []byte
	Playing                bool
	DurationMS, PositionMS int64
}

// PlaybackAction is a transport command for the host media session.
type PlaybackAction string

const (
	Play     PlaybackAction = "play"
	Pause    PlaybackAction = "pause"
	Previous PlaybackAction = "previous"
	Next     PlaybackAction = "next"
)

// HostSettings contains the host preferences used by the mini player.
type HostSettings struct {
	Appearance   string // system, light or dark
	Language     string // en or zh-CN
	Smoke        bool
	Fog          bool
	PlayerStyle  string // empty, vinyl or cassette
	ReduceMotion bool
}

// PluginSettings contains the plugin preferences used by the companion.
type PluginSettings struct {
	DisplayPolicy string // auto or pinned
	SmokeOverride string // follow, on or off
	GuardMode     bool
	Hotkey        string
}

// SettingsSnapshot contains the host and plugin preference snapshots.
type SettingsSnapshot struct {
	Host   HostSettings
	Plugin PluginSettings
}

// HostBridge reads and controls only MyGo Music's process, main window, media
// session and audio stream. The companion may read snapshots concurrently with
// commands. Adapters return errors when an operation fails.
type HostBridge interface {
	ObserveHost() (HostState, error)
	WakeMainWindow() error
	Media() (MediaSnapshot, error)
	ControlPlayback(PlaybackAction) error
	// SeekMS targets a nonnegative position in milliseconds.
	SeekMS(positionMS int64) error
	// Volume and SetVolume use a finite level in [0, 1] on the host's audio stream.
	Volume() (float64, error)
	SetVolume(level float64) error
	// PeakLevel returns a level in [0, 1], or available=false if metering is unsupported or absent.
	PeakLevel() (level float64, available bool, err error)
	// SetLoginStartup replaces the current user's companion startup command.
	// argv contains the executable followed by its arguments. Empty argv removes the entry.
	SetLoginStartup(argv []string) error
	Settings() (SettingsSnapshot, error)
	// SetHotkey replaces the companion's global binding, or removes it when combo is empty.
	// On error the old binding is kept. onPress is required for a nonempty combo,
	// and may be invoked off the UI thread. An in-flight press may finish after removal.
	SetHotkey(combo string, onPress func()) error
}
