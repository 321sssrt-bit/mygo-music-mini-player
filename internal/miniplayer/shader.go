package miniplayer

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

const coverSheenSource = `package main

var LightPos vec2
var Intensity float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	size := max(rectSize(), vec2(1, 1))
	uv := pos / size
	d := uv - LightPos
	dist2 := dot(d, d)
	spot := exp(-dist2*5.4) * 0.36

	diag := (uv.x-LightPos.x)*0.72 + (uv.y-LightPos.y)*0.69
	band := exp(-diag*diag*26.0) * 0.16

	edgeX := min(uv.x, 1.0-uv.x)
	edgeY := min(uv.y, 1.0-uv.y)
	edge := 1.0 - smoothstep(0.0, 0.14, min(edgeX, edgeY))
	lightEdge := clamp(1.0-length(d), 0.0, 1.0)
	rim := edge * lightEdge * 0.22

	a := clamp((spot+band+rim)*Intensity*Tint.a, 0.0, 0.56)
	return vec4(Tint.rgb*a, a)
}
`

var (
	coverSheenShader = shader.MustCompile(coverSheenSource)
	sheenLightPosU   = coverSheenShader.Uniform("LightPos")
	sheenIntensityU  = coverSheenShader.Uniform("Intensity")
	sheenTintU       = coverSheenShader.Uniform("Tint")
)

func drawCoverSheen(p *ui.Painter, r ui.Rect, radius, lightX, lightY, intensity float32, tint ui.Color) {
	if intensity <= 0.005 {
		return
	}
	vals := coverSheenShader.NewValues().
		Vec2(sheenLightPosU, lightX, lightY).
		Float(sheenIntensityU, intensity).
		Color(sheenTintU, tint)
	coverSheenShader.Draw(p, r, shader.Options{
		Values: vals,
		Radius: radius,
	})
}
