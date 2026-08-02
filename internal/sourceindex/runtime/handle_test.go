package sourceindexruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"relay/internal/sourceindex/reader"
)

type controlledReader struct {
	mu         sync.Mutex
	fallback   []reader.Candidate
	query      func(context.Context) ([]reader.Candidate, error)
	queries    int
	closeCalls int
	closeErr   error
}

func (*controlledReader) Descriptor() reader.Descriptor            { return reader.Descriptor{} }
func (r *controlledReader) FallbackCandidates() []reader.Candidate { return r.fallback }
func (r *controlledReader) IndexedTextCandidates(ctx context.Context, _ string) ([]reader.Candidate, error) {
	r.mu.Lock()
	r.queries++
	r.mu.Unlock()
	return r.query(ctx)
}
func (r *controlledReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls++
	return r.closeErr
}

func TestHandleQueryContextPolicy(t *testing.T) {
	t.Run("HANDLE-01 configured timeout wins without caller deadline", func(t *testing.T) {
		seen := make(chan time.Time, 1)
		r := &controlledReader{query: func(ctx context.Context) ([]reader.Candidate, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("query has no deadline")
			}
			seen <- deadline
			return nil, ctx.Err()
		}}
		h := &handle{reader: r, timeout: time.Minute, release: func() {}}
		before := time.Now()
		_, _ = h.IndexedTextCandidates(context.Background(), "abc")
		deadline := <-seen
		if deadline.Before(before.Add(59*time.Second)) || deadline.After(before.Add(61*time.Second)) {
			t.Fatalf("deadline = %v", deadline)
		}
	})
	t.Run("HANDLE-02 earlier caller deadline wins", func(t *testing.T) {
		caller, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
		defer cancel()
		want, _ := caller.Deadline()
		r := &controlledReader{query: func(ctx context.Context) ([]reader.Candidate, error) {
			got, _ := ctx.Deadline()
			if !got.Equal(want) {
				t.Fatalf("deadline = %v, want %v", got, want)
			}
			return nil, nil
		}}
		h := &handle{reader: r, timeout: 2 * time.Hour, release: func() {}}
		_, _ = h.IndexedTextCandidates(caller, "abc")
	})
	t.Run("HANDLE-03 caller cancellation wins", func(t *testing.T) {
		caller, cancel := context.WithCancel(context.Background())
		cancel()
		r := &controlledReader{query: func(ctx context.Context) ([]reader.Candidate, error) { return nil, ctx.Err() }}
		h := &handle{reader: r, timeout: time.Hour, release: func() {}}
		_, err := h.IndexedTextCandidates(caller, "abc")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("HANDLE-04 timeout does not close handle", func(t *testing.T) {
		calls := 0
		r := &controlledReader{query: func(ctx context.Context) ([]reader.Candidate, error) {
			calls++
			if calls == 1 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []reader.Candidate{{Path: []byte("ok")}}, nil
		}}
		h := &handle{reader: r, timeout: time.Nanosecond, release: func() {}}
		if _, err := h.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v", err)
		}
		got, err := h.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || string(got[0].Path) != "ok" {
			t.Fatalf("second query = %q, %v", got[0].Path, err)
		}
	})
}

func TestHandleCopiesCandidatesAndClosesOnce(t *testing.T) {
	t.Run("HANDLE-05 and HANDLE-06 candidates are deeply copied", func(t *testing.T) {
		fallback := []reader.Candidate{{Path: []byte("fallback")}}
		indexed := []reader.Candidate{{Path: []byte("indexed")}}
		r := &controlledReader{fallback: fallback, query: func(context.Context) ([]reader.Candidate, error) { return indexed, nil }}
		h := &handle{reader: r, timeout: time.Hour, release: func() {}}
		gotFallback := h.FallbackCandidates()
		gotFallback[0].Path[0] = 'X'
		gotIndexed, _ := h.IndexedTextCandidates(context.Background(), "abc")
		gotIndexed[0].Path[0] = 'X'
		if string(fallback[0].Path) != "fallback" || string(indexed[0].Path) != "indexed" {
			t.Fatal("reader-owned candidate data was mutated")
		}
	})
	t.Run("HANDLE-07 through HANDLE-09 close result and ownership are once", func(t *testing.T) {
		closeErr := errors.New("close failed")
		releases := 0
		r := &controlledReader{closeErr: closeErr, query: func(context.Context) ([]reader.Candidate, error) { return nil, nil }}
		h := &handle{reader: r, timeout: time.Hour, release: func() { releases++ }}
		if !errors.Is(h.Close(), closeErr) || !errors.Is(h.Close(), closeErr) {
			t.Fatal("repeated Close did not preserve error")
		}
		if r.closeCalls != 1 || releases != 1 {
			t.Fatalf("close calls = %d, releases = %d", r.closeCalls, releases)
		}
	})
	t.Run("HANDLE-10 query after Close is rejected", func(t *testing.T) {
		r := &controlledReader{query: func(context.Context) ([]reader.Candidate, error) { return nil, nil }}
		h := &handle{reader: r, timeout: time.Hour, release: func() {}}
		_ = h.Close()
		_, err := h.IndexedTextCandidates(context.Background(), "abc")
		if !errors.Is(err, reader.ErrClosed) || r.queries != 0 {
			t.Fatalf("error = %v, queries = %d", err, r.queries)
		}
	})
}
