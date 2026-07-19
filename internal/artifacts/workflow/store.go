package workflowartifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrClosed = errors.New("artifact batch is closed")

type Store struct {
	root string
}

type File struct {
	Kind         string
	RelativePath string
	AbsolutePath string
	MediaType    string
	SHA256       string
	SizeBytes    int64
	tempPath     string
}

type Batch struct {
	store      *Store
	namespace  string
	stagingDir string
	files      []File
	promoted   []string
	prepared   bool
	closed     bool
}

// VerifyFile verifies that file names one regular artifact-store file with the
// exact recorded size and digest. A negative SizeBytes means the caller has an
// exact digest but no durable size fact; the returned File from ReadVerifiedFile
// then supplies the observed size.
func (s *Store) VerifyFile(file File) (File, error) {
	if s == nil {
		return File{}, fmt.Errorf("artifact store is required")
	}
	relativePath, err := safeRelativePath(file.RelativePath)
	if err != nil || relativePath != file.RelativePath {
		return File{}, fmt.Errorf("artifact file path is invalid")
	}
	if strings.TrimSpace(file.SHA256) != file.SHA256 || len(file.SHA256) != 64 {
		return File{}, fmt.Errorf("artifact file SHA-256 is invalid")
	}
	for _, value := range file.SHA256 {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return File{}, fmt.Errorf("artifact file SHA-256 is invalid")
		}
	}
	if file.SizeBytes < -1 {
		return File{}, fmt.Errorf("artifact file size is invalid")
	}

	absolutePath := filepath.Join(s.root, filepath.FromSlash(relativePath))
	info, err := os.Lstat(absolutePath)
	if err != nil {
		return File{}, fmt.Errorf("inspect artifact file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("artifact file is not a regular managed file")
	}
	if file.SizeBytes >= 0 && info.Size() != file.SizeBytes {
		return File{}, fmt.Errorf("artifact file size does not match metadata")
	}

	opened, err := os.Open(absolutePath)
	if err != nil {
		return File{}, fmt.Errorf("open artifact file: %w", err)
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, opened)
	closeErr := opened.Close()
	if copyErr != nil {
		return File{}, fmt.Errorf("hash artifact file: %w", copyErr)
	}
	if closeErr != nil {
		return File{}, fmt.Errorf("close artifact file: %w", closeErr)
	}
	if hex.EncodeToString(digest.Sum(nil)) != file.SHA256 {
		return File{}, fmt.Errorf("artifact file SHA-256 does not match metadata")
	}
	file.RelativePath = relativePath
	file.AbsolutePath = absolutePath
	file.SizeBytes = info.Size()
	return file, nil
}

// ReadVerifiedFile is the bounded read primitive for an already-authorized
// artifact identity. It intentionally accepts a File rather than a caller
// supplied path so application owners must first bind a safe path and digest.
func (s *Store) ReadVerifiedFile(file File, maxBytes int) (File, []byte, error) {
	if maxBytes < 0 {
		return File{}, nil, fmt.Errorf("artifact read limit is invalid")
	}
	verified, err := s.VerifyFile(file)
	if err != nil {
		return File{}, nil, err
	}
	if verified.SizeBytes > int64(maxBytes) {
		return File{}, nil, fmt.Errorf("artifact file exceeds %d bytes", maxBytes)
	}
	data, err := os.ReadFile(verified.AbsolutePath)
	if err != nil {
		return File{}, nil, fmt.Errorf("read artifact file: %w", err)
	}
	if int64(len(data)) != verified.SizeBytes {
		return File{}, nil, fmt.Errorf("artifact file size changed during read")
	}
	return verified, data, nil
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact root is required")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	return &Store{root: absolute}, nil
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) Begin(namespace string) (*Batch, error) {
	namespace, err := safeRelativePath(namespace)
	if err != nil {
		return nil, fmt.Errorf("artifact namespace: %w", err)
	}
	stagingRoot := filepath.Join(s.root, ".staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create staging root: %w", err)
	}
	stagingDir, err := os.MkdirTemp(stagingRoot, "batch-")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	return &Batch{store: s, namespace: namespace, stagingDir: stagingDir}, nil
}

func (b *Batch) Stage(kind, filename, mediaType string, data []byte) (File, error) {
	if b.closed || b.prepared {
		return File{}, ErrClosed
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(kind) != kind {
		return File{}, fmt.Errorf("artifact kind must be nonblank without outer whitespace")
	}
	if strings.TrimSpace(mediaType) == "" || strings.TrimSpace(mediaType) != mediaType {
		return File{}, fmt.Errorf("artifact media type must be nonblank without outer whitespace")
	}
	if strings.TrimSpace(filename) == "" || strings.TrimSpace(filename) != filename || filename != filepath.Base(filename) || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\\`) {
		return File{}, fmt.Errorf("artifact filename must be a safe basename without outer whitespace")
	}
	for _, existing := range b.files {
		if filepath.Base(existing.RelativePath) == filename {
			return File{}, fmt.Errorf("artifact filename %q is already staged", filename)
		}
	}

	tempPath := filepath.Join(b.stagingDir, filename)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return File{}, fmt.Errorf("create staged artifact: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("write staged artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("sync staged artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("close staged artifact: %w", err)
	}

	digest := sha256.Sum256(data)
	relativePath := filepath.ToSlash(filepath.Join(b.namespace, filename))
	staged := File{
		Kind:         kind,
		RelativePath: relativePath,
		AbsolutePath: filepath.Join(b.store.root, filepath.FromSlash(relativePath)),
		MediaType:    mediaType,
		SHA256:       hex.EncodeToString(digest[:]),
		SizeBytes:    int64(len(data)),
		tempPath:     tempPath,
	}
	b.files = append(b.files, staged)
	return staged, nil
}

func (b *Batch) StageFile(kind, filename, mediaType, sourcePath string) (File, error) {
	if b.closed || b.prepared {
		return File{}, ErrClosed
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(kind) != kind {
		return File{}, fmt.Errorf("artifact kind must be nonblank without outer whitespace")
	}
	if strings.TrimSpace(mediaType) == "" || strings.TrimSpace(mediaType) != mediaType {
		return File{}, fmt.Errorf("artifact media type must be nonblank without outer whitespace")
	}
	if strings.TrimSpace(filename) == "" || strings.TrimSpace(filename) != filename || filename != filepath.Base(filename) || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\\`) {
		return File{}, fmt.Errorf("artifact filename must be a safe basename without outer whitespace")
	}
	for _, existing := range b.files {
		if filepath.Base(existing.RelativePath) == filename {
			return File{}, fmt.Errorf("artifact filename %q is already staged", filename)
		}
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return File{}, fmt.Errorf("open source artifact: %w", err)
	}
	defer source.Close()
	tempPath := filepath.Join(b.stagingDir, filename)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return File{}, fmt.Errorf("create staged artifact: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), source)
	if copyErr != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("copy staged artifact: %w", copyErr)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("sync staged artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return File{}, fmt.Errorf("close staged artifact: %w", err)
	}

	relativePath := filepath.ToSlash(filepath.Join(b.namespace, filename))
	staged := File{
		Kind:         kind,
		RelativePath: relativePath,
		AbsolutePath: filepath.Join(b.store.root, filepath.FromSlash(relativePath)),
		MediaType:    mediaType,
		SHA256:       hex.EncodeToString(digest.Sum(nil)),
		SizeBytes:    size,
		tempPath:     tempPath,
	}
	b.files = append(b.files, staged)
	return staged, nil
}

func (b *Batch) Files() []File {
	files := make([]File, len(b.files))
	copy(files, b.files)
	return files
}

func (b *Batch) Promote() error {
	if b.closed || b.prepared {
		return ErrClosed
	}
	for index := len(b.promoted); index < len(b.files); index++ {
		file := b.files[index]
		if err := os.MkdirAll(filepath.Dir(file.AbsolutePath), 0o755); err != nil {
			return fmt.Errorf("create artifact directory: %w", err)
		}
		if _, err := os.Lstat(file.AbsolutePath); err == nil {
			return fmt.Errorf("artifact already exists: %s", file.RelativePath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect artifact destination: %w", err)
		}
		if err := os.Rename(file.tempPath, file.AbsolutePath); err != nil {
			return fmt.Errorf("promote artifact %s: %w", file.RelativePath, err)
		}
		b.promoted = append(b.promoted, file.AbsolutePath)
	}
	return nil
}

// PrepareCommit removes the now-empty staging directory while retaining enough
// state for Rollback to remove promoted files if the database commit fails.
func (b *Batch) PrepareCommit() error {
	if b.closed || b.prepared {
		return ErrClosed
	}
	if len(b.promoted) != len(b.files) {
		return fmt.Errorf("all staged artifacts must be promoted before commit")
	}
	if err := os.RemoveAll(b.stagingDir); err != nil {
		return fmt.Errorf("remove staging directory: %w", err)
	}
	b.prepared = true
	return nil
}

// Commit marks a prepared batch durable after the coordinating database
// transaction commits. It performs no filesystem operation and cannot fail.
func (b *Batch) Commit() {
	if b.closed {
		return
	}
	b.closed = true
}

func (b *Batch) Rollback() error {
	if b.closed {
		return nil
	}
	var joined error
	for index := len(b.promoted) - 1; index >= 0; index-- {
		if err := os.Remove(b.promoted[index]); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, err)
		}
	}
	if err := os.RemoveAll(b.stagingDir); err != nil {
		joined = errors.Join(joined, err)
	}
	for _, file := range b.files {
		if err := removeEmptyParents(filepath.Dir(file.AbsolutePath), b.store.root); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	b.closed = true
	return joined
}

func removeEmptyParents(directory, stop string) error {
	for directory != stop {
		err := os.Remove(directory)
		if err == nil {
			directory = filepath.Dir(directory)
			continue
		}
		if errors.Is(err, os.ErrNotExist) || isDirectoryNotEmpty(err) {
			return nil
		}
		return err
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "directory not empty") ||
		strings.Contains(strings.ToLower(err.Error()), "not empty")
}

func safeRelativePath(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return "", fmt.Errorf("path must be repository-relative POSIX-style content without outer whitespace")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path contains an unsafe segment")
		}
	}
	return strings.Join(parts, "/"), nil
}
