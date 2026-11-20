package lifecycle_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/321sssrt-bit/mygo-music-mini-player/internal/lifecycle"
)

func run(t *testing.T, c *lifecycle.Controller) (context.Context, <-chan lifecycle.State, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	states := make(chan lifecycle.State, 8)
	done := make(chan error, 1)
	exited := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-exited
	})
	go func() {
		defer close(exited)
		done <- c.Run(ctx, func(state lifecycle.State) error {
			states <- state
			return nil
		})
	}()
	return ctx, states, done
}

func expectState(t *testing.T, ctx context.Context, states <-chan lifecycle.State, want lifecycle.State) {
	t.Helper()
	select {
	case got := <-states:
		if got != want {
			t.Fatalf("transition = %s, want %s", got, want)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for %s: %v", want, ctx.Err())
	}
}

func expectExit(t *testing.T, ctx context.Context, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCompanionStopsWhenHostExits(t *testing.T) {
	var host bridge.Fake
	host.SetSnapshot(bridge.FakeSnapshot{Host: bridge.HostState{Running: true, Window: bridge.WindowUnknown}})
	c := lifecycle.New(&host, false, []string{"mini-player", "--guard"})
	ctx, states, done := run(t, c)
	expectState(t, ctx, states, lifecycle.Active)
	host.SetSnapshot(bridge.FakeSnapshot{})
	expectState(t, ctx, states, lifecycle.Stopped)
	expectExit(t, ctx, done)
}

type transientObservation struct {
	bridge.Fake
	first bool
}

func (h *transientObservation) ObserveHost() (bridge.HostState, error) {
	if !h.first {
		h.first = true
		return bridge.HostState{}, errors.New("temporary host observation failure")
	}
	return h.Fake.ObserveHost()
}

func TestInitialObservationFailureDoesNotStopCompanion(t *testing.T) {
	var host transientObservation
	host.SetSnapshot(bridge.FakeSnapshot{Host: bridge.HostState{Running: true}})
	c := lifecycle.New(&host, false, nil)
	ctx, states, done := run(t, c)
	expectState(t, ctx, states, lifecycle.Active)
	host.SetSnapshot(bridge.FakeSnapshot{})
	expectState(t, ctx, states, lifecycle.Stopped)
	expectExit(t, ctx, done)
}

func TestRefreshReturnsDuringFinalCleanup(t *testing.T) {
	var host bridge.Fake
	c := lifecycle.New(&host, false, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := c.Run(ctx, func(state lifecycle.State) error {
		if state != lifecycle.Stopped {
			t.Fatalf("unexpected transition %s", state)
		}
		if err := c.Refresh(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("refresh while stopping = %v, want stopped error", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardLifecycle(t *testing.T) {
	var host bridge.Fake
	command := []string{"mini-player", "--guard"}
	c := lifecycle.New(&host, true, command)
	ctx, states, done := run(t, c)
	expectState(t, ctx, states, lifecycle.Waiting)

	snapshot := bridge.FakeSnapshot{Host: bridge.HostState{Running: true, Window: bridge.WindowHidden}}
	host.SetSnapshot(snapshot)
	expectState(t, ctx, states, lifecycle.Active)

	snapshot.Settings.Plugin.GuardMode = true
	host.SetSnapshot(snapshot)
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := host.Effects().LoginStartup; !slices.Equal(got, command) {
		t.Fatalf("registered command = %v, want %v", got, command)
	}

	snapshot.Host = bridge.HostState{}
	host.SetSnapshot(snapshot)
	expectState(t, ctx, states, lifecycle.Waiting)

	snapshot.Settings.Plugin.GuardMode = false
	host.SetSnapshot(snapshot)
	if err := c.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := host.Effects().LoginStartup; len(got) != 0 {
		t.Fatalf("disabled guard left startup command %v", got)
	}
	expectState(t, ctx, states, lifecycle.Stopped)
	expectExit(t, ctx, done)
}

type failedStartupUpdate struct {
	bridge.Fake
}

var errRemoveStartup = errors.New("cannot remove login startup")

func (h *failedStartupUpdate) SetLoginStartup(argv []string) error {
	if len(argv) == 0 {
		return errRemoveStartup
	}
	return h.Fake.SetLoginStartup(argv)
}

func TestFailedGuardRefreshKeepsWaiting(t *testing.T) {
	var host failedStartupUpdate
	snapshot := bridge.FakeSnapshot{Settings: bridge.SettingsSnapshot{Plugin: bridge.PluginSettings{GuardMode: true}}}
	host.SetSnapshot(snapshot)
	command := []string{"mini-player", "--guard"}
	c := lifecycle.New(&host, false, command)
	ctx, states, _ := run(t, c)
	expectState(t, ctx, states, lifecycle.Waiting)
	snapshot.Settings.Plugin.GuardMode = false
	host.SetSnapshot(snapshot)
	if err := c.Refresh(ctx); !errors.Is(err, errRemoveStartup) {
		t.Fatalf("refresh error = %v, want %v", err, errRemoveStartup)
	}
	if got := host.Effects().LoginStartup; !slices.Equal(got, command) {
		t.Fatalf("failed removal changed startup command to %v", got)
	}
	snapshot.Host.Running = true
	host.SetSnapshot(snapshot)
	expectState(t, ctx, states, lifecycle.Active)
}
