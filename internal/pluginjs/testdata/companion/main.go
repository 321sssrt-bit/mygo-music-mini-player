package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/control"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) < 3 || args[1] != "--data-dir" {
		return errors.New("expected discover or serve with --data-dir")
	}
	dir := args[2]
	statePath := filepath.Join(dir, control.StateFile)
	printState := func() error {
		data, err := os.ReadFile(statePath)
		if err != nil {
			return err
		}
		if !json.Valid(data) {
			return errors.New("invalid discovery JSON")
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	switch strings.Join(append([]string{args[0]}, args[3:]...), " ") {
	case "discover":
		return printState()
	case "serve --guard --detach":
		cmd := exec.Command(os.Args[0], "serve", "--data-dir", dir, "--guard")
		output, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		defer cmd.Process.Release()
		defer output.Close()
		line, err := bufio.NewReader(output).ReadString('\n')
		if err != nil {
			return err
		}
		fmt.Print(line)
		return nil
	case "serve", "serve --guard":
	default:
		return errors.New("unexpected companion command")
	}

	status := control.Status{State: "active", DisplayPolicy: "auto"}
	if len(args) == 4 {
		status.State = "waiting"
	}
	refresh := func() error {
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil {
			return err
		}
		var settings map[string]string
		if err := json.Unmarshal(data, &settings); err != nil {
			return err
		}
		if settings["displayPolicy"] == "pinned" {
			status.DisplayPolicy = "pinned"
		} else {
			status.DisplayPolicy = "auto"
		}
		return nil
	}
	if err := refresh(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	server, err := control.Start(dir, status, func(_ context.Context, server *control.Server, action control.Action) error {
		switch action {
		case control.Show:
			status.Visible = true
		case control.Hide:
			status.Visible = false
		case control.ResetPosition:
			status.Visible = true
		case control.Refresh:
			if err := refresh(); err != nil {
				return err
			}
		}
		trace, err := os.OpenFile(filepath.Join(dir, "actions"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(trace, action)
		if err := errors.Join(err, trace.Close()); err != nil {
			return err
		}
		return server.Publish(status)
	})
	if errors.Is(err, control.ErrAlreadyRunning) {
		return printState()
	}
	if err != nil {
		return err
	}
	defer server.Close()
	if err := printState(); err != nil {
		return err
	}
	// Bound the lifetime of the detached test fixture even if the runner crashes.
	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			return nil
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
				return nil
			}
		}
	}
}
