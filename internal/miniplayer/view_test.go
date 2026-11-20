package miniplayer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/egoist/mygo/ui"
)

func solidArtworkPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imagesEqual(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

func TestWriteFrameDimensionsAndScenes(t *testing.T) {
	for _, size := range []image.Point{{X: RestingSize, Y: RestingSize}, {X: ExpandedWidth, Y: ExpandedHeight}} {
		t.Run(fmt.Sprintf("default-%dx%d", size.X, size.Y), func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteFrame(&output, size.X, size.Y); err != nil {
				t.Fatal(err)
			}
			frame, err := png.Decode(&output)
			if err != nil {
				t.Fatal(err)
			}
			if got := frame.Bounds().Size(); got != size {
				t.Fatalf("frame size = %v; want %v", got, size)
			}
		})
	}

	scenes := []struct {
		name string
		w, h int
	}{
		{"resting-playing-dark", RestingSize, RestingSize},
		{"resting-playing-light", RestingSize, RestingSize},
		{"resting-paused-dark", RestingSize, RestingSize},
		{"resting-idle-dark", RestingSize, RestingSize},
		{"resting-idle-light", RestingSize, RestingSize},
		{"expanded-playing-dark", ExpandedWidth, ExpandedHeight},
		{"expanded-playing-light", ExpandedWidth, ExpandedHeight},
		{"expanded-paused-dark", ExpandedWidth, ExpandedHeight},
		{"expanded-idle-dark", ExpandedWidth, ExpandedHeight},
		{"expanded-volume-dark", ExpandedWidth, ExpandedHeight},
		{"expanded-volume-light", ExpandedWidth, ExpandedHeight},
	}
	for _, sc := range scenes {
		t.Run(sc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteSceneFrame(&out, sc.name, sc.w, sc.h); err != nil {
				t.Fatalf("WriteSceneFrame(%q): %v", sc.name, err)
			}
			cfg, err := png.DecodeConfig(&out)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width != sc.w || cfg.Height != sc.h {
				t.Fatalf("dimensions = %dx%d; want %dx%d", cfg.Width, cfg.Height, sc.w, sc.h)
			}
		})
	}

	var bad bytes.Buffer
	if err := WriteSceneFrame(&bad, "nonexistent-scene", RestingSize, RestingSize); err == nil {
		t.Fatal("expected error for unknown scene")
	}
}

func TestRestingPauseBreathingScaleAndIdle(t *testing.T) {
	art := solidArtworkPNG(t, color.NRGBA{R: 220, G: 60, B: 90, A: 255})
	state := State{
		Expanded: false,
		Media: bridge.MediaSnapshot{
			Available:  true,
			Title:      "Track",
			Artwork:    art,
			Playing:    true,
			DurationMS: 100000,
			PositionMS: 50000,
		},
		Settings: bridge.SettingsSnapshot{
			Host: bridge.HostSettings{Appearance: "dark", Language: "zh-CN"},
		},
	}
	p := New(state, Actions{})
	tester := ui.NewTesterWith(p.View, RestingSize, RestingSize, ui.TesterOptions{Dark: true})
	playingImg := tester.Image()

	// Paused state shrinks the cover to 0.88, visibly changing edge pixels of the tile.
	state.Media.Playing = false
	p.SetState(state)
	tester.Frame()
	tester.Advance(500 * time.Millisecond)
	pausedImg := tester.Image()
	if imagesEqual(playingImg, pausedImg) {
		t.Fatal("expected resting paused state to visually shrink cover compared to playing state")
	}

	// Idle state shows the musical note placeholder accessibility label.
	state.Media = bridge.MediaSnapshot{}
	p.SetState(state)
	tester.Frame()
	if _, ok := tester.Access("无曲目占位"); !ok {
		t.Fatalf("expected idle placeholder access label; got %+v", tester.AccessAll())
	}
}

func TestExpandedInteractionsSeekAndBilingualLabels(t *testing.T) {
	var (
		gotActions []bridge.PlaybackAction
		gotSeekMS  int64 = -1
		wokeMain   bool
		hid        bool
	)
	state := State{
		Expanded: true,
		Media: bridge.MediaSnapshot{
			Available:  true,
			Title:      "A Very Long Song Title That Must Be Ellipsized On A Single Line",
			Artist:     "MyGO!!!!!",
			Artwork:    sampleCoverArtwork(),
			Playing:    true,
			DurationMS: 200000,
			PositionMS: 40000,
		},
		Settings: bridge.SettingsSnapshot{
			Host: bridge.HostSettings{Appearance: "dark", Language: "zh-CN"},
		},
	}
	p := New(state, Actions{
		OnPlayback: func(a bridge.PlaybackAction) { gotActions = append(gotActions, a) },
		OnSeekMS:   func(ms int64) { gotSeekMS = ms },
		OnWakeMain: func() { wokeMain = true },
		OnHide:     func() { hid = true },
	})
	tester := ui.NewTesterWith(p.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})

	for _, name := range []string{"回主窗", "隐藏", "上一首", "暂停", "下一首", "播放进度"} {
		if _, ok := tester.Access(name); !ok {
			t.Fatalf("missing zh-CN accessible element %q", name)
		}
	}

	if err := tester.Click("上一首"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("回主窗"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("隐藏"); err != nil {
		t.Fatal(err)
	}

	if !wokeMain || !hid {
		t.Fatalf("window actions: wokeMain=%v hid=%v; want both true", wokeMain, hid)
	}
	wantActions := []bridge.PlaybackAction{bridge.Previous, bridge.Pause, bridge.Next}
	if len(gotActions) != len(wantActions) {
		t.Fatalf("playback actions = %v; want %v", gotActions, wantActions)
	}
	for i := range wantActions {
		if gotActions[i] != wantActions[i] {
			t.Fatalf("playback action[%d] = %q; want %q", i, gotActions[i], wantActions[i])
		}
	}

	// Hovering and dragging the progress bar triggers OnSeekMS near the release point.
	barInfo, ok := tester.Access("播放进度")
	if !ok {
		t.Fatal("progress bar not found")
	}
	beforeHover := tester.Image()
	cy := barInfo.Bounds.Y + barInfo.Bounds.H/2
	tester.Move(barInfo.Bounds.X+barInfo.Bounds.W*0.5, cy)
	tester.Advance(200 * time.Millisecond)
	afterHover := tester.Image()
	if imagesEqual(beforeHover, afterHover) {
		t.Fatal("expected progress bar hover to thicken track and reveal thumb")
	}

	targetX := barInfo.Bounds.X + barInfo.Bounds.W*0.75
	tester.Press(barInfo.Bounds.X+barInfo.Bounds.W*0.25, cy)
	tester.Move(targetX, cy)
	tester.Release(targetX, cy)
	if gotSeekMS < 140000 || gotSeekMS > 160000 {
		t.Fatalf("seekMS = %d; want near 150000", gotSeekMS)
	}

	// Switch to English and paused state; verify English labels and Play action.
	state.Settings.Host.Language = "en"
	state.Media.Playing = false
	p.SetState(state)
	tester.Frame()
	tester.Advance(400 * time.Millisecond)
	for _, name := range []string{"Main Window", "Hide", "Previous", "Play", "Next", "Playback progress"} {
		if _, ok := tester.Access(name); !ok {
			t.Fatalf("missing en accessible element %q", name)
		}
	}
	if err := tester.Click("Play"); err != nil {
		t.Fatal(err)
	}
	if gotActions[len(gotActions)-1] != bridge.Play {
		t.Fatalf("last playback action = %q; want %q", gotActions[len(gotActions)-1], bridge.Play)
	}
}

func TestCoverTiltSheenAndReduceMotion(t *testing.T) {
	baseState := State{
		Expanded: true,
		Media: bridge.MediaSnapshot{
			Available:  true,
			Title:      "春日影",
			Artist:     "MyGO!!!!!",
			Artwork:    sampleCoverArtwork(),
			Playing:    true,
			DurationMS: 200000,
			PositionMS: 80000,
		},
		Settings: bridge.SettingsSnapshot{
			Host: bridge.HostSettings{Appearance: "dark", Language: "zh-CN"},
		},
	}

	// Motion enabled: moving pointer over cover triggers 3D tilt + sheen shader.
	pMotion := New(baseState, Actions{})
	tMotion := ui.NewTesterWith(pMotion.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})
	artInfo, ok := tMotion.Access("专辑封面")
	if !ok {
		t.Fatal("artwork element not found")
	}
	restImg := tMotion.Image()
	tMotion.Move(artInfo.Bounds.X+artInfo.Bounds.W*0.85, artInfo.Bounds.Y+artInfo.Bounds.H*0.20)
	tMotion.Advance(300 * time.Millisecond)
	tiltedImg := tMotion.Image()
	if imagesEqual(restImg, tiltedImg) {
		t.Fatal("expected pointer hover over cover to render 3D tilt and specular sheen")
	}

	// Reduce Motion enabled: hovering cover keeps tilt & sheen disabled.
	rmState := baseState
	rmState.Settings.Host.ReduceMotion = true
	pRM := New(rmState, Actions{})
	tRM := ui.NewTesterWith(pRM.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{
		Dark:        true,
		Preferences: ui.Preferences{ReduceMotion: true},
	})
	artInfoRM, ok := tRM.Access("专辑封面")
	if !ok {
		t.Fatal("artwork element not found under reduce motion")
	}
	rmRest := tRM.Image()
	tRM.Move(artInfoRM.Bounds.X+artInfoRM.Bounds.W*0.85, artInfoRM.Bounds.Y+artInfoRM.Bounds.H*0.20)
	tRM.Advance(300 * time.Millisecond)
	rmHover := tRM.Image()
	if !imagesEqual(rmRest, rmHover) {
		t.Fatal("expected Reduce Motion to disable 3D cover tilt and sheen movement")
	}
}

func TestVolumeBubbleAppearanceAndAutoFade(t *testing.T) {
	state := State{
		Expanded: true,
		Media: bridge.MediaSnapshot{
			Available:  true,
			Title:      "春日影",
			Artist:     "MyGO!!!!!",
			Artwork:    sampleCoverArtwork(),
			Playing:    true,
			DurationMS: 200000,
			PositionMS: 80000,
		},
		Settings: bridge.SettingsSnapshot{
			Host: bridge.HostSettings{Appearance: "dark", Language: "zh-CN"},
		},
	}
	p := New(state, Actions{})
	tester := ui.NewTesterWith(p.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})

	if tester.HasText("72%") {
		t.Fatal("volume bubble should be hidden before volume trigger")
	}

	p.ShowVolume(0.72, tester.Now())
	tester.Frame()
	if !tester.HasText("72%") {
		t.Fatalf("expected volume bubble 72%%; texts=%v", tester.Texts())
	}
	if _, ok := tester.Access("音量 72%"); !ok {
		t.Fatal("expected accessible volume status label")
	}

	// Still visible during hold window.
	tester.Advance(500 * time.Millisecond)
	if !tester.HasText("72%") {
		t.Fatal("volume bubble disappeared too early")
	}

	// Fades out after hold + fade duration.
	tester.Advance(1200 * time.Millisecond)
	if tester.HasText("72%") {
		t.Fatal("expected volume bubble to fade out after hold and fade durations")
	}
}

func TestAppearanceSystemAndDominantColorExtraction(t *testing.T) {
	warmRed := solidArtworkPNG(t, color.NRGBA{R: 224, G: 58, B: 64, A: 255})
	coolBlue := solidArtworkPNG(t, color.NRGBA{R: 42, G: 112, B: 232, A: 255})

	pWarm := New(State{
		Expanded: true,
		Media:    bridge.MediaSnapshot{Available: true, Title: "Warm", Artwork: warmRed, Playing: true},
		Settings: bridge.SettingsSnapshot{Host: bridge.HostSettings{Appearance: "system"}},
	}, Actions{})
	tWarm := ui.NewTesterWith(pWarm.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})
	darkWarmImg := tWarm.Image()

	pCool := New(State{
		Expanded: true,
		Media:    bridge.MediaSnapshot{Available: true, Title: "Warm", Artwork: coolBlue, Playing: true},
		Settings: bridge.SettingsSnapshot{Host: bridge.HostSettings{Appearance: "system"}},
	}, Actions{})
	tCool := ui.NewTesterWith(pCool.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})
	darkCoolImg := tCool.Image()

	// Sample a card background pixel outside the cover to verify dominant-color backdrop tinting.
	rW, _, bW, _ := darkWarmImg.At(16, 16).RGBA()
	rC, _, bC, _ := darkCoolImg.At(16, 16).RGBA()
	if rW <= bW || bC <= rC {
		t.Fatalf("expected dominant cover color to tint card backdrop: warm=(%d,%d) cool=(%d,%d)", rW>>8, bW>>8, rC>>8, bC>>8)
	}

	// System appearance responds when desktop switches from dark to light.
	tWarm.SetDark(false)
	tWarm.Frame()
	lightWarmImg := tWarm.Image()
	if imagesEqual(darkWarmImg, lightWarmImg) {
		t.Fatal("expected system appearance to update when desktop switches to light mode")
	}
}

func TestExpandSpringTransition(t *testing.T) {
	state := State{
		Expanded: false,
		Media: bridge.MediaSnapshot{
			Available:  true,
			Title:      "春日影",
			Artist:     "MyGO!!!!!",
			Artwork:    sampleCoverArtwork(),
			Playing:    true,
			DurationMS: 200000,
			PositionMS: 80000,
		},
		Settings: bridge.SettingsSnapshot{
			Host: bridge.HostSettings{Appearance: "dark", Language: "zh-CN"},
		},
	}
	p := New(state, Actions{})
	tester := ui.NewTesterWith(p.View, ExpandedWidth, ExpandedHeight, ui.TesterOptions{Dark: true})
	if tester.HasText("春日影") {
		t.Fatal("resting state should not show expanded track title")
	}

	state.Expanded = true
	p.SetState(state)
	tester.Frame()
	tester.Advance(ExpandDuration + 100*time.Millisecond)
	if !tester.HasText("春日影") {
		t.Fatal("expanded state should show track title after spring transition")
	}
}
