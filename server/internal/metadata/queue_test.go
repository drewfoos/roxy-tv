package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/roxy-media/roxy/server/internal/model"
)

// holdProvider is a provider whose Match waits until it is let go, and says when it was entered.
type holdProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (h *holdProvider) Name() string { return "tmdb" }
func (h *holdProvider) Match(ctx context.Context, it *model.Item, hints Hints) (*Result, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	return nil, nil
}
func (h *holdProvider) Episodes(ctx context.Context, r *Result, n int) (*Season, error) {
	return nil, nil
}

// The Library page reloads its lists while QueueLength is above 0. The worker takes a lookup
// off the queue before working on it, so counted by the queue alone the last lookup (and a
// lone one from its first moment) was "nothing left": the page stopped before the result,
// or, with one title to look up, never started.
func TestQueueLengthCountsTheLookupInHand(t *testing.T) {
	f := newFixture(t, false)
	hold := &holdProvider{entered: make(chan struct{}, 1), release: make(chan struct{})}
	f.svc.providers = []Provider{hold}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.Start(ctx)

	if n := f.svc.QueueLength(); n != 0 {
		t.Fatalf("nothing asked for yet: QueueLength = %d", n)
	}
	f.svc.Refresh(f.series.ID)
	select {
	case <-hold.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never started")
	}
	// The one lookup is in the worker's hands, not in the queue.
	if len(f.svc.queue) != 0 {
		t.Fatalf("the lookup should be off the queue by now, queue has %d", len(f.svc.queue))
	}
	if n := f.svc.QueueLength(); n != 1 {
		t.Fatalf("one lookup is still running: QueueLength = %d, want 1", n)
	}
	close(hold.release)
	deadline := time.Now().Add(5 * time.Second)
	for f.svc.QueueLength() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the lookup finished long ago: QueueLength = %d, want 0", f.svc.QueueLength())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
