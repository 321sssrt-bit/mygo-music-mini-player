//go:build linux

package linuxbridge

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const maxArtworkBytes = 16 << 20

type artworkCache struct {
	mu         sync.Mutex
	owner, uri string
	data       []byte
}

func (c *artworkCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.owner, c.uri, c.data = "", "", nil
}

func (c *artworkCache) load(ctx context.Context, owner, uri string) []byte {
	if uri == "" {
		c.clear()
		return nil
	}
	c.mu.Lock()
	if c.owner == owner && c.uri == uri {
		data := bytes.Clone(c.data)
		c.mu.Unlock()
		return data
	}
	c.mu.Unlock()
	data := fetchArtwork(ctx, uri)
	if len(data) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.owner, c.uri, c.data = owner, uri, data
	return bytes.Clone(data)
}

func fetchArtwork(ctx context.Context, uri string) []byte {
	location, err := url.Parse(uri)
	if err != nil {
		return nil
	}
	var body io.ReadCloser
	switch location.Scheme {
	case "file":
		if (location.Host != "" && location.Host != "localhost") || !filepath.IsAbs(location.Path) {
			return nil
		}
		// Opening a special file must not block while checking its type.
		file, err := os.OpenFile(location.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxArtworkBytes {
			file.Close()
			return nil
		}
		body = file
	case "http", "https":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
		if err != nil {
			return nil
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return nil
		}
		if response.StatusCode != http.StatusOK || response.ContentLength > maxArtworkBytes {
			response.Body.Close()
			return nil
		}
		body = response.Body
	default:
		return nil
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxArtworkBytes+1))
	if err != nil || len(data) > maxArtworkBytes {
		return nil
	}
	return data
}
