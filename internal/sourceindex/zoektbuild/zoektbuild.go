//go:build linux || darwin || freebsd || netbsd

// Package zoektbuild isolates Zoekt's unstable index package.
package zoektbuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
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

// VerifyFile verifies one shard's deterministic metadata from the exact opened
// descriptor. The descriptor is left open and at its current offset.
func VerifyFile(f *os.File, generation string, sequence int, metadata Metadata) error {
	inf, err := newFileIndex(f)
	if err != nil {
		return err
	}
	return verifyIndexFile(inf, generation, sequence, metadata)
}

// DocumentsFile verifies one shard from the exact opened descriptor and
// returns its complete corpus. The descriptor is left open.
func DocumentsFile(f *os.File, generation string, sequence int, metadata Metadata) ([]string, error) {
	inf, err := newFileIndex(f)
	if err != nil {
		return nil, err
	}
	if err := verifyIndexFile(inf, generation, sequence, metadata); err != nil {
		return nil, err
	}
	searcher, err := index.NewSearcher(inf)
	if err != nil {
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

// fileIndex is a non-owning IndexFile over an already-opened descriptor. It
// never closes the descriptor, so one bound descriptor can be verified and
// then opened by the reader.
type fileIndex struct {
	f    *os.File
	size uint32
}

func newFileIndex(f *os.File) (*fileIndex, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() >= math.MaxUint32 {
		return nil, errors.New("index too large")
	}
	return &fileIndex{f: f, size: uint32(info.Size())}, nil
}

func (x *fileIndex) Read(off, sz uint32) ([]byte, error) {
	if sz == 0 {
		return nil, nil
	}
	b := make([]byte, sz)
	if _, err := x.f.ReadAt(b, int64(off)); err != nil {
		return nil, err
	}
	return b, nil
}

func (x *fileIndex) Size() (uint32, error) { return x.size, nil }
func (x *fileIndex) Close()                {}
func (x *fileIndex) Name() string          { return x.f.Name() }

func verifyIndexFile(inf index.IndexFile, generation string, sequence int, metadata Metadata) error {
	repos, indexMetadata, err := index.ReadMetadata(inf)
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

// Verify opens path and verifies one shard's deterministic metadata.
func Verify(path, generation string, sequence int, metadata Metadata) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := VerifyFile(f, generation, sequence, metadata); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Documents opens path and returns the complete corpus of one shard.
func Documents(path, generation string, sequence int, metadata Metadata) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	paths, err := DocumentsFile(f, generation, sequence, metadata)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return paths, nil
}
