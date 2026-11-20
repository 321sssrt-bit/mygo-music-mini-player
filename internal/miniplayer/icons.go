package miniplayer

import "github.com/egoist/mygo/ui"

func drawMusicNoteIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }

	beam := new(ui.Path)
	beam.MoveTo(px(0.37), py(0.24))
	beam.LineTo(px(0.79), py(0.15))
	beam.LineTo(px(0.79), py(0.29))
	beam.LineTo(px(0.37), py(0.38))
	beam.Close()
	p.FillPath(beam, color)

	stems := new(ui.Path).Cap(ui.RoundCap)
	stems.MoveTo(px(0.39), py(0.28))
	stems.LineTo(px(0.39), py(0.74))
	stems.MoveTo(px(0.77), py(0.20))
	stems.LineTo(px(0.77), py(0.66))
	p.StrokePath(stems, max(1.6, s*0.095), color)

	heads := new(ui.Path)
	heads.Circle(px(0.30), py(0.76), s*0.13)
	heads.Circle(px(0.68), py(0.68), s*0.13)
	p.FillPath(heads, color)
}

func drawWakeMainIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }
	sw := max(1.3, s*0.125)

	frame := new(ui.Path).Cap(ui.RoundCap).Join(ui.RoundJoin)
	frame.MoveTo(px(0.46), py(0.20))
	frame.LineTo(px(0.24), py(0.20))
	frame.QuadTo(px(0.16), py(0.20), px(0.16), py(0.28))
	frame.LineTo(px(0.16), py(0.76))
	frame.QuadTo(px(0.16), py(0.84), px(0.24), py(0.84))
	frame.LineTo(px(0.72), py(0.84))
	frame.QuadTo(px(0.80), py(0.84), px(0.80), py(0.76))
	frame.LineTo(px(0.80), py(0.54))
	p.StrokePath(frame, sw, color)

	arrow := new(ui.Path).Cap(ui.RoundCap).Join(ui.RoundJoin)
	arrow.MoveTo(px(0.58), py(0.16))
	arrow.LineTo(px(0.84), py(0.16))
	arrow.LineTo(px(0.84), py(0.42))
	arrow.MoveTo(px(0.46), py(0.54))
	arrow.LineTo(px(0.82), py(0.18))
	p.StrokePath(arrow, sw, color)
}

func drawHideIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }
	sw := max(1.4, s*0.135)

	bar := new(ui.Path).Cap(ui.RoundCap)
	bar.MoveTo(px(0.20), py(0.52))
	bar.LineTo(px(0.80), py(0.52))
	p.StrokePath(bar, sw, color)
}

func drawPrevIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }

	barW := max(1.8, s*0.14)
	p.Fill(ui.Rect{X: px(0.16), Y: py(0.20), W: barW, H: s * 0.60}, color, barW/2)

	tri := new(ui.Path).Join(ui.RoundJoin)
	tri.MoveTo(px(0.32), py(0.50))
	tri.LineTo(px(0.82), py(0.20))
	tri.LineTo(px(0.82), py(0.80))
	tri.Close()
	p.FillPath(tri, color)
}

func drawNextIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }

	tri := new(ui.Path).Join(ui.RoundJoin)
	tri.MoveTo(px(0.68), py(0.50))
	tri.LineTo(px(0.18), py(0.20))
	tri.LineTo(px(0.18), py(0.80))
	tri.Close()
	p.FillPath(tri, color)

	barW := max(1.8, s*0.14)
	p.Fill(ui.Rect{X: px(0.84) - barW, Y: py(0.20), W: barW, H: s * 0.60}, color, barW/2)
}

func drawPlayIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	// Optical nudge right so a triangle feels visually centered.
	ox := r.X + (r.W-s)/2 + s*0.05
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }

	tri := new(ui.Path).Join(ui.RoundJoin)
	tri.MoveTo(px(0.22), py(0.16))
	tri.LineTo(px(0.84), py(0.50))
	tri.LineTo(px(0.22), py(0.84))
	tri.Close()
	p.FillPath(tri, color)
}

func drawPauseIcon(p *ui.Painter, r ui.Rect, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	barW := s * 0.23
	barH := s * 0.68
	gap := s * 0.18
	startX := ox + (s-2*barW-gap)/2
	topY := oy + (s-barH)/2
	rad := barW * 0.42
	p.Fill(ui.Rect{X: startX, Y: topY, W: barW, H: barH}, color, rad)
	p.Fill(ui.Rect{X: startX + barW + gap, Y: topY, W: barW, H: barH}, color, rad)
}

func drawSpeakerIcon(p *ui.Painter, r ui.Rect, muted bool, color ui.Color) {
	s := min(r.W, r.H)
	ox := r.X + (r.W-s)/2
	oy := r.Y + (r.H-s)/2
	px := func(u float32) float32 { return ox + u*s }
	py := func(v float32) float32 { return oy + v*s }

	cone := new(ui.Path).Join(ui.RoundJoin)
	cone.MoveTo(px(0.12), py(0.38))
	cone.LineTo(px(0.32), py(0.38))
	cone.LineTo(px(0.54), py(0.18))
	cone.LineTo(px(0.54), py(0.82))
	cone.LineTo(px(0.32), py(0.62))
	cone.LineTo(px(0.12), py(0.62))
	cone.Close()
	p.FillPath(cone, color)

	sw := max(1.1, s*0.11)
	if muted {
		cross := new(ui.Path).Cap(ui.RoundCap)
		cross.MoveTo(px(0.68), py(0.36))
		cross.LineTo(px(0.92), py(0.64))
		cross.MoveTo(px(0.92), py(0.36))
		cross.LineTo(px(0.68), py(0.64))
		p.StrokePath(cross, sw, color)
		return
	}

	waves := new(ui.Path).Cap(ui.RoundCap)
	waves.MoveTo(px(0.68), py(0.34))
	waves.QuadTo(px(0.80), py(0.50), px(0.68), py(0.66))
	waves.MoveTo(px(0.79), py(0.22))
	waves.QuadTo(px(0.96), py(0.50), px(0.79), py(0.78))
	p.StrokePath(waves, sw, color)
}
