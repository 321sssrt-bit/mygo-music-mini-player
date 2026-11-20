package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderFrameCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.png")
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

	scenePath := filepath.Join(dir, "scene.png")
	if err := renderFrame([]string{"-scene", "expanded-volume-dark", "176", "204", scenePath}); err != nil {
		t.Fatalf("render scene: %v", err)
	}
	badScenePath := filepath.Join(dir, "bad.png")
	if err := renderFrame([]string{"-scene", "invalid", "64", "64", badScenePath}); err == nil {
		t.Fatal("expected error for unknown scene")
	}
	if _, err := os.Stat(badScenePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected failed render to remove output file, stat err = %v", err)
	}
}
