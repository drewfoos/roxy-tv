package api

import "context"

// playedLength is the length of the version a report was made from (playedVersion).
func (s *Server) playedLength(ctx context.Context, itemID, sourceID string, reportedMs int64) (int64, bool) {
	if v := s.playedVersion(ctx, itemID, sourceID, reportedMs); v != nil {
		return v.DurationMs, true
	}
	return 0, false
}
