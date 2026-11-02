package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	// until holds every caller back after the server said "slow down" (a 429 with
	// Retry-After), so the other requests sharing the limiter do not hit it too.
	until time.Time
}

func newLimiter(burst int, per time.Duration) *limiter {
	return &limiter{tokens: float64(burst), burst: float64(burst), rate: float64(burst) / per.Seconds(), last: time.Now()}
}

// Pause stops handing out tokens for d (extends, never shortens, a pause in force).
func (l *limiter) Pause(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t := time.Now().Add(d); t.After(l.until) {
		l.until = t
	}
	l.tokens = 0
}

// Wait blocks until a token is available or ctx ends.
func (l *limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		if now.Before(l.until) {
			wait := l.until.Sub(now)
			l.last = l.until
			l.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
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

// Retry policy for getJSON: a dropped connection is retried once; a 429 up to
// maxRateRetries times, each after the server's Retry-After (AniList sends about a
// minute when it is in its degraded 30/min mode), capped at maxRetryAfter.
const (
	maxRateRetries = 2
	maxRetryAfter  = 65 * time.Second
)

// getJSON performs one GET (or POST when body != nil) and decodes JSON into out. A 429
// pauses the limiter (every caller sharing it waits) for Retry-After and tries again;
// when the retries run out the 429 comes back as an *httpError.
func getJSON(ctx context.Context, client *http.Client, lim *limiter, method, url string, headers map[string]string, body []byte, out any) error {
	netRetries, rateRetries := 0, 0
	for {
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
			if netRetries == 0 && ctx.Err() == nil {
				netRetries++
				continue
			}
			return err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			if netRetries == 0 && ctx.Err() == nil {
				netRetries++
				continue
			}
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && rateRetries < maxRateRetries {
			rateRetries++
			wait := retryAfter(resp.Header.Get("Retry-After"))
			if lim != nil {
				lim.Pause(wait) // the next Wait sits it out, with everyone else
				continue
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
}

// retryAfter reads a Retry-After header (seconds, or an HTTP date); 5s when absent.
func retryAfter(h string) time.Duration {
	wait := 5 * time.Second
	if ra, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && ra >= 0 {
		wait = time.Duration(ra) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		wait = time.Until(t)
	}
	if wait < time.Second {
		wait = time.Second
	}
	if wait > maxRetryAfter {
		wait = maxRetryAfter
	}
	return wait
}

// isRateLimited reports a 429 that survived the retries.
func isRateLimited(err error) bool {
	he, ok := err.(*httpError)
	return ok && he.Status == http.StatusTooManyRequests
}

// isNotFound reports a 404 from the provider.
func isNotFound(err error) bool {
	he, ok := err.(*httpError)
	return ok && he.Status == http.StatusNotFound
}
