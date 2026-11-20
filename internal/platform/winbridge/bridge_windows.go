//go:build windows && (amd64 || arm64)

// Package winbridge connects the companion to MyGo Music on Windows.
package winbridge

import (
	"sync"
	"time"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

// Bridge implements the Windows host bridge. Use New to select a host data directory.
type Bridge struct {
	dataDir string

	mediaMu sync.Mutex
	mediaAt time.Time
	media   bridge.MediaSnapshot

	audioMu       sync.Mutex
	peakAt        time.Time
	peak          float64
	peakAvailable bool

	hotkeyMu sync.Mutex
	hotkey   *hotkeyBinding
}

var _ bridge.HostBridge = (*Bridge)(nil)

// New uses dataDir for host settings. An empty path uses the installed host's
// per-user directory. Plugin settings are read from plugin-data/mini-player.
func New(dataDir string) *Bridge {
	return &Bridge{dataDir: dataDir}
}
