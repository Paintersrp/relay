package audits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditGetCurrentPacketModes(t *testing.T) {
	modes := []executor.ExecutionMode{
		executor.ExecutionModeAbsent,
		executor.ExecutionModePreflightFailed,
		executor.ExecutionModePartialApplied,
		executor.ExecutionModeCompleteApplied,
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
		mode      executor.ExecutionMode
		configure func(*testing.T, *packageEvidenceFixture, *WorkflowAuditService) error
		preserved error
	}{
		{
			name:   "packet_row_base_commit_disagrees",
			reason: "packet_integrity_failed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, _ *WorkflowAuditService) error {
				t.Helper()
				allowPackageAuditPacketMutation(t, fixture)
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
			name:   "run_evidence_identity_disagrees",
			reason: "package_execution_evidence_changed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				overridePackageEvidence(t, fixture, service, func(evidence *WorkflowPackageExecutionEvidence) {
					evidence.Run.RunID = "run-different"
				})
				return nil
			},
		},
		{
			name:   "actor_kind_disagrees",
			reason: "package_execution_evidence_changed",
			mode:   executor.ExecutionModeAbsent,
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				overridePackageEvidence(t, fixture, service, func(evidence *WorkflowPackageExecutionEvidence) {
					evidence.Mode = executor.ExecutionModePartialApplied
				})
				return nil
			},
		},
		{
			name:   "execution_attempt_disagrees",
			reason: "package_execution_evidence_changed",
			mode:   executor.ExecutionModeAbsent,
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
				if err != nil {
					return err
				}
				overridePackageEvidence(t, fixture, service, func(evidence *WorkflowPackageExecutionEvidence) {
					evidence.Attempt.Attempt.ID = packet.ExecutionAttemptRowID.Int64 + 1
				})
				return nil
			},
		},
		{
			name:   "repository_target_fails",
			reason: "repository_state_changed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				overridePackageEvidence(t, fixture, service, func(*WorkflowPackageExecutionEvidence) {})
				if _, err := fixture.store.DB().Exec(`PRAGMA foreign_keys = OFF`); err != nil {
					return err
				}
				_, err := fixture.store.DB().Exec(`DELETE FROM repository_targets WHERE repo_target = ?`, fixture.run.RepoTarget)
				return err
			},
		},
		{
			name:   "repository_branch_disagrees",
			reason: "repository_state_changed",
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.inspector = func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{Branch: branch + "-changed", BaseCommit: baseCommit, AuditedCommit: auditedCommit}, nil
				}
				return nil
			},
		},
		{
			name:   "repository_base_commit_disagrees",
			reason: "repository_state_changed",
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.inspector = func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{Branch: branch, BaseCommit: strings.Repeat("d", 40), AuditedCommit: auditedCommit}, nil
				}
				return nil
			},
		},
		{
			name:   "repository_audited_commit_disagrees",
			reason: "repository_state_changed",
			configure: func(_ *testing.T, _ *packageEvidenceFixture, service *WorkflowAuditService) error {
				service.inspector = func(_ context.Context, _ string, branch, baseCommit, auditedCommit string) (workflowrepos.AuditCommitEvidence, error) {
					return workflowrepos.AuditCommitEvidence{Branch: branch, BaseCommit: baseCommit, AuditedCommit: strings.Repeat("d", 40)}, nil
				}
				return nil
			},
		},
		{
			name:   "input_assembly_fails",
			reason: "canonical_packet_changed",
			configure: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService) error {
				overridePackageEvidence(t, fixture, service, func(evidence *WorkflowPackageExecutionEvidence) {
					evidence.Authority.TicketRevision.Goal = ""
				})
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
			mode := test.mode
			if mode == "" {
				mode = executor.ExecutionModeCompleteApplied
			}
			fixture, service := newPackageAuditReadbackFixtureForMode(t, mode)
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

func overridePackageEvidence(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, mutate func(*WorkflowPackageExecutionEvidence)) {
	t.Helper()
	evidence, err := service.loadPackageEvidence(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&evidence)
	service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) {
		return evidence, nil
	}
}

func TestWorkflowPackageAuditGetCurrentPacketRejectsMalformedAndNoncanonicalDocuments(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		mutate func([]byte) []byte
	}{
		{
			name:   "malformed_json",
			reason: "packet_integrity_failed",
			mutate: func([]byte) []byte { return []byte(`{"schema_version":`) },
		},
		{
			name:   "changed_indentation",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte {
				return bytes.Replace(data, []byte("  \""), []byte("    \""), 1)
			},
		},
		{
			name:   "property_reordering",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte {
				var properties map[string]json.RawMessage
				if err := json.Unmarshal(data, &properties); err != nil {
					t.Fatal(err)
				}
				reordered, err := json.MarshalIndent(properties, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				return append(reordered, '\n')
			},
		},
		{
			name:   "wrong_schema_version",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte {
				return bytes.Replace(data, []byte(`"schema_version": "4.0"`), []byte(`"schema_version": "3.0"`), 1)
			},
		},
		{
			name:   "unknown_property",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte {
				return bytes.Replace(data, []byte("{\n"), []byte("{\n  \"unknown\": true,\n"), 1)
			},
		},
		{
			name:   "duplicate_property",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte {
				return bytes.Replace(data, []byte("{\n"), []byte("{\n  \"schema_version\": \"4.0\",\n"), 1)
			},
		},
		{
			name:   "missing_trailing_newline",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte { return bytes.TrimSuffix(data, []byte("\n")) },
		},
		{
			name:   "multiple_trailing_newlines",
			reason: "packet_schema_readback_failed",
			mutate: func(data []byte) []byte { return append(append([]byte(nil), data...), '\n') },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service := newPackageAuditReadbackFixture(t)
			data := currentPackagePacketBytes(t, fixture)
			replaceCurrentPackagePacketBytes(t, fixture, test.mutate(data))
			_, err := service.GetCurrentPacket(context.Background(), fixture.run.RunID)
			if !errors.Is(err, ErrWorkflowAuditPacketStale) {
				t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
			}
			requirePackageAuditPacketStale(t, fixture, test.reason)
		})
	}
}

func TestWorkflowPackageAuditGetCurrentPacketRejectsCoherentlyAlteredCanonicalDocument(t *testing.T) {
	fixture, service := newPackageAuditReadbackFixture(t)
	var document WorkflowPackageAuditPacket
	if err := json.Unmarshal(currentPackagePacketBytes(t, fixture), &document); err != nil {
		t.Fatal(err)
	}
	document.Run.UserIntent = "coherently altered persisted packet"
	altered, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	replaceCurrentPackagePacketBytes(t, fixture, append(altered, '\n'))
	if _, err := service.GetCurrentPacket(context.Background(), fixture.run.RunID); !errors.Is(err, ErrWorkflowAuditPacketStale) {
		t.Fatalf("error = %v, want ErrWorkflowAuditPacketStale", err)
	}
	requirePackageAuditPacketStale(t, fixture, "canonical_packet_changed")
}

func newPackageAuditReadbackFixture(t *testing.T) (*packageEvidenceFixture, *WorkflowAuditService) {
	return newPackageAuditReadbackFixtureForMode(t, executor.ExecutionModeCompleteApplied)
}

func newPackageAuditReadbackFixtureForMode(t *testing.T, mode executor.ExecutionMode) (*packageEvidenceFixture, *WorkflowAuditService) {
	t.Helper()
	fixture := buildPackageEvidence(t, mode)
	setPackageRunValidating(t, fixture)
	service, err := NewWorkflowAuditServiceWithSourceVaults(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	service.inspector = packagePrepareTestInspector()
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

func currentPackagePacketBytes(t *testing.T, fixture *packageEvidenceFixture) []byte {
	t.Helper()
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := fixture.store.GetArtifactByRowID(context.Background(), packet.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := readWorkflowArtifact(fixture.store, artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func replaceCurrentPackagePacketBytes(t *testing.T, fixture *packageEvidenceFixture, data []byte) {
	t.Helper()
	allowPackageAuditPacketMutation(t, fixture)
	packet, err := fixture.store.GetCurrentAuditPacketByRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := fixture.store.GetArtifactByRowID(context.Background(), packet.ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := workflowArtifactPath(fixture.store, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256HexBytes(data)
	if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET sha256 = ?, size_bytes = ? WHERE id = ?`, digest, len(data), artifact.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec(`UPDATE audit_packets SET packet_sha256 = ? WHERE id = ?`, digest, packet.ID); err != nil {
		t.Fatal(err)
	}
}

func allowPackageAuditPacketMutation(t *testing.T, fixture *packageEvidenceFixture) {
	t.Helper()
	if _, err := fixture.store.DB().Exec(`DROP TRIGGER IF EXISTS audit_packet_identity_immutable`); err != nil {
		t.Fatal(err)
	}
}
