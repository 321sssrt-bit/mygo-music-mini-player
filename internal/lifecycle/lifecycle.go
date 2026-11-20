// Package lifecycle manages the companion's attachment to MyGo Music.
package lifecycle

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

// State describes whether the companion should own host-related resources.
type State string

const (
	Active  State = "active"
	Waiting State = "waiting"
	Stopped State = "stopped"
)

const (
	activeInterval = 250 * time.Millisecond
	guardInterval  = 2 * time.Second
)

var errStopped = errors.New("companion lifecycle stopped")

type refreshRequest struct {
	ctx    context.Context
	result chan error
}

// Controller serializes host observation and guard mode changes.
type Controller struct {
	host         bridge.HostBridge
	loginCommand []string
	launchGuard  bool
	refresh      chan refreshRequest
	done         chan struct{}
}

// New prepares a single run. loginCommand is the executable and its arguments
// (including the guard launch argument) to use at the next login.
func New(host bridge.HostBridge, launchGuard bool, loginCommand []string) *Controller {
	return &Controller{
		host:         host,
		loginCommand: slices.Clone(loginCommand),
		launchGuard:  launchGuard,
		refresh:      make(chan refreshRequest),
		done:         make(chan struct{}),
	}
}

// Run blocks until the unguarded host is absent, the context is canceled, or
// setup or a transition fails. Call Run once per Controller. apply runs serially:
// Active attaches host resources, Waiting releases them, and Stopped performs
// final cleanup. apply must finish the transition before returning.
func (c *Controller) Run(ctx context.Context, apply func(State) error) (err error) {
	defer func() {
		close(c.done)
		err = errors.Join(err, apply(Stopped))
	}()
	if ctx.Err() != nil {
		return nil
	}
	configuredGuard, err := c.syncLoginStartup()
	if err != nil {
		return err
	}
	guard := c.launchGuard || configuredGuard
	var state State
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-c.refresh:
			if request.ctx.Err() != nil {
				continue
			}
			configuredGuard, err := c.syncLoginStartup()
			if err == nil {
				guard = configuredGuard
				timer.Reset(0)
			}
			request.result <- err
		case <-timer.C:
			host, observeErr := c.host.ObserveHost()
			if observeErr == nil {
				next := Active
				if !host.Running {
					if !guard {
						return nil
					}
					next = Waiting
				}
				if next != state {
					state = next
					if err := apply(state); err != nil {
						return err
					}
				}
			}
			interval := activeInterval
			if state == Waiting || (state == "" && guard) {
				interval = guardInterval
			}
			timer.Reset(interval)
		}
	}
}

// Refresh reads persisted settings and synchronizes the login startup entry.
// A successful refresh supersedes the guard launch argument for this run.
func (c *Controller) Refresh(ctx context.Context) error {
	request := refreshRequest{ctx: ctx, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errStopped
	case c.refresh <- request:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-request.result:
		return err
	}
}

func (c *Controller) syncLoginStartup() (bool, error) {
	settings, err := c.host.Settings()
	if err != nil {
		return false, err
	}
	var command []string
	if settings.Plugin.GuardMode {
		command = c.loginCommand
	}
	return settings.Plugin.GuardMode, c.host.SetLoginStartup(command)
}
