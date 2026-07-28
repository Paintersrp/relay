package audits

import (
	"bytes"
	"context"
	"errors"
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
				if artifact.Kind == "unified_diff" || artifact.Kind == "ticket_package_evidence" {
					t.Fatalf("legacy sidecar artifact created: %#v", artifact)
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

func TestWorkflowAuditPrepareLegacy(t *testing.T) {
	fixture := newPackageEvidenceFixture(t, false, "")
	setPackageRunValidating(t, fixture)
	service, err := NewWorkflowAuditServiceWithInspector(fixture.store, packagePrepareTestInspector())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("c", 40)})
	if !errors.Is(err, ErrWorkflowAuditPackageUnavailable) {
		t.Fatalf("error = %v, want ErrWorkflowAuditPackageUnavailable", err)
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
