package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"
)

type PrepareAnotherPrototypeExecutionInput struct {
	WorkspaceID, PriorRunID                        string
	ExpectedPriorRunVersion                        int64
	MutationIdentity, OperatorConfirmationEvidence string
}
type PrepareQADiscoveryPacketInput struct {
	WorkspaceID, RunID               string
	ExpectedRunVersion               int64
	MutationIdentity, OperatorPrompt string
	ValidationInstructions           []string
}
type OperatorQAEvidenceInput struct {
	SemanticRole, MediaType, SHA256 string
	Content                         []byte
}
type AdmitOperatorQAEvidenceInput struct {
	WorkspaceID, QAPacketID, MutationIdentity, OperatorConfirmationEvidence string
	Evidence                                                                []OperatorQAEvidenceInput
}
type PrototypeQAPacketDetail struct {
	Packet    workflowstore.PrototypeQAPacket
	Members   []workflowstore.PrototypeQAPacketMember
	Admission *workflowstore.PrototypeQAAdmission
	Evidence  []workflowstore.PrototypeQAEvidence
}
type PrototypeWayfinderEvidenceView struct {
	WorkspaceID, RunID, RunState, ProcessOutcome string
	Result                                       *workflowstore.PrototypeResult
	EvidenceBatches                              []workflowstore.PrototypeEvidenceImportBatch
	Evidence                                     []workflowstore.PrototypeEvidenceMember
	Cleanup                                      []workflowstore.PrototypeCleanupObligation
	QAPackets                                    []PrototypeQAPacketDetail
}

func (s *Service) ReconcilePrototypeCleanup(ctx context.Context, in prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error) {
	if s.prototypeCleaner == nil {
		return prototypeexecution.CleanupResult{}, fmt.Errorf("prototype cleaner is unavailable")
	}
	return s.prototypeCleaner.ReconcileCleanup(ctx, in)
}

func (s *Service) PrepareAnotherPrototypeExecution(ctx context.Context, in PrepareAnotherPrototypeExecutionInput) (PrototypeExecutionDetail, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.PriorRunID) == "" || in.ExpectedPriorRunVersion < 1 || strings.TrimSpace(in.MutationIdentity) == "" || strings.TrimSpace(in.OperatorConfirmationEvidence) == "" {
		return PrototypeExecutionDetail{}, prototypeexecution.ErrAnotherExecutionIneligible
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return PrototypeExecutionDetail{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return PrototypeExecutionDetail{}, ErrPrototypeCapabilityDisabled
	}
	if existing, e := s.store.GetPrototypeRunByApprovalMutationIdentity(ctx, in.MutationIdentity); e == nil {
		if existing.WorkspaceRowID != workspace.ID {
			return PrototypeExecutionDetail{}, ErrPrototypeAnotherExecutionIneligible
		}
		return s.ReadPrototypeExecution(ctx, workspace.WorkspaceID, existing.PrototypeRunID)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return PrototypeExecutionDetail{}, e
	}
	prior, err := s.store.GetPrototypeRun(ctx, in.PriorRunID)
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	if prior.WorkspaceRowID != workspace.ID || prior.Version != in.ExpectedPriorRunVersion {
		return PrototypeExecutionDetail{}, ErrPrototypeAnotherExecutionIneligible
	}
	if !oneOf(prior.LifecycleState, "succeeded", "failed", "cancelled", "timed_out", "launch_uncertain", "cleanup_required", "closed") {
		return PrototypeExecutionDetail{}, ErrPrototypeAnotherExecutionIneligible
	}
	ticket, err := s.getPrototypeTicketByRowID(ctx, prior.WorkItemRowID)
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	if ticket.WorkspaceRowID != workspace.ID || ticket.State == "resolved" || ticket.State == "cancelled" {
		return PrototypeDetailIneligible()
	}
	if _, err = s.store.GetCurrentIntegratedDiscoveryRevision(ctx, workspace.WorkspaceID); err != nil {
		return PrototypeDetailIneligible()
	}
	sourceClosureID := sourceClosureForRun(ctx, s.store, prior.ID)
	repoTarget := repoTargetForRun(ctx, s.store, prior.ID)
	baseCommit := baseCommitForRun(ctx, s.store, prior.ID)
	if sourceClosureID == "" || repoTarget == "" || baseCommit == "" {
		return PrototypeDetailIneligible()
	}
	lineage, _ := json.Marshal(map[string]any{"kind": "prepare_another_prototype_execution", "priorRunId": in.PriorRunID, "operatorConfirmationEvidence": strings.TrimSpace(in.OperatorConfirmationEvidence)})
	proposal, err := s.PreparePrototypeProposal(ctx, PreparePrototypeProposalInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, Proposal: lineage, MediaType: "application/json"})
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	authorization, run, err := s.PreparePrototypeExecution(ctx, PreparePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, WorkItemID: ticket.DiscoveryTicketID, ProposalID: proposal.ProposalID, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, SourceClosureID: sourceClosureID, RepoTarget: repoTarget, BaseCommit: baseCommit, Adapter: "operator-confirmed", Model: "another-execution", Variants: []string{"baseline"}, EvidenceObligations: []string{"result-envelope"}, Limits: map[string]any{}})
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	_, _, err = s.ApprovePrototypeExecution(ctx, ApprovePrototypeExecutionInput{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, ProposalID: proposal.ProposalID, AuthorizationID: authorization.AuthorizationID, InvocationSHA256: authorization.InvocationSHA256, MutationIdentity: in.MutationIdentity, OperatorConfirmationEvidence: strings.TrimSpace(in.OperatorConfirmationEvidence), ExpectedRunVersion: run.Version})
	if err != nil {
		return PrototypeExecutionDetail{}, err
	}
	return s.ReadPrototypeExecution(ctx, workspace.WorkspaceID, run.PrototypeRunID)
}

func PrototypeDetailIneligible() (PrototypeExecutionDetail, error) {
	return PrototypeExecutionDetail{}, ErrPrototypeAnotherExecutionIneligible
}
func sourceClosureForRun(ctx context.Context, store *workflowstore.Store, runRowID int64) string {
	var id string
	_ = store.DB().QueryRowContext(ctx, `SELECT c.closure_id FROM source_vault_closures c JOIN feature_workspace_prototype_authorizations a ON a.source_closure_row_id=c.id JOIN feature_workspace_prototype_runs r ON r.authorization_row_id=a.id WHERE r.id=? AND c.state='ready'`, runRowID).Scan(&id)
	return id
}
func repoTargetForRun(ctx context.Context, store *workflowstore.Store, runRowID int64) string {
	var v string
	_ = store.DB().QueryRowContext(ctx, `SELECT a.repo_target FROM feature_workspace_prototype_authorizations a JOIN feature_workspace_prototype_runs r ON r.authorization_row_id=a.id WHERE r.id=?`, runRowID).Scan(&v)
	return v
}
func baseCommitForRun(ctx context.Context, store *workflowstore.Store, runRowID int64) string {
	var v string
	_ = store.DB().QueryRowContext(ctx, `SELECT a.base_commit FROM feature_workspace_prototype_authorizations a JOIN feature_workspace_prototype_runs r ON r.authorization_row_id=a.id WHERE r.id=?`, runRowID).Scan(&v)
	return v
}

func (s *Service) PrepareQADiscoveryPacket(ctx context.Context, in PrepareQADiscoveryPacketInput) (PrototypeQAPacketDetail, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.RunID) == "" || in.ExpectedRunVersion < 1 || strings.TrimSpace(in.MutationIdentity) == "" || len(in.OperatorPrompt) > 16*1024 || len(in.ValidationInstructions) > 20 {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
	}
	for _, instruction := range in.ValidationInstructions {
		if len(instruction) > 2*1024 {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
		}
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return PrototypeQAPacketDetail{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return PrototypeQAPacketDetail{}, ErrPrototypeCapabilityDisabled
	}
	if existing, e := s.store.GetPrototypeQAPacketByMutationIdentity(ctx, in.MutationIdentity); e == nil {
		if existing.WorkspaceRowID != workspace.ID {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
		}
		return s.readQAPacketDetail(ctx, existing)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return PrototypeQAPacketDetail{}, e
	}
	run, err := s.store.GetPrototypeRun(ctx, in.RunID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	if run.WorkspaceRowID != workspace.ID || run.Version != in.ExpectedRunVersion || run.LifecycleState != "closed" {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
	}
	obligations, err := s.store.ListPrototypeCleanupObligationsByRunID(ctx, in.RunID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	if !allFeatureCleanupComplete(obligations) {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
	}
	result, evidence, err := s.packetSource(ctx, in.RunID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	if result == nil && len(evidence) == 0 {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
	}
	type member struct {
		kind     string
		artifact workflowstore.DiscoveryArtifact
		staged   workflowartifacts.File
	}
	members := make([]member, 0, 32)
	total := int64(0)
	if result != nil && result.ArtifactRowID.Valid {
		a, e := s.store.GetDiscoveryArtifactByRowID(ctx, result.ArtifactRowID.Int64)
		if e != nil {
			return PrototypeQAPacketDetail{}, e
		}
		if !qaPacketMediaAllowed(a.MediaType) {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
		}
		members = append(members, member{kind: "prototype_result", artifact: a})
		total += a.SizeBytes
	}
	for _, value := range evidence {
		a, e := s.store.GetDiscoveryArtifactByRowID(ctx, value.ArtifactRowID)
		if e != nil {
			return PrototypeQAPacketDetail{}, e
		}
		if !qaPacketMediaAllowed(a.MediaType) {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
		}
		members = append(members, member{kind: "prototype_evidence", artifact: a})
		total += a.SizeBytes
	}
	packetID := workflowstore.NewPrototypeQAPacketID()
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + packetID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	promptFile, err := batch.Stage("prototype_qa_operator_prompt", "operator-prompt.txt", "text/plain", []byte(in.OperatorPrompt))
	if err != nil {
		_ = batch.Rollback()
		return PrototypeQAPacketDetail{}, err
	}
	instructionFile, err := batch.Stage("prototype_qa_validation_instructions", "validation-instructions.md", "text/markdown", []byte(strings.Join(in.ValidationInstructions, "\n")))
	if err != nil {
		_ = batch.Rollback()
		return PrototypeQAPacketDetail{}, err
	}
	members = append(members, member{kind: "operator_prompt", staged: promptFile}, member{kind: "validation_instruction", staged: instructionFile})
	total += promptFile.SizeBytes + instructionFile.SizeBytes
	if len(members) < 1 || len(members) > 32 || total < 1 || total > 32*1024*1024 {
		_ = batch.Rollback()
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAPacketInvalid
	}
	var packet workflowstore.PrototypeQAPacket
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		var e error
		packet, e = tx.CreatePrototypeQAPacket(ctx, workflowstore.PrototypeQAPacket{QAPacketID: packetID, WorkspaceRowID: workspace.ID, RunRowID: run.ID, MutationIdentity: in.MutationIdentity, ExpectedRunVersion: in.ExpectedRunVersion, MemberCount: int64(len(members)), TotalBytes: total})
		if e != nil {
			return e
		}
		for i, m := range members {
			a := m.artifact
			if m.staged.RelativePath != "" {
				a, e = tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspace.ID, RelativePath: m.staged.RelativePath, SHA256: m.staged.SHA256, MediaType: m.staged.MediaType, SizeBytes: m.staged.SizeBytes})
				if e != nil {
					return e
				}
			}
			_, e = tx.CreatePrototypeQAPacketMember(ctx, workflowstore.PrototypeQAPacketMember{QAPacketRowID: packet.ID, Sequence: int64(i + 1), MemberKind: m.kind, ArtifactRowID: a.ID, SHA256: a.SHA256, MediaType: a.MediaType, SizeBytes: a.SizeBytes})
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	return s.readQAPacketDetail(ctx, packet)
}

func (s *Service) AdmitOperatorQAEvidence(ctx context.Context, in AdmitOperatorQAEvidenceInput) (PrototypeQAPacketDetail, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.QAPacketID) == "" || strings.TrimSpace(in.MutationIdentity) == "" || strings.TrimSpace(in.OperatorConfirmationEvidence) == "" || len(in.Evidence) < 1 || len(in.Evidence) > 20 {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
	}
	if existing, e := s.store.GetPrototypeQAAdmissionByMutationIdentity(ctx, in.MutationIdentity); e == nil {
		packet, e2 := s.store.GetPrototypeQAPacketByPacketID(ctx, in.QAPacketID)
		if e2 != nil || existing.QAPacketRowID != packet.ID {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
		}
		return s.readQAPacketDetail(ctx, packet)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return PrototypeQAPacketDetail{}, e
	}
	packet, err := s.store.GetPrototypeQAPacketByPacketID(ctx, in.QAPacketID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packet.WorkspaceRowID)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	if workspace.WorkspaceID != in.WorkspaceID {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrCleanupOwnershipMismatch
	}
	if workspace.DiscoveryCapabilityEnabled != 1 {
		return PrototypeQAPacketDetail{}, ErrPrototypeCapabilityDisabled
	}
	if packet.Status != "prepared" {
		return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
	}
	seen := map[string]bool{}
	total := int64(0)
	for _, value := range in.Evidence {
		role := strings.TrimSpace(value.SemanticRole)
		if role == "" || role != value.SemanticRole || seen[role] || !qaEvidenceMediaAllowed(value.MediaType) || len(value.Content) < 1 || len(value.Content) > 8*1024*1024 || !validSHA256(value.SHA256) {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
		}
		if hex.EncodeToString(sha256Sum(value.Content)) != value.SHA256 {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
		}
		if isSecretBearingEvidence(value.MediaType, value.Content) {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
		}
		seen[role] = true
		total += int64(len(value.Content))
		if total > 32*1024*1024 {
			return PrototypeQAPacketDetail{}, prototypeexecution.ErrQAEvidenceInvalid
		}
	}
	batch, err := s.store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + in.QAPacketID + "/qa-evidence-" + in.MutationIdentity)
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	files := make([]workflowartifacts.File, len(in.Evidence))
	for i, value := range in.Evidence {
		file, e := batch.Stage("prototype_qa_evidence", fmt.Sprintf("evidence-%02d.bin", i+1), value.MediaType, value.Content)
		if e != nil {
			_ = batch.Rollback()
			return PrototypeQAPacketDetail{}, e
		}
		files[i] = file
	}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		for i, value := range in.Evidence {
			a, e := tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: workflowstore.NewFeatureWorkspaceDiscoveryArtifactID(), WorkspaceRowID: workspace.ID, RelativePath: files[i].RelativePath, SHA256: files[i].SHA256, MediaType: files[i].MediaType, SizeBytes: files[i].SizeBytes})
			if e != nil {
				return e
			}
			if _, e = tx.CreatePrototypeQAEvidence(ctx, workflowstore.PrototypeQAEvidence{QAPacketRowID: packet.ID, Sequence: int64(i + 1), SemanticRole: strings.TrimSpace(value.SemanticRole), ArtifactRowID: a.ID, SHA256: value.SHA256, MediaType: value.MediaType, SizeBytes: int64(len(value.Content))}); e != nil {
				return e
			}
		}
		var e error
		_, e = tx.CreatePrototypeQAAdmission(ctx, workflowstore.PrototypeQAAdmission{QAPacketRowID: packet.ID, MutationIdentity: in.MutationIdentity, OperatorConfirmationEvidence: strings.TrimSpace(in.OperatorConfirmationEvidence), AdmittedMemberCount: int64(len(in.Evidence)), AdmittedTotalBytes: total})
		if e != nil {
			return e
		}
		packet, e = tx.MarkPrototypeQAPacketAdmitted(ctx, in.QAPacketID, in.MutationIdentity, time.Now().UTC().Format(time.RFC3339Nano))
		return e
	})
	if err != nil {
		return PrototypeQAPacketDetail{}, err
	}
	return s.readQAPacketDetail(ctx, packet)
}

func (s *Service) ReadPrototypeEvidenceForWayfinder(ctx context.Context, workspaceID, runID string) (PrototypeWayfinderEvidenceView, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return PrototypeWayfinderEvidenceView{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return PrototypeWayfinderEvidenceView{}, err
	}
	run, err := s.store.GetPrototypeRun(ctx, runID)
	if err != nil {
		return PrototypeWayfinderEvidenceView{}, err
	}
	if run.WorkspaceRowID != workspace.ID {
		return PrototypeWayfinderEvidenceView{}, ErrPrototypeOwnership
	}
	view := PrototypeWayfinderEvidenceView{WorkspaceID: workspace.WorkspaceID, RunID: run.PrototypeRunID, RunState: run.LifecycleState, ProcessOutcome: run.ProcessOutcome.String, EvidenceBatches: []workflowstore.PrototypeEvidenceImportBatch{}, Evidence: []workflowstore.PrototypeEvidenceMember{}, Cleanup: []workflowstore.PrototypeCleanupObligation{}, QAPackets: []PrototypeQAPacketDetail{}}
	if result, e := s.store.GetPrototypeResultByRunID(ctx, runID); e == nil {
		view.Result = &result
	}
	view.EvidenceBatches, _ = s.store.ListPrototypeEvidenceBatches(ctx, runID)
	view.Evidence, _ = s.store.ListPrototypeEvidenceMembers(ctx, runID)
	view.Cleanup, _ = s.store.ListPrototypeCleanupObligationsByRunID(ctx, runID)
	packets, err := s.store.ListPrototypeQAPacketsByRunID(ctx, runID)
	if err != nil {
		return PrototypeWayfinderEvidenceView{}, err
	}
	for _, packet := range packets {
		detail, e := s.readQAPacketDetail(ctx, packet)
		if e != nil {
			return PrototypeWayfinderEvidenceView{}, e
		}
		view.QAPackets = append(view.QAPackets, detail)
	}
	return view, nil
}

// ReadCurrentPrototypeState is the Feature-owned semantic read of the current
// prototype Run for one workspace. It resolves the latest Run bound to the
// workspace's current integrated discovery revision (source currentness and
// recovery: reopened workspaces advance the revision, so older prototype Runs
// are historical) and derives the cleanup and QA states from the existing
// prototype execution and cleanup-QA owner reads. Consumers must not list
// prototype rows or re-derive these states themselves.
func (s *Service) ReadCurrentPrototypeState(ctx context.Context, workspaceID string) (GuidedPrototypeSection, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return GuidedPrototypeSection{}, err
	}
	runs, err := s.store.ListPrototypeRunsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return GuidedPrototypeSection{}, err
	}
	none := GuidedPrototypeSection{RunState: "none", CleanupState: "none", QAState: "none", EvidenceState: "none"}
	if len(runs) == 0 {
		return none, nil
	}
	// The store returns Runs newest first; surface the latest Run that is still
	// bound to the current integrated discovery revision.
	for _, run := range runs {
		aggregate, e := s.store.ReadPrototypeExecution(ctx, workspaceID, run.PrototypeRunID)
		if e != nil {
			return GuidedPrototypeSection{}, e
		}
		if workspace.CurrentDiscoveryRevisionRowID.Valid && aggregate.Authorization.DiscoveryRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
			continue
		}
		view, e := s.ReadPrototypeEvidenceForWayfinder(ctx, workspaceID, run.PrototypeRunID)
		if e != nil {
			return GuidedPrototypeSection{}, e
		}
		return composeGuidedPrototypeSection(view), nil
	}
	return none, nil
}

func composeGuidedPrototypeSection(view PrototypeWayfinderEvidenceView) GuidedPrototypeSection {
	result := GuidedPrototypeSection{RunID: view.RunID, RunState: view.RunState, ProcessOutcome: view.ProcessOutcome}
	switch {
	case len(view.Cleanup) == 0:
		result.CleanupState = "none"
	default:
		result.CleanupState = "complete"
		for _, obligation := range view.Cleanup {
			if obligation.Status != "complete" {
				result.CleanupState = "pending"
				break
			}
		}
	}
	for _, packet := range view.QAPackets {
		result.QAState = packet.Packet.Status
		if packet.Admission != nil {
			result.EvidenceState = "admitted"
		}
	}
	if result.QAState == "" {
		result.QAState = "none"
	}
	if result.EvidenceState == "" {
		result.EvidenceState = "none"
	}
	result.Diagnostics = guidedPrototypeDiagnostics(result)
	return result
}

func (s *Service) packetSource(ctx context.Context, runID string) (*workflowstore.PrototypeResult, []workflowstore.PrototypeEvidenceMember, error) {
	result, e := s.store.GetPrototypeResultByRunID(ctx, runID)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, func() []workflowstore.PrototypeEvidenceMember {
			v, _ := s.store.ListPrototypeEvidenceMembers(ctx, runID)
			return v
		}(), nil
	}
	if e != nil {
		return nil, nil, e
	}
	evidence, e := s.store.ListPrototypeEvidenceMembers(ctx, runID)
	return &result, evidence, e
}
func (s *Service) readQAPacketDetail(ctx context.Context, packet workflowstore.PrototypeQAPacket) (PrototypeQAPacketDetail, error) {
	members, e := s.store.ListPrototypeQAPacketMembers(ctx, packet.QAPacketID)
	if e != nil {
		return PrototypeQAPacketDetail{}, e
	}
	evidence, e := s.store.ListPrototypeQAEvidenceByPacketID(ctx, packet.QAPacketID)
	if e != nil {
		return PrototypeQAPacketDetail{}, e
	}
	detail := PrototypeQAPacketDetail{Packet: packet, Members: members, Evidence: evidence}
	if admission, e := s.store.GetPrototypeQAAdmissionByPacketID(ctx, packet.QAPacketID); e == nil {
		detail.Admission = &admission
	} else if !errors.Is(e, sql.ErrNoRows) {
		return PrototypeQAPacketDetail{}, e
	}
	return detail, nil
}
func allFeatureCleanupComplete(values []workflowstore.PrototypeCleanupObligation) bool {
	if len(values) < 5 {
		return false
	}
	for _, v := range values {
		if v.Status != "complete" {
			return false
		}
	}
	return true
}
func qaPacketMediaAllowed(v string) bool {
	return v == "application/vnd.relay.prototype-result+json" || v == "application/json" || v == "text/plain" || v == "text/markdown" || v == "application/octet-stream"
}
func qaEvidenceMediaAllowed(v string) bool {
	return v == "application/json" || v == "text/plain" || v == "text/markdown" || v == "application/octet-stream"
}
func sha256Sum(v []byte) []byte { sum := sha256.Sum256(v); return sum[:] }
func isSecretBearingEvidence(media string, b []byte) bool {
	if media != "application/json" && media != "text/plain" && media != "text/markdown" {
		return false
	}
	text := string(b)
	for _, marker := range []string{"Authorization:", "Bearer ", "api_key", "api-key", "access_token", "-----BEGIN", "PRIVATE KEY", "AWS_SECRET_ACCESS_KEY", "OPENAI_API_KEY"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (s *Service) getPrototypeTicketByRowID(ctx context.Context, rowID int64) (workflowstore.FeatureWorkspaceDiscoveryTicket, error) {
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	err := s.store.DB().QueryRowContext(ctx, `SELECT id,discovery_ticket_id,workspace_row_id,ticket_key,subject,state,version,created_at,updated_at FROM feature_workspace_discovery_tickets WHERE id=?`, rowID).Scan(&ticket.ID, &ticket.DiscoveryTicketID, &ticket.WorkspaceRowID, &ticket.TicketKey, &ticket.Subject, &ticket.State, &ticket.Version, &ticket.CreatedAt, &ticket.UpdatedAt)
	return ticket, err
}
