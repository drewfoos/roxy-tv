package api

import (
	"testing"
	"time"
)

func TestStaleReport(t *testing.T) {
	finished := time.Date(2026, 10, 27, 23, 50, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
		last time.Time
		want bool
	}{
		{"offline position from before the episode was finished", finished.Add(-2 * time.Hour), finished, true},
		{"offline position after the latest activity", finished.Add(time.Minute), finished, false},
		{"nothing recorded yet", finished, time.Time{}, false},
		{"live report (no time sent)", time.Time{}, finished, false},
	} {
		at := int64(0)
		if !tc.at.IsZero() {
			at = tc.at.UnixMilli()
		}
		if got := staleReport(at, tc.last); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
