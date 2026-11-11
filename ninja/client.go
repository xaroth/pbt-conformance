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
	defaultTimeout = time.Minute
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
	// HTTP makes the requests; a client with a one-minute timeout when nil.
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

// Get returns the body at path with query q, from the cache if there, else from poe.ninja. A
// response with a 4xx status other than 429, such as a 404 for a removed character, is cached
// as that status and returned as ErrStatus on every later call; a 429 or 5xx that outlasts the
// retries is not cached.
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
	if b, ok, err := cached(file, u); ok || err != nil {
		return b, err
	}
	b, status, err := c.fetch(ctx, u)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		if err := writeAtomic(file+".status", []byte(strconv.Itoa(status))); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ninja: %s: %w %d", u, ErrStatus, status)
	}
	if err := writeAtomic(file, b); err != nil {
		return nil, err
	}
	return b, nil
}

// cached returns the cached response at file, the body or the failure, and whether there is
// one.
func cached(file, u string) ([]byte, bool, error) {
	b, err := os.ReadFile(file)
	if err == nil {
		return b, true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false, fmt.Errorf("ninja: %w", err)
	}
	st, err := os.ReadFile(file + ".status")
	if err == nil {
		return nil, true, fmt.Errorf("ninja: %s: %w %s (cached)", u, ErrStatus, st)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, false, fmt.Errorf("ninja: %w", err)
	}
	return nil, false, nil
}

// writeAtomic writes b to path through a temporary file, so an interrupted write leaves no
// partial file at path.
func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, filePerm); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("ninja: %w", err)
	}
	return nil
}

// fetch requests u, waiting out the delay since the last request and backing off on 429 and
// 5xx. It returns the body of a 200 response, or the status of a 4xx response other than 429,
// which is not retried.
func (c *Client) fetch(ctx context.Context, u string) ([]byte, int, error) {
	backoff := c.Backoff
	if backoff == 0 {
		backoff = defaultBackoff
	}
	for attempt := 1; ; attempt++ {
		b, status, wait, err := c.once(ctx, u)
		if err != nil {
			return nil, 0, err
		}
		transient := status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
		if final(status, transient) {
			return b, status, nil
		}
		if !transient || attempt == maxAttempts {
			return nil, 0, fmt.Errorf("ninja: %s: %w %d", u, ErrStatus, status)
		}
		if err := c.sleep(ctx, max(wait, backoff)); err != nil {
			return nil, 0, err
		}
		backoff *= 2
	}
}

// final reports whether a response is kept as it is: a 200, or a 4xx that is not transient.
func final(status int, transient bool) bool {
	return status == http.StatusOK || (status >= http.StatusBadRequest && !transient)
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

// once waits out the delay since the last request, makes one request and returns its body,
// status and the wait its Retry-After asks for.
func (c *Client) once(ctx context.Context, u string) ([]byte, int, time.Duration, error) {
	if err := c.pace(ctx); err != nil {
		return nil, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ninja: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
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
