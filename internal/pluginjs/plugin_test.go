package pluginjs

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"debug/pe"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestPluginJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the plugin host simulation")
	}
	root := filepath.Join("..", "..")
	temp := t.TempDir()
	fixture := filepath.Join(temp, "companion")
	if runtime.GOOS == "windows" {
		fixture += ".exe"
	}
	run(t, ".", "go", "build", "-o", fixture, "./testdata/companion")
	run(t, root, node, "--experimental-vm-modules", "internal/pluginjs/testdata/host.mjs", fixture, temp)
}

func TestPluginPackages(t *testing.T) {
	output := t.TempDir()
	run(t, filepath.Join("..", ".."), "go", "run", "./scripts/package-plugin", output)
	for _, target := range []struct {
		name string
		pe   uint16
		elf  elf.Machine
	}{
		{name: "windows-amd64", pe: pe.IMAGE_FILE_MACHINE_AMD64},
		{name: "windows-arm64", pe: pe.IMAGE_FILE_MACHINE_ARM64},
		{name: "linux-amd64", elf: elf.EM_X86_64},
		{name: "linux-arm64", elf: elf.EM_AARCH64},
	} {
		t.Run(target.name, func(t *testing.T) {
			archive, err := zip.OpenReader(filepath.Join(output, "plugin-"+target.name+".zip"))
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			binary := "bin/mini-player-" + target.name
			if target.pe != 0 {
				binary += ".exe"
			}
			expected := []string{"LICENSE", binary, "index.js", "locales/en.json", "locales/zh-CN.json", "plugin.json"}
			var names []string
			for _, f := range archive.File {
				names = append(names, f.Name)
				if f.Name != binary {
					continue
				}
				if mode := f.Mode(); !mode.IsRegular() || mode.Perm() != 0o755 {
					t.Fatalf("%s mode = %v, want regular 0755", binary, mode)
				}
				r, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				if target.pe != 0 {
					executable, err := pe.NewFile(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					if executable.Machine != target.pe {
						t.Fatalf("PE machine = %#x, want %#x", executable.Machine, target.pe)
					}
					t.Logf("%s: PE machine %#x, mode %04o", binary, executable.Machine, f.Mode().Perm())
				} else {
					executable, err := elf.NewFile(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					if executable.Machine != target.elf {
						t.Fatalf("ELF machine = %s, want %s", executable.Machine, target.elf)
					}
					t.Logf("%s: ELF machine %s, mode %04o", binary, executable.Machine, f.Mode().Perm())
				}
			}
			slices.Sort(names)
			if !slices.Equal(names, expected) {
				t.Fatalf("archive entries = %v, want %v", names, expected)
			}
		})
	}
}

func run(t *testing.T, dir, program string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", program, args, err, out)
	}
	if len(out) != 0 {
		t.Logf("%s", out)
	}
}
