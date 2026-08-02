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
	mu            sync.Mutex
	fallback      []reader.Candidate
	query         func(context.Context) ([]reader.Candidate, error)
	queryEntered  chan struct{}
	releaseQuery  <-chan struct{}
	queryReleased chan struct{}
	queries       int
	closeCalls    int
	closeErr      error
	releases      int
}

func (*controlledReader) Descriptor() reader.Descriptor            { return reader.Descriptor{} }
func (r *controlledReader) FallbackCandidates() []reader.Candidate { return r.fallback }
func (r *controlledReader) IndexedTextCandidates(ctx context.Context, _ string) ([]reader.Candidate, error) {
	r.mu.Lock()
	r.queries++
	r.mu.Unlock()
	if r.queryEntered != nil {
		r.queryEntered <- struct{}{}
	}
	if r.releaseQuery != nil {
		<-r.releaseQuery
	}
	if r.queryReleased != nil {
		r.queryReleased <- struct{}{}
	}
	return r.query(ctx)
}
func (r *controlledReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls++
	return r.closeErr
}

func (r *controlledReader) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases++
}

func (r *controlledReader) counts() (queries, closeCalls, releases int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.queries, r.closeCalls, r.releases
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

func TestHandleConcurrentCloseReturnsOneResultAndReleasesOnce(t *testing.T) {
	closeErr := errors.New("close failed")
	r := &controlledReader{closeErr: closeErr, query: func(context.Context) ([]reader.Candidate, error) { return nil, nil }}
	h := &handle{reader: r, timeout: time.Hour, release: r.release}

	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- h.Close()
		}()
	}
	close(start)
	for range callers {
		select {
		case err := <-results:
			if !errors.Is(err, closeErr) {
				t.Fatalf("Close error = %v, want %v", err, closeErr)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent Close blocked")
		}
	}
	if err := h.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("later Close error = %v, want %v", err, closeErr)
	}
	_, closeCalls, releases := r.counts()
	if closeCalls != 1 || releases != 1 {
		t.Fatalf("close calls = %d, ownership releases = %d", closeCalls, releases)
	}
}

func TestHandleCloseMayCompleteWhileQueryIsActive(t *testing.T) {
	releaseQuery := make(chan struct{})
	r := &controlledReader{
		query:         func(context.Context) ([]reader.Candidate, error) { return nil, nil },
		queryEntered:  make(chan struct{}, 1),
		releaseQuery:  releaseQuery,
		queryReleased: make(chan struct{}, 1),
	}
	h := &handle{reader: r, timeout: time.Hour, release: r.release}
	queryDone := make(chan error, 1)
	go func() {
		_, err := h.IndexedTextCandidates(context.Background(), "abc")
		queryDone <- err
	}()
	select {
	case <-r.queryEntered:
	case <-time.After(time.Second):
		t.Fatal("query did not enter reader")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- h.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not complete while query was active")
	}
	close(releaseQuery)
	select {
	case <-r.queryReleased:
	case <-time.After(time.Second):
		t.Fatal("query did not leave reader")
	}
	select {
	case err := <-queryDone:
		if err != nil {
			t.Fatalf("query error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("query did not finish")
	}
	if _, err := h.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, reader.ErrClosed) {
		t.Fatalf("later query error = %v, want %v", err, reader.ErrClosed)
	}
	queries, closeCalls, releases := r.counts()
	if queries != 1 || closeCalls != 1 || releases != 1 {
		t.Fatalf("queries = %d, close calls = %d, ownership releases = %d", queries, closeCalls, releases)
	}
}

func TestHandleCandidateCopyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		candidates []reader.Candidate
	}{
		{name: "nil", candidates: nil},
		{name: "empty", candidates: []reader.Candidate{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &controlledReader{fallback: tc.candidates, query: func(context.Context) ([]reader.Candidate, error) { return tc.candidates, nil }}
			h := &handle{reader: r, timeout: time.Hour, release: r.release}
			fallback := h.FallbackCandidates()
			indexed, err := h.IndexedTextCandidates(context.Background(), "abc")
			if err != nil || len(fallback) != 0 || len(indexed) != 0 {
				t.Fatalf("fallback = %#v, indexed = %#v, error = %v", fallback, indexed, err)
			}
			if len(r.fallback) != 0 || len(tc.candidates) != 0 {
				t.Fatal("reader-owned candidates changed")
			}
		})
	}

	shared := []byte("shared")
	owned := []reader.Candidate{
		{Path: []byte("ordinary")},
		{Path: nil},
		{Path: []byte{}},
		{Path: shared},
		{Path: shared},
	}
	r := &controlledReader{fallback: owned, query: func(context.Context) ([]reader.Candidate, error) { return owned, nil }}
	h := &handle{reader: r, timeout: time.Hour, release: r.release}
	firstFallback := h.FallbackCandidates()
	secondFallback := h.FallbackCandidates()
	firstIndexed, err := h.IndexedTextCandidates(context.Background(), "abc")
	if err != nil {
		t.Fatalf("first indexed error = %v", err)
	}
	secondIndexed, err := h.IndexedTextCandidates(context.Background(), "abc")
	if err != nil {
		t.Fatalf("second indexed error = %v", err)
	}
	firstFallback[0].Path[0] = 'X'
	firstFallback[0], firstFallback[1] = firstFallback[1], firstFallback[0]
	firstFallback[0].Path = []byte("changed")
	firstIndexed[0].Path[0] = 'X'
	firstIndexed[3].Path[0] = 'X'
	if string(firstIndexed[4].Path) != "shared" {
		t.Fatalf("candidates share path memory: %#v", firstIndexed)
	}
	firstIndexed[4].Path[1] = 'Y'
	if string(firstIndexed[3].Path) != "Xhared" {
		t.Fatalf("candidates share path memory: %#v", firstIndexed)
	}
	if string(owned[0].Path) != "ordinary" || string(owned[3].Path) != "shared" || string(owned[4].Path) != "shared" {
		t.Fatalf("reader-owned candidates changed: %#v", owned)
	}
	if string(secondFallback[0].Path) != "ordinary" || string(secondIndexed[0].Path) != "ordinary" || string(secondIndexed[3].Path) != "shared" || string(secondIndexed[4].Path) != "shared" {
		t.Fatalf("separate returned candidates changed: fallback %#v, indexed %#v", secondFallback, secondIndexed)
	}
	if string(firstFallback[0].Path) != "changed" || string(firstIndexed[0].Path) != "Xrdinary" || string(firstIndexed[4].Path) != "sYared" {
		t.Fatalf("mutated candidate paths = fallback %#v, indexed %#v", firstFallback, firstIndexed)
	}
}
