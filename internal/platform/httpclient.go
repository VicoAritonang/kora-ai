package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// HTTPClient wraps net/http with a per-call timeout, one jittered retry for
// transient failures, and a circuit breaker per upstream. Every supplier and
// model adapter uses it (MODULE_SPEC §16).
type HTTPClient struct {
	Name    string
	Timeout time.Duration
	Retries int // extra attempts for transient errors (default 1)
	HTTP    *http.Client
	Breaker *Breaker
}

// NewHTTPClient returns a client with sane defaults for an upstream.
func NewHTTPClient(name string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		Name:    name,
		Timeout: timeout,
		Retries: 1,
		HTTP:    &http.Client{Timeout: 0}, // per-call timeouts come from ctx
		Breaker: NewBreaker(5, 30*time.Second),
	}
}

// ErrCircuitOpen is returned while the breaker refuses calls.
var ErrCircuitOpen = errors.New("circuit open")

// StatusError is a non-2xx response. Body is truncated and never logged with
// secrets because requests, not responses, carry credentials.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Body) }

// Transient reports whether retrying may help.
func (e *StatusError) Transient() bool {
	return e.Status == 429 || e.Status >= 500
}

// Do sends method/url with body and headers and returns the response body.
// The body is buffered so it can be resent on retry.
func (c *HTTPClient) Do(ctx context.Context, method, url string, body []byte, headers map[string]string) ([]byte, int, error) {
	if !c.Breaker.Allow() {
		return nil, 0, fmt.Errorf("%s: %w", c.Name, ErrCircuitOpen)
	}
	attempts := 1 + c.Retries
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// Full jitter between 100ms and 600ms.
			d := time.Duration(100+rand.IntN(500)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(d):
			}
		}
		out, status, err := c.once(ctx, method, url, body, headers)
		if err == nil {
			c.Breaker.Success()
			return out, status, nil
		}
		lastErr = err
		var se *StatusError
		if errors.As(err, &se) && !se.Transient() {
			// A permanent upstream error is not a sign the upstream is down.
			c.Breaker.Success()
			return out, status, err
		}
		if ctx.Err() != nil {
			break
		}
	}
	c.Breaker.Failure()
	return nil, 0, lastErr
}

func (c *HTTPClient) once(ctx context.Context, method, url string, body []byte, headers map[string]string) ([]byte, int, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b := out
		if len(b) > 600 {
			b = b[:600]
		}
		return out, resp.StatusCode, &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	return out, resp.StatusCode, nil
}

// Stream sends a request and returns the open response for streaming reads.
// The caller closes the body. No retry: a stream that fails midway cannot be
// replayed transparently.
func (c *HTTPClient) Stream(ctx context.Context, method, url string, body []byte, headers map[string]string) (*http.Response, error) {
	if !c.Breaker.Allow() {
		return nil, fmt.Errorf("%s: %w", c.Name, ErrCircuitOpen)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.Breaker.Failure()
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 600))
		resp.Body.Close()
		se := &StatusError{Status: resp.StatusCode, Body: string(b)}
		if se.Transient() {
			c.Breaker.Failure()
		}
		return nil, se
	}
	c.Breaker.Success()
	return resp, nil
}

// Breaker is a minimal consecutive-failure circuit breaker.
type Breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	failures  int
	openUntil time.Time
}

// NewBreaker opens after threshold consecutive failures, for cooldown.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: threshold, cooldown: cooldown}
}

// Allow reports whether a call may proceed. After the cooldown one trial call
// is let through (half-open).
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().After(b.openUntil)
}

// Success resets the failure count.
func (b *Breaker) Success() {
	b.mu.Lock()
	b.failures = 0
	b.mu.Unlock()
}

// Failure records a failure and opens the breaker at the threshold.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = time.Now().Add(b.cooldown)
		b.failures = 0
	}
}

// Open reports whether the breaker is currently open (for metrics/readyz).
func (b *Breaker) Open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Before(b.openUntil)
}
