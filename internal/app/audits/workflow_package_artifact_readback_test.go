package audits

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
)

func TestWorkflowPackageAuditArtifactReadbackDeclaredReferences(t *testing.T) {
	for _, mode := range []executor.EffectiveExecutorBriefMode{
		executor.EffectiveExecutorBriefDeterministicComplete,
		executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication,
	} {
		t.Run(string(mode), func(t *testing.T) {
			fixture, service, current := newPackageArtifactReadbackFixture(t, mode)
			declared := []struct {
				name      string
				reference string
				artifact  workflowstore.Artifact
				wantOwner string
			}{
				{name: "execution_assignment", reference: currentPackageArtifactReference(current, "execution_assignment"), artifact: fixture.assignment.Artifact, wantOwner: workflowstore.ArtifactOwnerRun},
				{name: "effective_executor_brief", reference: currentPackageArtifactReference(current, "effective_executor_brief"), artifact: *fixture.brief.Artifact, wantOwner: workflowstore.ArtifactOwnerRun},
				{name: "deterministic_application.evidence", reference: currentPackageArtifactReference(current, "deterministic_application.evidence"), artifact: fixture.outcome.Artifact, wantOwner: workflowstore.ArtifactOwnerRun},
			}
			if mode != executor.EffectiveExecutorBriefDeterministicComplete {
				declared[1].wantOwner = workflowstore.ArtifactOwnerExecutionAttempt
			}
			for _, tc := range declared {
				t.Run(tc.name, func(t *testing.T) {
					wantBytes, err := readWorkflowArtifact(fixture.store, tc.artifact, MaxWorkflowAuditReadBytes)
					if err != nil {
						t.Fatal(err)
					}
					got, err := service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{
						RunID: fixture.run.RunID, ArtifactReference: tc.reference, MaxBytes: MaxWorkflowAuditReadBytes,
					})
					if err != nil {
						t.Fatal(err)
					}
					if got.Artifact.ArtifactID != tc.reference || got.Artifact.SHA256 != tc.artifact.SHA256 || !bytes.Equal(got.Content, wantBytes) || got.Truncated {
						t.Fatalf("readback = %#v, want reference=%q sha=%q bytes=%q truncated=false", got, tc.reference, tc.artifact.SHA256, wantBytes)
					}
					if got.Artifact.OwnerType != tc.wantOwner || got.Run.ID != current.Run.ID || got.Run.RunID != current.Run.RunID || got.Packet.AuditPacketID != current.Packet.AuditPacketID || got.Packet.ID != current.Packet.ID {
						t.Fatalf("readback identities = artifact=%#v run=%#v packet=%#v", got.Artifact, got.Run, got.Packet)
					}
				})
			}
		})
	}
}

func TestWorkflowPackageAuditArtifactReadbackRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name  string
		check func(*testing.T, *packageEvidenceFixture, *WorkflowAuditService, GetWorkflowAuditPacketResult)
	}{
		{name: "existing but undeclared same-Run artifact", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			content := []byte("undeclared artifact\n")
			createPackageArtifact(t, fixture, content, workflowstore.ArtifactOwnerRun, sql.NullInt64{Int64: fixture.run.ID, Valid: true}, sql.NullInt64{}, "undeclared")
			requirePackageArtifactError(t, service, fixture.run.RunID, "undeclared", 0, ErrWorkflowAuditArtifactReference)
		}},
		{name: "unknown or path-like reference", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, _ GetWorkflowAuditPacketResult) {
			requirePackageArtifactError(t, service, fixture.run.RunID, "missing", 0, ErrWorkflowAuditArtifactReference)
			requirePackageArtifactError(t, service, fixture.run.RunID, "../audit-packets/current", 0, ErrWorkflowAuditArtifactReference)
		}},
		{name: "ambiguous declared reference", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			evidence, err := service.loadPackageEvidence(context.Background(), fixture.run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			evidence.Assignment.Artifact.ArtifactID = evidence.EffectiveBrief.Artifact.ArtifactID
			evidence.Assignment.Artifact.SHA256 = evidence.EffectiveBrief.Artifact.SHA256
			service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) { return evidence, nil }
			if _, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: current.Packet.AuditedCommit}); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, evidence.EffectiveBrief.Artifact.ArtifactID, 0, ErrWorkflowAuditArtifactReference)
		}},
		{name: "declared SHA-256 disagrees with stored artifact", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			ref := currentPackageArtifactReference(current, "execution_assignment")
			if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET sha256 = ? WHERE artifact_id = ?`, strings.Repeat("f", 64), ref); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, ref, 0, ErrWorkflowAuditArtifactIntegrity)
		}},
		{name: "declared artifact has wrong owner", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			ref := currentPackageArtifactReference(current, "execution_assignment")
			var planID int64
			if err := fixture.store.DB().QueryRow(`SELECT id FROM plans WHERE plan_id = 'plan-package'`).Scan(&planID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET owner_type = ?, plan_row_id = ?, run_row_id = NULL, execution_attempt_row_id = NULL WHERE artifact_id = ?`, workflowstore.ArtifactOwnerPlan, planID, ref); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, ref, 0, ErrWorkflowAuditArtifactOwnership)
		}},
		{name: "declared non-text artifact", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			ref := currentPackageArtifactReference(current, "execution_assignment")
			if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET media_type = 'application/octet-stream' WHERE artifact_id = ?`, ref); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, ref, 0, ErrWorkflowAuditArtifactUnsupported)
		}},
		{name: "tampered artifact bytes", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			ref := currentPackageArtifactReference(current, "execution_assignment")
			artifact, err := fixture.store.GetArtifactByArtifactID(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			path, err := workflowArtifactPath(fixture.store, artifact)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("tampered\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, ref, 0, ErrWorkflowAuditArtifactIntegrity)
		}},
		{name: "tampered artifact size metadata", check: func(t *testing.T, fixture *packageEvidenceFixture, service *WorkflowAuditService, current GetWorkflowAuditPacketResult) {
			ref := currentPackageArtifactReference(current, "execution_assignment")
			if _, err := fixture.store.DB().Exec(`UPDATE artifacts SET size_bytes = size_bytes + 1 WHERE artifact_id = ?`, ref); err != nil {
				t.Fatal(err)
			}
			requirePackageArtifactError(t, service, fixture.run.RunID, ref, 0, ErrWorkflowAuditArtifactIntegrity)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, service, current := newPackageArtifactReadbackFixture(t, executor.EffectiveExecutorBriefDeterministicComplete)
			test.check(t, fixture, service, current)
		})
	}
}

func TestWorkflowPackageAuditArtifactReadbackTruncatesText(t *testing.T) {
	fixture, service, current := newPackageArtifactReadbackFixture(t, executor.EffectiveExecutorBriefDeterministicComplete)
	ref := currentPackageArtifactReference(current, "execution_assignment")
	full, err := fixture.store.GetArtifactByArtifactID(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if full.SizeBytes <= 1 {
		t.Fatalf("fixture artifact is too small for bounded-read coverage: %#v", full)
	}
	got, err := service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{RunID: fixture.run.RunID, ArtifactReference: ref, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Content) != 1 {
		t.Fatalf("bounded read = %#v, want one byte and truncated=true", got)
	}
}

func newPackageArtifactReadbackFixture(t *testing.T, mode executor.EffectiveExecutorBriefMode) (*packageEvidenceFixture, *WorkflowAuditService, GetWorkflowAuditPacketResult) {
	t.Helper()
	fixture, service := newPackageAuditReadbackFixtureForMode(t, mode)
	current, err := service.GetCurrentPacket(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.loadPackageEvidence(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if mode != executor.EffectiveExecutorBriefDeterministicComplete {
		if evidence.Attempt == nil {
			t.Fatal("adaptive package evidence has no execution attempt")
		}
		attemptArtifact := createPackageArtifact(t, fixture, evidence.EffectiveBrief.Bytes, workflowstore.ArtifactOwnerExecutionAttempt, sql.NullInt64{}, sql.NullInt64{Int64: evidence.Attempt.Attempt.ID, Valid: true}, "synthetic-effective-brief")
		evidence.EffectiveBrief.Artifact = &attemptArtifact
		fixture.brief.Artifact = &attemptArtifact
		service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) { return evidence, nil }
		if _, err := service.Prepare(context.Background(), PrepareWorkflowAuditInput{RunID: fixture.run.RunID, AuditedCommit: current.Packet.AuditedCommit}); err != nil {
			t.Fatal(err)
		}
		current, err = service.GetCurrentPacket(context.Background(), fixture.run.RunID)
		if err != nil {
			t.Fatal(err)
		}
	}
	service.loadPackageEvidence = func(context.Context, string) (WorkflowPackageExecutionEvidence, error) { return evidence, nil }
	return fixture, service, current
}

func currentPackageArtifactReference(current GetWorkflowAuditPacketResult, role string) string {
	var packet WorkflowPackageAuditPacket
	if err := json.Unmarshal(current.Document, &packet); err != nil {
		panic(err)
	}
	switch role {
	case "execution_assignment":
		return packet.Authority.ExecutionAssignment.ArtifactReference
	case "effective_executor_brief":
		return packet.Authority.EffectiveExecutorBrief.ArtifactReference
	case "deterministic_application.evidence":
		return packet.DeterministicApplication.Evidence.ArtifactReference
	default:
		panic("unknown package artifact role")
	}
}

func createPackageArtifact(t *testing.T, fixture *packageEvidenceFixture, content []byte, ownerType string, runRowID, attemptRowID sql.NullInt64, name string) workflowstore.Artifact {
	t.Helper()
	artifactID := "artifact-" + name
	relativePath := filepath.ToSlash(filepath.Join("runs", fixture.run.RunID, name+".txt"))
	path := filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := workflowstore.Artifact{ArtifactID: artifactID, OwnerType: ownerType, RunRowID: runRowID, ExecutionAttemptRowID: attemptRowID, Kind: "undeclared", RelativePath: relativePath, MediaType: "text/plain", SHA256: packageEvidenceSHA(content), SizeBytes: int64(len(content))}
	if err := fixture.store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateArtifact(context.Background(), workflowstore.CreateArtifactParams{
			ArtifactID: artifact.ArtifactID, OwnerType: artifact.OwnerType, RunRowID: artifact.RunRowID, ExecutionAttemptRowID: artifact.ExecutionAttemptRowID,
			Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func requirePackageArtifactError(t *testing.T, service *WorkflowAuditService, runID, reference string, maxBytes int, target error) {
	t.Helper()
	_, err := service.GetCurrentArtifact(context.Background(), GetWorkflowAuditArtifactInput{RunID: runID, ArtifactReference: reference, MaxBytes: maxBytes})
	if !errors.Is(err, target) {
		t.Fatalf("artifact reference %q error = %v, want %v", reference, err, target)
	}
}
