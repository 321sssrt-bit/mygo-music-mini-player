package miniplayer

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"testing"
)

func TestBlankFrame(t *testing.T) {
	for _, size := range []image.Point{{X: RestingSize, Y: RestingSize}, {X: 176, Y: 204}} {
		t.Run(fmt.Sprintf("%dx%d", size.X, size.Y), func(t *testing.T) {
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
			for y := range size.Y {
				for x := range size.X {
					if _, _, _, alpha := frame.At(x, y).RGBA(); alpha != 0 {
						t.Fatalf("pixel (%d, %d) has alpha %d; want 0", x, y, alpha)
					}
				}
			}
		})
	}
}
