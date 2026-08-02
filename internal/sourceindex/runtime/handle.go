package sourceindexruntime

import (
	"context"
	"sync"
	"time"

	"relay/internal/sourceindex/reader"
)

type handle struct {
	reader   generationReader
	timeout  time.Duration
	release  func()
	once     sync.Once
	mu       sync.Mutex
	closed   bool
	closeErr error
}

func (h *handle) Descriptor() reader.Descriptor { return h.reader.Descriptor() }
func (h *handle) FallbackCandidates() []reader.Candidate {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	return copyCandidates(h.reader.FallbackCandidates())
}
func (h *handle) IndexedTextCandidates(ctx context.Context, literal string) ([]reader.Candidate, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, reader.ErrClosed
	}
	h.mu.Unlock()
	q, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	candidates, err := h.reader.IndexedTextCandidates(q, literal)
	return copyCandidates(candidates), err
}
func (h *handle) Close() error {
	h.once.Do(func() { h.mu.Lock(); h.closed = true; h.mu.Unlock(); h.closeErr = h.reader.Close(); h.release() })
	return h.closeErr
}

func copyCandidates(in []reader.Candidate) []reader.Candidate {
	out := make([]reader.Candidate, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Path = append([]byte(nil), in[i].Path...)
	}
	return out
}
