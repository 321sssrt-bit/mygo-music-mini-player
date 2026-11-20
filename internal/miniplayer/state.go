package miniplayer

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/egoist/mygo/ui"
)

const (
	// RestingSize is the width and height of the resting canvas in DIPs.
	RestingSize = 64
	// ExpandedWidth is the width of the expanded mini player card in DIPs.
	ExpandedWidth = 176
	// ExpandedHeight is the height of the expanded mini player card in DIPs.
	ExpandedHeight = 204
	// ExpandDuration is the spring transition duration between resting and expanded states.
	ExpandDuration = 520 * time.Millisecond

	pausedCoverScale    = 0.88
	maxCoverTiltDegrees = 9.0
	volumeHoldDuration  = 1100 * time.Millisecond
	volumeFadeDuration  = 260 * time.Millisecond
)

// State holds the visual snapshot rendered by the mini player.
type State struct {
	Expanded      bool
	Media         bridge.MediaSnapshot
	Settings      bridge.SettingsSnapshot
	Volume        float64
	VolumeVisible bool
}

// Actions holds optional callbacks invoked by mini player interactions.
type Actions struct {
	OnPlayback func(bridge.PlaybackAction)
	OnSeekMS   func(positionMS int64)
	OnWakeMain func()
	OnHide     func()
}

// Player renders the mini player's resting and expanded views and caches decoded artwork.
type Player struct {
	state   State
	actions Actions

	cachedArtBytes []byte
	artBitmap      *ui.Bitmap
	artDominant    ui.Color
	hasArtDominant bool

	scrubbing   bool
	scrubRatio  float64
	volumeTimed bool
	volumeShown time.Time
	timedVolume float64
}

// New creates a mini player view controller from an initial state and action hooks.
func New(state State, actions Actions) *Player {
	p := &Player{actions: actions}
	p.SetState(state)
	return p
}

// SetState replaces the rendered state snapshot and refreshes cached artwork only when bytes change.
func (p *Player) SetState(state State) {
	p.state = state
	if !state.Media.Available || state.Media.DurationMS <= 0 {
		p.scrubbing = false
	}
	if state.VolumeVisible {
		p.timedVolume = clamp01(state.Volume)
	}
	p.syncArtwork(state.Media.Artwork)
}

// ShowVolume displays the top volume percentage bubble for the given [0, 1] level
// and schedules its fade-out relative to now.
func (p *Player) ShowVolume(level float64, now time.Time) {
	p.timedVolume = clamp01(level)
	p.volumeTimed = true
	p.volumeShown = now
}

func (p *Player) displayVolume() float64 {
	if p.state.VolumeVisible {
		return clamp01(p.state.Volume)
	}
	return p.timedVolume
}

func (p *Player) syncArtwork(raw []byte) {
	if len(raw) == 0 {
		p.cachedArtBytes = nil
		p.artBitmap = nil
		p.hasArtDominant = false
		return
	}
	if bytes.Equal(p.cachedArtBytes, raw) {
		return
	}
	p.cachedArtBytes = append(p.cachedArtBytes[:0], raw...)
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		p.artBitmap = nil
		p.hasArtDominant = false
		return
	}
	p.artBitmap = ui.NewBitmap(img)
	p.artDominant, p.hasArtDominant = extractDominantColor(img)
}

func (p *Player) volumeBubbleTarget(c *ui.Context) float32 {
	if p.state.VolumeVisible {
		return 1
	}
	if !p.volumeTimed {
		return 0
	}
	elapsed := c.Now().Sub(p.volumeShown)
	if elapsed < volumeHoldDuration {
		c.After(volumeHoldDuration - elapsed)
		return 1
	}
	return 0
}

func (p *Player) progressRatio() float64 {
	if p.scrubbing {
		return clamp01(p.scrubRatio)
	}
	if !p.state.Media.Available || p.state.Media.DurationMS <= 0 {
		return 0
	}
	return clamp01(float64(p.state.Media.PositionMS) / float64(p.state.Media.DurationMS))
}

func (p *Player) reduceMotion(c *ui.Context) bool {
	return p.state.Settings.Host.ReduceMotion || c.Preferences().ReduceMotion
}

func resolveDark(c *ui.Context, appearance string) bool {
	switch strings.ToLower(strings.TrimSpace(appearance)) {
	case "dark":
		return true
	case "light":
		return false
	default:
		return c.Theme().Dark
	}
}

type copyStrings struct {
	regionName   string
	idleTitle    string
	idleSubtitle string
	wakeMain     string
	hide         string
	previous     string
	play         string
	pause        string
	next         string
	progress     string
	artwork      string
	idleArtwork  string
	volumeFormat string
}

func localizedStrings(lang, locale string) copyStrings {
	tag := strings.ToLower(strings.TrimSpace(lang))
	if tag == "" {
		tag = strings.ToLower(strings.TrimSpace(locale))
	}
	if strings.HasPrefix(tag, "en") {
		return copyStrings{
			regionName:   "Mini Player",
			idleTitle:    "Not Playing",
			idleSubtitle: "MyGo Music",
			wakeMain:     "Main Window",
			hide:         "Hide",
			previous:     "Previous",
			play:         "Play",
			pause:        "Pause",
			next:         "Next",
			progress:     "Playback progress",
			artwork:      "Album artwork",
			idleArtwork:  "No track playing",
			volumeFormat: "Volume %d%%",
		}
	}
	return copyStrings{
		regionName:   "迷你悬浮窗",
		idleTitle:    "未在播放",
		idleSubtitle: "MyGo Music",
		wakeMain:     "回主窗",
		hide:         "隐藏",
		previous:     "上一首",
		play:         "播放",
		pause:        "暂停",
		next:         "下一首",
		progress:     "播放进度",
		artwork:      "专辑封面",
		idleArtwork:  "无曲目占位",
		volumeFormat: "音量 %d%%",
	}
}

type surfacePalette struct {
	dark          bool
	cardTop       ui.Color
	cardBottom    ui.Color
	cardGlow      ui.Color
	cardBorder    ui.Color
	restingTile   ui.Color
	restingBorder ui.Color
	idleTileTop   ui.Color
	idleTileBot   ui.Color
	idleNote      ui.Color
	title         ui.Color
	artist        ui.Color
	icon          ui.Color
	iconHoverBg   ui.Color
	iconPressBg   ui.Color
	toolIcon      ui.Color
	toolBg        ui.Color
	toolHoverBg   ui.Color
	track         ui.Color
	trackFill     ui.Color
	thumb         ui.Color
	thumbShadow   ui.Color
	coverShadow   ui.Color
	sheenTint     ui.Color
	bubbleBg      ui.Color
	bubbleBorder  ui.Color
	bubbleText    ui.Color
	bubbleShadow  ui.Color
}

func buildPalette(dark, highContrast bool, dominant ui.Color, hasDominant bool) surfacePalette {
	if dark {
		cardTop := ui.RGB(28, 28, 34)
		cardBottom := ui.RGB(17, 17, 21)
		cardGlow := ui.RGBA(255, 255, 255, 0.04)
		restingTile := ui.RGB(26, 26, 32)
		trackFill := ui.RGB(246, 246, 250)
		sheenTint := ui.RGBA(255, 255, 255, 0.92)
		if hasDominant {
			tintedTop := adjustLuminance(dominant, 0.24, 0.36)
			tintedBot := adjustLuminance(dominant, 0.10, 0.18)
			cardTop = cardTop.MixOklab(tintedTop, 0.56)
			cardBottom = cardBottom.MixOklab(tintedBot, 0.42)
			cardGlow = adjustLuminance(dominant, 0.46, 0.62).Alpha(0.28)
			restingTile = restingTile.MixOklab(tintedTop, 0.38)
			trackFill = adjustLuminance(dominant, 0.72, 0.88).Mix(ui.RGB(255, 255, 255), 0.58)
			sheenTint = adjustLuminance(dominant, 0.78, 0.94).Mix(ui.RGB(255, 255, 255), 0.75).Alpha(0.92)
		}
		borderAlpha := float32(0.13)
		artistAlpha := float32(0.64)
		if highContrast {
			borderAlpha = 0.28
			artistAlpha = 0.80
		}
		return surfacePalette{
			dark:          true,
			cardTop:       cardTop,
			cardBottom:    cardBottom,
			cardGlow:      cardGlow,
			cardBorder:    ui.RGBA(255, 255, 255, borderAlpha),
			restingTile:   restingTile,
			restingBorder: ui.RGBA(255, 255, 255, borderAlpha),
			idleTileTop:   ui.RGB(42, 42, 50),
			idleTileBot:   ui.RGB(30, 30, 37),
			idleNote:      ui.RGBA(255, 255, 255, 0.46),
			title:         ui.RGBA(255, 255, 255, 0.95),
			artist:        ui.RGBA(255, 255, 255, artistAlpha),
			icon:          ui.RGBA(255, 255, 255, 0.92),
			iconHoverBg:   ui.RGBA(255, 255, 255, 0.12),
			iconPressBg:   ui.RGBA(255, 255, 255, 0.18),
			toolIcon:      ui.RGBA(255, 255, 255, 0.74),
			toolBg:        ui.RGBA(255, 255, 255, 0.06),
			toolHoverBg:   ui.RGBA(255, 255, 255, 0.16),
			track:         ui.RGBA(255, 255, 255, 0.18),
			trackFill:     trackFill,
			thumb:         ui.RGB(255, 255, 255),
			thumbShadow:   ui.RGBA(0, 0, 0, 0.45),
			coverShadow:   ui.RGBA(0, 0, 0, 0.42),
			sheenTint:     sheenTint,
			bubbleBg:      ui.RGBA(28, 28, 35, 0.92),
			bubbleBorder:  ui.RGBA(255, 255, 255, 0.18),
			bubbleText:    ui.RGB(255, 255, 255),
			bubbleShadow:  ui.RGBA(0, 0, 0, 0.35),
		}
	}

	cardTop := ui.RGB(249, 249, 252)
	cardBottom := ui.RGB(238, 239, 244)
	cardGlow := ui.RGBA(255, 255, 255, 0.35)
	restingTile := ui.RGB(243, 244, 248)
	trackFill := ui.RGB(28, 28, 34)
	sheenTint := ui.RGBA(255, 255, 255, 0.88)
	if hasDominant {
		tintedTop := adjustLuminance(dominant, 0.84, 0.93)
		tintedBot := adjustLuminance(dominant, 0.76, 0.87)
		cardTop = cardTop.MixOklab(tintedTop, 0.46)
		cardBottom = cardBottom.MixOklab(tintedBot, 0.34)
		cardGlow = adjustLuminance(dominant, 0.68, 0.82).Alpha(0.24)
		restingTile = restingTile.MixOklab(tintedTop, 0.32)
		trackFill = adjustLuminance(dominant, 0.24, 0.38).Mix(ui.RGB(20, 20, 26), 0.35)
	}
	borderAlpha := float32(0.11)
	artistAlpha := float32(0.60)
	if highContrast {
		borderAlpha = 0.24
		artistAlpha = 0.78
	}
	return surfacePalette{
		dark:          false,
		cardTop:       cardTop,
		cardBottom:    cardBottom,
		cardGlow:      cardGlow,
		cardBorder:    ui.RGBA(18, 20, 28, borderAlpha),
		restingTile:   restingTile,
		restingBorder: ui.RGBA(18, 20, 28, borderAlpha),
		idleTileTop:   ui.RGB(233, 234, 240),
		idleTileBot:   ui.RGB(220, 222, 230),
		idleNote:      ui.RGBA(20, 22, 30, 0.42),
		title:         ui.RGBA(18, 19, 24, 0.92),
		artist:        ui.RGBA(18, 19, 24, artistAlpha),
		icon:          ui.RGBA(18, 19, 24, 0.88),
		iconHoverBg:   ui.RGBA(18, 20, 28, 0.08),
		iconPressBg:   ui.RGBA(18, 20, 28, 0.14),
		toolIcon:      ui.RGBA(18, 19, 24, 0.66),
		toolBg:        ui.RGBA(18, 20, 28, 0.05),
		toolHoverBg:   ui.RGBA(18, 20, 28, 0.12),
		track:         ui.RGBA(18, 20, 28, 0.14),
		trackFill:     trackFill,
		thumb:         trackFill,
		thumbShadow:   ui.RGBA(0, 0, 0, 0.22),
		coverShadow:   ui.RGBA(18, 20, 28, 0.20),
		sheenTint:     sheenTint,
		bubbleBg:      ui.RGBA(255, 255, 255, 0.94),
		bubbleBorder:  ui.RGBA(18, 20, 28, 0.13),
		bubbleText:    ui.RGB(18, 19, 24),
		bubbleShadow:  ui.RGBA(18, 20, 28, 0.16),
	}
}

func extractDominantColor(img image.Image) (ui.Color, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ui.Color{}, false
	}

	const bins = 18
	type bucket struct {
		weight  float64
		r, g, b float64
	}
	var hist [bins]bucket
	var avgR, avgG, avgB, avgW float64

	stepX := max(1, w/24)
	stepY := max(1, h/24)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0x4000 {
				continue
			}
			// Unpremultiply standard Go image.Color RGBA values.
			af := float64(a16)
			rf := float64(r16) / af
			gf := float64(g16) / af
			bf := float64(b16) / af

			avgR += rf
			avgG += gf
			avgB += bf
			avgW++

			hue, sat, lit := rgbToHSL(rf, gf, bf)
			if sat < 0.08 {
				continue
			}
			lumFactor := 1.0 - math.Abs(2*lit-1)*0.68
			weight := (0.12 + sat*sat*1.8) * max(0.15, lumFactor)
			idx := int(math.Floor(hue*bins)) % bins
			if idx < 0 {
				idx += bins
			}
			hist[idx].weight += weight
			hist[idx].r += rf * weight
			hist[idx].g += gf * weight
			hist[idx].b += bf * weight
		}
	}

	if avgW == 0 {
		return ui.Color{}, false
	}

	best := -1
	bestWeight := 0.0
	for i := range hist {
		if hist[i].weight > bestWeight {
			bestWeight = hist[i].weight
			best = i
		}
	}
	if best >= 0 && bestWeight > 0 {
		bk := hist[best]
		return ui.RGB(toByte(bk.r/bk.weight), toByte(bk.g/bk.weight), toByte(bk.b/bk.weight)), true
	}
	return ui.RGB(toByte(avgR/avgW), toByte(avgG/avgW), toByte(avgB/avgW)), true
}

func adjustLuminance(c ui.Color, minL, maxL float64) ui.Color {
	h, s, l := rgbToHSL(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
	if l < minL {
		l = minL
	} else if l > maxL {
		l = maxL
	}
	if s > 0.72 {
		s = 0.72
	}
	r, g, b := hslToRGB(h, s, l)
	return ui.RGB(toByte(r), toByte(g), toByte(b))
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	maxC := max(r, max(g, b))
	minC := min(r, min(g, b))
	l = (maxC + minC) / 2
	d := maxC - minC
	if d == 0 {
		return 0, 0, l
	}
	if l > 0.5 {
		s = d / (2 - maxC - minC)
	} else {
		s = d / (maxC + minC)
	}
	switch maxC {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	return h, s, l
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	return hueToRGB(p, q, h+1.0/3.0), hueToRGB(p, q, h), hueToRGB(p, q, h-1.0/3.0)
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func toByte(v float64) uint8 {
	return uint8(math.Round(clamp01(v) * 255))
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
