package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacySourceVaultMigrationStateMachine(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "repositories/vault.git/HEAD", "ref: refs/heads/main\n")

	migration, err := migrateLegacySourceVault(destination, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readMigrationManifest(destination)
	if err != nil || manifest.State != "promoted" {
		t.Fatalf("manifest=%+v err=%v", manifest, err)
	}
	if err := verifyTree(legacy, destination); err != nil {
		t.Fatal(err)
	}
	if err := migration.reconciled(); err != nil {
		t.Fatal(err)
	}
	if err := migration.cleanup(); err != nil {
		t.Fatal(err)
	}
	if exists, err := populatedDirectory(legacy); err != nil || exists {
		t.Fatalf("legacy exists=%v err=%v", exists, err)
	}

	// Reconciliation and cleanup are idempotent after a completed restart.
	if err := migration.reconciled(); err != nil {
		t.Fatal(err)
	}
	if err := migration.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacySourceVaultMigrationRejectsStaleStagingEntries(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	staging := destination + ".migrating"
	writeMigrationFile(t, staging, "stale", "stale")
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale staging entry remained: %v", err)
	}
}

func TestLegacySourceVaultMigrationRestartsFromVerifiedAndPromotedStates(t *testing.T) {
	for _, state := range []string{"verified", "promoted"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			useMigrationWorkingDirectory(t, root)
			legacy := filepath.Join(root, "data", "workflow", "source-vaults")
			destination := filepath.Join(root, "durable", "source-vaults")
			writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
			migration, err := migrateLegacySourceVault(destination, false)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := readMigrationManifest(destination)
			if err != nil {
				t.Fatal(err)
			}
			manifest.State = state
			if err := writeMigrationManifest(destination, manifest); err != nil {
				t.Fatal(err)
			}
			resumed, err := migrateLegacySourceVault(destination, false)
			if err != nil || resumed == nil {
				t.Fatalf("resume=%v err=%v", resumed, err)
			}
			if err := resumed.reconciled(); err != nil {
				t.Fatal(err)
			}
			if err := migration.cleanup(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacySourceVaultMigrationRetriesCleanupAfterFailure(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	migration, err := migrateLegacySourceVault(destination, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.reconciled(); err != nil {
		t.Fatal(err)
	}
	original := removeLegacyStorage
	removeLegacyStorage = func(string) error { return errors.New("cleanup unavailable") }
	t.Cleanup(func() { removeLegacyStorage = original })
	if err := migration.cleanup(); err == nil {
		t.Fatal("expected cleanup failure")
	}
	removeLegacyStorage = original
	resumed, err := migrateLegacySourceVault(destination, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacySourceVaultMigrationRejectsDestinationOnlyEntries(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	writeMigrationFile(t, destination, "unrelated", "data")
	if _, err := migrateLegacySourceVault(destination, false); err == nil {
		t.Fatal("expected independent-storage conflict")
	}
}

func TestLegacySourceVaultMigrationAllowsEmptyDestination(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTreeRejectsDestinationOnlyEntry(t *testing.T) {
	root := t.TempDir()
	left, right := filepath.Join(root, "left"), filepath.Join(root, "right")
	writeMigrationFile(t, left, "vault/HEAD", "ref: refs/heads/main\n")
	writeMigrationFile(t, right, "vault/HEAD", "ref: refs/heads/main\n")
	writeMigrationFile(t, right, "extra", "extra")
	if err := verifyTree(left, right); err == nil {
		t.Fatal("expected exact-tree verification failure")
	}
}

func writeMigrationFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func useMigrationWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
}
