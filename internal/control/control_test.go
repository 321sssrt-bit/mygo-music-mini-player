package control_test

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/control"
)

type discovery struct {
	Address string
	Token   string
	control.Status
}

func readDiscovery(t *testing.T, dir string) discovery {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, control.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	var entry discovery
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	token, err := hex.DecodeString(entry.Token)
	if err != nil || len(token) != 32 || !strings.HasPrefix(entry.Address, "http://127.0.0.1:") {
		t.Fatal("invalid discovery address or token format")
	}
	return entry
}

func TestLoopbackControl(t *testing.T) {
	dir := t.TempDir()
	initial := control.Status{State: "active", DisplayPolicy: "auto"}
	visible := initial
	visible.Visible = true
	refreshed := initial
	refreshed.DisplayPolicy = "pinned"
	actions := make(chan control.Action, 4)
	server, err := control.Start(dir, initial, func(_ context.Context, server *control.Server, action control.Action) error {
		actions <- action
		switch action {
		case control.Show:
			return server.Publish(visible)
		case control.Hide:
			return server.Publish(initial)
		case control.Refresh:
			return server.Publish(refreshed)
		default:
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	entry := readDiscovery(t, dir)
	if entry.Status != initial {
		t.Fatalf("discovery status = %+v", entry.Status)
	}
	t.Logf("random loopback endpoint %s, token length 256 bits", entry.Address)

	client := &http.Client{Timeout: 3 * time.Second}
	for _, tc := range []struct {
		method, route, token, host string
		code                       int
		status                     control.Status
		action                     control.Action
	}{
		{method: "GET", route: "/status", code: 401},
		{method: "POST", route: "/show", token: "wrong", code: 401},
		{method: "GET", route: "/status", token: entry.Token, host: "another.invalid", code: 401},
		{method: "GET", route: "/show", token: entry.Token, code: 405},
		{method: "GET", route: "/status", token: entry.Token, code: 200, status: initial},
		{method: "POST", route: "/show", token: entry.Token, code: 200, status: visible, action: control.Show},
		{method: "POST", route: "/hide", token: entry.Token, code: 200, status: initial, action: control.Hide},
		{method: "POST", route: "/reset-position", token: entry.Token, code: 200, status: initial, action: control.ResetPosition},
		{method: "POST", route: "/refresh", token: entry.Token, code: 200, status: refreshed, action: control.Refresh},
	} {
		req, err := http.NewRequest(tc.method, entry.Address+tc.route, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		if tc.host != "" {
			req.Host = tc.host
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.code || strings.Contains(string(body), entry.Token) {
			t.Fatalf("%s %s: HTTP %d, want %d and no token", tc.method, tc.route, resp.StatusCode, tc.code)
		}
		var action control.Action
		select {
		case action = <-actions:
		default:
		}
		if action != tc.action {
			t.Fatalf("%s %s dispatched %q, want %q", tc.method, tc.route, action, tc.action)
		}
		if tc.code == 200 {
			var got control.Status
			if err := json.Unmarshal(body, &got); err != nil || got != tc.status {
				t.Fatalf("status = %+v (%v), want %+v", got, err, tc.status)
			}
			if persisted := readDiscovery(t, dir); persisted.Status != tc.status || persisted.Token != entry.Token || persisted.Address != entry.Address {
				t.Fatal("discovery did not match the returned status or changed credentials")
			}
			t.Logf("%s %s -> %d %+v", tc.method, tc.route, resp.StatusCode, got)
		} else {
			t.Logf("%s %s -> %d", tc.method, tc.route, resp.StatusCode)
		}
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, control.StateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed server left discovery: %v", err)
	}
	if resp, err := client.Get(entry.Address + "/status"); err == nil {
		resp.Body.Close()
		t.Fatal("closed server still accepts requests")
	}
	t.Log("close removed discovery and stopped the listener")
}

func TestPublishOverlappingDiscoveryRead(t *testing.T) {
	dir := t.TempDir()
	initial := control.Status{State: "waiting", DisplayPolicy: "auto"}
	s, err := control.Start(dir, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	oldFile, err := os.Open(filepath.Join(dir, control.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	var old discovery
	go func() {
		defer close(readDone)
		time.Sleep(30 * time.Millisecond)
		err := json.NewDecoder(oldFile).Decode(&old)
		readDone <- errors.Join(err, oldFile.Close())
	}()
	next := control.Status{State: "active", DisplayPolicy: "pinned", Visible: true}
	publishErr := s.Publish(next)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	if publishErr != nil {
		t.Fatal(publishErr)
	}
	if current := readDiscovery(t, dir); old.Status != initial || current.Status != next || old.Token != current.Token {
		t.Fatal("discovery reader did not get a complete old or new snapshot")
	}
	t.Logf("open handle saw %+v; a fresh reader saw %+v", old.Status, next)

	currentFile, err := os.Open(filepath.Join(dir, control.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		released <- currentFile.Close()
	}()
	if err := errors.Join(s.Close(), <-released); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, control.StateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a concurrent reader prevented discovery cleanup: %v", err)
	}
	t.Log("close removed discovery despite an overlapping reader")
}

func TestSingleInstanceAcrossProcesses(t *testing.T) {
	const childDir = "MINI_PLAYER_TEST_LOCK_DIR"
	if dir := os.Getenv(childDir); dir != "" {
		s, err := control.Start(dir, control.Status{State: "waiting"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		fmt.Println("ready")
		io.Copy(io.Discard, os.Stdin)
		return
	}

	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSingleInstanceAcrossProcesses$")
	cmd.Env = append(os.Environ(), childDir+"="+dir)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		if cmd.ProcessState == nil {
			cmd.Wait()
		}
	}()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("child failed before acquiring the lock: %q, %v", line, err)
	}
	first := readDiscovery(t, dir)
	if duplicate, err := control.Start(dir, control.Status{}, nil); !errors.Is(err, control.ErrAlreadyRunning) {
		if duplicate != nil {
			duplicate.Close()
		}
		t.Fatalf("duplicate start: got %v, want ErrAlreadyRunning", err)
	}
	if readDiscovery(t, dir) != first {
		t.Fatal("duplicate start changed the active instance's discovery")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()

	for i := 0; i < 2; i++ {
		s, err := control.Start(dir, control.Status{State: "active"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if readDiscovery(t, dir).Token == first.Token {
			s.Close()
			t.Fatal("replacement process reused the previous token")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("duplicate start returned ErrAlreadyRunning, discovery stayed unchanged, restart succeeded after process kill and clean close")
}
