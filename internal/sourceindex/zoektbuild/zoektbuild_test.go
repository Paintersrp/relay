package zoektbuild

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteVerifyAndEnumerateShard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pinned Zoekt builder is unsupported on Windows")
	}
	metadata := Metadata{
		RepositoryName: "relay-generation/" + strings.Repeat("a", 64), Branch: "relay-revision", Version: strings.Repeat("b", 40),
		IndexOptions: strings.Repeat("c", 64), Values: map[string]string{"relay_generation_id": strings.Repeat("a", 64), "relay_vault_id": "vault"},
	}
	path := filepath.Join(t.TempDir(), "000000.zoekt")
	if err := Write(path, strings.Repeat("a", 64), 0, metadata, []Document{{Name: "a.txt", Content: []byte("alpha beta")}, {Name: "dir/b.txt", Content: []byte("gamma delta")}}); err != nil {
		t.Fatal(err)
	}
	if err := Verify(path, strings.Repeat("a", 64), 0, metadata); err != nil {
		t.Fatal(err)
	}
	docs, err := Documents(path, strings.Repeat("a", 64), 0, metadata)
	if err != nil || len(docs) != 2 {
		t.Fatalf("documents: %v, %#v", err, docs)
	}
	if err := Verify(path, strings.Repeat("b", 64), 0, metadata); err == nil {
		t.Fatal("accepted wrong generation")
	}
	wrongMetadata := metadata
	wrongMetadata.Branch = "wrong"
	if err := Verify(path, strings.Repeat("a", 64), 0, wrongMetadata); err == nil {
		t.Fatal("accepted metadata mismatch")
	}
	if err := Verify(path, strings.Repeat("a", 64), 1, metadata); err == nil {
		t.Fatal("accepted wrong sequence")
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{bytes[:len(bytes)/2], bytes[:len(bytes)-1]} {
		badPath := filepath.Join(t.TempDir(), "bad.zoekt")
		if err := os.WriteFile(badPath, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Documents(badPath, strings.Repeat("a", 64), 0, metadata); err == nil {
			t.Fatal("accepted corrupt or truncated shard")
		}
	}
	if err := Write(path, strings.Repeat("a", 64), 0, metadata, nil); err == nil {
		t.Fatal("replaced existing shard")
	}
	empty := filepath.Join(t.TempDir(), "empty.zoekt")
	if err := Write(empty, strings.Repeat("a", 64), 1, metadata, nil); err != nil {
		t.Fatal(err)
	}
	docs, err = Documents(empty, strings.Repeat("a", 64), 1, metadata)
	if err != nil || len(docs) != 0 {
		t.Fatalf("empty shard: %v, %#v", err, docs)
	}
}
