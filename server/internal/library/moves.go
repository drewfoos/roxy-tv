package library

import (
	"context"
	"log/slog"

	"github.com/roxy-media/roxy/server/internal/store"
)

// Move is a media file whose old and new paths are both known (an arr app's rename
// notification): OldChain is its old item and that item's parents, read before its record
// was removed. Files whose move is not announced are found through departures (see
// store.SettleDepartures).
type Move struct {
	OldPath  string
	OldChain []store.ChainEntry
	NewPath  string
}

// CarryMoves carries watch history from each move's old item to the item now at its new
// path (see store.CarryChain). Run after the old records are removed and before orphans
// are pruned.
func (l *Library) CarryMoves(ctx context.Context, moves []Move) {
	for _, m := range moves {
		if len(m.OldChain) == 0 {
			continue
		}
		to, err := l.store.ItemForPath(ctx, m.NewPath)
		if err != nil || to == nil {
			continue
		}
		if _, err := l.store.CarryChain(ctx, m.OldChain, to.ID); err != nil {
			slog.Warn("moved: carrying watch history", "old", m.OldPath, "new", m.NewPath, "err", err)
		}
	}
}

// SettleDepartures is settleDepartures for callers outside the scan (the arr webhooks).
func (l *Library) SettleDepartures(ctx context.Context) { l.settleDepartures(ctx) }

// settleDepartures matches files that left the library against files that arrived, and
// carries watch history across (store.SettleDepartures). Failures are logged: history
// that cannot be carried now stays where it is, and the departure waits for a later try.
func (l *Library) settleDepartures(ctx context.Context) {
	if n, err := l.store.SettleDepartures(ctx); err != nil {
		slog.Warn("moved: settling departures", "err", err)
	} else if n > 0 {
		slog.Info("moved: watch history followed files", "count", n)
	}
}
