package library

import (
	"context"

	"github.com/roxy-media/roxy/server/internal/model"
)

// resolveItem is resolveItemHinted with no hint.
func (l *Library) resolveItem(ctx context.Context, lib model.Library, path string) (*model.Item, error) {
	return l.resolveItemHinted(ctx, lib, path, nil)
}
