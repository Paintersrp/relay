package audits

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/artifactschema"
	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type workflowPackageEvidenceLoader func(context.Context, string) (WorkflowPackageExecutionEvidence, error)

type WorkflowAuditService struct {
	store               *workflowstore.Store
	inspector           WorkflowAuditInspector
	loadPackageEvidence workflowPackageEvidenceLoader
	packetValidator     func([]byte) (bool, error)
}

func NewWorkflowAuditService(store *workflowstore.Store) (*WorkflowAuditService, error) {
	return newWorkflowAuditService(store, workflowrepos.InspectAuditCommit, nil)
}

func NewWorkflowAuditServiceWithInspector(store *workflowstore.Store, inspector WorkflowAuditInspector) (*WorkflowAuditService, error) {
	return newWorkflowAuditService(store, inspector, nil)
}

func NewWorkflowAuditServiceWithSourceVaults(
	store *workflowstore.Store,
	sourceVaults workflowpackages.SourceVaultReader,
) (*WorkflowAuditService, error) {
	if sourceVaults == nil {
		return nil, fmt.Errorf("source-vault reader is required")
	}
	evidenceService, err := NewWorkflowPackageExecutionEvidenceService(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	return newWorkflowAuditService(store, workflowrepos.InspectAuditCommit, evidenceService.Load)
}

func newWorkflowAuditService(
	store *workflowstore.Store,
	inspector WorkflowAuditInspector,
	loadPackageEvidence workflowPackageEvidenceLoader,
) (*WorkflowAuditService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if inspector == nil {
		return nil, fmt.Errorf("workflow audit inspector is required")
	}
	return &WorkflowAuditService{
		store: store, inspector: inspector, loadPackageEvidence: loadPackageEvidence,
		packetValidator: func(raw []byte) (bool, error) {
			return artifactschema.Validate(artifactschema.KindAuditPacket, raw)
		},
	}, nil
}

func (s *WorkflowAuditService) Prepare(ctx context.Context, input PrepareWorkflowAuditInput) (PrepareWorkflowAuditResult, error) {
	input.RunID = strings.TrimSpace(input.RunID)
	input.AuditedCommit = strings.ToLower(strings.TrimSpace(input.AuditedCommit))
	if input.RunID == "" || len(input.AuditedCommit) != 40 {
		return PrepareWorkflowAuditResult{}, fmt.Errorf("run_id and full audited_commit are required")
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	if run.Status != workflowstore.RunStatusValidating && run.Status != workflowstore.RunStatusAuditReady {
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditNotReady
	}
	if run.ExecutionPackageRowID.Valid {
		return s.preparePackage(ctx, run, input)
	}
	implementation, err := resolveWorkflowImplementationEvidence(ctx, s.store, run)
	if err != nil {
		return PrepareWorkflowAuditResult{}, fmt.Errorf("resolve implementation evidence: %w", err)
	}
	ticketPackage, err := resolveWorkflowAuditTicketPackage(ctx, s.store, run, implementation)
	if err != nil {
		return PrepareWorkflowAuditResult{}, fmt.Errorf("resolve ticket package evidence: %w", err)
	}
	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	commit, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, input.AuditedCommit)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	if current, currentErr := s.GetCurrentPacket(ctx, run.RunID); currentErr == nil &&
		current.Packet.AuditedCommit == input.AuditedCommit &&
		current.Packet.ImplementationActorKind == implementation.ActorKind &&
		current.Packet.ExecutionAttemptRowID == implementationExecutionAttemptRowID(implementation) {
		return PrepareWorkflowAuditResult{Run: current.Run, Packet: current.Packet, Artifact: current.Artifact}, nil
	}

	packetID := workflowstore.NewAuditPacketID()
	batch, err := s.store.ArtifactStore().Begin("audit-packets/" + packetID)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	diffArtifactID := workflowstore.NewArtifactID()
	stagedDiff, err := batch.Stage("unified_diff", "unified-diff.patch", "text/x-diff; charset=utf-8", []byte(executor.RedactSensitiveText(commit.Diff)))
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, err
	}
	diffArtifact := workflowstore.Artifact{ArtifactID: diffArtifactID, OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: stagedDiff.Kind, RelativePath: stagedDiff.RelativePath, MediaType: stagedDiff.MediaType, SHA256: stagedDiff.SHA256, SizeBytes: stagedDiff.SizeBytes}
	stagedTicketPackage := workflowAuditStagedTicketPackage{}
	if ticketPackage != nil {
		stagedTicketPackage, err = stageWorkflowAuditTicketPackage(batch, ticketPackage, commit, diffArtifact)
		if err != nil {
			_ = batch.Rollback()
			return PrepareWorkflowAuditResult{}, err
		}
	}
	packetBytes, err := buildWorkflowAuditPacket(ctx, s.store, run, packetID, implementation, commit, diffArtifact, stagedTicketPackage.Artifacts)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, err
	}
	validPacket, err := s.packetValidator(packetBytes)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, fmt.Errorf("validate generated workflow audit packet: %w", err)
	}
	if !validPacket {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketSchemaInvalid
	}
	freshTicketPackage, ticketErr := resolveWorkflowAuditTicketPackage(ctx, s.store, run, implementation)
	if ticketErr != nil || (ticketPackage == nil) != (freshTicketPackage == nil) ||
		(ticketPackage != nil && !sameWorkflowAuditTicketPackageBasis(ticketPackage.Evidence, freshTicketPackage.Evidence)) {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}
	staged, err := batch.Stage("audit_packet", "audit-packet.json", "application/json", packetBytes)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, err
	}

	result := PrepareWorkflowAuditResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		currentRun, err := tx.GetRunByRunID(ctx, run.RunID)
		if err != nil {
			return err
		}
		if currentRun.Status != workflowstore.RunStatusValidating && currentRun.Status != workflowstore.RunStatusAuditReady {
			return ErrWorkflowAuditNotReady
		}
		currentImplementation, err := resolveWorkflowImplementationEvidenceWithReader(ctx, s.store, tx, currentRun)
		if err != nil {
			return err
		}
		if currentImplementation.ActorKind != implementation.ActorKind || implementationExecutionAttemptRowID(currentImplementation) != implementationExecutionAttemptRowID(implementation) {
			return ErrWorkflowAuditPacketStale
		}
		currentRepo, err := tx.GetRepositoryTarget(ctx, currentRun.RepoTarget)
		if err != nil {
			return err
		}
		verifiedCommit, err := s.inspector(ctx, currentRepo.LocalPath, currentRun.Branch, currentRun.BaseCommit, input.AuditedCommit)
		if err != nil {
			return err
		}
		if verifiedCommit.AuditedCommit != commit.AuditedCommit ||
			verifiedCommit.Diff != commit.Diff ||
			strings.Join(verifiedCommit.ChangedFiles, "\x00") != strings.Join(commit.ChangedFiles, "\x00") {
			return ErrWorkflowAuditPacketStale
		}
		if err := tx.MarkCurrentAuditPacketsStale(ctx, currentRun.ID, "superseded_by_new_packet"); err != nil {
			return err
		}
		if _, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: diffArtifactID, OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: currentRun.ID, Valid: true}, Kind: stagedDiff.Kind, RelativePath: stagedDiff.RelativePath, MediaType: stagedDiff.MediaType, SHA256: stagedDiff.SHA256, SizeBytes: stagedDiff.SizeBytes}); err != nil {
			return err
		}
		for _, stagedArtifact := range stagedTicketPackage.Artifacts {
			if _, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
				ArtifactID: stagedArtifact.ArtifactID, OwnerType: workflowstore.ArtifactOwnerRun,
				RunRowID: sql.NullInt64{Int64: currentRun.ID, Valid: true}, Kind: stagedArtifact.Kind,
				RelativePath: stagedArtifact.RelativePath, MediaType: stagedArtifact.MediaType,
				SHA256: stagedArtifact.SHA256, SizeBytes: stagedArtifact.SizeBytes,
			}); err != nil {
				return err
			}
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:   workflowstore.NewArtifactID(),
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: currentRun.ID, Valid: true},
			Kind:         staged.Kind,
			RelativePath: staged.RelativePath,
			MediaType:    staged.MediaType,
			SHA256:       staged.SHA256,
			SizeBytes:    staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		packet, err := tx.CreateAuditPacket(ctx, workflowstore.CreateAuditPacketParams{
			AuditPacketID:           packetID,
			RunRowID:                currentRun.ID,
			ImplementationActorKind: currentImplementation.ActorKind,
			ExecutionAttemptRowID:   implementationExecutionAttemptRowID(currentImplementation),
			ArtifactRowID:           artifact.ID,
			BaseCommit:              currentRun.BaseCommit,
			AuditedCommit:           input.AuditedCommit,
			PacketSHA256:            staged.SHA256,
		})
		if err != nil {
			return err
		}
		if err := bindWorkflowAuditPacketTicketObligations(ctx, tx, currentRun, packet); err != nil {
			return err
		}
		if currentRun.Status == workflowstore.RunStatusValidating {
			currentRun, err = tx.TransitionRun(ctx, currentRun.RunID, workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady)
			if err != nil {
				return err
			}
		}
		result = PrepareWorkflowAuditResult{Run: currentRun, Packet: packet, Artifact: artifact}
		return nil
	})
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	return result, nil
}

func (s *WorkflowAuditService) preparePackage(
	ctx context.Context,
	run workflowstore.Run,
	input PrepareWorkflowAuditInput,
) (PrepareWorkflowAuditResult, error) {
	if s.loadPackageEvidence == nil {
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPackageUnavailable
	}

	evidence, err := s.loadPackageEvidence(ctx, run.RunID)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	if !sameWorkflowPackageRunIdentity(run, evidence.Run) {
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}

	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	commit, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, input.AuditedCommit)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	if commit.AuditedCommit != input.AuditedCommit {
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}

	packetInput, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	_, packetBytes, err := buildWorkflowPackageAuditPacket(packetInput)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	actorKind, attemptRowID, err := packageAuditPersistenceMetadata(evidence)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	if result, ok := s.currentPackagePreparation(ctx, run, input.AuditedCommit, actorKind, attemptRowID, packetBytes); ok {
		return result, nil
	}

	packetID := workflowstore.NewAuditPacketID()
	batch, err := s.store.ArtifactStore().Begin("audit-packets/" + packetID)
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	staged, err := batch.Stage("audit_packet", "audit-packet.json", "application/json", packetBytes)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, err
	}

	freshEvidence, err := s.loadPackageEvidence(ctx, run.RunID)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, fmt.Errorf("%w: reload package execution evidence: %w", ErrWorkflowAuditPacketStale, err)
	}
	if !sameWorkflowPackageRunIdentity(run, freshEvidence.Run) || !reflect.DeepEqual(evidence, freshEvidence) {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}
	freshCommit, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, input.AuditedCommit)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, fmt.Errorf("%w: reinspect audited commit: %w", ErrWorkflowAuditPacketStale, err)
	}
	if freshCommit.AuditedCommit != input.AuditedCommit || !reflect.DeepEqual(commit, freshCommit) {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}
	freshInput, err := assemblePackageAuditInput(freshEvidence, freshCommit)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}
	_, freshPacketBytes, err := buildWorkflowPackageAuditPacket(freshInput)
	if err != nil {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}
	if !bytes.Equal(packetBytes, freshPacketBytes) {
		_ = batch.Rollback()
		return PrepareWorkflowAuditResult{}, ErrWorkflowAuditPacketStale
	}

	result := PrepareWorkflowAuditResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		currentRun, err := tx.GetRunByRunID(ctx, run.RunID)
		if err != nil {
			return err
		}
		if currentRun.Status != workflowstore.RunStatusValidating && currentRun.Status != workflowstore.RunStatusAuditReady {
			return ErrWorkflowAuditNotReady
		}
		if !sameWorkflowPackageRunIdentity(run, currentRun) ||
			currentRun.ExecutionPackageRowID != evidence.Run.ExecutionPackageRowID ||
			currentRun.PackageApprovalRowID != evidence.Run.PackageApprovalRowID {
			return ErrWorkflowAuditPacketStale
		}

		pkg, err := tx.GetExecutionPackageByRowID(ctx, currentRun.ExecutionPackageRowID.Int64)
		if err != nil {
			return fmt.Errorf("%w: reload execution package: %w", ErrWorkflowAuditPacketStale, err)
		}
		approval, err := tx.GetRunExecutionPackageApproval(ctx, currentRun.ID)
		if err != nil {
			return ErrWorkflowAuditPacketStale
		}
		if pkg.ID != evidence.Authority.Package.ID || pkg.PackageSha256 != evidence.Authority.Package.PackageSha256 ||
			pkg.RepoTarget != currentRun.RepoTarget || pkg.Branch != currentRun.Branch || pkg.BaseCommit != currentRun.BaseCommit ||
			approval.ID != evidence.Authority.PackageApproval.ID || approval.PackageRowID != pkg.ID || approval.PackageSha256 != pkg.PackageSha256 {
			return ErrWorkflowAuditPacketStale
		}
		currentActorKind, currentAttemptRowID, err := packageAuditPersistenceMetadata(evidence)
		if err != nil || currentActorKind != actorKind || currentAttemptRowID != attemptRowID {
			return ErrWorkflowAuditPacketStale
		}

		if err := tx.MarkCurrentAuditPacketsStale(ctx, currentRun.ID, "superseded_by_new_packet"); err != nil {
			return err
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun,
			RunRowID: sql.NullInt64{Int64: currentRun.ID, Valid: true}, Kind: staged.Kind,
			RelativePath: staged.RelativePath, MediaType: staged.MediaType,
			SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		packet, err := tx.CreateAuditPacket(ctx, workflowstore.CreateAuditPacketParams{
			AuditPacketID: packetID, RunRowID: currentRun.ID,
			ImplementationActorKind: actorKind, ExecutionAttemptRowID: attemptRowID,
			ArtifactRowID: artifact.ID, BaseCommit: currentRun.BaseCommit,
			AuditedCommit: input.AuditedCommit, PacketSHA256: staged.SHA256,
		})
		if err != nil {
			return err
		}
		if err := bindWorkflowAuditPacketTicketObligations(ctx, tx, currentRun, packet); err != nil {
			return err
		}
		if err := tx.RecheckPacketApprovalConsistency(ctx, packet.ID); err != nil {
			return ErrWorkflowAuditPacketStale
		}
		if currentRun.Status == workflowstore.RunStatusValidating {
			currentRun, err = tx.TransitionRun(ctx, currentRun.RunID, workflowstore.RunStatusValidating, workflowstore.RunStatusAuditReady)
			if err != nil {
				return err
			}
		}
		result = PrepareWorkflowAuditResult{Run: currentRun, Packet: packet, Artifact: artifact}
		return nil
	})
	if err != nil {
		return PrepareWorkflowAuditResult{}, err
	}
	return result, nil
}

func sameWorkflowPackageRunIdentity(expected, actual workflowstore.Run) bool {
	return expected.ID == actual.ID && expected.RunID == actual.RunID &&
		expected.RepoTarget == actual.RepoTarget && expected.Branch == actual.Branch &&
		expected.BaseCommit == actual.BaseCommit &&
		expected.ExecutionPackageRowID == actual.ExecutionPackageRowID &&
		expected.PackageApprovalRowID == actual.PackageApprovalRowID
}

func packageAuditPersistenceMetadata(evidence WorkflowPackageExecutionEvidence) (string, sql.NullInt64, error) {
	mode := evidence.EffectiveBrief.Mode
	switch mode {
	case executor.EffectiveExecutorBriefAdaptiveNoOperations, executor.EffectiveExecutorBriefAdaptivePreflightFailed:
		if evidence.Attempt == nil {
			return "", sql.NullInt64{}, ErrWorkflowAuditPacketStale
		}
		return workflowstore.ImplementationActorExecutor, sql.NullInt64{Int64: evidence.Attempt.Attempt.ID, Valid: true}, nil
	case executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication:
		if evidence.Attempt == nil {
			return "", sql.NullInt64{}, ErrWorkflowAuditPacketStale
		}
		return workflowstore.ImplementationActorHybrid, sql.NullInt64{Int64: evidence.Attempt.Attempt.ID, Valid: true}, nil
	case executor.EffectiveExecutorBriefDeterministicComplete:
		if evidence.Attempt != nil {
			return "", sql.NullInt64{}, ErrWorkflowAuditPacketStale
		}
		return workflowstore.ImplementationActorApplier, sql.NullInt64{}, nil
	default:
		return "", sql.NullInt64{}, ErrWorkflowAuditPacketStale
	}
}

func (s *WorkflowAuditService) currentPackagePreparation(
	ctx context.Context,
	run workflowstore.Run,
	auditedCommit, actorKind string,
	attemptRowID sql.NullInt64,
	packetBytes []byte,
) (PrepareWorkflowAuditResult, bool) {
	current, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if err != nil {
		return PrepareWorkflowAuditResult{}, false
	}
	if current.RunRowID != run.ID || current.BaseCommit != run.BaseCommit || current.AuditedCommit != auditedCommit ||
		current.ImplementationActorKind != actorKind || current.ExecutionAttemptRowID != attemptRowID ||
		current.PacketSHA256 != sha256HexBytes(packetBytes) {
		return PrepareWorkflowAuditResult{}, false
	}
	artifact, err := s.store.GetArtifactByRowID(ctx, current.ArtifactRowID)
	if err != nil || artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID ||
		artifact.Kind != "audit_packet" || artifact.RelativePath != "audit-packets/"+current.AuditPacketID+"/audit-packet.json" ||
		artifact.MediaType != "application/json" || artifact.SHA256 != current.PacketSHA256 || artifact.SHA256 != sha256HexBytes(packetBytes) || artifact.SizeBytes != int64(len(packetBytes)) {
		return PrepareWorkflowAuditResult{}, false
	}
	data, err := readWorkflowArtifact(s.store, artifact, MaxWorkflowAuditPacketBytes)
	if err != nil || !bytes.Equal(data, packetBytes) {
		return PrepareWorkflowAuditResult{}, false
	}
	return PrepareWorkflowAuditResult{Run: run, Packet: current, Artifact: artifact}, true
}

func (s *WorkflowAuditService) GetCurrentPacket(ctx context.Context, runID string) (GetWorkflowAuditPacketResult, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	packet, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketNotFound
		}
		return GetWorkflowAuditPacketResult{}, err
	}
	if run.ExecutionPackageRowID.Valid {
		return s.getCurrentPackagePacket(ctx, run, packet)
	}
	implementation, err := resolveWorkflowImplementationEvidence(ctx, s.store, run)
	if err != nil || implementation.ActorKind != packet.ImplementationActorKind || implementationExecutionAttemptRowID(implementation) != packet.ExecutionAttemptRowID {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "later_execution_attempt")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	if _, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, packet.AuditedCommit); err != nil {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "repository_state_changed")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	artifact, err := s.store.GetArtifactByRowID(ctx, packet.ArtifactRowID)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	data, err := readWorkflowArtifact(s.store, artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "packet_integrity_failed")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	if sha256HexBytes(data) != packet.PacketSHA256 || packet.PacketSHA256 != artifact.SHA256 {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "packet_integrity_failed")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	var document WorkflowAuditPacket
	if err := json.Unmarshal(data, &document); err != nil {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "packet_schema_readback_failed")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	if err := verifyWorkflowAuditTicketPackageEvidence(ctx, s.store, run, implementation, packet, document); err != nil {
		_ = s.store.MarkCurrentAuditPacketsStale(ctx, run.ID, "ticket_package_evidence_changed")
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	return GetWorkflowAuditPacketResult{
		Run: run, Packet: packet, Artifact: artifact,
		Document: append(json.RawMessage(nil), data...),
	}, nil
}

func (s *WorkflowAuditService) getCurrentPackagePacket(
	ctx context.Context,
	run workflowstore.Run,
	packet workflowstore.AuditPacket,
) (GetWorkflowAuditPacketResult, error) {
	if s.loadPackageEvidence == nil {
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPackageUnavailable
	}
	current, err := s.readCurrentPackagePacket(ctx, run, packet)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, s.staleCurrentPackagePacket(ctx, run.ID, packagePacketReadbackStaleReason(err), err)
	}

	evidence, err := s.loadPackageEvidence(ctx, run.RunID)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, s.staleCurrentPackagePacket(ctx, run.ID, "package_execution_evidence_changed", err)
	}
	reason, err := s.reconstructCurrentPackagePacket(ctx, run, current, evidence)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, s.staleCurrentPackagePacket(ctx, run.ID, reason, err)
	}
	return current, nil
}

// packagePacketReadbackStaleReason keeps persisted packet corruption distinct
// from canonical schema readback failures. The reader retains the underlying
// error so callers can still use errors.Is to inspect injected infrastructure
// failures.
func packagePacketReadbackStaleReason(err error) string {
	if errors.Is(err, errWorkflowPackagePacketSchemaReadback) {
		return "packet_schema_readback_failed"
	}
	return "packet_integrity_failed"
}

var errWorkflowPackagePacketSchemaReadback = errors.New("workflow package packet schema readback failed")

// readCurrentPackagePacket is the shared persisted-packet boundary. Evidence
// freshness is deliberately separate so a decision can load evidence once.
func (s *WorkflowAuditService) readCurrentPackagePacket(ctx context.Context, run workflowstore.Run, packet workflowstore.AuditPacket) (GetWorkflowAuditPacketResult, error) {
	if packet.RunRowID != run.ID || packet.Status != workflowstore.AuditPacketStatusCurrent || packet.BaseCommit != run.BaseCommit || !workflowPackageValidSHA40(packet.AuditedCommit) || !workflowPackageValidSHA256(packet.PacketSHA256) {
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	artifact, err := s.store.GetArtifactByRowID(ctx, packet.ArtifactRowID)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.Kind != "audit_packet" || artifact.RelativePath != "audit-packets/"+packet.AuditPacketID+"/audit-packet.json" || artifact.MediaType != "application/json" || artifact.SizeBytes < 0 || artifact.SizeBytes > MaxWorkflowAuditPacketBytes || artifact.SHA256 != packet.PacketSHA256 {
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	data, err := readWorkflowArtifact(s.store, artifact, MaxWorkflowAuditPacketBytes)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	if int64(len(data)) != artifact.SizeBytes || sha256HexBytes(data) != artifact.SHA256 || sha256HexBytes(data) != packet.PacketSHA256 || !utf8.Valid(data) || !json.Valid(data) {
		return GetWorkflowAuditPacketResult{}, ErrWorkflowAuditPacketStale
	}
	if err := validateWorkflowPackageAuditPacketBytes(data); err != nil {
		return GetWorkflowAuditPacketResult{}, fmt.Errorf("%w: %w", errWorkflowPackagePacketSchemaReadback, err)
	}
	return GetWorkflowAuditPacketResult{Run: run, Packet: packet, Artifact: artifact, Document: append(json.RawMessage(nil), data...)}, nil
}

func (s *WorkflowAuditService) reconstructCurrentPackagePacket(ctx context.Context, run workflowstore.Run, current GetWorkflowAuditPacketResult, evidence WorkflowPackageExecutionEvidence) (string, error) {
	if !sameWorkflowPackageRunIdentity(run, evidence.Run) {
		return "package_execution_evidence_changed", ErrWorkflowAuditPacketStale
	}
	actorKind, attemptRowID, err := packageAuditPersistenceMetadata(evidence)
	if err != nil {
		return "package_execution_evidence_changed", err
	}
	if actorKind != current.Packet.ImplementationActorKind || attemptRowID != current.Packet.ExecutionAttemptRowID {
		return "package_execution_evidence_changed", ErrWorkflowAuditPacketStale
	}
	repository, err := s.store.GetRepositoryTarget(ctx, run.RepoTarget)
	if err != nil {
		return "repository_state_changed", err
	}
	commit, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, current.Packet.AuditedCommit)
	if err != nil {
		return "repository_state_changed", err
	}
	if commit.Branch != run.Branch || commit.BaseCommit != run.BaseCommit || commit.AuditedCommit != current.Packet.AuditedCommit {
		return "repository_state_changed", ErrWorkflowAuditPacketStale
	}
	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		return "canonical_packet_changed", err
	}
	_, rebuilt, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		return "canonical_packet_changed", err
	}
	if sha256HexBytes(rebuilt) != current.Packet.PacketSHA256 || sha256HexBytes(rebuilt) != current.Artifact.SHA256 || !bytes.Equal(rebuilt, current.Document) {
		return "canonical_packet_changed", ErrWorkflowAuditPacketStale
	}
	return "", nil
}

func (s *WorkflowAuditService) staleCurrentPackagePacket(ctx context.Context, runRowID int64, reason string, cause error) error {
	if markErr := s.store.MarkCurrentAuditPacketsStale(ctx, runRowID, reason); markErr != nil {
		if cause != nil {
			return errors.Join(ErrWorkflowAuditPacketStale, cause, markErr)
		}
		return errors.Join(ErrWorkflowAuditPacketStale, markErr)
	}
	if cause != nil {
		return fmt.Errorf("%w: %w", ErrWorkflowAuditPacketStale, cause)
	}
	return ErrWorkflowAuditPacketStale
}

func workflowArtifactSupportsTextReadback(mediaType string) bool {
	mediaType = strings.ToLower(strings.TrimSpace(strings.Split(mediaType, ";")[0]))
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		mediaType == "application/x-ndjson" ||
		strings.HasSuffix(mediaType, "+json")
}

func readWorkflowArtifactForAudit(
	store *workflowstore.Store,
	artifact workflowstore.Artifact,
	maxBytes int,
) ([]byte, bool, error) {
	if !workflowArtifactSupportsTextReadback(artifact.MediaType) {
		return nil, false, ErrWorkflowAuditArtifactUnsupported
	}
	path, err := workflowArtifactPath(store, artifact)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrWorkflowAuditArtifactIntegrity, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrWorkflowAuditArtifactIntegrity, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.Size() != artifact.SizeBytes {
		return nil, false, ErrWorkflowAuditArtifactIntegrity
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrWorkflowAuditArtifactIntegrity, err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return nil, false, ErrWorkflowAuditArtifactIntegrity
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrWorkflowAuditArtifactIntegrity, err)
	}

	buffer := make([]byte, maxBytes+1)
	count, err := io.ReadFull(file, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, fmt.Errorf("%w: %v", ErrWorkflowAuditArtifactIntegrity, err)
	}
	truncated := count > maxBytes
	if truncated {
		count = maxBytes
	}
	content := append([]byte(nil), buffer[:count]...)
	for trimmed := 0; trimmed < utf8.UTFMax-1 && !utf8.Valid(content); trimmed++ {
		content = content[:len(content)-1]
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return nil, false, ErrWorkflowAuditArtifactUnsupported
	}
	return content, truncated, nil
}

func (s *WorkflowAuditService) GetCurrentArtifact(ctx context.Context, input GetWorkflowAuditArtifactInput) (GetWorkflowAuditArtifactResult, error) {
	input.RunID = strings.TrimSpace(input.RunID)
	input.ArtifactReference = strings.TrimSpace(input.ArtifactReference)
	if input.RunID == "" || input.ArtifactReference == "" {
		return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditArtifactReference
	}
	if input.MaxBytes == 0 {
		input.MaxBytes = 12000
	}
	if input.MaxBytes < 1 || input.MaxBytes > MaxWorkflowAuditReadBytes {
		return GetWorkflowAuditArtifactResult{}, fmt.Errorf("max_bytes must be between 1 and %d", MaxWorkflowAuditReadBytes)
	}

	current, err := s.GetCurrentPacket(ctx, input.RunID)
	if err != nil {
		return GetWorkflowAuditArtifactResult{}, err
	}
	var packet WorkflowAuditPacket
	if err := json.Unmarshal(current.Document, &packet); err != nil {
		return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditPacketStale
	}
	declared, err := resolvePacketArtifact(packet.Artifacts, input.ArtifactReference)
	if err != nil {
		return GetWorkflowAuditArtifactResult{}, err
	}

	artifact, err := s.store.GetArtifactByArtifactID(ctx, declared.ArtifactReference)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditArtifactReference
		}
		return GetWorkflowAuditArtifactResult{}, err
	}
	if !workflowAuditArtifactOwnerAllowed(current.Packet, artifact) {
		return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditArtifactOwnership
	}
	if artifact.SHA256 != declared.SHA256 {
		return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditArtifactIntegrity
	}
	if declared.ArtifactType == "unified_diff" || declared.ArtifactType == "execution_evidence" {
		if artifact.Kind != declared.ArtifactType {
			return GetWorkflowAuditArtifactResult{}, ErrWorkflowAuditArtifactIntegrity
		}
	}

	content, truncated, err := readWorkflowArtifactForAudit(s.store, artifact, input.MaxBytes)
	if err != nil {
		return GetWorkflowAuditArtifactResult{}, err
	}
	return GetWorkflowAuditArtifactResult{
		Run:       current.Run,
		Packet:    current.Packet,
		Artifact:  artifact,
		Content:   content,
		Truncated: truncated,
	}, nil
}

func workflowAuditArtifactOwnerAllowed(packet workflowstore.AuditPacket, artifact workflowstore.Artifact) bool {
	if artifact.OwnerType == workflowstore.ArtifactOwnerRun && artifact.RunRowID.Valid && artifact.RunRowID.Int64 == packet.RunRowID {
		return true
	}
	if artifact.OwnerType == workflowstore.ArtifactOwnerExecutionAttempt && packet.ExecutionAttemptRowID.Valid && artifact.ExecutionAttemptRowID.Valid {
		return artifact.ExecutionAttemptRowID.Int64 == packet.ExecutionAttemptRowID.Int64
	}
	return false
}

func resolvePacketArtifact(artifacts []WorkflowAuditPacketArtifact, reference string) (WorkflowAuditPacketArtifact, error) {
	reference = strings.TrimSpace(reference)
	var found *WorkflowAuditPacketArtifact
	for index := range artifacts {
		artifact := &artifacts[index]
		if artifact.ArtifactReference != reference {
			continue
		}
		if found != nil {
			return WorkflowAuditPacketArtifact{}, ErrWorkflowAuditArtifactReference
		}
		copy := *artifact
		found = &copy
	}
	if found == nil {
		return WorkflowAuditPacketArtifact{}, ErrWorkflowAuditArtifactReference
	}
	return *found, nil
}

func (s *WorkflowAuditService) GetStatus(ctx context.Context, runID string) (WorkflowAuditStatus, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return WorkflowAuditStatus{}, err
	}
	if run.Status == workflowstore.RunStatusAuditReady {
		if _, refreshErr := s.GetCurrentPacket(ctx, run.RunID); refreshErr != nil &&
			!errors.Is(refreshErr, ErrWorkflowAuditPacketStale) &&
			!errors.Is(refreshErr, ErrWorkflowAuditPacketNotFound) {
			return WorkflowAuditStatus{}, refreshErr
		}
	}
	status := WorkflowAuditStatus{RunID: run.RunID, RunStatus: run.Status}
	if current, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID); err == nil {
		copy := current
		status.CurrentPacket = &copy
	}
	if latest, err := s.store.GetLatestAuditPacketByRun(ctx, run.ID); err == nil {
		copy := latest
		status.LatestPacket = &copy
	}
	if decision, err := s.store.GetAuditDecisionByRun(ctx, run.ID); err == nil {
		copy := decision
		status.Decision = &copy
	}
	return status, nil
}

func (s *WorkflowAuditService) RecordDecision(ctx context.Context, input RecordWorkflowAuditDecisionInput) (RecordWorkflowAuditDecisionResult, error) {
	input.RunID = strings.TrimSpace(input.RunID)
	input.AuditPacketID = strings.TrimSpace(input.AuditPacketID)
	input.PacketSHA256 = strings.ToLower(strings.TrimSpace(input.PacketSHA256))
	input.AuditedCommit = strings.ToLower(strings.TrimSpace(input.AuditedCommit))
	input.Rationale = strings.TrimSpace(input.Rationale)
	if !input.OperatorConfirmed {
		return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditConfirmation
	}
	if input.Decision != workflowstore.AuditDecisionAccepted && input.Decision != workflowstore.AuditDecisionNeedsRevision {
		return RecordWorkflowAuditDecisionResult{}, fmt.Errorf("unsupported workflow audit decision %q", input.Decision)
	}
	if input.RunID == "" || input.AuditPacketID == "" || len(input.PacketSHA256) != 64 || len(input.AuditedCommit) != 40 {
		return RecordWorkflowAuditDecisionResult{}, fmt.Errorf("run_id, audit_packet_id, packet_sha256, and audited_commit are required")
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	var current GetWorkflowAuditPacketResult
	if run.ExecutionPackageRowID.Valid {
		current, err = s.getCurrentPackagePacketForDecision(ctx, run)
	} else {
		current, err = s.GetCurrentPacket(ctx, input.RunID)
	}
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	if current.Packet.AuditPacketID != input.AuditPacketID ||
		current.Packet.PacketSHA256 != input.PacketSHA256 ||
		current.Packet.AuditedCommit != input.AuditedCommit {
		return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
	}
	input.MaterialFindings = normalizeWorkflowAuditFindings(input.MaterialFindings)
	input.Observations = normalizeWorkflowAuditObservations(input.Observations)
	if current.Run.ExecutionPackageRowID.Valid {
		return s.recordPackageDecision(ctx, input, current)
	}
	if err := validateWorkflowAuditDecisionInput(input, current.Run.ExecutionPackageRowID.Valid); err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}

	decisionID := workflowstore.NewAuditDecisionID()
	decisionBody, err := json.MarshalIndent(struct {
		AuditDecisionID  string                         `json:"audit_decision_id"`
		RunID            string                         `json:"run_id"`
		AuditPacketID    string                         `json:"audit_packet_id"`
		PacketSHA256     string                         `json:"packet_sha256"`
		AuditedCommit    string                         `json:"audited_commit"`
		Decision         string                         `json:"decision"`
		Rationale        string                         `json:"rationale"`
		MaterialFindings []WorkflowAuditMaterialFinding `json:"material_findings"`
		Observations     []string                       `json:"observations"`
	}{
		AuditDecisionID:  decisionID,
		RunID:            input.RunID,
		AuditPacketID:    input.AuditPacketID,
		PacketSHA256:     input.PacketSHA256,
		AuditedCommit:    input.AuditedCommit,
		Decision:         input.Decision,
		Rationale:        input.Rationale,
		MaterialFindings: input.MaterialFindings,
		Observations:     input.Observations,
	}, "", "  ")
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	decisionBody = append(decisionBody, '\n')
	batch, err := s.store.ArtifactStore().Begin("audit-decisions/" + decisionID)
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	staged, err := batch.Stage("audit_decision", "audit-decision.json", "application/json", decisionBody)
	if err != nil {
		_ = batch.Rollback()
		return RecordWorkflowAuditDecisionResult{}, err
	}

	result := RecordWorkflowAuditDecisionResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, input.RunID)
		if err != nil {
			return err
		}
		if run.Status != workflowstore.RunStatusAuditReady {
			return ErrWorkflowAuditDecisionRecorded
		}
		packet, err := tx.GetAuditPacketByPacketID(ctx, input.AuditPacketID)
		if err != nil {
			return err
		}
		if packet.RunRowID != run.ID ||
			packet.Status != workflowstore.AuditPacketStatusCurrent ||
			packet.ArtifactRowID != current.Artifact.ID ||
			packet.PacketSHA256 != input.PacketSHA256 ||
			packet.AuditedCommit != input.AuditedCommit {
			return ErrWorkflowAuditPacketStale
		}
		currentImplementation, err := resolveWorkflowImplementationEvidenceWithReader(ctx, s.store, tx, run)
		if err != nil || currentImplementation.ActorKind != packet.ImplementationActorKind || implementationExecutionAttemptRowID(currentImplementation) != packet.ExecutionAttemptRowID {
			return ErrWorkflowAuditPacketStale
		}
		repository, err := tx.GetRepositoryTarget(ctx, run.RepoTarget)
		if err != nil {
			return err
		}
		if _, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, input.AuditedCommit); err != nil {
			return ErrWorkflowAuditPacketStale
		}
		if run.ExecutionPackageRowID.Valid {
			if !run.PackageApprovalRowID.Valid {
				return ErrWorkflowAuditPacketStale
			}
			approval, approvalErr := tx.GetRunExecutionPackageApproval(ctx, run.ID)
			if approvalErr != nil || approval.PackageRowID != run.ExecutionPackageRowID.Int64 {
				return ErrWorkflowAuditPacketStale
			}
		}
		packetArtifact, err := tx.GetArtifactByRowID(ctx, packet.ArtifactRowID)
		if err != nil ||
			packetArtifact.ID != packet.ArtifactRowID ||
			packetArtifact.SHA256 != packet.PacketSHA256 ||
			packetArtifact.SHA256 != input.PacketSHA256 {
			return ErrWorkflowAuditPacketStale
		}
		packetBytes, err := readWorkflowArtifact(s.store, packetArtifact, MaxWorkflowAuditPacketBytes)
		if err != nil || sha256HexBytes(packetBytes) != packet.PacketSHA256 {
			return ErrWorkflowAuditPacketStale
		}
		var packetDocument WorkflowAuditPacket
		if json.Unmarshal(packetBytes, &packetDocument) != nil {
			return ErrWorkflowAuditPacketStale
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:   workflowstore.NewArtifactID(),
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: run.ID, Valid: true},
			Kind:         staged.Kind,
			RelativePath: staged.RelativePath,
			MediaType:    staged.MediaType,
			SHA256:       staged.SHA256,
			SizeBytes:    staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		decision, err := tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{
			AuditDecisionID:          decisionID,
			RunRowID:                 run.ID,
			AuditPacketArtifactRowID: packet.ArtifactRowID,
			AuditedCommit:            input.AuditedCommit,
			PacketSHA256:             input.PacketSHA256,
			Decision:                 input.Decision,
			Rationale:                input.Rationale,
		})
		if err != nil {
			return err
		}
		ticketDecisions, satisfactions, remediationSeeds, err := applyWorkflowAuditTicketDecisionEffects(
			ctx, tx, run, packet, decision, packetDocument, input,
		)
		if err != nil {
			return err
		}
		nextStatus := workflowstore.RunStatusNeedsRevision
		if input.Decision == workflowstore.AuditDecisionAccepted {
			nextStatus = workflowstore.RunStatusCompleted
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusAuditReady, nextStatus)
		if err != nil {
			return err
		}
		var completedPass *workflowstore.PlanPass
		var completedPlan *workflowstore.Plan
		if input.Decision == workflowstore.AuditDecisionAccepted && run.PlanPassRowID.Valid && run.PlanRowID.Valid {
			pass, err := tx.GetPlanPassByRowID(ctx, run.PlanPassRowID.Int64)
			if err != nil {
				return err
			}
			pass, err = tx.TransitionPlanPass(ctx, pass.PassID, workflowstore.PassStatusInProgress, workflowstore.PassStatusCompleted)
			if err != nil {
				return err
			}
			completedPass = &pass
			incomplete, err := tx.CountIncompletePlanPasses(ctx, run.PlanRowID.Int64)
			if err != nil {
				return err
			}
			if incomplete == 0 {
				plan, err := tx.CompletePlan(ctx, run.PlanRowID.Int64)
				if err != nil {
					return err
				}
				completedPlan = &plan
			}
		}
		result = RecordWorkflowAuditDecisionResult{
			Run: run, Pass: completedPass, Plan: completedPlan,
			Packet: packet, Decision: decision, Artifact: artifact,
			TicketRevisionDecisions: ticketDecisions,
			TicketSatisfactions:     satisfactions,
			RemediationSeeds:        remediationSeeds,
		}
		return nil
	})
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	return result, nil
}

// getCurrentPackagePacketForDecision shares the authoritative persisted-packet
// boundary without loading evidence. recordPackageDecision loads it once.
func (s *WorkflowAuditService) getCurrentPackagePacketForDecision(ctx context.Context, run workflowstore.Run) (GetWorkflowAuditPacketResult, error) {
	packet, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if err != nil {
		return GetWorkflowAuditPacketResult{}, err
	}
	return s.readCurrentPackagePacket(ctx, run, packet)
}

// recordPackageDecision deliberately has no legacy packet or implementation
// evidence dependency.  Package packets are a separate canonical contract.
func (s *WorkflowAuditService) recordPackageDecision(ctx context.Context, input RecordWorkflowAuditDecisionInput, current GetWorkflowAuditPacketResult) (RecordWorkflowAuditDecisionResult, error) {
	if s.loadPackageEvidence == nil {
		return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPackageUnavailable
	}
	if err := validateWorkflowPackageAuditDecisionInput(input, current.Document); err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	var document WorkflowPackageAuditPacket
	if err := validateWorkflowPackageAuditPacketBytes(current.Document); err != nil || json.Unmarshal(current.Document, &document) != nil {
		return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
	}
	evidence, err := s.loadPackageEvidence(ctx, current.Run.RunID)
	if err != nil {
		if errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict) {
			return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
		}
		return RecordWorkflowAuditDecisionResult{}, err
	}
	if !sameWorkflowPackageRunIdentity(current.Run, evidence.Run) {
		return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
	}
	reason, err := s.reconstructCurrentPackagePacket(ctx, current.Run, current, evidence)
	if err != nil {
		if (reason == "package_execution_evidence_changed" || reason == "canonical_packet_changed") &&
			(errors.Is(err, ErrWorkflowAuditPacketStale) || errors.Is(err, ErrWorkflowPackageExecutionEvidenceConflict)) {
			return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
		}
		if reason == "repository_state_changed" && errors.Is(err, ErrWorkflowAuditPacketStale) {
			return RecordWorkflowAuditDecisionResult{}, ErrWorkflowAuditPacketStale
		}
		return RecordWorkflowAuditDecisionResult{}, err
	}
	decisionID := workflowstore.NewAuditDecisionID()
	decisionBody, err := json.MarshalIndent(workflowPackageDecisionDocument{AuditDecisionID: decisionID, RunID: current.Run.RunID, RunRowID: current.Run.ID, Decision: input.Decision, Rationale: input.Rationale, MaterialFindings: input.MaterialFindings, Observations: input.Observations, AuditPacketID: current.Packet.AuditPacketID, AuditPacketRowID: current.Packet.ID, AuditPacketArtifactRowID: current.Packet.ArtifactRowID, PacketSHA256: current.Packet.PacketSHA256, AuditedCommit: current.Packet.AuditedCommit, ExecutionPackageID: evidence.Authority.Package.PackageID, ExecutionPackageRowID: evidence.Authority.Package.ID, PackageSHA256: evidence.Authority.Package.PackageSha256, PackageApprovalID: evidence.Authority.PackageApproval.ApprovalID, PackageApprovalRowID: evidence.Authority.PackageApproval.ID, ApprovedPackageSHA256: evidence.Authority.PackageApproval.PackageSha256, DeliveryTicketID: evidence.Authority.Ticket.TicketID, DeliveryTicketRowID: evidence.Authority.Ticket.ID, DeliveryTicketRevisionRowID: evidence.Authority.TicketRevision.ID, DeliveryTicketRevisionNumber: evidence.Authority.TicketRevision.RevisionNumber, DeliveryTicketApprovalID: evidence.Authority.TicketApproval.ApprovalID, DeliveryTicketApprovalRowID: evidence.Authority.TicketApproval.ID, AuthorityRevisionID: evidence.Authority.Authority.AuthorityRevisionID, AuthorityRevisionRowID: evidence.Authority.Authority.ID, SourceClosureID: evidence.Authority.Source.ClosureID, SourceClosureRowID: evidence.Authority.Source.ID, SourceCommit: evidence.Authority.Source.CommitOID}, "", "  ")
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	decisionBody = append(decisionBody, '\n')
	batch, err := s.store.ArtifactStore().Begin("audit-decisions/" + decisionID)
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	staged, err := batch.Stage("audit_decision", "audit-decision.json", "application/json", decisionBody)
	if err != nil {
		_ = batch.Rollback()
		return RecordWorkflowAuditDecisionResult{}, err
	}
	result := RecordWorkflowAuditDecisionResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		run, err := tx.GetRunByRunID(ctx, input.RunID)
		if err != nil {
			return err
		}
		if run.Status != workflowstore.RunStatusAuditReady {
			return ErrWorkflowAuditDecisionRecorded
		}
		if !sameWorkflowPackageRunIdentity(current.Run, run) {
			return ErrWorkflowAuditPacketStale
		}
		packet, err := tx.GetAuditPacketByPacketID(ctx, input.AuditPacketID)
		if err != nil {
			return err
		}
		if packet.RunRowID != run.ID || packet.Status != workflowstore.AuditPacketStatusCurrent || packet.ArtifactRowID != current.Artifact.ID || packet.PacketSHA256 != input.PacketSHA256 || packet.AuditedCommit != input.AuditedCommit {
			return ErrWorkflowAuditPacketStale
		}
		if err := verifyWorkflowPackageDecisionAuthority(ctx, tx, run, packet, evidence); err != nil {
			return err
		}
		actor, attempt, err := packageAuditPersistenceMetadata(evidence)
		if err != nil {
			return err
		}
		if actor != packet.ImplementationActorKind || attempt != packet.ExecutionAttemptRowID {
			return ErrWorkflowAuditPacketStale
		}
		repository, err := tx.GetRepositoryTarget(ctx, run.RepoTarget)
		if err != nil {
			return err
		}
		commit, err := s.inspector(ctx, repository.LocalPath, run.Branch, run.BaseCommit, input.AuditedCommit)
		if err != nil {
			return err
		}
		if commit.Branch != run.Branch || commit.BaseCommit != run.BaseCommit || commit.AuditedCommit != input.AuditedCommit {
			return ErrWorkflowAuditPacketStale
		}
		packetArtifact, err := tx.GetArtifactByRowID(ctx, packet.ArtifactRowID)
		if err != nil {
			return err
		}
		packetBytes, err := readWorkflowArtifact(s.store, packetArtifact, MaxWorkflowAuditPacketBytes)
		if err != nil {
			return ErrWorkflowAuditPacketStale
		}
		if packetArtifact.ID != current.Artifact.ID || packetArtifact.OwnerType != workflowstore.ArtifactOwnerRun || !packetArtifact.RunRowID.Valid || packetArtifact.RunRowID.Int64 != run.ID ||
			packetArtifact.Kind != "audit_packet" || packetArtifact.RelativePath != "audit-packets/"+packet.AuditPacketID+"/audit-packet.json" || packetArtifact.MediaType != "application/json" ||
			packetArtifact.SizeBytes < 0 || packetArtifact.SizeBytes > MaxWorkflowAuditPacketBytes || packetArtifact.SizeBytes != int64(len(packetBytes)) ||
			packetArtifact.SHA256 != packet.PacketSHA256 || sha256HexBytes(packetBytes) != packet.PacketSHA256 || !utf8.Valid(packetBytes) || !json.Valid(packetBytes) || !bytes.Equal(packetBytes, current.Document) || validateWorkflowPackageAuditPacketBytes(packetBytes) != nil {
			return ErrWorkflowAuditPacketStale
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		if err != nil {
			return err
		}
		decision, err := tx.CreateAuditDecision(ctx, workflowstore.CreateAuditDecisionParams{AuditDecisionID: decisionID, RunRowID: run.ID, AuditPacketArtifactRowID: packet.ArtifactRowID, AuditedCommit: input.AuditedCommit, PacketSHA256: input.PacketSHA256, Decision: input.Decision, Rationale: input.Rationale})
		if err != nil {
			return err
		}
		decisions, satisfactions, err := applyWorkflowPackageAuditTicketDecisionEffects(ctx, tx, run, packet, decision, document, input)
		if err != nil {
			return err
		}
		next := workflowstore.RunStatusNeedsRevision
		if input.Decision == workflowstore.AuditDecisionAccepted {
			next = workflowstore.RunStatusCompleted
		}
		run, err = tx.TransitionRun(ctx, run.RunID, workflowstore.RunStatusAuditReady, next)
		if err != nil {
			return err
		}
		var pass *workflowstore.PlanPass
		var plan *workflowstore.Plan
		if input.Decision == workflowstore.AuditDecisionAccepted && run.PlanPassRowID.Valid && run.PlanRowID.Valid {
			completedPass, err := tx.GetPlanPassByRowID(ctx, run.PlanPassRowID.Int64)
			if err != nil {
				return err
			}
			completedPass, err = tx.TransitionPlanPass(ctx, completedPass.PassID, workflowstore.PassStatusInProgress, workflowstore.PassStatusCompleted)
			if err != nil {
				return err
			}
			pass = &completedPass
			if n, err := tx.CountIncompletePlanPasses(ctx, run.PlanRowID.Int64); err != nil {
				return err
			} else if n == 0 {
				completedPlan, err := tx.CompletePlan(ctx, run.PlanRowID.Int64)
				if err != nil {
					return err
				}
				plan = &completedPlan
			}
		}
		result = RecordWorkflowAuditDecisionResult{Run: run, Pass: pass, Plan: plan, Packet: packet, Decision: decision, Artifact: artifact, TicketRevisionDecisions: decisions, TicketSatisfactions: satisfactions, RemediationSeeds: []workflowstore.AuditRemediationSeed{}}
		return nil
	})
	if err != nil {
		return RecordWorkflowAuditDecisionResult{}, err
	}
	return result, nil
}

func normalizeWorkflowAuditFindings(findings []WorkflowAuditMaterialFinding) []WorkflowAuditMaterialFinding {
	if len(findings) == 0 {
		return []WorkflowAuditMaterialFinding{}
	}
	result := make([]WorkflowAuditMaterialFinding, len(findings))
	for index, finding := range findings {
		result[index] = WorkflowAuditMaterialFinding{
			Source:              strings.TrimSpace(finding.Source),
			Summary:             strings.TrimSpace(finding.Summary),
			Evidence:            strings.TrimSpace(finding.Evidence),
			RequiredRemediation: strings.TrimSpace(finding.RequiredRemediation),
		}
	}
	return result
}

func normalizeWorkflowAuditObservations(observations []string) []string {
	if len(observations) == 0 {
		return []string{}
	}
	result := make([]string, len(observations))
	for index, observation := range observations {
		result[index] = strings.TrimSpace(observation)
	}
	return result
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
