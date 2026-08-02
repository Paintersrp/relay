package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type legacyVaultMigration struct{ legacy string }

// migrateLegacySourceVault retains the old root until Open has reconciled the
// database against the promoted copy. A fixed sibling staging path makes an
// interrupted copy safe to resume.
func migrateLegacySourceVault(destination string, explicit bool) (*legacyVaultMigration, error) {
	if explicit {
		return nil, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("locate legacy source-vault storage: %w", err)
	}
	legacy := filepath.Join(workingDirectory, "data", "workflow", "source-vaults")
	hasLegacy, err := populatedDirectory(legacy)
	if err != nil {
		return nil, fmt.Errorf("inspect legacy source-vault storage: %w", err)
	}
	if !hasLegacy {
		return nil, nil
	}
	if _, err := os.Stat(destination); err == nil {
		return nil, fmt.Errorf("legacy source-vault root %q and destination %q both exist; choose RELAY_SOURCE_VAULT_DIR explicitly rather than merging storage", legacy, destination)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect source-vault destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return nil, fmt.Errorf("create source-vault destination parent: %w", err)
	}
	staging := destination + ".migrating"
	if err := copyTree(legacy, staging); err != nil {
		return nil, fmt.Errorf("copy legacy source-vault storage: %w", err)
	}
	if err := verifyTree(legacy, staging); err != nil {
		return nil, fmt.Errorf("verify copied legacy source-vault storage: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return nil, fmt.Errorf("promote copied source-vault storage: %w", err)
	}
	return &legacyVaultMigration{legacy: legacy}, nil
}

func (m *legacyVaultMigration) cleanup() error {
	if m == nil {
		return nil
	}
	return os.RemoveAll(m.legacy)
}

func populatedDirectory(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
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
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func verifyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		left, err := fileSHA256(path)
		if err != nil {
			return err
		}
		right, err := fileSHA256(filepath.Join(destination, rel))
		if err != nil {
			return err
		}
		if left != right {
			return fmt.Errorf("content mismatch for %q", rel)
		}
		return nil
	})
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
