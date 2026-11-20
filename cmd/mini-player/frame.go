package main

import (
	"errors"
	"os"
	"strconv"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/miniplayer"
)

func renderFrame(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: mini-player render-frame <width> <height> <output.png>")
	}
	width, widthErr := strconv.Atoi(args[0])
	height, heightErr := strconv.Atoi(args[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return errors.New("frame width and height must be positive integers")
	}

	path := args[2]
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return err
	}
	if err := errors.Join(miniplayer.WriteFrame(file, width, height), file.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}
