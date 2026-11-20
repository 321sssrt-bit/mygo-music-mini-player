package miniplayer

import (
	"fmt"
	"image/png"
	"io"
	"math"
	"strings"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/egoist/mygo/ui"
)

var (
	expandSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: ExpandDuration,
			Bounce:   0.18,
		},
	}
	coverSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 360 * time.Millisecond,
			Bounce:   0.16,
		},
	}
	controlSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 260 * time.Millisecond,
			Bounce:   0.22,
		},
	}
	tiltSpring = ui.Animation{
		Spring: ui.SpringOptions{
			Duration: 220 * time.Millisecond,
			Bounce:   0.08,
		},
	}
	hoverFade = ui.Animation{
		Duration:  140 * time.Millisecond,
		Ease:      ui.EaseOut,
		NonMotion: true,
	}
	volumeFade = ui.Animation{
		Duration:  volumeFadeDuration,
		Ease:      ui.EaseInOut,
		NonMotion: true,
	}
)

// View renders the default mini player idle canvas, adapting between resting and expanded
// proportions from the current viewport size.
func View(c *ui.Context) {
	w, h := c.Size()
	expanded := w >= 120 || h >= 120
	p := New(State{Expanded: expanded}, Actions{})
	p.View(c)
}

// WriteFrame exports the default view to a PNG at 1 pixel per DIP without opening a window.
// The caller supplies positive viewport dimensions.
func WriteFrame(w io.Writer, width, height int) error {
	return png.Encode(w, ui.NewTester(View, width, height).Image())
}

// View builds the mini player UI for the current state snapshot.
func (p *Player) View(c *ui.Context) {
	dark := resolveDark(c, p.state.Settings.Host.Appearance)
	pal := buildPalette(dark, c.Preferences().HighContrast, p.artDominant, p.hasArtDominant)
	tr := localizedStrings(p.state.Settings.Host.Language, c.Locale())
	reduceMotion := p.reduceMotion(c)

	root := c.Root().
		Background(ui.Transparent).
		Fill().
		Center().
		Role(ui.RoleGroup).
		Label(tr.regionName)

	targetExpand := float32(0)
	if p.state.Expanded {
		targetExpand = 1
	}
	expandProgress := animateMotion(root, "expand", targetExpand, expandSpring, reduceMotion)

	root.Children(func() {
		if p.state.Expanded || expandProgress > 0.5 {
			p.buildExpanded(c, pal, tr, expandProgress, reduceMotion)
			return
		}
		p.buildResting(c, pal, tr, reduceMotion)
	})
}

func (p *Player) buildResting(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	const (
		tileSize   = 62
		tileRadius = 14
		lineHeight = 2.5
	)

	hasTrack := p.state.Media.Available
	hasArtwork := hasTrack && p.artBitmap != nil
	targetScale := float32(1.0)
	if hasArtwork && !p.state.Media.Playing {
		targetScale = pausedCoverScale
	}
	progress := float32(p.progressRatio())

	tile := ui.Box(c).
		Key("resting-tile").
		Size(tileSize, tileSize).
		Radius(tileRadius).
		Background(pal.restingTile).
		Border(1, pal.restingBorder).
		Clip().
		Center().
		Role(ui.RoleImage).
		Label(artworkLabel(hasTrack, tr))

	coverScale := animateMotion(tile, "resting-cover-scale", targetScale, coverSpring, reduceMotion)

	tile.Children(func() {
		if hasArtwork {
			coverW := float32(tileSize)
			coverRadius := float32(tileRadius)
			if coverScale < 0.995 {
				coverRadius = 11
			}
			art := ui.Box(c).
				Key("resting-artwork").
				Size(coverW, coverW).
				Radius(coverRadius).
				Clip().
				Transform(ui.Scaling(coverScale, coverScale))
			if coverScale < 0.995 {
				art.Shadow(0, 3, 8, 0, pal.coverShadow)
			}
			art.Children(func() {
				ui.Image(c, p.artBitmap).Fill().Fit(ui.Cover)
			})
		} else {
			p.buildIdleArtwork(c, tileSize, tileRadius, pal)
		}

		// Bottom edge scrim and subtle progress line inside the rounded clip.
		ui.Box(c).
			Key("resting-progress").
			Absolute().
			Left(0).
			Right(0).
			Bottom(0).
			Height(10).
			PassThrough().
			Draw(func(pt *ui.Painter, r ui.Rect) {
				if hasArtwork {
					pt.FillGradient(r, ui.LinearGradient{
						From:  ui.Transparent,
						To:    ui.RGBA(0, 0, 0, 0.42),
						Angle: 180,
					}, 0)
				}
				trackRect := ui.Rect{
					X: r.X,
					Y: r.Y + r.H - lineHeight,
					W: r.W,
					H: lineHeight,
				}
				if hasTrack {
					trackColor := pal.track
					fillColor := pal.trackFill
					if hasArtwork {
						trackColor = ui.RGBA(255, 255, 255, 0.24)
						fillColor = ui.RGBA(255, 255, 255, 0.94)
					}
					pt.Fill(trackRect, trackColor, 0)
					if progress > 0 {
						pt.Fill(ui.Rect{
							X: trackRect.X,
							Y: trackRect.Y,
							W: trackRect.W * progress,
							H: trackRect.H,
						}, fillColor, lineHeight/2)
					}
				}
			})
	})
}

func (p *Player) buildExpanded(c *ui.Context, pal surfacePalette, tr copyStrings, expandProgress float32, reduceMotion bool) {
	const (
		cardW      = ExpandedWidth - 2
		cardH      = ExpandedHeight - 2
		cardRadius = 18
	)

	cardScale := float32(1.0)
	cardOpacity := float32(1.0)
	if !reduceMotion && expandProgress != 1 {
		cardScale = 0.92 + 0.08*expandProgress
		cardOpacity = clamp32(expandProgress*1.25, 0.2, 1.0)
	}

	card := ui.Box(c).
		Key("expanded-card").
		Size(cardW, cardH).
		Radius(cardRadius).
		LinearGradient(ui.LinearGradient{
			From:  pal.cardTop,
			To:    pal.cardBottom,
			Angle: 180,
			Oklab: true,
		}).
		Border(1, pal.cardBorder).
		Clip().
		Opacity(cardOpacity)

	if cardScale != 1 {
		card.Transform(ui.Scaling(cardScale, cardScale))
	}

	card.Children(func() {
		// Soft dominant-color radial glow behind the album artwork.
		ui.Box(c).
			Key("card-glow").
			Absolute().
			Left(0).
			Top(0).
			Right(0).
			Bottom(0).
			PassThrough().
			RadialGradient(ui.RadialGradient{
				Center: ui.Point{X: 0.5, Y: 0.30},
				Radius: 112,
				Stops: []ui.GradientStop{
					{Color: pal.cardGlow, At: 0},
					{Color: ui.Transparent, At: 1},
				},
			})

		// Vertical card content stack.
		ui.Column(c).
			Fill().
			Padding(28, 12, 6, 12).
			AlignItems(ui.Center).
			Children(func() {
				p.buildExpandedArtwork(c, pal, tr, reduceMotion)

				ui.Box(c).Height(7)
				p.buildTrackInfo(c, pal, tr)

				ui.Box(c).Height(7)
				p.buildProgressBar(c, pal, tr, reduceMotion)

				ui.Box(c).Height(4)
				p.buildTransportRow(c, pal, tr, reduceMotion)
			})

		// Top-right frameless window action buttons.
		p.buildWindowTools(c, pal, tr, reduceMotion)

		// Top-centered fading volume percentage capsule bubble.
		p.buildVolumeBubble(c, pal, tr)
	})
}

func (p *Player) buildExpandedArtwork(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	const (
		slotSize    = 78
		coverRadius = 12
	)

	hasTrack := p.state.Media.Available
	hasArtwork := hasTrack && p.artBitmap != nil

	slot := ui.Box(c).
		Key("expanded-artwork-slot").
		Size(slotSize, slotSize).
		Center().
		Role(ui.RoleImage).
		Label(artworkLabel(hasTrack, tr))

	targetScale := float32(1.0)
	if hasArtwork && !p.state.Media.Playing {
		targetScale = pausedCoverScale
	}
	breathScale := animateMotion(slot, "expanded-cover-scale", targetScale, coverSpring, reduceMotion)

	var targetRotX, targetRotY, targetSheen float32
	targetLightX, targetLightY := float32(0.5), float32(0.5)
	if hasArtwork && !reduceMotion {
		if px, py, ok := slot.PointerPosition(); ok && slot.Hovered() {
			nx := clamp32((px/slotSize)*2-1, -1, 1)
			ny := clamp32((py/slotSize)*2-1, -1, 1)
			targetRotY = nx * maxCoverTiltDegrees
			targetRotX = -ny * maxCoverTiltDegrees
			targetLightX = (nx + 1) * 0.5
			targetLightY = (ny + 1) * 0.5
			targetSheen = 1
		}
	}

	rotX := animateMotion(slot, "cover-rot-x", targetRotX, tiltSpring, reduceMotion)
	rotY := animateMotion(slot, "cover-rot-y", targetRotY, tiltSpring, reduceMotion)
	lightX := animateMotion(slot, "cover-light-x", targetLightX, tiltSpring, reduceMotion)
	lightY := animateMotion(slot, "cover-light-y", targetLightY, tiltSpring, reduceMotion)
	sheenIntensity := float32(0)
	if !reduceMotion {
		sheenIntensity = slot.AnimateTo("cover-sheen", targetSheen, hoverFade)
	}

	slot.Children(func() {
		if !hasArtwork {
			p.buildIdleArtwork(c, slotSize, coverRadius, pal)
			return
		}

		tf := ui.Scaling(breathScale, breathScale)
		if !reduceMotion && (rotX != 0 || rotY != 0) {
			tf = ui.Perspective(420).
				Then(ui.RotationX(rotX)).
				Then(ui.RotationY(rotY)).
				Then(ui.Scaling(breathScale, breathScale))
		}

		cover := ui.Box(c).
			Key("expanded-cover").
			Size(slotSize, slotSize).
			Radius(coverRadius).
			Clip().
			Shadow(0, 6, 14, -2, pal.coverShadow).
			Transform(tf).
			DrawOver(func(pt *ui.Painter, r ui.Rect) {
				drawCoverSheen(pt, r, coverRadius, lightX, lightY, sheenIntensity, pal.sheenTint)
				pt.Stroke(r, ui.RGBA(255, 255, 255, 0.14), coverRadius, 1)
			})
		cover.Children(func() {
			ui.Image(c, p.artBitmap).Fill().Fit(ui.Cover)
		})
	})
}

func (p *Player) buildIdleArtwork(c *ui.Context, size, radius float32, pal surfacePalette) {
	// Pre-composite onto the tile midpoint so overlapping beam, stems, and noteheads stay uniform without alpha seams.
	noteColor := pal.idleNote.Over(pal.idleTileTop.Mix(pal.idleTileBot, 0.5))
	ui.Box(c).
		Key("idle-artwork").
		Size(size, size).
		Radius(radius).
		LinearGradient(ui.LinearGradient{
			From:  pal.idleTileTop,
			To:    pal.idleTileBot,
			Angle: 165,
		}).
		Border(1, pal.restingBorder).
		Clip().
		Center().
		Draw(func(pt *ui.Painter, r ui.Rect) {
			iconSize := size * 0.44
			iconRect := ui.Rect{
				X: r.X + (r.W-iconSize)/2,
				Y: r.Y + (r.H-iconSize)/2,
				W: iconSize,
				H: iconSize,
			}
			drawMusicNoteIcon(pt, iconRect, noteColor)
		})
}

func (p *Player) buildTrackInfo(c *ui.Context, pal surfacePalette, tr copyStrings) {
	title := strings.TrimSpace(p.state.Media.Title)
	artist := strings.TrimSpace(p.state.Media.Artist)
	if !p.state.Media.Available || title == "" {
		title = tr.idleTitle
		if !p.state.Media.Available {
			artist = tr.idleSubtitle
		}
	}

	ui.Column(c).
		Key("track-info").
		FillWidth().
		AlignItems(ui.Center).
		Gap(1).
		Children(func() {
			ui.Text(c, title).
				Key("track-title").
				FillWidth().
				SingleLine().
				Ellipsis("…").
				TextAlign(ui.Center).
				FontSize(12.5).
				FontWeight(600).
				FixedLineHeight(16).
				TextColor(pal.title)

			ui.Text(c, artist).
				Key("track-artist").
				FillWidth().
				SingleLine().
				Ellipsis("…").
				TextAlign(ui.Center).
				FontSize(10.5).
				FixedLineHeight(14).
				TextColor(pal.artist)
		})
}

func (p *Player) buildProgressBar(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	canSeek := p.state.Media.Available && p.state.Media.DurationMS > 0
	ratio := p.progressRatio()
	displayMS := int64(0)
	if canSeek {
		displayMS = int64(math.Round(ratio * float64(p.state.Media.DurationMS)))
	}

	bar := ui.Box(c).
		Key("progress-bar").
		FillWidth().
		Height(14).
		Role(ui.RoleSlider).
		Label(tr.progress).
		Range(0, float64(max(int64(0), p.state.Media.DurationMS)), float64(displayMS))
	if canSeek {
		bar.Cursor(ui.CursorPointer)
	}

	var barRef ui.Handle
	bar.Bind(&barRef)

	bar.HandleInput(func(ev ui.InputEvent) bool {
		if !canSeek {
			return false
		}
		width := c.Resolve(barRef).Bounds().W
		if width <= 0 {
			width = ExpandedWidth - 26
		}
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			p.scrubbing = true
			p.scrubRatio = clamp01(float64(ev.X) / float64(width))
			return true
		case ui.InputPointerMove:
			if !p.scrubbing {
				return false
			}
			p.scrubRatio = clamp01(float64(ev.X) / float64(width))
			return true
		case ui.InputPointerUp:
			if !p.scrubbing {
				return false
			}
			finalRatio := clamp01(float64(ev.X) / float64(width))
			p.scrubbing = false
			if !ev.Canceled && p.actions.OnSeekMS != nil {
				targetMS := int64(math.Round(finalRatio * float64(p.state.Media.DurationMS)))
				p.actions.OnSeekMS(targetMS)
			}
			return true
		default:
			return false
		}
	})

	activeTarget := float32(0)
	if canSeek && (bar.Hovered() || p.scrubbing) {
		activeTarget = 1
	}
	active := animateMotion(bar, "progress-active", activeTarget, hoverFade, reduceMotion)

	bar.Draw(func(pt *ui.Painter, r ui.Rect) {
		trackH := 3.0 + 2.0*active
		trackY := r.Y + (r.H-trackH)/2
		trackRect := ui.Rect{X: r.X, Y: trackY, W: r.W, H: trackH}
		pt.Fill(trackRect, pal.track, trackH/2)

		fillW := r.W * float32(ratio)
		if fillW > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: trackY, W: fillW, H: trackH}, pal.trackFill, trackH/2)
		}

		if active > 0.02 {
			thumbR := 4.5 * active
			cx := clamp32(r.X+fillW, r.X+thumbR, r.X+r.W-thumbR)
			cy := r.Y + r.H/2
			thumbRect := ui.Rect{
				X: cx - thumbR,
				Y: cy - thumbR,
				W: thumbR * 2,
				H: thumbR * 2,
			}
			pt.Shadow(thumbRect, thumbR, 0, 1, 3, 0, pal.thumbShadow)
			pt.Fill(thumbRect, pal.thumb, thumbR)
		}
	})
}

func (p *Player) buildTransportRow(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	ui.Row(c).
		Key("transport-row").
		FillWidth().
		Height(28).
		Justify(ui.Center).
		AlignItems(ui.Center).
		Gap(14).
		Children(func() {
			p.buildTransportButton(c, "btn-prev", tr.previous, 26, pal, reduceMotion, func() {
				if p.actions.OnPlayback != nil {
					p.actions.OnPlayback(bridge.Previous)
				}
			}, func(pt *ui.Painter, r ui.Rect, color ui.Color) {
				drawPrevIcon(pt, insetRect(r, 6), color)
			})

			p.buildPlayPauseButton(c, pal, tr, reduceMotion)

			p.buildTransportButton(c, "btn-next", tr.next, 26, pal, reduceMotion, func() {
				if p.actions.OnPlayback != nil {
					p.actions.OnPlayback(bridge.Next)
				}
			}, func(pt *ui.Painter, r ui.Rect, color ui.Color) {
				drawNextIcon(pt, insetRect(r, 6), color)
			})
		})
}

func (p *Player) buildTransportButton(
	c *ui.Context,
	key, label string,
	size float32,
	pal surfacePalette,
	reduceMotion bool,
	onClick func(),
	draw func(pt *ui.Painter, r ui.Rect, color ui.Color),
) {
	btn := ui.ButtonBase(c).
		Key(key).
		Size(size, size).
		Radius(size / 2).
		Cursor(ui.CursorPointer).
		Label(label)

	targetScale := float32(1.0)
	bg := ui.Transparent
	if btn.Pressed() {
		targetScale = 0.86
		bg = pal.iconPressBg
	} else if btn.Hovered() {
		if !reduceMotion {
			targetScale = 1.06
		}
		bg = pal.iconHoverBg
	}
	scale := animateMotion(btn, key+"-scale", targetScale, controlSpring, reduceMotion)

	btn.Background(bg).
		Transform(ui.Scaling(scale, scale)).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			draw(pt, r, pal.icon)
		})

	if btn.Clicked() {
		onClick()
	}
}

func (p *Player) buildPlayPauseButton(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	playing := p.state.Media.Available && p.state.Media.Playing
	label := tr.play
	if playing {
		label = tr.pause
	}

	btn := ui.ButtonBase(c).
		Key("btn-play-pause").
		Size(30, 30).
		Radius(15).
		Cursor(ui.CursorPointer).
		Label(label)

	targetScale := float32(1.0)
	bg := ui.Transparent
	if btn.Pressed() {
		targetScale = 0.86
		bg = pal.iconPressBg
	} else if btn.Hovered() {
		if !reduceMotion {
			targetScale = 1.07
		}
		bg = pal.iconHoverBg
	}
	btnScale := animateMotion(btn, "play-btn-scale", targetScale, controlSpring, reduceMotion)

	morphTarget := float32(0)
	if playing {
		morphTarget = 1
	}
	morph := animateMotion(btn, "play-pause-morph", morphTarget, controlSpring, reduceMotion)

	btn.Background(bg).
		Transform(ui.Scaling(btnScale, btnScale)).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			iconRect := insetRect(r, 6)
			cx := iconRect.X + iconRect.W/2
			cy := iconRect.Y + iconRect.H/2

			playAlpha := clamp32(1-morph, 0, 1)
			if playAlpha > 0.02 {
				playScale := clamp32(1-morph*0.55, 0.25, 1.15)
				pt.Transform(ui.Scaling(playScale, playScale), cx, cy, func() {
					drawPlayIcon(pt, iconRect, pal.icon.Alpha(float32(pal.icon.A)/255*playAlpha))
				})
			}

			pauseAlpha := clamp32(morph, 0, 1)
			if pauseAlpha > 0.02 {
				pauseScale := clamp32(0.45+morph*0.55, 0.25, 1.15)
				pt.Transform(ui.Scaling(pauseScale, pauseScale), cx, cy, func() {
					drawPauseIcon(pt, iconRect, pal.icon.Alpha(float32(pal.icon.A)/255*pauseAlpha))
				})
			}
		})

	if btn.Clicked() && p.actions.OnPlayback != nil {
		if playing {
			p.actions.OnPlayback(bridge.Pause)
		} else {
			p.actions.OnPlayback(bridge.Play)
		}
	}
}

func (p *Player) buildWindowTools(c *ui.Context, pal surfacePalette, tr copyStrings, reduceMotion bool) {
	ui.Row(c).
		Key("window-tools").
		Absolute().
		Top(5).
		Right(7).
		Gap(3).
		Children(func() {
			p.buildToolButton(c, "btn-wake", tr.wakeMain, pal, reduceMotion, func() {
				if p.actions.OnWakeMain != nil {
					p.actions.OnWakeMain()
				}
			}, func(pt *ui.Painter, r ui.Rect, color ui.Color) {
				drawWakeMainIcon(pt, insetRect(r, 4.5), color)
			})

			p.buildToolButton(c, "btn-hide", tr.hide, pal, reduceMotion, func() {
				if p.actions.OnHide != nil {
					p.actions.OnHide()
				}
			}, func(pt *ui.Painter, r ui.Rect, color ui.Color) {
				drawHideIcon(pt, insetRect(r, 4.5), color)
			})
		})
}

func (p *Player) buildToolButton(
	c *ui.Context,
	key, label string,
	pal surfacePalette,
	reduceMotion bool,
	onClick func(),
	draw func(pt *ui.Painter, r ui.Rect, color ui.Color),
) {
	btn := ui.ButtonBase(c).
		Key(key).
		Size(19, 19).
		Radius(9.5).
		Cursor(ui.CursorPointer).
		Label(label)

	bg := pal.toolBg
	iconColor := pal.toolIcon
	targetScale := float32(1.0)
	if btn.Pressed() {
		bg = pal.iconPressBg
		iconColor = pal.icon
		targetScale = 0.88
	} else if btn.Hovered() {
		bg = pal.toolHoverBg
		iconColor = pal.icon
	}
	scale := animateMotion(btn, key+"-scale", targetScale, controlSpring, reduceMotion)

	btn.Background(bg).
		Transform(ui.Scaling(scale, scale)).
		Draw(func(pt *ui.Painter, r ui.Rect) {
			draw(pt, r, iconColor)
		})

	if btn.Clicked() {
		onClick()
	}
}

func (p *Player) buildVolumeBubble(c *ui.Context, pal surfacePalette, tr copyStrings) {
	targetOpacity := p.volumeBubbleTarget(c)
	pct := int(math.Round(clamp01(p.state.Volume) * 100))

	holder := ui.Box(c).
		Key("volume-bubble-holder").
		Absolute().
		Top(5).
		Left(0).
		Right(0).
		Justify(ui.Center).
		AlignItems(ui.Center).
		PassThrough()

	var opacity float32
	if targetOpacity >= 1 {
		// Show immediately when volume changes; only the dismissal fades out.
		opacity = holder.Jump("volume-opacity", 1)
	} else {
		opacity = holder.AnimateTo("volume-opacity", 0, volumeFade)
	}
	if opacity <= 0.01 {
		if targetOpacity == 0 {
			p.volumeTimed = false
		}
		return
	}

	holder.Children(func() {
		ui.Row(c).
			Key("volume-bubble").
			Height(18).
			PaddingX(7).
			Radius(9).
			Gap(3).
			AlignItems(ui.Center).
			Background(pal.bubbleBg).
			Border(1, pal.bubbleBorder).
			Shadow(0, 2, 6, 0, pal.bubbleShadow).
			Opacity(opacity).
			PassThrough().
			Role(ui.RoleStatus).
			Label(fmt.Sprintf(tr.volumeFormat, pct)).
			Children(func() {
				ui.Box(c).
					Size(10, 10).
					Draw(func(pt *ui.Painter, r ui.Rect) {
						drawSpeakerIcon(pt, r, pct == 0, pal.bubbleText)
					})
				ui.Text(c, fmt.Sprintf("%d%%", pct)).
					FontSize(10).
					FontWeight(600).
					FontFeatures("tnum").
					TextColor(pal.bubbleText)
			})
	})
}

func animateMotion(e ui.Element, key any, target float32, anim ui.Animation, reduceMotion bool) float32 {
	if reduceMotion {
		return e.Jump(key, target)
	}
	return e.AnimateTo(key, target, anim)
}

func artworkLabel(hasTrack bool, tr copyStrings) string {
	if hasTrack {
		return tr.artwork
	}
	return tr.idleArtwork
}

func insetRect(r ui.Rect, inset float32) ui.Rect {
	return ui.Rect{
		X: r.X + inset,
		Y: r.Y + inset,
		W: max(0, r.W-2*inset),
		H: max(0, r.H-2*inset),
	}
}
