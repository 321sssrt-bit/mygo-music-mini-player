package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderFrameCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.png")
	if err := renderFrame([]string{"0", "204", path}); err == nil {
		t.Fatal("rendered a zero-width frame")
	}
	if err := renderFrame([]string{"176", "204", path}); err != nil {
		t.Fatal(err)
	}
	if err := renderFrame([]string{"64", "64", path}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing output: %v, want file already exists", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := png.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 176 || info.Height != 204 {
		t.Fatalf("saved PNG dimensions = %dx%d; want 176x204", info.Width, info.Height)
	}
}
