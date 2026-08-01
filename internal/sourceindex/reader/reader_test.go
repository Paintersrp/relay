package reader

import (
	"context"
	"errors"
	"testing"
)

func TestFallbackCandidatesAreSortedAndCopied(t *testing.T) {
	r := &Reader{fallback: [][]byte{{0xff, 'z'}, []byte("a")}}
	got := r.FallbackCandidates()
	if len(got) != 2 || string(got[0].Path) != "a" || len(got[1].Path) != 2 || got[1].Path[0] != 0xff {
		t.Fatalf("fallback = %#v", got)
	}
	got[0].Path[0] = 'x'
	if string(r.FallbackCandidates()[0].Path) != "a" {
		t.Fatal("fallback path aliases reader state")
	}
}

func TestQueryEligibilityAndClosure(t *testing.T) {
	r := &Reader{}
	for _, literal := range []string{"", "a", "ab", string([]byte{0xff, 0xff, 0xff})} {
		if _, err := r.IndexedTextCandidates(context.Background(), literal); !errors.Is(err, ErrQueryIneligible) {
			t.Fatalf("%q error = %v", literal, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
