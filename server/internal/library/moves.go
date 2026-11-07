package library

import (
	"context"
	"log/slog"
	"math"
	"path/filepath"
	"strings"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Move is a media file that left one path and turned up at another: a folder renamed, a
// file moved, a provider id added to a name. Its identity comes from its path (or the id
// in it), so at the new path it is a new item; OldItem is the one it had.
type Move struct {
	OldPath string
	OldItem *model.Item
	NewPath string
}

// CarryMoves moves what people recorded about each moved file's old item (watch position,
// watched, plays, watchlist, stream choices, hand-picked artwork) to its new item, and the
// same up the tree (season to season, series to series) for an old parent left with no
// files. An old item that still has a file (one version of a film moved, the other not)
// keeps its data. Run after the old paths' records are removed and before orphans are
// pruned: the old items must still exist.
func (l *Library) CarryMoves(ctx context.Context, moves []Move) {
	done := map[[2]string]bool{}
	for _, m := range moves {
		if m.OldItem == nil {
			continue
		}
		to, err := l.store.ItemForPath(ctx, m.NewPath)
		if err != nil || to == nil {
			continue
		}
		from := m.OldItem
		for from != nil && to != nil && from.ID != to.ID && from.Type == to.Type {
			key := [2]string{from.ID, to.ID}
			if done[key] {
				break
			}
			done[key] = true
			if has, err := l.store.HasMediaUnder(ctx, from.ID); err != nil || has {
				break // still has files (and so do its parents): nothing moved away from it
			}
			if err := l.store.CarryOver(ctx, from.ID, to.ID); err != nil {
				slog.Warn("carrying watch history to a moved file's new item", "from", from.Title, "to", to.Title, "err", err)
				break
			}
			slog.Info("moved: watch history carried over", "type", from.Type, "from", from.Title, "to", to.Title, "old", m.OldPath, "new", m.NewPath)
			if from.ParentID == nil || to.ParentID == nil {
				break
			}
			if from, err = l.store.GetItem(ctx, *from.ParentID); err != nil {
				break
			}
			if to, err = l.store.GetItem(ctx, *to.ParentID); err != nil {
				break
			}
		}
	}
}

// detectMoves pairs the files a scan found gone (vanished: path -> source) with the files
// it added (added: size -> paths) that are the same file somewhere else: the same size
// and extension, the size held by exactly one gone and one new file (a size shared by
// several is not guessed at), and, when both durations are known, the same duration.
func (l *Library) detectMoves(ctx context.Context, vanished []string, added map[int64][]string) []Move {
	if len(vanished) == 0 || len(added) == 0 {
		return nil
	}
	type gone struct {
		path     string
		duration int64
	}
	bySize := map[int64][]gone{}
	for _, p := range vanished {
		part, err := l.store.GetPartByPath(ctx, p)
		if err != nil || part == nil || part.Size <= 0 {
			continue
		}
		bySize[part.Size] = append(bySize[part.Size], gone{p, part.DurationMs})
	}
	var out []Move
	for size, olds := range bySize {
		news := added[size]
		if len(olds) != 1 || len(news) != 1 {
			continue
		}
		old, nw := olds[0], news[0]
		if !strings.EqualFold(filepath.Ext(old.path), filepath.Ext(nw)) {
			continue
		}
		if np, err := l.store.GetPartByPath(ctx, nw); err == nil && np != nil && np.DurationMs > 0 && old.duration > 0 &&
			math.Abs(float64(np.DurationMs-old.duration)) > 2000 {
			continue
		}
		item, err := l.store.ItemForPath(ctx, old.path)
		if err != nil || item == nil {
			continue
		}
		out = append(out, Move{OldPath: old.path, OldItem: item, NewPath: nw})
	}
	return out
}
