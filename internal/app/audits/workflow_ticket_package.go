package audits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sort"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

const (
	workflowAuditArtifactTypeApprovedExecutionSpec = "approved_execution_spec"
	workflowAuditArtifactTypeTicketDesignBrief     = "ticket_design_brief"
	workflowAuditArtifactTypeTicketPackageEvidence = "ticket_package_evidence"
)

type workflowAuditTicketPackageFile struct {
	PackageMemberRowID int64
	ArtifactType       string
	Description        string
	File               workflowartifacts.File
	Bytes              []byte
}

type workflowAuditTicketPackageState struct {
	Evidence WorkflowAuditTicketPackageEvidence
	Files    []workflowAuditTicketPackageFile
}

type workflowAuditStagedTicketPackage struct {
	Artifacts []workflowstore.Artifact
}

func resolveWorkflowAuditTicketPackage(
	ctx context.Context,
	store *workflowstore.Store,
	run workflowstore.Run,
	implementation WorkflowImplementationEvidence,
) (*workflowAuditTicketPackageState, error) {
	if !run.ExecutionPackageRowID.Valid {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("ticket audit package dependencies are required")
	}
	if implementation.Executor != nil && (!implementation.Executor.AttemptResult.TerminationVerified || implementation.Executor.AttemptResult.CleanupPending) {
		return nil, fmt.Errorf("ticket package executor termination evidence is incomplete")
	}

	pkg, err := getWorkflowAuditExecutionPackageByRowID(ctx, store, run.ExecutionPackageRowID.Int64)
	if err != nil {
		return nil, fmt.Errorf("load ticket audit execution package: %w", err)
	}
	if pkg.RepoTarget != run.RepoTarget || pkg.Branch != run.Branch || pkg.BaseCommit != run.BaseCommit {
		return nil, fmt.Errorf("ticket audit Run no longer matches its execution package basis")
	}
	selection, err := store.GetDeliveryTicketSelectionByRowID(ctx, pkg.SelectionRowID)
	if err != nil {
		return nil, fmt.Errorf("load ticket audit selection: %w", err)
	}
	if selection.State != "consumed" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return nil, fmt.Errorf("ticket audit selection is not the consumed package bundle")
	}
	workspace, err := store.GetFeatureWorkspaceByRowID(ctx, pkg.WorkspaceRowID)
	if err != nil {
		return nil, fmt.Errorf("load ticket audit workspace: %w", err)
	}
	if !workspace.CurrentAuthorityRevisionRowID.Valid || workspace.CurrentAuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
		return nil, fmt.Errorf("ticket audit governing authority no longer matches the package")
	}
	authority, err := store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, pkg.AuthorityRevisionRowID)
	if err != nil {
		return nil, fmt.Errorf("load ticket audit authority: %w", err)
	}
	if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return nil, fmt.Errorf("ticket audit authority source no longer matches the package")
	}
	closure, err := store.GetSourceVaultClosureByRowID(ctx, pkg.SourceClosureRowID)
	if err != nil {
		return nil, fmt.Errorf("load ticket audit source closure: %w", err)
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != pkg.BaseCommit {
		return nil, fmt.Errorf("ticket audit source closure is not the ready package basis")
	}
	members, err := store.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil {
		return nil, fmt.Errorf("list ticket audit package members: %w", err)
	}
	bindings, err := store.ListExecutionPackageApprovalBindings(ctx, pkg.ID)
	if err != nil {
		return nil, fmt.Errorf("list ticket audit package approvals: %w", err)
	}
	selectionMembers, err := store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return nil, fmt.Errorf("list ticket audit selection members: %w", err)
	}
	if len(members) == 0 || len(members) != len(bindings) || len(members) != len(selectionMembers) {
		return nil, fmt.Errorf("ticket audit package members, approvals, and bundle members are incomplete")
	}
	selectionByID := make(map[int64]workflowstore.DeliveryTicketSelectionMember, len(selectionMembers))
	for _, value := range selectionMembers {
		selectionByID[value.ID] = value
	}
	bindingByMemberID := make(map[int64]workflowstore.ExecutionPackageApprovalBinding, len(bindings))
	for _, value := range bindings {
		if value.PackageRowID != pkg.ID || value.AuthorityRevisionRowID != pkg.AuthorityRevisionRowID || value.SourceClosureRowID != pkg.SourceClosureRowID {
			return nil, fmt.Errorf("ticket audit approval binding does not match the package basis")
		}
		if _, exists := bindingByMemberID[value.PackageMemberRowID]; exists {
			return nil, fmt.Errorf("ticket audit package member has duplicate approval bindings")
		}
		bindingByMemberID[value.PackageMemberRowID] = value
	}

	state := &workflowAuditTicketPackageState{Evidence: WorkflowAuditTicketPackageEvidence{
		SchemaVersion: WorkflowAuditTicketPackageEvidenceSchemaVersion,
		Package: WorkflowAuditExecutionPackageEvidence{
			PackageRowID: pkg.ID, PackageID: pkg.PackageID, PackageSHA256: pkg.PackageSha256,
			RepoTarget: pkg.RepoTarget, Branch: pkg.Branch, BaseCommit: pkg.BaseCommit,
			SelectionRowID: selection.ID, SelectionID: selection.SelectionID, SelectionState: selection.State,
			WorkspaceRowID: workspace.ID, WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug,
			Authority: WorkflowAuditAuthorityBasisEvidence{
				AuthorityRevisionRowID: authority.ID, AuthorityRevisionID: authority.AuthorityRevisionID,
				RevisionNumber: authority.RevisionNumber, SHA256: pkg.AuthoritySha256,
				SourceClosureRowID: pkg.SourceClosureRowID,
			},
			Source: WorkflowAuditSourceBasisEvidence{
				SourceClosureRowID: closure.ID, ClosureID: closure.ClosureID, CommitOID: closure.CommitOID,
				TreeOID: closure.TreeOID, RefName: closure.RefName, SHA256: pkg.SourceSha256,
			},
			DesignBriefSHA256: pkg.DesignBriefSha256, ExecutionSpecSHA256: pkg.ExecutionSpecSha256,
			ExecutionSpec: WorkflowAuditPacketArtifact{
				ArtifactType: workflowAuditArtifactTypeApprovedExecutionSpec, SHA256: pkg.ExecutionSpecSha256,
				Description: "Immutable copy of the approved package Execution Spec.",
			},
		},
		BundleIntegration: WorkflowAuditBundleIntegrationEvidence{
			RunID: run.RunID, ExecutionPackageRowID: pkg.ID, ExecutionPackageID: pkg.PackageID,
			SelectionID: selection.SelectionID, SelectionState: selection.State, ApprovedRunStatus: "package_linked",
		},
	}}

	sort.Slice(members, func(i, j int) bool {
		if members[i].Sequence == members[j].Sequence {
			return members[i].ID < members[j].ID
		}
		return members[i].Sequence < members[j].Sequence
	})
	for _, member := range members {
		selectionMember, ok := selectionByID[member.SelectionMemberRowID]
		if !ok || member.PackageRowID != pkg.ID || selectionMember.RevisionRowID != member.RevisionRowID {
			return nil, fmt.Errorf("ticket audit package member does not match the consumed bundle")
		}
		binding, ok := bindingByMemberID[member.ID]
		if !ok || binding.ApprovalRowID != selectionMember.ApprovalRowID {
			return nil, fmt.Errorf("ticket audit package member has no exact approved binding")
		}
		revision, err := store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if err != nil {
			return nil, fmt.Errorf("load ticket audit revision: %w", err)
		}
		ticket, err := store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil {
			return nil, fmt.Errorf("load ticket audit ticket: %w", err)
		}
		if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID || revision.CancellationReason.Valid ||
			revision.RepoTarget != pkg.RepoTarget || revision.Branch != pkg.Branch || revision.BaseCommit != pkg.BaseCommit ||
			revision.SourceClosureRowID != pkg.SourceClosureRowID {
			return nil, fmt.Errorf("ticket audit revision is no longer the current package obligation")
		}
		approvals, err := store.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
		if err != nil {
			return nil, fmt.Errorf("list ticket audit approvals: %w", err)
		}
		var approval workflowstore.DeliveryTicketRevisionApproval
		for _, candidate := range approvals {
			if candidate.ID == binding.ApprovalRowID {
				approval = candidate
				break
			}
		}
		if approval.ID == 0 || approval.ApprovalKind != "delivery" || approval.ApprovalState != "approved" ||
			approval.SourceClosureRowID != pkg.SourceClosureRowID || !approval.AuthorityRevisionRowID.Valid || approval.AuthorityRevisionRowID.Int64 != pkg.AuthorityRevisionRowID {
			return nil, fmt.Errorf("ticket audit approval is no longer exact for the package basis")
		}
		briefName := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
		brief, briefBytes, err := readWorkflowAuditPackageFile(store.ArtifactStore(), pkg.PackageID, briefName, member.MemberSha256, "text/markdown", MaxWorkflowAuditSourceBytes)
		if err != nil {
			return nil, fmt.Errorf("read ticket audit design brief: %w", err)
		}
		state.Files = append(state.Files, workflowAuditTicketPackageFile{
			PackageMemberRowID: member.ID, ArtifactType: workflowAuditArtifactTypeTicketDesignBrief,
			Description: "Immutable copy of the approved Ticket Design Brief.", File: brief, Bytes: briefBytes,
		})
		state.Evidence.Tickets = append(state.Evidence.Tickets, WorkflowAuditTicketObligationEvidence{
			PackageMemberRowID: member.ID, SelectionMemberRowID: selectionMember.ID, Sequence: member.Sequence,
			DeliveryTicketRowID: ticket.ID, TicketID: ticket.TicketID, DeliveryTicketRevisionID: revision.ID,
			RevisionNumber: revision.RevisionNumber, SourcePath: revision.SourcePath, MemberSHA256: member.MemberSha256,
			Approval: WorkflowAuditApprovalEvidence{
				ApprovalRowID: approval.ID, ApprovalID: approval.ApprovalID, ApprovalBasisSHA256: binding.ApprovalBasisSha256,
				AuthorityRevisionRowID: binding.AuthorityRevisionRowID, SourceClosureRowID: binding.SourceClosureRowID,
			},
			DesignBrief: WorkflowAuditPacketArtifact{
				ArtifactType: workflowAuditArtifactTypeTicketDesignBrief, SHA256: brief.SHA256,
				Description: "Immutable copy of the approved Ticket Design Brief.",
			},
		})
	}
	specName := workspace.FeatureSlug + ".execution-spec.json"
	spec, specBytes, err := readWorkflowAuditPackageFile(store.ArtifactStore(), pkg.PackageID, specName, pkg.ExecutionSpecSha256, "application/json", MaxWorkflowAuditSourceBytes)
	if err != nil {
		return nil, fmt.Errorf("read ticket audit execution spec: %w", err)
	}
	state.Files = append(state.Files, workflowAuditTicketPackageFile{
		ArtifactType: workflowAuditArtifactTypeApprovedExecutionSpec,
		Description:  "Immutable copy of the approved package Execution Spec.", File: spec, Bytes: specBytes,
	})

	leases, err := store.ListRepositoryBranchMutationLeases(ctx, run.RepoTarget, run.Branch)
	if err != nil {
		return nil, fmt.Errorf("list ticket audit mutation leases: %w", err)
	}
	for _, lease := range leases {
		if lease.OwnerKind != "run_execution" || lease.OwnerIdentity != run.RunID {
			continue
		}
		if lease.State != workflowstore.RepositoryBranchMutationLeaseStateReleased ||
			lease.UncertaintyState != workflowstore.RepositoryBranchMutationLeaseCertaintyCertain ||
			(lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired &&
				lease.ReconciliationState != workflowstore.RepositoryBranchMutationLeaseReconciliationReconciled) ||
			!lease.ReleasedAt.Valid {
			return nil, fmt.Errorf("ticket audit mutation lease is uncertain or unreconciled")
		}
		value := WorkflowAuditMutationLeaseEvidence{
			LeaseID: lease.LeaseID, OwnerKind: lease.OwnerKind, OwnerIdentity: lease.OwnerIdentity,
			State: lease.State, Certainty: lease.UncertaintyState, ReconciliationState: lease.ReconciliationState,
			AcquiredAt: lease.AcquiredAt, ReleasedAt: lease.ReleasedAt.String,
		}
		if lease.ReconciliationStartedAt.Valid {
			value.ReconciliationStartedAt = lease.ReconciliationStartedAt.String
		}
		if lease.ReconciledAt.Valid {
			value.ReconciledAt = lease.ReconciledAt.String
		}
		state.Evidence.MutationLeases = append(state.Evidence.MutationLeases, value)
	}
	return state, nil
}

func readWorkflowAuditPackageFile(
	store *workflowartifacts.Store,
	packageID, filename, sha256, mediaType string,
	maxBytes int,
) (workflowartifacts.File, []byte, error) {
	file := workflowartifacts.File{
		RelativePath: filepath.ToSlash(filepath.Join("packages", packageID, filename)),
		MediaType:    mediaType,
		SHA256:       sha256,
		SizeBytes:    -1,
	}
	return store.ReadVerifiedFile(file, maxBytes)
}

func getWorkflowAuditExecutionPackageByRowID(ctx context.Context, store *workflowstore.Store, rowID int64) (workflowstore.ExecutionPackage, error) {
	var value workflowstore.ExecutionPackage
	err := store.DB().QueryRowContext(ctx, `
SELECT id, package_id, selection_row_id, workspace_row_id, repo_target, branch, base_commit,
       source_closure_row_id, authority_revision_row_id, package_sha256, authority_sha256,
       source_sha256, design_brief_sha256, execution_spec_sha256, created_at
FROM execution_packages
WHERE id = ?`, rowID).Scan(
		&value.ID, &value.PackageID, &value.SelectionRowID, &value.WorkspaceRowID,
		&value.RepoTarget, &value.Branch, &value.BaseCommit, &value.SourceClosureRowID,
		&value.AuthorityRevisionRowID, &value.PackageSha256, &value.AuthoritySha256,
		&value.SourceSha256, &value.DesignBriefSha256, &value.ExecutionSpecSha256, &value.CreatedAt,
	)
	return value, err
}

func sameWorkflowAuditTicketPackageBasis(left, right WorkflowAuditTicketPackageEvidence) bool {
	return reflect.DeepEqual(workflowAuditTicketPackageBasis(left), workflowAuditTicketPackageBasis(right))
}

func workflowAuditTicketPackageBasis(value WorkflowAuditTicketPackageEvidence) WorkflowAuditTicketPackageEvidence {
	result := value
	result.Tickets = append([]WorkflowAuditTicketObligationEvidence(nil), value.Tickets...)
	result.Package.ExecutionSpec = WorkflowAuditPacketArtifact{}
	for index := range result.Tickets {
		result.Tickets[index].DesignBrief = WorkflowAuditPacketArtifact{}
	}
	result.Commit = WorkflowAuditTicketPackageCommitEvidence{}
	result.Implementation = nil
	result.Validation = nil
	return result
}

func stageWorkflowAuditTicketPackage(
	batch *workflowartifacts.Batch,
	state *workflowAuditTicketPackageState,
	commit workflowrepos.AuditCommitEvidence,
	diffArtifact workflowstore.Artifact,
) (workflowAuditStagedTicketPackage, error) {
	if batch == nil || state == nil {
		return workflowAuditStagedTicketPackage{}, fmt.Errorf("ticket audit package staging input is required")
	}
	evidence := state.Evidence
	staged := workflowAuditStagedTicketPackage{}
	for _, source := range state.Files {
		filename := filepath.Base(source.File.RelativePath)
		file, err := batch.Stage(source.ArtifactType, filename, source.File.MediaType, source.Bytes)
		if err != nil {
			return workflowAuditStagedTicketPackage{}, err
		}
		artifact := workflowAuditArtifactFromFile(workflowstore.NewArtifactID(), file)
		staged.Artifacts = append(staged.Artifacts, artifact)
		ref := WorkflowAuditPacketArtifact{
			ArtifactReference: artifact.ArtifactID, ArtifactType: source.ArtifactType,
			SHA256: artifact.SHA256, Description: source.Description,
		}
		if source.PackageMemberRowID == 0 {
			evidence.Package.ExecutionSpec = ref
			continue
		}
		found := false
		for index := range evidence.Tickets {
			if evidence.Tickets[index].PackageMemberRowID == source.PackageMemberRowID {
				evidence.Tickets[index].DesignBrief = ref
				found = true
				break
			}
		}
		if !found {
			return workflowAuditStagedTicketPackage{}, fmt.Errorf("ticket audit design brief has no package member")
		}
	}
	if evidence.Package.ExecutionSpec.ArtifactReference == "" {
		return workflowAuditStagedTicketPackage{}, fmt.Errorf("ticket audit package has no approved Execution Spec")
	}
	for _, ticket := range evidence.Tickets {
		if ticket.DesignBrief.ArtifactReference == "" {
			return workflowAuditStagedTicketPackage{}, fmt.Errorf("ticket audit package has no approved Ticket Design Brief")
		}
	}
	evidence.Commit = WorkflowAuditTicketPackageCommitEvidence{
		RepoTarget: evidence.Package.RepoTarget, Branch: commit.Branch, BaseCommit: commit.BaseCommit,
		AuditedCommit: commit.AuditedCommit, NameStatus: commit.NameStatus, DiffStat: commit.DiffStat,
		CommitLog: commit.CommitLog,
		UnifiedDiff: WorkflowAuditPacketArtifact{
			ArtifactReference: diffArtifact.ArtifactID, ArtifactType: "unified_diff", SHA256: diffArtifact.SHA256,
			Description: "Complete unified diff for the audited commit range.",
		},
	}
	document, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return workflowAuditStagedTicketPackage{}, err
	}
	document = append(document, '\n')
	if len(document) > MaxWorkflowAuditEvidenceBytes {
		return workflowAuditStagedTicketPackage{}, ErrWorkflowAuditPacketTooLarge
	}
	file, err := batch.Stage(workflowAuditArtifactTypeTicketPackageEvidence, "ticket-package-evidence.json", "application/json", document)
	if err != nil {
		return workflowAuditStagedTicketPackage{}, err
	}
	staged.Artifacts = append(staged.Artifacts, workflowAuditArtifactFromFile(workflowstore.NewArtifactID(), file))
	return staged, nil
}

func workflowAuditArtifactFromFile(artifactID string, file workflowartifacts.File) workflowstore.Artifact {
	return workflowstore.Artifact{
		ArtifactID: artifactID, OwnerType: workflowstore.ArtifactOwnerRun,
		Kind: file.Kind, RelativePath: file.RelativePath, MediaType: file.MediaType,
		SHA256: file.SHA256, SizeBytes: file.SizeBytes,
	}
}

func workflowAuditTicketPackageArtifact(packet WorkflowAuditPacket) (WorkflowAuditPacketArtifact, bool, error) {
	var found *WorkflowAuditPacketArtifact
	for index := range packet.Artifacts {
		value := &packet.Artifacts[index]
		if value.ArtifactType != workflowAuditArtifactTypeTicketPackageEvidence {
			continue
		}
		if found != nil {
			return WorkflowAuditPacketArtifact{}, false, fmt.Errorf("ticket audit packet declares multiple package evidence artifacts")
		}
		copyValue := *value
		found = &copyValue
	}
	if found == nil {
		return WorkflowAuditPacketArtifact{}, false, nil
	}
	return *found, true, nil
}

func decodeWorkflowAuditTicketPackageEvidence(data []byte) (WorkflowAuditTicketPackageEvidence, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value WorkflowAuditTicketPackageEvidence
	if err := decoder.Decode(&value); err != nil {
		return WorkflowAuditTicketPackageEvidence{}, fmt.Errorf("decode ticket audit package evidence: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return WorkflowAuditTicketPackageEvidence{}, fmt.Errorf("ticket audit package evidence has trailing content")
	}
	if value.SchemaVersion != WorkflowAuditTicketPackageEvidenceSchemaVersion || value.Package.PackageID == "" || len(value.Tickets) == 0 {
		return WorkflowAuditTicketPackageEvidence{}, fmt.Errorf("ticket audit package evidence identity is incomplete")
	}
	return value, nil
}

func verifyWorkflowAuditTicketPackageEvidence(
	ctx context.Context,
	store *workflowstore.Store,
	run workflowstore.Run,
	implementation WorkflowImplementationEvidence,
	packetRow workflowstore.AuditPacket,
	packet WorkflowAuditPacket,
) error {
	state, err := resolveWorkflowAuditTicketPackage(ctx, store, run, implementation)
	if err != nil {
		return err
	}
	reference, declared, err := workflowAuditTicketPackageArtifact(packet)
	if err != nil {
		return err
	}
	if state == nil {
		if declared {
			return fmt.Errorf("legacy audit packet unexpectedly declares ticket package evidence")
		}
		return nil
	}
	if !declared {
		return fmt.Errorf("ticket audit packet has no package evidence artifact")
	}
	artifact, err := store.GetArtifactByArtifactID(ctx, reference.ArtifactReference)
	if err != nil {
		return fmt.Errorf("load ticket audit package evidence artifact: %w", err)
	}
	if artifact.ID == 0 || artifact.Kind != workflowAuditArtifactTypeTicketPackageEvidence ||
		artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != packetRow.RunRowID ||
		artifact.SHA256 != reference.SHA256 {
		return fmt.Errorf("ticket audit package evidence artifact identity is invalid")
	}
	data, err := readWorkflowArtifact(store, artifact, MaxWorkflowAuditEvidenceBytes)
	if err != nil {
		return fmt.Errorf("read ticket audit package evidence: %w", err)
	}
	evidence, err := decodeWorkflowAuditTicketPackageEvidence(data)
	if err != nil {
		return err
	}
	if !sameWorkflowAuditTicketPackageBasis(evidence, state.Evidence) {
		return fmt.Errorf("ticket audit package basis changed")
	}
	if err := verifyWorkflowAuditTicketPackageArtifactReference(ctx, store, packetRow, packet, evidence.Package.ExecutionSpec, workflowAuditArtifactTypeApprovedExecutionSpec); err != nil {
		return fmt.Errorf("verify ticket audit approved Execution Spec: %w", err)
	}
	if len(evidence.Tickets) != len(state.Evidence.Tickets) {
		return fmt.Errorf("ticket audit package obligation count changed")
	}
	for index, ticket := range evidence.Tickets {
		if err := verifyWorkflowAuditTicketPackageArtifactReference(ctx, store, packetRow, packet, ticket.DesignBrief, workflowAuditArtifactTypeTicketDesignBrief); err != nil {
			return fmt.Errorf("verify ticket audit Ticket Design Brief %d: %w", index+1, err)
		}
	}
	return nil
}

func verifyWorkflowAuditTicketPackageArtifactReference(
	ctx context.Context,
	store *workflowstore.Store,
	packetRow workflowstore.AuditPacket,
	packet WorkflowAuditPacket,
	reference WorkflowAuditPacketArtifact,
	wantKind string,
) error {
	if reference.ArtifactReference == "" || reference.ArtifactType != wantKind {
		return fmt.Errorf("ticket audit package artifact reference is incomplete")
	}
	declared, err := resolvePacketArtifact(packet.Artifacts, reference.ArtifactReference)
	if err != nil || declared != reference {
		return fmt.Errorf("ticket audit package artifact is not declared by the packet")
	}
	artifact, err := store.GetArtifactByArtifactID(ctx, reference.ArtifactReference)
	if err != nil {
		return fmt.Errorf("load ticket audit package artifact: %w", err)
	}
	if artifact.Kind != wantKind || artifact.OwnerType != workflowstore.ArtifactOwnerRun ||
		!artifact.RunRowID.Valid || artifact.RunRowID.Int64 != packetRow.RunRowID || artifact.SHA256 != reference.SHA256 {
		return fmt.Errorf("ticket audit package artifact identity is invalid")
	}
	if _, err := store.ArtifactStore().VerifyFile(workflowartifacts.File{
		RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes,
	}); err != nil {
		return fmt.Errorf("verify ticket audit package artifact: %w", err)
	}
	return nil
}
