package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexerprotocol"
	"relay/internal/sourceindex/zoektbuild"
)

// OpenedArtifact is one regular artifact of a generation, opened through the
// authoritative boundary and already identified and hashed from its descriptor.
type OpenedArtifact struct {
	Kind         sourceindex.ArtifactKind
	RelativePath string
	SHA256       string
	SizeBytes    int64
	Identity     FileIdentity
	File         *os.File
}

// VerifiedGenerationFiles is the boundary the complete verifier uses to read
// one generation exclusively through bound descriptors.
type VerifiedGenerationFiles interface {
	ReadManifest(name string) ([]byte, FileIdentity, error)
	ListArtifacts() ([]OpenedArtifact, error)
}

// VerifiedGeneration is the verified outcome of one generation.
type VerifiedGeneration struct {
	Generation sourceindex.GenerationManifest
	Coverage   sourceindex.CoverageManifest
	Artifacts  sourceindex.ArtifactManifest
	Opened     []OpenedArtifact

	GenerationRawSHA256 string
	CoverageRawSHA256   string
	ArtifactRawSHA256   string
	ShardCount          int64
}

// VerifyGenerationFiles performs the complete verification of one generation
// through bound descriptors: canonical manifests, digest chains, complete
// artifact membership, no unlisted or missing file, file size and SHA-256,
// no unsafe file type, no hard-linked or repeated artifact identity,
// contiguous shards, deterministic shard metadata, and complete
// indexed-document membership.
func VerifyGenerationFiles(files VerifiedGenerationFiles, r indexerprotocol.BuildRequest) (*VerifiedGeneration, error) {
	gb, gi, err := files.ReadManifest(sourceindex.GenerationManifestFileName)
	if err != nil {
		return nil, err
	}
	g, err := sourceindex.ParseGenerationManifest(gb)
	if err != nil {
		return nil, err
	}
	cb, ci, err := files.ReadManifest(sourceindex.CoverageManifestFileName)
	if err != nil {
		return nil, err
	}
	c, err := sourceindex.ParseCoverageManifest(cb)
	if err != nil {
		return nil, err
	}
	ab, ai, err := files.ReadManifest(sourceindex.ArtifactManifestFileName)
	if err != nil {
		return nil, err
	}
	a, err := sourceindex.ParseArtifactManifest(ab)
	if err != nil {
		return nil, err
	}
	gd, err := sourceindex.GenerationManifestSHA256(g)
	if err != nil {
		return nil, err
	}
	cd, err := sourceindex.CoverageManifestSHA256(c)
	if err != nil {
		return nil, err
	}
	ad, err := sourceindex.ArtifactManifestSHA256(a)
	if err != nil {
		return nil, err
	}
	if gd != digest(gb) || cd != digest(cb) || ad != digest(ab) || g.GenerationID != r.GenerationID || c.GenerationID != r.GenerationID || c.CommitOID != r.Identity.CommitOID || c.TreeOID != r.Identity.TreeOID || a.GenerationID != r.GenerationID || g.Identity != r.Identity || g.CoverageManifestSHA256 != digest(cb) || g.ArtifactManifestSHA256 != digest(ab) {
		return nil, errors.New("manifest integrity")
	}
	if sameIdentity(gi, ci) || sameIdentity(gi, ai) || sameIdentity(ci, ai) {
		return nil, errors.New("repeated manifest identity")
	}
	artifacts, err := files.ListArtifacts()
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			for _, o := range artifacts {
				if o.File != nil {
					_ = o.File.Close()
				}
			}
		}
	}()
	byPath := make(map[string]OpenedArtifact, len(artifacts))
	byIdentity := make(map[FileIdentity]string, len(artifacts)+2)
	for _, o := range artifacts {
		if _, ok := byPath[o.RelativePath]; ok {
			return nil, errors.New("repeated artifact path")
		}
		if prior, ok := byIdentity[o.Identity]; ok {
			return nil, fmt.Errorf("artifact aliases %q and %q", prior, o.RelativePath)
		}
		byPath[o.RelativePath] = o
		byIdentity[o.Identity] = o.RelativePath
	}
	for _, m := range []struct {
		name string
		id   FileIdentity
	}{
		{sourceindex.GenerationManifestFileName, gi},
		{sourceindex.ArtifactManifestFileName, ai},
	} {
		if prior, ok := byIdentity[m.id]; ok {
			return nil, fmt.Errorf("artifact %q aliases %q", prior, m.name)
		}
	}
	for rel, o := range byPath {
		if rel != sourceindex.CoverageManifestFileName && o.Kind != sourceindex.ArtifactZoektShard {
			return nil, errors.New("unexpected artifact")
		}
	}
	if len(byPath) != len(a.Files) {
		return nil, errors.New("artifact membership")
	}
	shards := make([]OpenedArtifact, 0, len(a.Files))
	for _, want := range a.Files {
		o, ok := byPath[want.RelativePath]
		if !ok {
			return nil, errors.New("missing artifact")
		}
		if o.Kind != want.Kind || o.SizeBytes != want.SizeBytes || o.SHA256 != want.SHA256 {
			return nil, errors.New("artifact integrity")
		}
		if want.Kind == sourceindex.ArtifactZoektShard {
			shards = append(shards, o)
		}
	}
	m, err := metadata(r)
	if err != nil {
		return nil, err
	}
	expectedDocuments := make(map[string]bool)
	for _, entry := range c.Entries {
		path, er := entry.Path.Bytes()
		if er != nil {
			return nil, er
		}
		if entry.Status == sourceindex.CoverageIndexedText {
			expectedDocuments[string(path)] = true
		}
	}
	seenDocuments := make(map[string]bool)
	for i, shard := range shards {
		seq := fmt.Sprintf("shards/%06d.zoekt", i)
		if shard.RelativePath != seq {
			return nil, errors.New("noncontiguous shard")
		}
		if shard.File == nil {
			return nil, errors.New("missing shard descriptor")
		}
		documents, er := zoektbuild.DocumentsFile(shard.File, r.GenerationID, i, m)
		if er != nil {
			return nil, er
		}
		for _, document := range documents {
			if !expectedDocuments[document] || seenDocuments[document] {
				return nil, errors.New("unexpected shard document")
			}
			seenDocuments[document] = true
		}
	}
	if len(seenDocuments) != len(expectedDocuments) {
		return nil, errors.New("missing shard document")
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].RelativePath < artifacts[j].RelativePath })
	complete = true
	return &VerifiedGeneration{
		Generation:          g,
		Coverage:            c,
		Artifacts:           a,
		Opened:              artifacts,
		GenerationRawSHA256: digest(gb),
		CoverageRawSHA256:   digest(cb),
		ArtifactRawSHA256:   digest(ab),
		ShardCount:          int64(len(shards)),
	}, nil
}

func sameIdentity(a, b FileIdentity) bool { return a == b }

// pathFiles is the builder's path-based VerifiedGenerationFiles adapter over a
// staged generation directory.
type pathFiles struct{ root string }

func (p pathFiles) ReadManifest(name string) ([]byte, FileIdentity, error) {
	path := filepath.Join(p.root, name)
	info, e := os.Lstat(path)
	if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, FileIdentity{}, os.ErrInvalid
	}
	identity, e := artifactFileIdentity(info)
	if e != nil {
		return nil, FileIdentity{}, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, FileIdentity{}, e
	}
	return b, identity, nil
}

func (p pathFiles) ListArtifacts() ([]OpenedArtifact, error) {
	var out []OpenedArtifact
	identities := make(map[FileIdentity]string)
	e := filepath.Walk(p.root, func(path string, info os.FileInfo, er error) error {
		if er != nil {
			return er
		}
		if path == p.root {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
			return errors.New("unsafe output")
		}
		if info.IsDir() {
			if filepath.ToSlash(path) != filepath.ToSlash(filepath.Join(p.root, sourceindex.ShardDirectoryName)) {
				return errors.New("unexpected directory")
			}
			return nil
		}
		identity, er := artifactFileIdentity(info)
		if er != nil {
			return errors.New("unsafe output")
		}
		if prior, ok := identities[identity]; ok {
			return fmt.Errorf("artifact aliases %q and %q", prior, path)
		}
		identities[identity] = path
		rel, er := filepath.Rel(p.root, path)
		if er != nil {
			return er
		}
		rel = filepath.ToSlash(rel)
		if rel == sourceindex.ArtifactManifestFileName || rel == sourceindex.GenerationManifestFileName {
			return nil
		}
		f, er := os.Open(path)
		if er != nil {
			return er
		}
		h := sha256.New()
		n, er := io.Copy(h, f)
		if er != nil {
			_ = f.Close()
			return er
		}
		if _, er := f.Seek(0, 0); er != nil {
			_ = f.Close()
			return er
		}
		kind := sourceindex.ArtifactZoektMetadata
		if rel == sourceindex.CoverageManifestFileName {
			kind = sourceindex.ArtifactCoverage
		} else if strings.HasSuffix(rel, ".zoekt") {
			kind = sourceindex.ArtifactZoektShard
		}
		out = append(out, OpenedArtifact{Kind: kind, RelativePath: rel, SHA256: hex.EncodeToString(h.Sum(nil)), SizeBytes: n, Identity: identity, File: f})
		return nil
	})
	return out, e
}

// CloseArtifacts closes every opened artifact descriptor.
func CloseArtifacts(artifacts []OpenedArtifact) error {
	var errs []error
	for _, o := range artifacts {
		if o.File != nil {
			errs = append(errs, o.File.Close())
		}
	}
	return errors.Join(errs...)
}
