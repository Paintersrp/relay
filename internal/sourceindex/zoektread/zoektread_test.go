//go:build linux || darwin || freebsd || netbsd

package zoektread

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/sourceindex/zoektbuild"
)

func TestSearchUsesCaseSensitiveContentSubstring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "000000.zoekt")
	generation := strings.Repeat("a", 64)
	meta := Metadata{RepositoryName: "relay-generation/" + generation, Branch: "relay-revision", Version: strings.Repeat("b", 40), IndexOptions: strings.Repeat("c", 64), Values: map[string]string{"relay_generation_id": generation}}
	if err := zoektbuild.Write(path, generation, 0, zoektbuild.Metadata{RepositoryName: meta.RepositoryName, Branch: meta.Branch, Version: meta.Version, IndexOptions: meta.IndexOptions, Values: meta.Values}, []zoektbuild.Document{{Name: "literal-in-name", Content: []byte("lowercase content")}, {Name: "content", Content: []byte("Needle needle")}}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(f, generation, 0, meta)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Search(context.Background(), "Needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 1 || got.Matches[0].FileName != "content" {
		t.Fatalf("matches = %#v", got.Matches)
	}
	got, err = r.Search(context.Background(), "needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 0 {
		t.Fatalf("case-sensitive content matches = %#v", got.Matches)
	}
}
