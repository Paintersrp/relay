package workflowartifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	publicationManifestVersion  = "relay.operation-packet-publication-manifest.v1"
	publicationManifestName     = ".relay-publication-manifest.json"
	publicationStagingName      = ".publication-staging"
	publicationRootName         = "operation-packet-publications"
	MaxPublicationManifestBytes = 1 << 20
	MaxPublicationFiles         = 4096
)

type PublicationExpectations struct {
	RetainedArtifactCount  int64 `json:"retainedArtifactCount"`
	BindingCount           int64 `json:"bindingCount"`
	DependencyCount        int64 `json:"dependencyCount"`
	VaultRelationshipCount int64 `json:"vaultRelationshipCount"`
}

type PublicationManifestFile struct {
	Kind         string `json:"kind"`
	RelativePath string `json:"relativePath"`
	MediaType    string `json:"mediaType"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type PublicationManifest struct {
	Version       string                    `json:"version"`
	PublicationID string                    `json:"publicationId"`
	Namespace     string                    `json:"namespace"`
	Files         []PublicationManifestFile `json:"files"`
	Expectations  PublicationExpectations   `json:"expectations"`
}

type publicationFile struct {
	file         File
	manifestPath string
}

type PublicationBatch struct {
	store          *Store
	publicationID  string
	namespace      string
	stagingDir     string
	finalDir       string
	files          []publicationFile
	manifest       PublicationManifest
	manifestBytes  []byte
	manifestSHA256 string
	sealed         bool
	promoted       bool
	closed         bool
}

func (s *Store) BeginPublication(publicationID string) (*PublicationBatch, error) {
	if !validPublicationID(publicationID) {
		return nil, fmt.Errorf("publication ID is invalid")
	}
	stagingRoot := filepath.Join(s.root, publicationStagingName)
	finalRoot := filepath.Join(s.root, publicationRootName)
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create publication staging root: %w", err)
	}
	if err := os.MkdirAll(finalRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create publication root: %w", err)
	}
	stagingDir := filepath.Join(stagingRoot, publicationID)
	if err := os.Mkdir(stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("create publication staging directory: %w", err)
	}
	namespace := filepath.ToSlash(filepath.Join(publicationRootName, publicationID))
	return &PublicationBatch{
		store:         s,
		publicationID: publicationID,
		namespace:     namespace,
		stagingDir:    stagingDir,
		finalDir:      filepath.Join(finalRoot, publicationID),
	}, nil
}

func (b *PublicationBatch) PublicationID() string  { return b.publicationID }
func (b *PublicationBatch) Namespace() string      { return b.namespace }
func (b *PublicationBatch) ManifestSHA256() string { return b.manifestSHA256 }
func (b *PublicationBatch) Manifest() PublicationManifest {
	return clonePublicationManifest(b.manifest)
}
func (b *PublicationBatch) IsSealed() bool { return b.sealed && !b.closed }

func (b *PublicationBatch) Files() []File {
	files := make([]File, 0, len(b.files))
	for _, value := range b.files {
		files = append(files, value.file)
	}
	return files
}

func (b *PublicationBatch) Stage(kind, relativePath, mediaType string, data []byte) (File, error) {
	if b.closed || b.sealed {
		return File{}, ErrClosed
	}
	path, err := validatePublicationPayload(kind, relativePath, mediaType)
	if err != nil {
		return File{}, err
	}
	if err := b.ensureUnique(path); err != nil {
		return File{}, err
	}
	if len(b.files) >= MaxPublicationFiles {
		return File{}, fmt.Errorf("publication file limit exceeded")
	}
	tempPath := filepath.Join(b.stagingDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o700); err != nil {
		return File{}, fmt.Errorf("create publication payload directory: %w", err)
	}
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return File{}, fmt.Errorf("create publication payload: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("write publication payload: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("sync publication payload: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("close publication payload: %w", err)
	}
	digest := sha256.Sum256(data)
	return b.addFile(kind, path, mediaType, hex.EncodeToString(digest[:]), int64(len(data)), tempPath), nil
}

func (b *PublicationBatch) StageFile(kind, relativePath, mediaType, sourcePath string) (File, error) {
	if b.closed || b.sealed {
		return File{}, ErrClosed
	}
	path, err := validatePublicationPayload(kind, relativePath, mediaType)
	if err != nil {
		return File{}, err
	}
	if err := b.ensureUnique(path); err != nil {
		return File{}, err
	}
	if len(b.files) >= MaxPublicationFiles {
		return File{}, fmt.Errorf("publication file limit exceeded")
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return File{}, fmt.Errorf("inspect publication source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("publication source must be a regular file")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return File{}, fmt.Errorf("open publication source: %w", err)
	}
	defer source.Close()
	tempPath := filepath.Join(b.stagingDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o700); err != nil {
		return File{}, fmt.Errorf("create publication payload directory: %w", err)
	}
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return File{}, fmt.Errorf("create publication payload: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), source)
	if copyErr != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("copy publication payload: %w", copyErr)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("sync publication payload: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("close publication payload: %w", err)
	}
	return b.addFile(kind, path, mediaType, hex.EncodeToString(digest.Sum(nil)), size, tempPath), nil
}

func (b *PublicationBatch) Seal(expectations PublicationExpectations) (PublicationManifest, error) {
	if b.closed || b.sealed {
		return PublicationManifest{}, ErrClosed
	}
	if len(b.files) == 0 {
		return PublicationManifest{}, fmt.Errorf("publication requires at least one payload")
	}
	if expectations.RetainedArtifactCount < 0 || expectations.BindingCount < 1 || expectations.DependencyCount < 1 || expectations.VaultRelationshipCount < 0 {
		return PublicationManifest{}, fmt.Errorf("publication expectations are invalid")
	}
	manifestFiles := make([]PublicationManifestFile, 0, len(b.files))
	for _, value := range b.files {
		manifestFiles = append(manifestFiles, PublicationManifestFile{
			Kind:         value.file.Kind,
			RelativePath: value.manifestPath,
			MediaType:    value.file.MediaType,
			SHA256:       value.file.SHA256,
			SizeBytes:    value.file.SizeBytes,
		})
	}
	sort.Slice(manifestFiles, func(i, j int) bool { return manifestFiles[i].RelativePath < manifestFiles[j].RelativePath })
	manifest := PublicationManifest{
		Version:       publicationManifestVersion,
		PublicationID: b.publicationID,
		Namespace:     b.namespace,
		Files:         manifestFiles,
		Expectations:  expectations,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return PublicationManifest{}, fmt.Errorf("encode publication manifest: %w", err)
	}
	if len(encoded) > MaxPublicationManifestBytes {
		return PublicationManifest{}, fmt.Errorf("publication manifest limit exceeded")
	}
	manifestPath := filepath.Join(b.stagingDir, publicationManifestName)
	file, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return PublicationManifest{}, fmt.Errorf("create publication manifest: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		_ = os.Remove(manifestPath)
		return PublicationManifest{}, fmt.Errorf("write publication manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(manifestPath)
		return PublicationManifest{}, fmt.Errorf("sync publication manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(manifestPath)
		return PublicationManifest{}, fmt.Errorf("close publication manifest: %w", err)
	}
	if err := syncPublicationTree(b.stagingDir); err != nil {
		return PublicationManifest{}, err
	}
	digest := sha256.Sum256(encoded)
	b.manifest = manifest
	b.manifestBytes = append([]byte(nil), encoded...)
	b.manifestSHA256 = hex.EncodeToString(digest[:])
	b.sealed = true
	return clonePublicationManifest(manifest), nil
}

func (b *PublicationBatch) Promote() error {
	if b.closed || !b.sealed || b.promoted {
		return ErrClosed
	}
	if _, err := os.Lstat(b.finalDir); err == nil {
		return fmt.Errorf("publication already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect publication destination: %w", err)
	}
	if err := os.Rename(b.stagingDir, b.finalDir); err != nil {
		return fmt.Errorf("promote publication: %w", err)
	}
	b.promoted = true
	if err := syncDirectory(filepath.Dir(b.stagingDir)); err != nil {
		return fmt.Errorf("sync publication staging parent: %w", err)
	}
	if err := syncDirectory(filepath.Dir(b.finalDir)); err != nil {
		return fmt.Errorf("sync publication parent: %w", err)
	}
	return nil
}

func (b *PublicationBatch) Commit() {
	if b.closed {
		return
	}
	b.closed = true
}

func (b *PublicationBatch) Rollback() error {
	if b.closed {
		return nil
	}
	var err error
	if b.promoted {
		err = removePublicationDirectory(b.finalDir, filepath.Join(b.store.root, publicationRootName))
	} else {
		err = removePublicationDirectory(b.stagingDir, filepath.Join(b.store.root, publicationStagingName))
	}
	b.closed = true
	return err
}

func (b *PublicationBatch) ensureUnique(path string) error {
	if path == publicationManifestName {
		return fmt.Errorf("publication payload path is reserved")
	}
	for _, value := range b.files {
		if value.manifestPath == path {
			return fmt.Errorf("publication payload path is already staged")
		}
	}
	return nil
}

func (b *PublicationBatch) addFile(kind, path, mediaType, digest string, size int64, tempPath string) File {
	relativePath := filepath.ToSlash(filepath.Join(b.namespace, filepath.FromSlash(path)))
	file := File{
		Kind:         kind,
		RelativePath: relativePath,
		AbsolutePath: filepath.Join(b.store.root, filepath.FromSlash(relativePath)),
		MediaType:    mediaType,
		SHA256:       digest,
		SizeBytes:    size,
		tempPath:     tempPath,
	}
	b.files = append(b.files, publicationFile{file: file, manifestPath: path})
	return file
}

func validatePublicationPayload(kind, path, mediaType string) (string, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(kind) != kind || len(kind) > 128 {
		return "", fmt.Errorf("publication kind is invalid")
	}
	if strings.TrimSpace(mediaType) == "" || strings.TrimSpace(mediaType) != mediaType || len(mediaType) > 255 {
		return "", fmt.Errorf("publication media type is invalid")
	}
	return safePublicationRelativePath(path)
}

func safePublicationRelativePath(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("publication path is invalid")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("publication path contains an unsafe segment")
		}
	}
	return strings.Join(parts, "/"), nil
}

func validPublicationID(value string) bool {
	if !strings.HasPrefix(value, "publication-") || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "publication-") {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return len(value) > len("publication-")
}

func syncPublicationTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("publication staging contains a symlink")
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect publication staging tree: %w", err)
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return fmt.Errorf("sync publication directory: %w", err)
		}
	}
	return nil
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Sync(); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return nil
}

func removePublicationDirectory(path, parent string) error {
	cleanParent := filepath.Clean(parent)
	cleanPath := filepath.Clean(path)
	if filepath.Dir(cleanPath) != cleanParent {
		return fmt.Errorf("publication cleanup path is outside its managed parent")
	}
	info, err := os.Lstat(cleanPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(cleanPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("publication cleanup target is not a directory")
	}
	return os.RemoveAll(cleanPath)
}

func clonePublicationManifest(value PublicationManifest) PublicationManifest {
	copyValue := value
	copyValue.Files = append([]PublicationManifestFile(nil), value.Files...)
	return copyValue
}
