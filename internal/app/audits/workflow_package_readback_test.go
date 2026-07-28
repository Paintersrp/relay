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

func TestWorkflowPackageAuditGetCurrentPacketModes(t *testing.T) {
	modes := []executor.EffectiveExecutorBriefMode{
		executor.EffectiveExecutorBriefAdaptiveNoOperations,
		executor.EffectiveExecutorBriefAdaptivePreflightFailed,
		executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication,
		executor.EffectiveExecutorBriefDeterministicComplete,
	}
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			fixture := buildPackageEvidence(t, mode)
			setPackageRunValidating(t, fixture)
			service, err := NewWorkflowAuditServiceWithSourceVaults(fixture.store, fixture.sourceVaultReader)
			if err != nil {
				t.Fatal(err)
			}
			service.inspector = packagePrepareTestInspector()
			service.packetValidator = func([]byte) (bool, error) {
				t.Fatal("package readback used the legacy packet validator")
				return false, nil
			}

			prepared, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{
				RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("c", 40),
			})
			if err != nil {
				t.Fatal(err)
			}
			before := capturePackageAuditPrepareState(t, fixture)
			current, err := service.GetCurrentPacket(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			exact, err := readWorkflowArtifact(fixture.store, prepared.Artifact, MaxWorkflowAuditPacketBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(current.Document, exact) {
				t.Fatal("readback document does not equal the persisted packet bytes")
			}
			current.Document[0] ^= 1
			stored, err := readWorkflowArtifact(fixture.store, prepared.Artifact, MaxWorkflowAuditPacketBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, exact) {
				t.Fatal("returned document aliases persisted packet bytes")
			}
			requirePackageAuditPrepareState(t, fixture, before)
		})
	}
}

func TestWorkflowPackageAuditGetCurrentPacketMarksStale(t *testing.T) {
	tests := []struct {
		name      string
		reason    string
		configure func(*testing.T, *packageEvidenceFixture, *WorkflowAuditService) error
		preserved error
	}{
		{
			name:   "packet_row_base_commit_disagrees",
			reason: "packet_integrity_failed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, _ *WorkflowAuditService) error {
				t.Helper()
				_, err := fixture.store.DB().Exec(`UPDATE audit_packets SET base_commit = ? WHERE run_row_id = ?`, strings.Repeat("d", 40), fixture.run.ID)
				return err
			},
		},
		{
			name:   "artifact_media_type_disagrees",
			reason: "packet_integrity_failed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				t.Helper()
				packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
				if err != nil {
					return err
				}
				_, err = fixture.store.DB().Exec(`UPDATE artifacts SET media_type = 'text/plain' WHERE id = ?`, packet.ArtifactRowID)
				return err
			},
		},
		{
			name:      "package_evidence_loader_fails",
			reason:    "package_execution_evidence_changed",
			preserved: errors.New("load package evidence"),
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
					return WorkflowPackageExecutionEvidence{}, errors.New("load package evidence")
				}
				return nil
			},
		},
		{
			name:      "repository_inspection_fails",
			reason:    "repository_state_changed",
			preserved: errors.New("inspect repository"),
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.inspector = func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{}, errors.New("inspect repository")
				}
				return nil
			},
		},
		{
			name:   "reconstructed_commit_evidence_changes",
			reason: "canonical_packet_changed",
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.inspector = func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{
						Branch: branch, BaseCommit: baseCommit, AuditedCommit: auditedCommit,
						ChangedFiles: []string{"internal/example.go"},
						Diff:         "diff --git a/internal/example.go b/internal/example.go\n+changed after preparation\n",
						FileChanges:  []workflowrepos.AuditFileChange{{Path: "internal/example.go", ChangeType: "added", Additions: 1}},
					}, nil
				}
				return nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditReadbackFixture(t)
			if err := test.configure(t, fixture, service); err != nil {
				t.Fatal(err)
			}
			_, err := service.GetCurrentPacket(context.Background(), fixture.run.RunID)
			if !errors.Is(err, ErrWorkflowAuditPacketStale) {
				t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
			}
			if test.preserved != nil && !strings.Contains(err.Error(), test.preserved.Error()) {
				t.Fatalf("error = %v, want preserved %q", err, test.preserved)
			}
			requirePackageAuditPacketStale(t, fixture, test.reason)
		})
	}
}

func TestWorkflowPackageAuditGetCurrentPacketWithoutEvidenceLoaderDoesNotMarkStale(t *testing.T) {
	fixture, service := newPackageAuditReadbackFixture(t)
	legacyOnly, err := NewWorkflowAuditServiceWithInspector(fixture.store, service.inspector)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyOnly.GetCurrentPacket(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowAuditPackageUnavailable) {
		t.Fatalf("error = %v, want ErrWorkflowAuditPackageUnavailable", err)
	}
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Status != workflowstore.AuditPacketStatusCurrent {
		t.Fatalf("packet status = %q, want current", packet.Status)
	}
}

func newPackageAuditReadbackFixture(t *testing.T) (*packageEvidenceFixture, *WorkflowAuditService) {
	t.Helper()
	fixture, service := newPackageAuditPrepareFixture(t, false)
	if _, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{
		RunID: fixture.run.RunID, AuditedCommit: strings.Repeat("c", 40),
	}); err != nil {
		t.Fatal(err)
	}
	return fixture, service
}

func requirePackageAuditPacketStale(t *testing.T, fixture *packageEvidenceFixture, reason string) {
	t.Helper()
	var status, staleReason string
	if err := fixture.store.DB().QueryRow(`SELECT status, stale_reason FROM audit_packets WHERE run_row_id = ?`, fixture.run.ID).Scan(&status, &staleReason); err != nil {
		t.Fatal(err)
	}
	if status != workflowstore.AuditPacketStatusStale || staleReason != reason {
		t.Fatalf("packet = (%q, %q), want (stale, %q)", status, staleReason, reason)
	}
}
