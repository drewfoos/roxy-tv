package auth

import (
	"context"

	"github.com/roxy-media/roxy/server/internal/model"
)

// Resolve is ResolveFrom with no address.
func (s *Service) Resolve(ctx context.Context, token string) (*model.Principal, error) {
	return s.ResolveFrom(ctx, token, "")
}
