package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const userAgent = "RoxyServer/0.1 (+https://github.com/roxy-media/roxy)"

// limiter is a small token bucket: burst tokens, refilled at rate per second.
type limiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	rate   float64
	last   time.Time
}

func newLimiter(burst int, per time.Duration) *limiter {
	return &limiter{tokens: float64(burst), burst: float64(burst), rate: float64(burst) / per.Seconds(), last: time.Now()}
}

// Wait blocks until a token is available or ctx ends.
func (l *limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// httpError is a non-2xx response.
type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Body) }

// getJSON performs one GET (or POST when body != nil) and decodes JSON into out. A 429
// is retried once after Retry-After (capped at 10s).
func getJSON(ctx context.Context, client *http.Client, lim *limiter, method, url string, headers map[string]string, body []byte, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		if lim != nil {
			if err := lim.Wait(ctx); err != nil {
				return err
			}
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rdr)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			// A dropped connection (reset, EOF) gets one retry; the request is idempotent.
			if attempt == 0 {
				continue
			}
			return err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			if attempt == 0 {
				continue
			}
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait := 2 * time.Second
			if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && ra > 0 {
				wait = time.Duration(ra) * time.Second
			}
			if wait > 10*time.Second {
				wait = 10 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			msg := string(data)
			if len(msg) > 200 {
				msg = msg[:200]
			}
			return &httpError{Status: resp.StatusCode, Body: msg}
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
	return fmt.Errorf("rate limited")
}

// isNotFound reports a 404 from the provider.
func isNotFound(err error) bool {
	he, ok := err.(*httpError)
	return ok && he.Status == http.StatusNotFound
}
