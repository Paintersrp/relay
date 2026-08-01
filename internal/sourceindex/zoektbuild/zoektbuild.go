//go:build linux || darwin || freebsd || netbsd

// Package zoektbuild isolates Zoekt's unstable index package.
package zoektbuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
)

type Document struct {
	Name    string
	Content []byte
}
type Metadata struct {
	RepositoryName, Branch, Version, IndexOptions string
	Values                                        map[string]string
}

func shardID(generation string, sequence int) string {
	s := sha256.Sum256([]byte(generation + ":" + fmtInt(sequence)))
	return hex.EncodeToString(s[:])[:20]
}
func fmtInt(v int) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 12)
	for v > 0 {
		b = append(b, digits[v%10])
		v /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
func Write(path, generation string, sequence int, metadata Metadata, docs []Document) error {
	r := &zoekt.Repository{Name: metadata.RepositoryName, Branches: []zoekt.RepositoryBranch{{Name: metadata.Branch, Version: metadata.Version}}, IndexOptions: metadata.IndexOptions, Metadata: metadata.Values}
	b, err := index.NewShardBuilder(r)
	if err != nil {
		return err
	}
	b.IndexTime = time.Unix(0, 0).UTC()
	b.ID = shardID(generation, sequence)
	for _, d := range docs {
		if err := b.Add(index.Document{Name: d.Name, Content: d.Content, Branches: []string{metadata.Branch}}); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = b.Write(f)
	if err == nil {
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			err = os.ErrInvalid
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(path)
	}
	return closeErr
}
func Verify(path, generation string, sequence int, metadata Metadata) error {
	repos, indexMetadata, err := index.ReadMetadataPath(path)
	if err != nil {
		return err
	}
	if indexMetadata == nil || indexMetadata.ID != shardID(generation, sequence) || !indexMetadata.IndexTime.Equal(time.Unix(0, 0).UTC()) {
		return os.ErrInvalid
	}
	if len(repos) != 1 {
		return os.ErrInvalid
	}
	r := repos[0]
	if r.Tombstone || r.Name != metadata.RepositoryName || r.IndexOptions != metadata.IndexOptions || len(r.Branches) != 1 || r.Branches[0].Name != metadata.Branch || r.Branches[0].Version != metadata.Version {
		return os.ErrInvalid
	}
	if len(r.Metadata) != len(metadata.Values) {
		return os.ErrInvalid
	}
	for k, v := range metadata.Values {
		if r.Metadata[k] != v {
			return os.ErrInvalid
		}
	}
	return nil
}

// Documents opens the complete shard reader and returns its corpus. Metadata
// parsing alone accepts truncated shards, so callers use this for verification.
func Documents(path, generation string, sequence int, metadata Metadata) ([]string, error) {
	if err := Verify(path, generation, sequence, metadata); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	indexFile, err := index.NewIndexFile(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	searcher, err := index.NewSearcher(indexFile)
	if err != nil {
		indexFile.Close()
		return nil, err
	}
	defer searcher.Close()
	result, err := searcher.Search(context.Background(), &query.Const{Value: true}, &zoekt.SearchOptions{MaxDocDisplayCount: 1 << 30, TotalMaxMatchCount: 1 << 30})
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(result.Files))
	for i, match := range result.Files {
		paths[i] = match.FileName
	}
	return paths, nil
}
