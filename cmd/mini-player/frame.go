package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"strconv"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/miniplayer"
)

func renderFrame(args []string) error {
	fs := flag.NewFlagSet("render-frame", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scene := fs.String("scene", "", "named UI scene to render")
	if err := fs.Parse(args); err != nil {
		return errors.New("usage: mini-player render-frame [-scene <name>] <width> <height> <output.png>")
	}
	pos := fs.Args()
	if len(pos) != 3 {
		return errors.New("usage: mini-player render-frame [-scene <name>] <width> <height> <output.png>")
	}
	width, widthErr := strconv.Atoi(pos[0])
	height, heightErr := strconv.Atoi(pos[1])
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return errors.New("frame width and height must be positive integers")
	}

	path := pos[2]
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return err
	}
	if err := errors.Join(miniplayer.WriteSceneFrame(file, *scene, width, height), file.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}
