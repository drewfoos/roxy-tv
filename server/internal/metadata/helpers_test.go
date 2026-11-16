package metadata

import "net/http"

// isRateLimited reports a 429 that survived the retries.
func isRateLimited(err error) bool {
	he, ok := err.(*httpError)
	return ok && he.Status == http.StatusTooManyRequests
}
