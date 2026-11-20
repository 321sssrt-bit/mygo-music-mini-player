// Run from the repository root: go run ./scripts/package-plugin <output-dir>
package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./scripts/package-plugin <output-dir>")
	}
	if err := buildPackages(os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func buildPackages(output string) error {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	for _, platform := range []string{"windows", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if err := buildPackage(output, platform, arch); err != nil {
				return fmt.Errorf("%s/%s: %w", platform, arch, err)
			}
		}
	}
	return nil
}

func buildPackage(output, platform, arch string) error {
	target := platform + "-" + arch
	binary := "mini-player-" + target
	if platform == "windows" {
		binary += ".exe"
	}
	work, err := os.MkdirTemp("", "mini-player-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	executable := filepath.Join(work, binary)
	build := exec.Command("go", "build", "-trimpath", "-o", executable, "./cmd/mini-player")
	build.Env = append(os.Environ(), "GOOS="+platform, "GOARCH="+arch, "CGO_ENABLED=0")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return err
	}

	file, err := os.CreateTemp(output, ".plugin-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	archive := zip.NewWriter(file)
	for _, name := range []string{"plugin.json", "index.js", "locales/en.json", "locales/zh-CN.json", "LICENSE"} {
		if err = addFile(archive, name, name, 0o644); err != nil {
			break
		}
	}
	if err == nil {
		err = addFile(archive, "bin/"+binary, executable, 0o755)
	}
	if err := errors.Join(err, archive.Close(), file.Close()); err != nil {
		return err
	}
	name := filepath.Join(output, "plugin-"+target+".zip")
	if err := os.Rename(file.Name(), name); err != nil {
		return err
	}
	fmt.Println(name)
	return nil
}

func addFile(archive *zip.Writer, name, path string, mode os.FileMode) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	dst, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}
