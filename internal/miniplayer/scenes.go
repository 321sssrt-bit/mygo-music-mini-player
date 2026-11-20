package miniplayer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"sync"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/egoist/mygo/ui"
)

var (
	sampleArtworkOnce sync.Once
	sampleArtworkPNG  []byte
)

// WriteSceneFrame renders a named UI acceptance scene to PNG at 1 pixel per DIP.
// Passing an empty scene or "default" renders the default view via WriteFrame.
func WriteSceneFrame(w io.Writer, scene string, width, height int) error {
	if scene == "" || scene == "default" {
		return WriteFrame(w, width, height)
	}
	state, opts, err := sceneSpec(scene)
	if err != nil {
		return err
	}
	player := New(state, Actions{})
	tester := ui.NewTesterWith(player.View, width, height, opts)
	return png.Encode(w, tester.Image())
}

func sceneSpec(name string) (State, ui.TesterOptions, error) {
	sampleTrack := bridge.MediaSnapshot{
		Available:  true,
		Title:      "春日影 (Haruhikage) - MyGO!!!!! Acoustic Live",
		Artist:     "MyGO!!!!!",
		Album:      "迷星叫",
		Artwork:    sampleCoverArtwork(),
		Playing:    true,
		DurationMS: 254000,
		PositionMS: 108000,
	}
	pausedTrack := sampleTrack
	pausedTrack.Playing = false

	darkSettings := bridge.SettingsSnapshot{
		Host: bridge.HostSettings{
			Appearance: "dark",
			Language:   "zh-CN",
		},
	}
	lightSettings := bridge.SettingsSnapshot{
		Host: bridge.HostSettings{
			Appearance: "light",
			Language:   "zh-CN",
		},
	}

	switch name {
	case "resting-playing-dark":
		return State{Expanded: false, Media: sampleTrack, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "resting-playing-light":
		return State{Expanded: false, Media: sampleTrack, Settings: lightSettings}, ui.TesterOptions{Dark: false}, nil
	case "resting-paused-dark":
		return State{Expanded: false, Media: pausedTrack, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "resting-idle-dark":
		return State{Expanded: false, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "resting-idle-light":
		return State{Expanded: false, Settings: lightSettings}, ui.TesterOptions{Dark: false}, nil
	case "expanded-playing-dark":
		return State{Expanded: true, Media: sampleTrack, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "expanded-playing-light":
		return State{Expanded: true, Media: sampleTrack, Settings: lightSettings}, ui.TesterOptions{Dark: false}, nil
	case "expanded-paused-dark":
		return State{Expanded: true, Media: pausedTrack, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "expanded-idle-dark":
		return State{Expanded: true, Settings: darkSettings}, ui.TesterOptions{Dark: true}, nil
	case "expanded-volume-dark":
		return State{
			Expanded:      true,
			Media:         sampleTrack,
			Settings:      darkSettings,
			Volume:        0.72,
			VolumeVisible: true,
		}, ui.TesterOptions{Dark: true}, nil
	case "expanded-volume-light":
		return State{
			Expanded:      true,
			Media:         sampleTrack,
			Settings:      lightSettings,
			Volume:        0.72,
			VolumeVisible: true,
		}, ui.TesterOptions{Dark: false}, nil
	default:
		return State{}, ui.TesterOptions{}, fmt.Errorf("unknown scene %q", name)
	}
}

func sampleCoverArtwork() []byte {
	sampleArtworkOnce.Do(func() {
		const size = 160
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			ny := float64(y) / float64(size-1)
			for x := range size {
				nx := float64(x) / float64(size-1)

				// Deep twilight-indigo to warm sunset-coral album art gradient.
				r := 28 + 168*math.Pow(ny, 1.35) + 42*(1-nx)*ny
				g := 44 + 72*ny + 54*math.Sin((nx+ny)*math.Pi*0.85)
				b := 124 + 98*(1-ny) + 28*nx

				// Luminous celestial disc in upper-right quadrant.
				dx := nx - 0.66
				dy := ny - 0.34
				dist := math.Hypot(dx, dy)
				if dist < 0.24 {
					t := 1 - dist/0.24
					glow := math.Pow(t, 0.7)
					r = r*(1-glow) + 255*glow
					g = g*(1-glow) + 224*glow
					b = b*(1-glow) + 178*glow
				} else if dist < 0.50 {
					halo := math.Exp(-math.Pow((dist-0.24)*5.5, 2)) * 0.38
					r += 255 * halo
					g += 170 * halo
					b += 140 * halo
				}

				// Soft diagonal reflection wave across lower half.
				wave := math.Exp(-math.Pow((ny-0.72-0.06*math.Sin(nx*math.Pi*2.2))*9.0, 2)) * 0.28
				r += 110 * wave
				g += 140 * wave
				b += 255 * wave

				img.SetNRGBA(x, y, color.NRGBA{
					R: clampByte(r),
					G: clampByte(g),
					B: clampByte(b),
					A: 255,
				})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			panic(err)
		}
		sampleArtworkPNG = buf.Bytes()
	})
	return sampleArtworkPNG
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
