//go:build !linux

package supervisor

import (
	"context"
	"errors"
	"relay/internal/sourceindex/indexerprotocol"
)

// Process-group ownership and non-reaping exit observation are Linux-only.
func (s *Supervisor) runChild(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
	return indexerprotocol.BuildResponse{}, "indexer_start_failed", "source-index process supervision is unsupported", errors.New("source-index process supervision is unsupported")
}
