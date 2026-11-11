package ninja_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xaroth/pbt-conformance/ninja"
)

// sleeps records the waits a client asks for instead of waiting.
type sleeps []time.Duration

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	*s = append(*s, d)
	return nil
}

func TestClientCaches(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != ninja.UserAgent {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(r.URL.RawQuery))
	}))
	defer srv.Close()
	dir := t.TempDir()
	var s sleeps
	c := &ninja.Client{Base: srv.URL, Dir: dir, Sleep: s.sleep}
	for range 2 {
		b, err := c.Get(t.Context(), "/x", url.Values{"a": {"1"}})
		if err != nil || string(b) != "a=1" {
			t.Fatalf("got %q, %v", b, err)
		}
	}
	if hits.Load() != 1 || c.Requests != 1 {
		t.Errorf("%d hits, %d requests, want 1", hits.Load(), c.Requests)
	}
	// A new client on the same cache makes no request either.
	c2 := &ninja.Client{Base: srv.URL, Dir: dir, Sleep: s.sleep}
	if _, err := c2.Get(t.Context(), "/x", url.Values{"a": {"1"}}); err != nil || c2.Requests != 0 {
		t.Errorf("cached: %d requests, %v", c2.Requests, err)
	}
	if _, err := c.Get(t.Context(), "/x", url.Values{"a": {"2"}}); err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 || s[0] <= 0 || s[0] > 1500*time.Millisecond {
		t.Errorf("waits %v, want one of up to the delay between two requests", s)
	}
}

func TestClientBacksOff(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	statuses := []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		st := statuses[0]
		statuses = statuses[1:]
		mu.Unlock()
		if st == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "30")
		}
		w.WriteHeader(st)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	var s sleeps
	c := &ninja.Client{Base: srv.URL, Dir: t.TempDir(), Sleep: s.sleep, Delay: time.Nanosecond}
	b, err := c.Get(t.Context(), "/x", nil)
	if err != nil || string(b) != "body" {
		t.Fatalf("got %q, %v", b, err)
	}
	var backoffs []time.Duration
	for _, d := range s {
		if d >= time.Second {
			backoffs = append(backoffs, d)
		}
	}
	// Retry-After 30 s, then twice the first backoff of 5 s.
	if want := []time.Duration{30 * time.Second, 10 * time.Second}; len(backoffs) != 2 || backoffs[0] != want[0] || backoffs[1] != want[1] {
		t.Errorf("backoffs %v, want %v", backoffs, want)
	}
}

func TestClientFails(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	var s sleeps
	c := &ninja.Client{Base: srv.URL, Dir: t.TempDir(), Sleep: s.sleep}
	if _, err := c.Get(t.Context(), "/x", nil); !errors.Is(err, ninja.ErrStatus) {
		t.Errorf("got %v, want %v", err, ninja.ErrStatus)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests for a 404, want 1", hits.Load())
	}
}
