package workflowartifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type VerifiedPublication struct {
	Manifest       PublicationManifest
	ManifestSHA256 string
}

func (s *Store) RemovePublicationStagingResidue() error {
	root := filepath.Join(s.root, publicationStagingName)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list publication staging residue: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if err := removePublicationDirectory(path, root); err != nil {
			return fmt.Errorf("remove publication staging residue: %w", err)
		}
	}
	return syncDirectory(root)
}

func (s *Store) ListPublicationIDs() ([]string, error) {
	root := filepath.Join(s.root, publicationRootName)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list publication directories: %w", err)
	}
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !validPublicationID(entry.Name()) {
			return nil, fmt.Errorf("publication directory has invalid identity")
		}
		info, err := os.Lstat(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("inspect publication directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("publication entry is not a managed directory")
		}
		values = append(values, entry.Name())
	}
	sort.Strings(values)
	return values, nil
}

func (s *Store) VerifyPublication(publicationID string) (VerifiedPublication, error) {
	if !validPublicationID(publicationID) {
		return VerifiedPublication{}, fmt.Errorf("publication ID is invalid")
	}
	root := filepath.Join(s.root, publicationRootName)
	directory := filepath.Join(root, publicationID)
	info, err := os.Lstat(directory)
	if err != nil {
		return VerifiedPublication{}, fmt.Errorf("inspect publication directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return VerifiedPublication{}, fmt.Errorf("publication entry is not a managed directory")
	}
	manifestPath := filepath.Join(directory, publicationManifestName)
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return VerifiedPublication{}, fmt.Errorf("inspect publication manifest: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Size() > MaxPublicationManifestBytes {
		return VerifiedPublication{}, fmt.Errorf("publication manifest is invalid")
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return VerifiedPublication{}, fmt.Errorf("open publication manifest: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, MaxPublicationManifestBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return VerifiedPublication{}, fmt.Errorf("read publication manifest: %w", readErr)
	}
	if closeErr != nil {
		return VerifiedPublication{}, fmt.Errorf("close publication manifest: %w", closeErr)
	}
	if len(raw) > MaxPublicationManifestBytes {
		return VerifiedPublication{}, fmt.Errorf("publication manifest limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest PublicationManifest
	if err := decoder.Decode(&manifest); err != nil {
		return VerifiedPublication{}, fmt.Errorf("decode publication manifest: %w", err)
	}
	if err := requireManifestEOF(decoder); err != nil {
		return VerifiedPublication{}, err
	}
	if err := validatePublicationManifest(manifest, publicationID); err != nil {
		return VerifiedPublication{}, err
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, raw) {
		return VerifiedPublication{}, fmt.Errorf("publication manifest is not canonical")
	}
	if err := verifyPublicationFiles(directory, manifest); err != nil {
		return VerifiedPublication{}, err
	}
	digest := sha256.Sum256(raw)
	return VerifiedPublication{
		Manifest:       clonePublicationManifest(manifest),
		ManifestSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func (s *Store) RemoveUncommittedPublication(publicationID string) error {
	if !validPublicationID(publicationID) {
		return fmt.Errorf("publication ID is invalid")
	}
	root := filepath.Join(s.root, publicationRootName)
	if err := removePublicationDirectory(filepath.Join(root, publicationID), root); err != nil {
		return err
	}
	return syncDirectory(root)
}

func validatePublicationManifest(manifest PublicationManifest, publicationID string) error {
	if manifest.Version != publicationManifestVersion || manifest.PublicationID != publicationID || manifest.Namespace != filepath.ToSlash(filepath.Join(publicationRootName, publicationID)) {
		return fmt.Errorf("publication manifest identity mismatch")
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > MaxPublicationFiles {
		return fmt.Errorf("publication manifest file count is invalid")
	}
	if manifest.Expectations.RetainedArtifactCount < 0 || manifest.Expectations.BindingCount < 1 || manifest.Expectations.DependencyCount < 1 || manifest.Expectations.VaultRelationshipCount < 0 {
		return fmt.Errorf("publication manifest expectations are invalid")
	}
	previous := ""
	for _, value := range manifest.Files {
		path, err := validatePublicationPayload(value.Kind, value.RelativePath, value.MediaType)
		if err != nil || path != value.RelativePath || path == publicationManifestName || value.SizeBytes < 0 || !validDigest(value.SHA256) {
			return fmt.Errorf("publication manifest file is invalid")
		}
		if previous != "" && previous >= value.RelativePath {
			return fmt.Errorf("publication manifest files are not strictly ordered")
		}
		previous = value.RelativePath
	}
	return nil
}

func verifyPublicationFiles(directory string, manifest PublicationManifest) error {
	expected := map[string]PublicationManifestFile{}
	for _, value := range manifest.Files {
		expected[value.RelativePath] = value
	}
	seen := map[string]struct{}{}
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == directory {
			return nil
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("publication contains a symlink")
		}
		if entry.IsDir() {
			return nil
		}
		if relative == publicationManifestName {
			return nil
		}
		value, ok := expected[relative]
		if !ok {
			return fmt.Errorf("publication contains an unexpected file")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() != value.SizeBytes {
			return fmt.Errorf("publication file metadata mismatch")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		digest := sha256.New()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(digest.Sum(nil)) != value.SHA256 {
			return fmt.Errorf("publication file digest mismatch")
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify publication files: %w", err)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("publication file is missing")
	}
	return nil
}

func requireManifestEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("publication manifest has trailing content")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
