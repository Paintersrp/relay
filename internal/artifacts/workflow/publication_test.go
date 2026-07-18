package workflowartifacts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationBatchPromotesAndVerifiesOneDirectory(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.BeginPublication("publication-test-one")
	if err != nil {
		t.Fatal(err)
	}
	packet, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{\"packet\":true}\n"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := batch.Stage("inline_input", "inputs/request.txt", "text/plain", []byte("input\n"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := batch.Seal(PublicationExpectations{RetainedArtifactCount: 1, BindingCount: 2, DependencyCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files[0].RelativePath != "inputs/request.txt" || manifest.Files[1].RelativePath != "operation-packet.json" {
		t.Fatalf("manifest order = %#v", manifest.Files)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	for _, file := range []File{packet, input} {
		if _, err := os.Stat(filepath.Join(store.Root(), filepath.FromSlash(file.RelativePath))); err != nil {
			t.Fatalf("published file %s: %v", file.RelativePath, err)
		}
	}
	verified, err := store.VerifyPublication("publication-test-one")
	if err != nil {
		t.Fatal(err)
	}
	if verified.ManifestSHA256 == "" || verified.Manifest.PublicationID != "publication-test-one" {
		t.Fatalf("verified publication = %#v", verified)
	}
}

func TestPublicationRollbackRemovesPromotedDirectory(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.BeginPublication("publication-test-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Seal(PublicationExpectations{BindingCount: 1, DependencyCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), publicationRootName, "publication-test-rollback")); !os.IsNotExist(err) {
		t.Fatalf("promoted publication survived rollback: %v", err)
	}
}

func TestPublicationRejectsUnsafeAndCorruptContent(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.BeginPublication("publication-test-safety")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape", "/absolute", "a\\b", "a//b", publicationManifestName} {
		if _, err := batch.Stage("inline_input", path, "text/plain", []byte("x")); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	if _, err := batch.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Seal(PublicationExpectations{BindingCount: 1, DependencyCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	path := filepath.Join(store.Root(), publicationRootName, "publication-test-safety", "operation-packet.json")
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyPublication("publication-test-safety"); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}

func TestPublicationReconciliationRemovesOnlyUncommittedResidue(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staging, err := store.BeginPublication("publication-staging-residue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staging.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	orphan, err := store.BeginPublication("publication-final-residue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orphan.Stage("operation_packet_document", "operation-packet.json", "application/json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := orphan.Seal(PublicationExpectations{BindingCount: 1, DependencyCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := orphan.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePublicationStagingResidue(); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveUncommittedPublication("publication-final-residue"); err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListPublicationIDs()
	if err != nil || len(ids) != 0 {
		t.Fatalf("publication residue = %#v, %v", ids, err)
	}
	_ = staging.Rollback()
	_ = orphan.Rollback()
}

func TestPublicationBatchStagesFileBackedPayloadAndLocksAfterSeal(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "input.bin")
	if err := os.WriteFile(source, []byte("file-backed\x00payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	batch, err := store.BeginPublication("publication-test-file-backed")
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.StageFile("direct_uploaded_input", "inputs/input.bin", "application/octet-stream", source)
	if err != nil {
		t.Fatal(err)
	}
	if file.SizeBytes != int64(len("file-backed\x00payload")) || file.SHA256 == "" {
		t.Fatalf("staged file = %#v", file)
	}
	if _, err := batch.Seal(PublicationExpectations{RetainedArtifactCount: 1, BindingCount: 2, DependencyCount: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := batch.Stage("inline_input", "late.txt", "text/plain", []byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("post-seal stage error = %v", err)
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
}
