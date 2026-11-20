package miniplayer

import (
	"image/png"
	"io"

	"github.com/egoist/mygo/ui"
)

// RestingSize is the width and height of the resting canvas in DIPs.
const RestingSize = 64

// View draws the mini player's initial transparent canvas.
func View(c *ui.Context) {
	c.Root().Background(ui.Transparent)
}

// WriteFrame exports the view to a PNG at 1 pixel per DIP without opening a window.
// The caller supplies positive viewport dimensions.
func WriteFrame(w io.Writer, width, height int) error {
	return png.Encode(w, ui.NewTester(View, width, height).Image())
}
