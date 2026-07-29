package audits

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditPrepareModes(t *testing.T) {
	tests := []struct {
		name  string
		mode  executor.EffectiveExecutorBriefMode
		actor string
		tryID bool
	}{
		{name: "adaptive_no_operations", mode: executor.EffectiveExecutorBriefAdaptiveNoOperations, actor: workflowstore.ImplementationActorExecutor, tryID: true},
		{name: "adaptive_preflight_failed", mode: executor.EffectiveExecutorBriefAdaptivePreflightFailed, actor: workflowstore.ImplementationActorExecutor, tryID: true},
		{name: "adaptive_after_partial_application", mode: executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication, actor: workflowstore.ImplementationActorHybrid, tryID: true},
		{name: "deterministic_complete", mode: executor.EffectiveExecutorBriefDeterministicComplete, actor: workflowstore.ImplementationActorApplier},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := buildPackageEvidence(t, test.mode)
			setPackageRunValidating(t, fixture)
			service, err := NewWorkflowAuditServiceWithSourceVaults(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			inspector := packagePrepareTestInspector()
			service.inspector = inspector
			ctx := context.Background()
			commit := strings.Repeat("c", 40)
			result, err := service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: commit})
			if err != nil {
				t.Fatal(err)
			}
			if result.Packet.ImplementationActorKind != test.actor {
				t.Fatalf("actor kind = %q, want %q", result.Packet.ImplementationActorKind, test.actor)
			}
			if result.Packet.ExecutionAttemptRowID.Valid != test.tryID {
				t.Fatalf("attempt validity = %v, want %v", result.Packet.ExecutionAttemptRowID.Valid, test.tryID)
			}
			if result.Run.Status != workflowstore.RunStatusAuditReady {
				t.Fatalf("Run status = %q, want audit_ready", result.Run.Status)
			}
			if result.Artifact.Kind != "audit_packet" || result.Artifact.RelativePath != "audit-packets/"+result.Packet.AuditPacketID+"/audit-packet.json" {
				t.Fatalf("unexpected packet artifact: %#v", result.Artifact)
			}
			data, err := readWorkflowArtifact(fixture.store, result.Artifact, MaxWorkflowAuditPacketBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte(`"schema_version": "3.0"`)) {
				t.Fatalf("packet schema is not 3.0: %s", data)
			}
			if !bytes.Contains(data, []byte("diff --git a/internal/example.go")) {
				t.Fatal("canonical committed diff was not embedded")
			}
			artifacts, err := fixture.store.ListArtifactsByRun(ctx, fixture.run.ID)
			if err != nil {
				t.Fatal(err)
			}
			packetArtifacts := 0
			for _, artifact := range artifacts {
				if artifact.Kind == "audit_packet" {
					packetArtifacts++
				}
				if artifact.Kind == "unified_diff" {
					t.Fatalf("unexpected sidecar artifact created: %#v", artifact)
				}
			}
			if packetArtifacts != 1 {
				t.Fatalf("Run-owned audit packet artifacts = %d, want 1", packetArtifacts)
			}

			repeated, err := service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: commit})
			if err != nil {
				t.Fatal(err)
			}
			if repeated.Packet.ID != result.Packet.ID || repeated.Artifact.ID != result.Artifact.ID {
				t.Fatalf("repeated preparation did not reuse packet/artifact: first (%d,%d), second (%d,%d)", result.Packet.ID, result.Artifact.ID, repeated.Packet.ID, repeated.Artifact.ID)
			}
		})
	}
}

func TestWorkflowAuditPackageConstruction(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	if service, err := NewWorkflowAuditServiceWithSourceVaults(fixture.store, nil); err == nil || service != nil {
		t.Fatal("nil source-vault reader returned a service")
	}
	if service, err := NewWorkflowAuditServiceWithSourceVaults(nil, fixture.sourceVaultReader); err == nil || service != nil {
		t.Fatal("nil store returned a service")
	}
}

func TestWorkflowPackageAuditPrepareFreshness(t *testing.T) {
	t.Run("reload_package_execution_evidence", func(t *testing.T) {
		fixture, service := newPackageAuditPrepareFixture(t, true)
		state := capturePackageAuditPrepareState(t, fixture)
		loadErr := errors.New("reload package evidence failed")
		realLoader := service.loadPackageEvidence
		loads := 0
		service.loadPackageEvidence = func(ctx context.Context, runID string) (WorkflowPackageExecutionEvidence, error) {
			loads++
			if loads == 2 {
				return WorkflowPackageExecutionEvidence{}, loadErr
			}
			return realLoader(ctx, runID)
		}

		_, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("d", 40)})
		if !errors.Is(err, ErrWorkflowAuditPacketStale) || !errors.Is(err, loadErr) {
			t.Fatalf("error = %v, want stale error wrapping reload failure", err)
		}
		if loads != 2 {
			t.Fatalf("package evidence loads = %d, want 2", loads)
		}
		requirePackageAuditPrepareState(t, fixture, state)
	})

	t.Run("reinspect_audited_commit", func(t *testing.T) {
		fixture, service := newPackageAuditPrepareFixture(t, true)
		state := capturePackageAuditPrepareState(t, fixture)
		inspectErr := errors.New("reinspect audited commit failed")
		realInspector := service.inspector
		inspections := 0
		service.inspector = func(ctx context.Context, localPath, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
			inspections++
			if inspections == 2 {
				return workflowrepos.AuditCommitEvidence{}, inspectErr
			}
			return realInspector(ctx, localPath, branch, baseCommit, auditedCommit)
		}

		_, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("d", 40)})
		if !errors.Is(err, ErrWorkflowAuditPacketStale) || !errors.Is(err, inspectErr) {
			t.Fatalf("error = %v, want stale error wrapping reinspect failure", err)
		}
		if inspections != 2 {
			t.Fatalf("repository inspections = %d, want 2", inspections)
		}
		requirePackageAuditPrepareState(t, fixture, state)
	})
}

func TestWorkflowPackageAuditPrepareRollback(t *testing.T) {
	t.Run("package_authority_changes_before_persist", func(t *testing.T) {
		fixture, service := newPackageAuditPrepareFixture(t, false)
		state := capturePackageAuditPrepareState(t, fixture)
		realLoader := service.loadPackageEvidence
		loads := 0
		service.loadPackageEvidence = func(ctx context.Context, runID string) (WorkflowPackageExecutionEvidence, error) {
			evidence, err := realLoader(ctx, runID)
			loads++
			if err == nil && loads == 2 {
				removePackageForReloadFailure(t, fixture, evidence.Run.ExecutionPackageRowID.Int64)
			}
			return evidence, err
		}

		_, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("d", 40)})
		if !errors.Is(err, ErrWorkflowAuditPacketStale) || !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("error = %v, want stale error wrapping package reload failure", err)
		}
		requirePackageAuditPrepareState(t, fixture, state)
	})

	t.Run("commit_evidence_changes_during_reinspection", func(t *testing.T) {
		fixture, service := newPackageAuditPrepareFixture(t, true)
		state := capturePackageAuditPrepareState(t, fixture)
		realInspector := service.inspector
		inspections := 0
		service.inspector = func(ctx context.Context, localPath, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
			commit, err := realInspector(ctx, localPath, branch, baseCommit, auditedCommit)
			inspections++
			if err == nil && inspections == 2 {
				commit.Diff += "# changed during preparation\n"
			}
			return commit, err
		}

		_, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("d", 40)})
		if !errors.Is(err, ErrWorkflowAuditPacketStale) {
			t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
		}
		requirePackageAuditPrepareState(t, fixture, state)
	})

	t.Run("verified_package_evidence_changes_during_reload", func(t *testing.T) {
		fixture, service := newPackageAuditPrepareFixture(t, true)
		state := capturePackageAuditPrepareState(t, fixture)
		realLoader := service.loadPackageEvidence
		loads := 0
		service.loadPackageEvidence = func(ctx context.Context, runID string) (WorkflowPackageExecutionEvidence, error) {
			evidence, err := realLoader(ctx, runID)
			loads++
			if err == nil && loads == 2 {
				evidence.Validation = append(evidence.Validation, WorkflowPackageAuditValidationResult{})
			}
			return evidence, err
		}

		_, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("d", 40)})
		if !errors.Is(err, ErrWorkflowAuditPacketStale) {
			t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
		}
		requirePackageAuditPrepareState(t, fixture, state)
	})
}

func TestWorkflowPackageAuditPrepareReplacement(t *testing.T) {
	fixture, service := newPackageAuditPrepareFixture(t, false)
	service.inspector = packageReplacementTestInspector()
	ctx := context.Background()
	firstCommit := strings.Repeat("c", 40)
	first, err := service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: firstCommit})
	if err != nil {
		t.Fatal(err)
	}
	secondCommit := strings.Repeat("d", 40)
	second, err := service.Prepare(ctx, PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: secondCommit})
	if err != nil {
		t.Fatal(err)
	}
	if second.Packet.ID == first.Packet.ID || second.Artifact.ID == first.Artifact.ID {
		t.Fatalf("replacement reused packet/artifact: first (%d,%d), second (%d,%d)", first.Packet.ID, first.Artifact.ID, second.Packet.ID, second.Artifact.ID)
	}

	var firstStatus string
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT status FROM audit_packets WHERE id = ?`, first.Packet.ID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != workflowstore.AuditPacketStatusStale {
		t.Fatalf("first packet status = %q, want stale", firstStatus)
	}
	var currentCount int
	if err := fixture.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_packets WHERE run_row_id = ? AND status = ?`, fixture.run.ID, workflowstore.AuditPacketStatusCurrent).Scan(&currentCount); err != nil {
		t.Fatal(err)
	}
	if currentCount != 1 {
		t.Fatalf("current packet count = %d, want 1", currentCount)
	}

	data, err := readWorkflowArtifact(fixture.store, second.Artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		t.Fatal(err)
	}
	var packet WorkflowPackageAuditPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Repository.AuditedCommit != secondCommit || packet.Execution.CommittedSHA != secondCommit ||
		len(packet.ChangedFiles) != 1 || packet.ChangedFiles[0].Path != "internal/second.go" {
		t.Fatalf("replacement packet does not contain second commit evidence: %#v", packet)
	}
}

type packageAuditPrepareState struct {
	run                workflowstore.Run
	packetRows         int
	packetArtifacts    int
	ticketObligations  int
	packetDirectories  []string
	hasCurrent         bool
	currentPacket      workflowstore.AuditPacket
	currentPacketBytes []byte
}

func newPackageAuditPrepareFixture(t *testing.T, withCurrentPacket bool) (*packageEvidenceFixture, *WorkflowAuditService) {
	t.Helper()
	fixture := buildPackageEvidence(t, executor.EffectiveExecutorBriefDeterministicComplete)
	setPackageRunValidating(t, fixture)
	service, err := NewWorkflowAuditServiceWithSourceVaults(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.inspector = packagePrepareTestInspector()
	if withCurrentPacket {
		if _, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("c", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, service
}

func capturePackageAuditPrepareState(t *testing.T, fixture *packageEvidenceFixture) packageAuditPrepareState {
	t.Helper()
	ctx := context.Background()
	state := packageAuditPrepareState{}
	var err error
	state.run, err = fixture.store.GetRunByRunID(ctx, fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		query string
		into  *int
	}{
		{`SELECT COUNT(*) FROM audit_packets WHERE run_row_id = ?`, &state.packetRows},
		{`SELECT COUNT(*) FROM artifacts WHERE run_row_id = ? AND kind = 'audit_packet'`, &state.packetArtifacts},
		{`SELECT COUNT(*) FROM audit_packet_ticket_obligations WHERE audit_packet_row_id IN (SELECT id FROM audit_packets WHERE run_row_id = ?)`, &state.ticketObligations},
	} {
		if err := fixture.store.DB().QueryRowContext(ctx, item.query, fixture.run.ID).Scan(item.into); err != nil {
			t.Fatal(err)
		}
	}
	state.packetDirectories = packageAuditPacketDirectories(t, fixture)
	state.currentPacket, err = fixture.store.GetCurrentAuditPacketByRun(ctx, fixture.run.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return state
	}
	if err != nil {
		t.Fatal(err)
	}
	state.hasCurrent = true
	artifact, err := fixture.store.GetArtifactByRowID(ctx, state.currentPacket.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	state.currentPacketBytes, err = readWorkflowArtifact(fixture.store, artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requirePackageAuditPrepareState(t *testing.T, fixture *packageEvidenceFixture, want packageAuditPrepareState) {
	t.Helper()
	got := capturePackageAuditPrepareState(t, fixture)
	if got.run.Status != want.run.Status || got.packetRows != want.packetRows ||
		got.packetArtifacts != want.packetArtifacts || got.ticketObligations != want.ticketObligations ||
		!reflect.DeepEqual(got.packetDirectories, want.packetDirectories) || got.hasCurrent != want.hasCurrent {
		t.Fatalf("state after failed preparation = %#v, want %#v", got, want)
	}
	if want.hasCurrent && (!reflect.DeepEqual(got.currentPacket, want.currentPacket) || !bytes.Equal(got.currentPacketBytes, want.currentPacketBytes)) {
		t.Fatalf("current packet changed after failed preparation: got %#v, want %#v", got.currentPacket, want.currentPacket)
	}
}

func packageAuditPacketDirectories(t *testing.T, fixture *packageEvidenceFixture) []string {
	t.Helper()
	dir := filepath.Join(fixture.store.ArtifactStore().Root(), "audit-packets")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry.Name())
		}
	}
	return directories
}

func removePackageForReloadFailure(t *testing.T, fixture *packageEvidenceFixture, packageRowID int64) {
	t.Helper()
	db := fixture.store.DB()
	if _, err := db.Exec(`DROP TRIGGER execution_package_delete_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM execution_packages WHERE id = ?`, packageRowID); err != nil {
		t.Fatal(err)
	}
}

func packageReplacementTestInspector() WorkflowAuditInspector {
	return func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
		path := "internal/first.go"
		if auditedCommit == strings.Repeat("d", 40) {
			path = "internal/second.go"
		}
		return workflowrepos.AuditCommitEvidence{
			Branch: branch, BaseCommit: baseCommit, AuditedCommit: auditedCommit,
			ChangedFiles: []string{path},
			Diff:         "diff --git a/" + path + " b/" + path + "\n+package example\n",
			FileChanges:  []workflowrepos.AuditFileChange{{Path: path, ChangeType: "added", Additions: 1}},
		}, nil
	}
}

func packagePrepareTestInspector() WorkflowAuditInspector {
	return func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
		if auditedCommit != strings.Repeat("c", 40) && auditedCommit != strings.Repeat("d", 40) {
			return workflowrepos.AuditCommitEvidence{}, errors.New("unexpected audited commit")
		}
		return workflowrepos.AuditCommitEvidence{
			Branch: branch, BaseCommit: baseCommit, AuditedCommit: auditedCommit,
			ChangedFiles: []string{"internal/example.go"},
			Diff:         "diff --git a/internal/example.go b/internal/example.go\n+package example\n",
			FileChanges:  []workflowrepos.AuditFileChange{{Path: "internal/example.go", ChangeType: "added", Additions: 1}},
		}, nil
	}
}

func setPackageRunValidating(t *testing.T, fixture *packageEvidenceFixture) {
	t.Helper()
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(context.Background(), fixture.run.RunID)
		if err != nil {
			return err
		}
		switch run.Status {
		case "setup_ready":
			run, err = tx.TransitionRun(context.Background(), run.RunID, "setup_ready", "executing")
			if err != nil {
				return err
			}
			fallthrough
		case "executing":
			_, err = tx.TransitionRun(context.Background(), run.RunID, "executing", workflowstore.RunStatusValidating)
		case "execution_failed":
			_, err = tx.TransitionRun(context.Background(), run.RunID, "execution_failed", workflowstore.RunStatusValidating)
		case workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady:
			return nil
		default:
			return errors.New("package fixture is not in a transitionable Run state: " + run.Status)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
