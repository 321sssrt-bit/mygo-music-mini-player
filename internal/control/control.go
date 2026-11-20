// Package control owns the companion's single-instance lock and loopback protocol.
package control

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrAlreadyRunning means another companion owns the plugin data directory.
var ErrAlreadyRunning = errors.New("mini player already running")

// StateFile is the endpoint discovery file in the plugin data directory.
const StateFile = "control.json"

// Status is the running state exposed by GET /status and successful actions.
type Status struct {
	State         string `json:"state"` // starting, waiting or active
	Visible       bool   `json:"visible"`
	DisplayPolicy string `json:"displayPolicy"` // auto or pinned
}

// Action names an authenticated POST route.
type Action string

const (
	Show          Action = "/show"
	Hide          Action = "/hide"
	ResetPosition Action = "/reset-position"
	Refresh       Action = "/refresh"
)

// Server owns discovery, the listener and the per-directory lock until Close.
type Server struct {
	http     *http.Server
	lock     *os.File
	path     string
	address  string
	token    string
	mu       sync.RWMutex
	status   Status
	closed   bool
	served   chan struct{}
	close    sync.Once
	closeErr error
}

// Start claims a user-private dataDir and listens on 127.0.0.1:0. The discovery
// file contains the HTTP base URL (address), Bearer token and current status.
// handle runs on HTTP goroutines and must finish the action (and Publish any
// resulting status change) before returning. It should honor request cancellation.
func Start(dataDir string, initial Status, handle func(context.Context, Action) error) (*Server, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	// Keep the lock file's identity stable across exits and crashes.
	lock, err := os.OpenFile(filepath.Join(dataDir, "companion.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := tryLock(lock); err != nil {
		lock.Close()
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		lock.Close()
		return nil, err
	}
	s := &Server{
		lock:    lock,
		path:    filepath.Join(dataDir, StateFile),
		address: "http://" + listener.Addr().String(),
		token:   hex.EncodeToString(randBytes()),
		served:  make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		s.writeStatus(w)
	})
	for _, action := range []Action{Show, Hide, ResetPosition, Refresh} {
		mux.HandleFunc("POST "+string(action), func(w http.ResponseWriter, r *http.Request) {
			if err := handle(r.Context(), action); err != nil {
				http.Error(w, "control action failed", http.StatusServiceUnavailable)
				return
			}
			s.writeStatus(w)
		})
	}
	s.http = &http.Server{
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		IdleTimeout:    5 * time.Second,
		MaxHeaderBytes: 8 << 10,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != listener.Addr().String() ||
				subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			mux.ServeHTTP(w, r)
		}),
	}
	if err := s.Publish(initial); err != nil {
		listener.Close()
		lock.Close()
		return nil, err
	}
	go func() {
		defer close(s.served)
		s.http.Serve(listener)
	}()
	return s, nil
}

func randBytes() []byte {
	var token [32]byte
	rand.Read(token[:])
	return token[:]
}

// Publish replaces discovery atomically and updates the HTTP status snapshot.
func (s *Server) Publish(status Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".control-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(struct {
		Address string `json:"address"`
		Token   string `json:"token"`
		Status
	}{s.address, s.token, status})
	if err := errors.Join(err, file.Close()); err != nil {
		return err
	}
	if err := replaceState(file.Name(), s.path); err != nil {
		return err
	}
	s.status = status
	return nil
}

func (s *Server) writeStatus(w http.ResponseWriter) {
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Close drains requests, removes discovery and releases the single-instance lock.
// Call it from outside a request handler.
func (s *Server) Close() error {
	s.close.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.http.Shutdown(ctx)
		if err != nil {
			err = errors.Join(err, s.http.Close())
		}
		<-s.served
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closed = true
		removeErr := removeState(s.path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		s.closeErr = errors.Join(err, removeErr, s.lock.Close())
	})
	return s.closeErr
}
