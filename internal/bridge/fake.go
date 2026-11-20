package bridge

import (
	"errors"
	"slices"
	"sync"
)

// FakeSnapshot supplies readings from the host and desktop.
type FakeSnapshot struct {
	Host            HostState
	Media           MediaSnapshot
	Settings        SettingsSnapshot
	Volume          float64
	VolumeAvailable bool
	Peak            float64
	PeakAvailable   bool
}

// FakeEffects records the observable requests and registrations made against Fake.
type FakeEffects struct {
	WakeCount    int
	Playback     []PlaybackAction
	SeeksMS      []int64
	LoginStartup []string
	Hotkey       string
}

// Fake is a concurrency-safe in-memory HostBridge. Its zero value represents
// an absent host. Playback and wake requests are recorded so tests can provide
// the resulting snapshots independently.
type Fake struct {
	mu       sync.Mutex
	snapshot FakeSnapshot
	effects  FakeEffects
	onPress  func()
}

var _ HostBridge = (*Fake)(nil)

// SetSnapshot replaces all readings. The fake keeps its own copy of the artwork.
func (f *Fake) SetSnapshot(snapshot FakeSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot.Media.Artwork = slices.Clone(snapshot.Media.Artwork)
	f.snapshot = snapshot
}

// Effects returns a copy of the recorded requests and registrations.
func (f *Fake) Effects() FakeEffects {
	f.mu.Lock()
	defer f.mu.Unlock()
	effects := f.effects
	effects.Playback = slices.Clone(effects.Playback)
	effects.SeeksMS = slices.Clone(effects.SeeksMS)
	effects.LoginStartup = slices.Clone(effects.LoginStartup)
	return effects
}

// ObserveHost returns the configured host state.
func (f *Fake) ObserveHost() (HostState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot.Host, nil
}

// WakeMainWindow records a request to activate the running host.
func (f *Fake) WakeMainWindow() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.snapshot.Host.Running {
		return ErrUnavailable
	}
	f.effects.WakeCount++
	return nil
}

// Media returns the configured media snapshot with caller-owned artwork.
func (f *Fake) Media() (MediaSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	media := f.snapshot.Media
	media.Artwork = slices.Clone(media.Artwork)
	return media, nil
}

// ControlPlayback records a transport command for an available host session.
func (f *Fake) ControlPlayback(action PlaybackAction) error {
	switch action {
	case Play, Pause, Previous, Next:
	default:
		return errors.New("unknown playback action")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.snapshot.Media.Available {
		return ErrUnavailable
	}
	f.effects.Playback = append(f.effects.Playback, action)
	return nil
}

// SeekMS records a position in milliseconds for an available host session.
func (f *Fake) SeekMS(positionMS int64) error {
	if positionMS < 0 {
		return errors.New("seek position must be nonnegative")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.snapshot.Media.Available {
		return ErrUnavailable
	}
	f.effects.SeeksMS = append(f.effects.SeeksMS, positionMS)
	return nil
}

// Volume returns the configured host stream volume.
func (f *Fake) Volume() (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.snapshot.VolumeAvailable {
		return 0, ErrUnavailable
	}
	return f.snapshot.Volume, nil
}

// SetVolume updates the host stream volume when the stream is available.
func (f *Fake) SetVolume(level float64) error {
	if !(0 <= level && level <= 1) {
		return errors.New("volume must be in [0, 1]")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.snapshot.VolumeAvailable {
		return ErrUnavailable
	}
	f.snapshot.Volume = level
	return nil
}

// PeakLevel returns the configured reading and whether metering is available.
func (f *Fake) PeakLevel() (float64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot.Peak, f.snapshot.PeakAvailable, nil
}

// SetLoginStartup stores the companion startup command in memory.
func (f *Fake) SetLoginStartup(argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.effects.LoginStartup = slices.Clone(argv)
	return nil
}

// Settings returns the configured host and plugin preferences.
func (f *Fake) Settings() (SettingsSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot.Settings, nil
}

// SetHotkey stores or clears the in-memory binding.
func (f *Fake) SetHotkey(combo string, onPress func()) error {
	if combo != "" && onPress == nil {
		return errors.New("hotkey callback is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if combo == "" {
		onPress = nil
	}
	f.effects.Hotkey = combo
	f.onPress = onPress
	return nil
}

// PressHotkey simulates a global hotkey press and reports whether a binding fired.
func (f *Fake) PressHotkey() bool {
	f.mu.Lock()
	onPress := f.onPress
	f.mu.Unlock()
	if onPress == nil {
		return false
	}
	onPress()
	return true
}
