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

func TestLegacySourceVaultMigrationRecoversTemporaryManifest(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(destination, migrationManifestName)
	temporary := filepath.Join(destination, migrationManifestTemporaryName)
	if err := os.Rename(final, temporary); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatalf("final manifest was not recovered: %v", err)
	}
	if err := verifyTreeDigest(destination, mustMigrationDigest(t, legacy)); err != nil {
		t.Fatal(err)
	}
}

func TestLegacySourceVaultMigrationIgnoresStaleTemporaryManifestBesideFinal(t *testing.T) {
	root := t.TempDir()
	useMigrationWorkingDirectory(t, root)
	legacy := filepath.Join(root, "data", "workflow", "source-vaults")
	destination := filepath.Join(root, "durable", "source-vaults")
	writeMigrationFile(t, legacy, "vault/HEAD", "ref: refs/heads/main\n")
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, destination, migrationManifestTemporaryName, "not json")
	if _, err := migrateLegacySourceVault(destination, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, migrationManifestTemporaryName)); !os.IsNotExist(err) {
		t.Fatalf("stale temporary manifest remained: %v", err)
	}
}

func TestLegacySourceVaultMigrationPreservesDivergedLegacyAfterReconciliation(t *testing.T) {
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
	writeMigrationFile(t, destination, "vault/new", "destination evolution")
	writeMigrationFile(t, legacy, "vault/legacy-change", "diverged")
	resumed, err := migrateLegacySourceVault(destination, false)
	if err == nil || resumed != nil {
		t.Fatalf("resume=%v err=%v, want conflict", resumed, err)
	}
	if _, err := os.Stat(filepath.Join(legacy, "vault/legacy-change")); err != nil {
		t.Fatalf("diverged legacy was deleted: %v", err)
	}
}

func TestLegacySourceVaultMigrationAllowsDestinationEvolutionAfterCleanupFailure(t *testing.T) {
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
	writeMigrationFile(t, destination, "vault/new", "destination evolution")
	resumed, err := migrateLegacySourceVault(destination, false)
	if err != nil || resumed == nil {
		t.Fatalf("resume=%v err=%v", resumed, err)
	}
	if err := resumed.cleanup(); err != nil {
		t.Fatal(err)
	}
}

func mustMigrationDigest(t *testing.T, root string) string {
	t.Helper()
	digest, err := treeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	return digest
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
