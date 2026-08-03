package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"relay/internal/sourcevaultpolicy"
)

const migrationManifestName = ".relay-source-vault-migration.json"
const migrationManifestTemporaryName = migrationManifestName + ".tmp"

type migrationManifest struct {
	Version         int    `json:"version"`
	LegacyRoot      string `json:"legacy_root"`
	DestinationRoot string `json:"destination_root"`
	TreeDigest      string `json:"tree_digest"`
	State           string `json:"state"`
	CreatedAt       string `json:"created_at"`
}

type legacyVaultMigration struct {
	legacy      string
	destination string
}

var removeLegacyStorage = os.RemoveAll

// migrateLegacySourceVault progresses an on-disk state machine. The legacy
// root remains authoritative until sourcevault.Open has reconciled the copy.
func migrateLegacySourceVault(destination string, explicit bool) (*legacyVaultMigration, error) {
	if explicit {
		return nil, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("locate legacy source-vault storage: %w", err)
	}
	legacy, err := sourcevaultpolicy.CanonicalPathForCreation(filepath.Join(workingDirectory, "data", "workflow", "source-vaults"))
	if err != nil {
		return nil, fmt.Errorf("resolve legacy source-vault storage: %w", err)
	}
	destination, err = sourcevaultpolicy.CanonicalPathForCreation(destination)
	if err != nil {
		return nil, fmt.Errorf("resolve source-vault destination: %w", err)
	}
	hasLegacy, err := populatedDirectory(legacy)
	if err != nil {
		return nil, fmt.Errorf("inspect legacy source-vault storage: %w", err)
	}
	if !hasLegacy {
		return nil, nil
	}

	populated, err := populatedDirectory(destination)
	if err != nil {
		return nil, fmt.Errorf("inspect source-vault destination: %w", err)
	}
	if populated {
		if err := recoverMigrationManifest(destination, legacy); err != nil {
			return nil, fmt.Errorf("recover source-vault migration metadata: %w", err)
		}
		manifest, err := readMigrationManifest(destination)
		if err != nil || !sameMigration(manifest, legacy, destination) || (manifest.State != "verified" && manifest.State != "promoted" && manifest.State != "reconciled") {
			return nil, fmt.Errorf("legacy source-vault root %q and destination %q both contain independent storage; choose RELAY_SOURCE_VAULT_DIR explicitly rather than merging storage", legacy, destination)
		}
		if err := verifyTreeDigest(legacy, manifest.TreeDigest); err != nil {
			return nil, fmt.Errorf("legacy source-vault migration conflict: retained legacy storage no longer matches migration source: %w", err)
		}
		if manifest.State != "reconciled" {
			if err := verifyTreeDigest(destination, manifest.TreeDigest); err != nil {
				return nil, fmt.Errorf("verify promoted source-vault storage: %w", err)
			}
		}
		return &legacyVaultMigration{legacy: legacy, destination: destination}, nil
	}
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Remove(destination); err != nil {
			return nil, fmt.Errorf("clear empty source-vault destination: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect source-vault destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return nil, fmt.Errorf("create source-vault destination parent: %w", err)
	}

	staging := destination + ".migrating"
	// A partial tree is never resumed: it is cheap to rebuild and cannot carry
	// destination-only entries across an interrupted copy.
	if err := os.RemoveAll(staging); err != nil {
		return nil, fmt.Errorf("clear source-vault migration staging: %w", err)
	}
	digest, err := treeDigest(legacy)
	if err != nil {
		return nil, fmt.Errorf("digest legacy source-vault storage: %w", err)
	}
	manifest := migrationManifest{Version: 1, LegacyRoot: legacy, DestinationRoot: destination, TreeDigest: digest, State: "copying", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return nil, err
	}
	if err := writeMigrationManifest(staging, manifest); err != nil {
		return nil, err
	}
	if err := copyTree(legacy, staging); err != nil {
		return nil, fmt.Errorf("copy legacy source-vault storage: %w", err)
	}
	if err := verifyTree(legacy, staging); err != nil {
		return nil, fmt.Errorf("verify copied legacy source-vault storage: %w", err)
	}
	manifest.State = "verified"
	if err := writeMigrationManifest(staging, manifest); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return nil, fmt.Errorf("promote copied source-vault storage: %w", err)
	}
	// If interrupted before this write, verified is deliberately accepted as a
	// promoted, verified copy on the next startup.
	manifest.State = "promoted"
	if err := writeMigrationManifest(destination, manifest); err != nil {
		return nil, err
	}
	return &legacyVaultMigration{legacy: legacy, destination: destination}, nil
}

func (m *legacyVaultMigration) reconciled() error {
	if m == nil {
		return nil
	}
	manifest, err := readMigrationManifest(m.destination)
	if err != nil {
		return err
	}
	if manifest.State == "reconciled" {
		return nil
	}
	if manifest.State != "verified" && manifest.State != "promoted" {
		return fmt.Errorf("invalid source-vault migration state %q", manifest.State)
	}
	manifest.State = "reconciled"
	return writeMigrationManifest(m.destination, manifest)
}

func (m *legacyVaultMigration) cleanup() error {
	if m == nil {
		return nil
	}
	hasLegacy, err := populatedDirectory(m.legacy)
	if err != nil {
		return fmt.Errorf("inspect retained legacy source-vault storage: %w", err)
	}
	if !hasLegacy {
		return nil
	}
	manifest, err := readMigrationManifest(m.destination)
	if err != nil {
		return fmt.Errorf("read source-vault migration manifest: %w", err)
	}
	if !sameMigration(manifest, m.legacy, m.destination) || manifest.State != "reconciled" {
		return fmt.Errorf("invalid reconciled source-vault migration state")
	}
	if err := verifyTreeDigest(m.legacy, manifest.TreeDigest); err != nil {
		return fmt.Errorf("legacy source-vault migration conflict: retained legacy storage no longer matches migration source: %w", err)
	}
	return removeLegacyStorage(m.legacy)
}

func populatedDirectory(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func sameMigration(m migrationManifest, legacy, destination string) bool {
	if m.Version != 1 || m.LegacyRoot != legacy || m.DestinationRoot != destination || len(m.TreeDigest) != sha256.Size*2 {
		return false
	}
	if _, err := hex.DecodeString(m.TreeDigest); err != nil {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, m.CreatedAt)
	return err == nil
}

func readMigrationManifest(root string) (migrationManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, migrationManifestName))
	if err != nil {
		return migrationManifest{}, err
	}
	var manifest migrationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return migrationManifest{}, err
	}
	return manifest, nil
}

// recoverMigrationManifest ensures metadata is never mistaken for vault data.
// A final manifest wins; a valid matching temporary manifest is only promoted
// when no final manifest survived the interruption.
func recoverMigrationManifest(root, legacy string) error {
	finalPath := filepath.Join(root, migrationManifestName)
	temporaryPath := filepath.Join(root, migrationManifestTemporaryName)
	_, finalErr := os.Stat(finalPath)
	_, temporaryErr := os.Stat(temporaryPath)
	if finalErr == nil {
		if temporaryErr == nil {
			if err := os.Remove(temporaryPath); err != nil {
				return fmt.Errorf("remove stale temporary manifest: %w", err)
			}
		}
		return nil
	}
	if !errors.Is(finalErr, os.ErrNotExist) {
		return finalErr
	}
	if errors.Is(temporaryErr, os.ErrNotExist) {
		return nil
	}
	if temporaryErr != nil {
		return temporaryErr
	}
	data, err := os.ReadFile(temporaryPath)
	if err != nil {
		return err
	}
	var manifest migrationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("invalid temporary manifest: %w", err)
	}
	if !sameMigration(manifest, legacy, root) {
		return fmt.Errorf("temporary manifest belongs to a different migration")
	}
	if manifest.State != "verified" && manifest.State != "promoted" && manifest.State != "reconciled" {
		return fmt.Errorf("temporary manifest has invalid state %q", manifest.State)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return fmt.Errorf("promote temporary manifest: %w", err)
	}
	return nil
}

func writeMigrationManifest(root string, manifest migrationManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	temporary := filepath.Join(root, migrationManifestTemporaryName)
	f, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(root, migrationManifestName))
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || migrationMetadataPath(rel) {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported legacy source-vault entry %q", path)
		}
		return copyFile(path, target)
	})
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	if syncErr := out.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func verifyTree(source, destination string) error {
	left, err := treeEntries(source)
	if err != nil {
		return err
	}
	right, err := treeEntries(destination)
	if err != nil {
		return err
	}
	if len(left) != len(right) {
		return fmt.Errorf("tree entry count mismatch")
	}
	for path, leftEntry := range left {
		rightEntry, ok := right[path]
		if !ok || leftEntry != rightEntry {
			return fmt.Errorf("tree mismatch for %q", path)
		}
	}
	return nil
}

func verifyTreeDigest(root, want string) error {
	got, err := treeDigest(root)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("tree digest mismatch")
	}
	return nil
}

func treeDigest(root string) (string, error) {
	entries, err := treeEntries(root)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		fmt.Fprintf(hash, "%s\x00%s\x00", path, entries[path])
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func treeEntries(root string) (map[string]string, error) {
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." || migrationMetadataPath(rel) {
			return nil
		}
		if entry.IsDir() {
			entries[filepath.ToSlash(rel)] = "dir"
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported source-vault entry %q", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = fmt.Sprintf("file:%d:%x", info.Size(), digest)
		return nil
	})
	return entries, err
}

func migrationMetadataPath(relative string) bool {
	return relative == migrationManifestName || relative == migrationManifestTemporaryName
}

func fileSHA256(path string) ([sha256.Size]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer f.Close()
	return sha256File(f)
}

func sha256File(reader io.Reader) ([sha256.Size]byte, error) {
	hash := sha256.New()
	_, err := io.Copy(hash, reader)
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, err
}
