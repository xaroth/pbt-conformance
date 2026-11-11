// Package ninja reads Path of Exile builds from poe.ninja's builds API and picks test builds
// from them.
package ninja

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// UserAgent names the tool to poe.ninja.
const UserAgent = "pbt-conformance/1 (test build picker; +https://github.com/xaroth/pbt-conformance)"

// BaseURL is poe.ninja's address.
const BaseURL = "https://poe.ninja"

// Client defaults.
const (
	defaultDelay   = 1500 * time.Millisecond
	defaultBackoff = 5 * time.Second
	maxAttempts    = 6
	dirPerm        = 0o750
	filePerm       = 0o600
)

// ErrStatus marks a response with a status the client does not retry.
var ErrStatus = errors.New("unexpected status")

// Client gets poe.ninja responses one at a time and caches each body on disk by its URL, so a
// later run makes no request it already made.
type Client struct {
	// Base is the address requests go to; BaseURL when empty.
	Base string
	// Dir holds the cached responses.
	Dir string
	// HTTP makes the requests; http.DefaultClient when nil.
	HTTP *http.Client
	// Delay is the least time between two requests; 1.5 s when zero.
	Delay time.Duration
	// Backoff is the first wait after a 429 or 5xx response, doubled on each retry, unless the
	// response asks for longer; 5 s when zero.
	Backoff time.Duration
	// Sleep waits; a context-aware sleep when nil. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Requests counts the requests made.
	Requests int

	last time.Time
}

// Get returns the body at path with query q, from the cache if there, else from poe.ninja.
func (c *Client) Get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	sum := sha256.Sum256([]byte(path + "?" + q.Encode()))
	file := filepath.Join(c.Dir, hex.EncodeToString(sum[:]))
	b, err := os.ReadFile(file)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	if b, err = c.fetch(ctx, u); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.Dir, dirPerm); err != nil {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, filePerm); err != nil {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return nil, fmt.Errorf("ninja: %w", err)
	}
	return b, nil
}

// fetch requests u, waiting out the delay since the last request and backing off on 429 and
// 5xx.
func (c *Client) fetch(ctx context.Context, u string) ([]byte, error) {
	backoff := c.Backoff
	if backoff == 0 {
		backoff = defaultBackoff
	}
	for attempt := 1; ; attempt++ {
		if err := c.pace(ctx); err != nil {
			return nil, err
		}
		b, status, wait, err := c.once(ctx, u)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusOK:
			return b, nil
		case status != http.StatusTooManyRequests && status < http.StatusInternalServerError,
			attempt == maxAttempts:
			return nil, fmt.Errorf("ninja: %s: %w %d", u, ErrStatus, status)
		}
		if err := c.sleep(ctx, max(wait, backoff)); err != nil {
			return nil, err
		}
		backoff *= 2
	}
}

// pace waits until the delay since the last request has passed.
func (c *Client) pace(ctx context.Context) error {
	delay := c.Delay
	if delay == 0 {
		delay = defaultDelay
	}
	if !c.last.IsZero() {
		if err := c.sleep(ctx, delay-time.Since(c.last)); err != nil {
			return err
		}
	}
	c.last = time.Now()
	return nil
}

// once makes one request and returns its body, status and the wait its Retry-After asks for.
func (c *Client) once(ctx context.Context, u string) ([]byte, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ninja: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	c.Requests++
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ninja: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ninja: %w", err)
	}
	var wait time.Duration
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		wait = time.Duration(s) * time.Second
	}
	return b, resp.StatusCode, wait, nil
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("ninja: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
