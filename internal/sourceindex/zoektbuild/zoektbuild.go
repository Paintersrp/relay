//go:build linux || darwin || freebsd || netbsd

// Package zoektbuild isolates Zoekt's unstable index package.
package zoektbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
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
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = b.Write(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func Verify(path string, metadata Metadata) error {
	repos, _, err := index.ReadMetadataPath(path)
	if err != nil {
		return err
	}
	if len(repos) != 1 {
		return os.ErrInvalid
	}
	r := repos[0]
	if r.Tombstone || r.Name != metadata.RepositoryName || r.IndexOptions != metadata.IndexOptions || len(r.Branches) != 1 || r.Branches[0].Name != metadata.Branch || r.Branches[0].Version != metadata.Version {
		return os.ErrInvalid
	}
	for k, v := range metadata.Values {
		if r.Metadata[k] != v {
			return os.ErrInvalid
		}
	}
	return nil
}
