package bridge_test

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func TestHostBridgeSnapshots(t *testing.T) {
	var fake bridge.Fake
	var host bridge.HostBridge = &fake

	state, err := host.ObserveHost()
	if err != nil || state != (bridge.HostState{}) {
		t.Fatalf("absent host = %+v, %v", state, err)
	}
	if err := host.WakeMainWindow(); !errors.Is(err, bridge.ErrUnavailable) {
		t.Fatalf("wake absent host: %v", err)
	}
	for _, window := range []bridge.WindowState{
		bridge.WindowUnknown, bridge.WindowVisible, bridge.WindowHidden, bridge.WindowMinimized,
	} {
		want := bridge.HostState{Running: true, Window: window}
		fake.SetSnapshot(bridge.FakeSnapshot{Host: want})
		if got, err := host.ObserveHost(); err != nil || got != want {
			t.Fatalf("host = %+v, %v; want %+v", got, err, want)
		}
	}
	if err := host.WakeMainWindow(); err != nil || fake.Effects().WakeCount != 1 {
		t.Fatalf("wake requests = %d, %v", fake.Effects().WakeCount, err)
	}

	settings := bridge.SettingsSnapshot{
		Host: bridge.HostSettings{
			Appearance: "dark", Language: "zh-CN", PlayerStyle: "cassette",
			Smoke: true, Fog: true, ReduceMotion: true,
		},
		Plugin: bridge.PluginSettings{
			DisplayPolicy: "pinned", SmokeOverride: "follow", GuardMode: true, Hotkey: "Ctrl+M",
		},
	}
	media := bridge.MediaSnapshot{
		Available: true, Title: "Title", Artist: "Artist", Album: "Album",
		Artwork: []byte{1, 2, 3}, Playing: true, DurationMS: 120000, PositionMS: 1234,
	}
	fake.SetSnapshot(bridge.FakeSnapshot{Media: media, Settings: settings})
	gotMedia, err := host.Media()
	if err != nil || !reflect.DeepEqual(gotMedia, media) {
		t.Fatalf("media = %+v, %v; want %+v", gotMedia, err, media)
	}
	t.Logf("media=%+v", gotMedia)
	media.Artwork[0] = 9
	gotMedia.Artwork[1] = 9
	gotMedia, err = host.Media()
	if err != nil || !slices.Equal(gotMedia.Artwork, []byte{1, 2, 3}) {
		t.Fatalf("artwork after input and output mutation = %v, %v", gotMedia.Artwork, err)
	}
	gotSettings, err := host.Settings()
	if err != nil || gotSettings != settings {
		t.Fatalf("settings = %+v, %v; want %+v", gotSettings, err, settings)
	}
	t.Logf("host states: absent, unknown, visible, hidden, minimized; wakes=%d; settings=%+v",
		fake.Effects().WakeCount, gotSettings)
}

func TestHostBridgePlaybackAndAudio(t *testing.T) {
	var fake bridge.Fake
	var host bridge.HostBridge = &fake
	if media, err := host.Media(); err != nil || media.Available {
		t.Fatalf("absent media = %+v, %v", media, err)
	}
	for _, err := range []error{
		host.ControlPlayback(bridge.Play), host.SeekMS(1234), host.SetVolume(0.5),
	} {
		if !errors.Is(err, bridge.ErrUnavailable) {
			t.Fatalf("command without a host session or stream: %v", err)
		}
	}
	if _, err := host.Volume(); !errors.Is(err, bridge.ErrUnavailable) {
		t.Fatalf("volume without a host stream: %v", err)
	}
	if level, available, err := host.PeakLevel(); err != nil || available {
		t.Fatalf("absent peak = %v, %v, %v", level, available, err)
	}
	fake.SetSnapshot(bridge.FakeSnapshot{
		Media: bridge.MediaSnapshot{Available: true}, Volume: 0.5,
		VolumeAvailable: true, PeakAvailable: true,
	})

	actions := []bridge.PlaybackAction{bridge.Play, bridge.Pause, bridge.Previous, bridge.Next}
	for _, action := range actions {
		if err := host.ControlPlayback(action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if err := host.SeekMS(1234); err != nil {
		t.Fatal(err)
	}
	if host.SeekMS(-1) == nil || host.ControlPlayback("toggle") == nil {
		t.Fatal("invalid playback request succeeded")
	}
	effects := fake.Effects()
	if !slices.Equal(effects.Playback, actions) || !slices.Equal(effects.SeeksMS, []int64{1234}) {
		t.Fatalf("playback effects = %+v", effects)
	}

	if err := host.SetVolume(0.53); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []float64{-0.1, 1.1, math.NaN()} {
		if host.SetVolume(invalid) == nil {
			t.Fatalf("invalid volume %v succeeded", invalid)
		}
	}
	volume, err := host.Volume()
	if err != nil || volume != 0.53 {
		t.Fatalf("volume = %v, %v; want 0.53", volume, err)
	}
	level, available, err := host.PeakLevel()
	if err != nil || !available || level != 0 {
		t.Fatalf("silent peak = %v, %v, %v", level, available, err)
	}
	t.Logf("playback=%v seekMS=%v volume=%v silent peak=%v (available=%t); absent peak available=false",
		effects.Playback, effects.SeeksMS, volume, level, available)
}

func TestHostBridgeRegistrations(t *testing.T) {
	var fake bridge.Fake
	var host bridge.HostBridge = &fake
	argv := []string{"mini-player", "--guard"}
	for range 2 {
		if err := host.SetLoginStartup(argv); err != nil {
			t.Fatal(err)
		}
	}
	got := fake.Effects().LoginStartup
	if !slices.Equal(got, argv) {
		t.Fatalf("startup argv = %q; want %q", got, argv)
	}
	t.Logf("startup argv=%q", got)
	argv[0] = "changed input"
	got[1] = "changed output"
	if got := fake.Effects().LoginStartup; !slices.Equal(got, []string{"mini-player", "--guard"}) {
		t.Fatalf("startup argv after input and output mutation = %q", got)
	}
	for range 2 {
		if err := host.SetLoginStartup(nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.Effects().LoginStartup; len(got) != 0 {
		t.Fatalf("startup entry still present: %q", got)
	}

	var fired string
	if err := host.SetHotkey("Ctrl+M", func() { fired = fake.Effects().Hotkey }); err != nil {
		t.Fatal(err)
	}
	if err := host.SetHotkey("Ctrl+N", nil); err == nil {
		t.Fatal("registered a hotkey without a callback")
	}
	if !fake.PressHotkey() || fired != "Ctrl+M" {
		t.Fatalf("expected old hotkey after failed replacement, fired %q", fired)
	}
	if err := host.SetHotkey("", nil); err != nil {
		t.Fatal(err)
	}
	if fake.PressHotkey() || fake.Effects().Hotkey != "" {
		t.Fatal("hotkey still active after removal")
	}
	t.Logf("startup argv cleared; hotkey %s fired, then was removed", fired)
}
