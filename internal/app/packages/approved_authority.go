package packages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/planningartifacts"
	"relay/internal/sourcevault"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const approvedAuthorityReadLimit = 64 << 20

type approvedPackageInput struct {
	input          PrepareInput
	briefFile      workflowartifacts.File
	briefBytes     []byte
	operations     *workflowartifacts.File
	operationBytes []byte
}

type approvedAuthorityRows struct {
	basis        packageBasis
	members      []workflowstore.DeliveryTicketRevisionMember
	dependencies []workflowstore.DeliveryTicketRevisionDependency
	approvals    []workflowstore.DeliveryTicketRevisionApproval
	layers       []ApprovedAuthorityLayer
}

func (s *Service) LoadApprovedAuthorityForRun(ctx context.Context, runID string) (ApprovedAuthority, error) {
	if runID == "" || strings.TrimSpace(runID) != runID {
		return ApprovedAuthority{}, fmt.Errorf("%w: Run ID must be nonblank without outer whitespace", ErrApprovedAuthorityInvalid)
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovedAuthority{}, fmt.Errorf("%w: %s: %w", ErrRunNotFound, runID, sql.ErrNoRows)
	}
	if err != nil {
		return ApprovedAuthority{}, err
	}
	if !run.ExecutionPackageRowID.Valid {
		return ApprovedAuthority{}, fmt.Errorf("%w: %w", ErrApprovedAuthorityInvalid, ErrRunNotPackage)
	}
	if !run.PackageApprovalRowID.Valid {
		return ApprovedAuthority{}, fmt.Errorf("%w: %w", ErrApprovedAuthorityInvalid, ErrPackageApprovalMissing)
	}

	packageRow, err := s.store.GetExecutionPackageByRowID(ctx, run.ExecutionPackageRowID.Int64)
	if err != nil {
		return ApprovedAuthority{}, fmt.Errorf("%w: load package row: %v", ErrApprovedAuthorityInvalid, err)
	}
	if packageRow.ID != run.ExecutionPackageRowID.Int64 || packageRow.ID <= 0 {
		return ApprovedAuthority{}, fmt.Errorf("%w: Run package row identity is inconsistent", ErrApprovedAuthorityInvalid)
	}
	if run.RepoTarget != packageRow.RepoTarget || run.Branch != packageRow.Branch || run.BaseCommit != packageRow.BaseCommit {
		return ApprovedAuthority{}, fmt.Errorf("%w: Run repository target, branch, or base commit does not match package", ErrApprovedAuthorityInvalid)
	}

	packageApproval, err := s.store.GetExecutionPackageApprovalByPackageRowID(ctx, packageRow.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovedAuthority{}, fmt.Errorf("%w: %w", ErrApprovedAuthorityInvalid, ErrPackageApprovalMissing)
	}
	if err != nil {
		return ApprovedAuthority{}, fmt.Errorf("%w: load package approval: %v", ErrApprovedAuthorityInvalid, err)
	}
	if packageApproval.ID != run.PackageApprovalRowID.Int64 || packageApproval.PackageRowID != packageRow.ID {
		return ApprovedAuthority{}, fmt.Errorf("%w: Run package approval linkage is inconsistent", ErrApprovedAuthorityInvalid)
	}
	if packageApproval.PackageSha256 != packageRow.PackageSha256 {
		return ApprovedAuthority{}, fmt.Errorf("%w: package approval SHA-256 does not match immutable package", ErrApprovedAuthorityInvalid)
	}

	input, err := s.readApprovedPackageInput(ctx, packageRow)
	if err != nil {
		return ApprovedAuthority{}, err
	}
	validated, err := validateInput(input.input)
	if err != nil {
		return ApprovedAuthority{}, fmt.Errorf("%w: validate approved package bytes: %v", ErrApprovedAuthorityInvalid, err)
	}

	var rows approvedAuthorityRows
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		basis, basisErr := s.validateBasis(ctx, tx, input.input, validated, &packageRow, "consumed")
		if basisErr != nil {
			return basisErr
		}
		if err := validateApprovedPackageBindings(ctx, tx, packageRow, basis); err != nil {
			return err
		}
		members, err := tx.ListDeliveryTicketRevisionMembers(ctx, basis.members[0].revision.ID)
		if err != nil {
			return err
		}
		dependencies, err := tx.ListDeliveryTicketRevisionDependencies(ctx, basis.members[0].revision.ID)
		if err != nil {
			return err
		}
		approvals, err := tx.ListDeliveryTicketRevisionApprovals(ctx, basis.members[0].revision.ID)
		if err != nil {
			return err
		}
		layers, err := loadApprovedAuthorityLayers(ctx, tx, s.store.ArtifactStore(), basis.authority, basis.closure)
		if err != nil {
			return err
		}
		rows = approvedAuthorityRows{basis: basis, members: members, dependencies: dependencies, approvals: approvals, layers: layers}
		return nil
	})
	if err != nil {
		return ApprovedAuthority{}, fmt.Errorf("%w: %v", ErrApprovedAuthorityInvalid, err)
	}
	if run.FeatureSlug != rows.basis.workspace.FeatureSlug {
		return ApprovedAuthority{}, fmt.Errorf("%w: Run feature does not match package workspace", ErrApprovedAuthorityInvalid)
	}

	briefProjection, diagnostics := planningartifacts.ProjectTicketDesignBrief(input.briefBytes)
	if len(diagnostics) != 0 {
		return ApprovedAuthority{}, fmt.Errorf("%w: approved Ticket Design Brief projection failed: %v", ErrApprovedAuthorityInvalid, diagnostics)
	}
	deliveryTicketDocument, err := s.loadApprovedDeliveryTicketSource(ctx, rows.basis.closure, rows.basis.workspace, rows.basis.members[0].ticket, rows.basis.members[0].revision)
	if err != nil {
		return ApprovedAuthority{}, err
	}
	result := ApprovedAuthority{
		Run:                run,
		Package:            packageRow,
		PackageApproval:    packageApproval,
		Workspace:          rows.basis.workspace,
		Authority:          rows.basis.authority,
		Source:             rows.basis.closure,
		Ticket:             rows.basis.members[0].ticket,
		TicketRevision:     rows.basis.members[0].revision,
		TicketMembers:      append([]workflowstore.DeliveryTicketRevisionMember(nil), rows.members...),
		TicketDependencies: append([]workflowstore.DeliveryTicketRevisionDependency(nil), rows.dependencies...),
		TicketApproval:     rows.basis.members[0].approval,
		AuthorityLayers:    cloneApprovedAuthorityLayers(rows.layers),
		TicketDesignBrief: ApprovedDocument{
			DisplayName:  filepath.Base(input.briefFile.RelativePath),
			RelativePath: input.briefFile.RelativePath,
			MediaType:    input.briefFile.MediaType,
			SHA256:       input.briefFile.SHA256,
			Bytes:        append([]byte(nil), input.briefBytes...),
		},
		DeliveryTicket: deliveryTicketDocument,
		BriefProjection: planningartifacts.TicketDesignBriefProjection{
			ValidationCommands: append([]planningartifacts.ValidationCommand(nil), briefProjection.ValidationCommands...),
		},
	}
	if input.operations != nil {
		result.DeterministicOperations = &ApprovedDeterministicOperations{
			ApprovedDocument: ApprovedDocument{
				DisplayName:  filepath.Base(input.operations.RelativePath),
				RelativePath: input.operations.RelativePath,
				MediaType:    input.operations.MediaType,
				SHA256:       input.operations.SHA256,
				Bytes:        append([]byte(nil), input.operationBytes...),
			},
			Coverage: packageRow.DeterministicOperationsCoverage.String,
			Document: cloneDeterministicOperations(validated.operations.document),
		}
	}
	return result, nil
}

func (s *Service) readApprovedPackageInput(ctx context.Context, packageRow workflowstore.ExecutionPackage) (approvedPackageInput, error) {
	selection, err := s.store.GetDeliveryTicketSelectionByRowID(ctx, packageRow.SelectionRowID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load package selection: %v", ErrApprovedAuthorityInvalid, err)
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packageRow.WorkspaceRowID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load package workspace: %v", ErrApprovedAuthorityInvalid, err)
	}
	selectionMembers, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load selection members: %v", ErrApprovedAuthorityInvalid, err)
	}
	packageMembers, err := s.store.ListExecutionPackageMembers(ctx, packageRow.ID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load package members: %v", ErrApprovedAuthorityInvalid, err)
	}
	if len(selectionMembers) != 1 || len(packageMembers) != 1 {
		return approvedPackageInput{}, fmt.Errorf("%w: approved package must have exactly one selection member and package member", ErrApprovedAuthorityInvalid)
	}
	selectionMember, packageMember := selectionMembers[0], packageMembers[0]
	if packageMember.SelectionMemberRowID != selectionMember.ID || packageMember.RevisionRowID != selectionMember.RevisionRowID || packageMember.PackageRowID != packageRow.ID {
		return approvedPackageInput{}, fmt.Errorf("%w: package member identity does not match selection member", ErrApprovedAuthorityInvalid)
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, packageMember.RevisionRowID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load selected Ticket revision: %v", ErrApprovedAuthorityInvalid, err)
	}
	ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return approvedPackageInput{}, fmt.Errorf("%w: load selected Ticket: %v", ErrApprovedAuthorityInvalid, err)
	}
	briefName := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	briefFile, briefBytes, err := s.readVerifiedPackageFile(packageRow.PackageID, briefName, "ticket_design_brief", "text/markdown", packageMember.MemberSha256)
	if err != nil {
		return approvedPackageInput{}, err
	}
	loaded := approvedPackageInput{input: PrepareInput{SelectionID: selection.SelectionID, TicketDesignBrief: ArtifactInput{DisplayName: briefName, ExpectedSHA256: packageMember.MemberSha256, Bytes: append([]byte(nil), briefBytes...)}}, briefFile: briefFile, briefBytes: briefBytes}
	if packageRow.DeterministicOperationsSha256.Valid != packageRow.DeterministicOperationsCoverage.Valid || !packageRow.DeterministicOperationsSha256.Valid {
		if packageRow.DeterministicOperationsSha256.Valid != packageRow.DeterministicOperationsCoverage.Valid {
			return approvedPackageInput{}, fmt.Errorf("%w: Deterministic Operations SHA and coverage must be both present or absent", ErrApprovedAuthorityInvalid)
		}
		return loaded, nil
	}
	operationsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	operationsFile, operationsBytes, err := s.readVerifiedPackageFile(packageRow.PackageID, operationsName, "deterministic_operations", "application/json", packageRow.DeterministicOperationsSha256.String)
	if err != nil {
		return approvedPackageInput{}, err
	}
	operations := ArtifactInput{DisplayName: operationsName, ExpectedSHA256: packageRow.DeterministicOperationsSha256.String, Bytes: append([]byte(nil), operationsBytes...)}
	loaded.input.DeterministicOperations = &operations
	loaded.operations, loaded.operationBytes = &operationsFile, operationsBytes
	return loaded, nil
}

func (s *Service) readVerifiedPackageFile(packageID, filename, kind, mediaType, expectedSHA string) (workflowartifacts.File, []byte, error) {
	if packageID == "" || strings.TrimSpace(packageID) != packageID || filename == "" || filepath.Base(filename) != filename {
		return workflowartifacts.File{}, nil, fmt.Errorf("%w: unsafe approved package artifact path", ErrApprovedAuthorityInvalid)
	}
	file, bytes, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{
		Kind: kind, RelativePath: filepath.ToSlash(filepath.Join("packages", packageID, filename)), MediaType: mediaType, SHA256: expectedSHA, SizeBytes: -1,
	}, approvedAuthorityReadLimit)
	if err != nil {
		return workflowartifacts.File{}, nil, fmt.Errorf("%w: package artifact %s is unavailable or changed: %v", ErrApprovedAuthorityInvalid, filename, err)
	}
	return file, bytes, nil
}

func validateApprovedPackageBindings(ctx context.Context, tx *workflowstore.Tx, packageRow workflowstore.ExecutionPackage, basis packageBasis) error {
	selectionMembers, err := tx.ListDeliveryTicketSelectionMembers(ctx, basis.selection.ID)
	if err != nil {
		return err
	}
	packageMembers, err := tx.ListExecutionPackageMembers(ctx, packageRow.ID)
	if err != nil {
		return err
	}
	bindings, err := tx.ListExecutionPackageApprovalBindings(ctx, packageRow.ID)
	if err != nil {
		return err
	}
	if len(selectionMembers) != 1 || len(packageMembers) != 1 || len(bindings) != 1 {
		return fmt.Errorf("%w: approved package selection, member, and approval binding cardinality is invalid", ErrApprovedAuthorityInvalid)
	}
	selectionMember, packageMember, binding := selectionMembers[0], packageMembers[0], bindings[0]
	member := basis.members[0]
	if selectionMember.ID != member.selectionMember.ID || packageMember.ID <= 0 || packageMember.PackageRowID != packageRow.ID || packageMember.SelectionMemberRowID != selectionMember.ID || packageMember.RevisionRowID != member.revision.ID || packageMember.Sequence != selectionMember.Sequence || packageMember.MemberSha256 != member.brief.sha256 {
		return fmt.Errorf("%w: approved package member identity or digest is inconsistent", ErrApprovedAuthorityInvalid)
	}
	if binding.PackageRowID != packageRow.ID || binding.PackageMemberRowID != packageMember.ID || binding.ApprovalRowID != member.approval.ID || binding.AuthorityRevisionRowID != basis.authority.ID || binding.SourceClosureRowID != basis.closure.ID {
		return fmt.Errorf("%w: approved package binding identity is inconsistent", ErrApprovedAuthorityInvalid)
	}
	wantBasis := compoundSHA256("approval-basis-v1", packageRow.PackageSha256, member.approval.ApprovalID, fmt.Sprint(packageMember.ID), member.brief.sha256, fmt.Sprint(member.approval.AuthorityRevisionRowID.Int64), fmt.Sprint(member.approval.SourceClosureRowID))
	if binding.ApprovalBasisSha256 != wantBasis {
		return fmt.Errorf("%w: approved package binding digest is inconsistent", ErrApprovedAuthorityInvalid)
	}
	return nil
}

func loadApprovedAuthorityLayers(ctx context.Context, tx *workflowstore.Tx, artifactStore *workflowartifacts.Store, authority workflowstore.FeatureWorkspaceAuthorityRevision, closure workflowstore.SourceVaultClosure) ([]ApprovedAuthorityLayer, error) {
	layers, err := tx.ListFeatureWorkspaceAuthorityLayers(ctx, authority.ID)
	if err != nil {
		return nil, err
	}
	result := make([]ApprovedAuthorityLayer, 0, len(layers))
	seenSequences := make(map[int64]struct{}, len(layers))
	for _, layer := range layers {
		if layer.AuthorityRevisionRowID != authority.ID || layer.Sequence < 1 || layer.SourceClosureRowID.Valid == false || layer.SourceClosureRowID.Int64 != closure.ID {
			return nil, fmt.Errorf("%w: authority layer %d has an invalid revision or source closure", ErrApprovedAuthorityInvalid, layer.ID)
		}
		if _, exists := seenSequences[layer.Sequence]; exists {
			return nil, fmt.Errorf("%w: authority layer sequence %d is duplicated", ErrApprovedAuthorityInvalid, layer.Sequence)
		}
		seenSequences[layer.Sequence] = struct{}{}
		if layer.ArtifactRowID.Valid == layer.RetainedArtifactRowID.Valid || layer.ArtifactSha256 == "" {
			return nil, fmt.Errorf("%w: authority layer %d has an ambiguous artifact source", ErrApprovedAuthorityInvalid, layer.ID)
		}
		var file workflowartifacts.File
		if layer.ArtifactRowID.Valid {
			artifact, err := tx.GetArtifactByRowID(ctx, layer.ArtifactRowID.Int64)
			if err != nil {
				return nil, fmt.Errorf("%w: load current authority artifact for layer %d: %v", ErrApprovedAuthorityInvalid, layer.ID, err)
			}
			if artifact.Kind != layer.LayerKind || artifact.SHA256 != layer.ArtifactSha256 || artifact.RelativePath == "" || artifact.MediaType == "" {
				return nil, fmt.Errorf("%w: current authority artifact metadata does not match layer %d", ErrApprovedAuthorityInvalid, layer.ID)
			}
			file = workflowartifacts.File{Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}
		} else {
			artifact, err := tx.GetOperationPacketRetainedArtifactByRowID(ctx, layer.RetainedArtifactRowID.Int64)
			if err != nil {
				return nil, fmt.Errorf("%w: load retained authority artifact for layer %d: %v", ErrApprovedAuthorityInvalid, layer.ID, err)
			}
			if artifact.Kind != layer.LayerKind || artifact.SHA256 != layer.ArtifactSha256 || artifact.RelativePath == "" || artifact.MediaType == "" {
				return nil, fmt.Errorf("%w: retained authority artifact metadata does not match layer %d", ErrApprovedAuthorityInvalid, layer.ID)
			}
			file = workflowartifacts.File{Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}
		}
		verified, bytes, err := artifactStore.ReadVerifiedFile(file, approvedAuthorityReadLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: authority layer %d bytes are unavailable or changed: %v", ErrApprovedAuthorityInvalid, layer.ID, err)
		}
		result = append(result, ApprovedAuthorityLayer{Layer: layer, Kind: layer.LayerKind, Sequence: layer.Sequence, RelativePath: verified.RelativePath, MediaType: verified.MediaType, SHA256: verified.SHA256, SizeBytes: verified.SizeBytes, Bytes: append([]byte(nil), bytes...)})
	}
	return result, nil
}

func cloneApprovedAuthorityLayers(layers []ApprovedAuthorityLayer) []ApprovedAuthorityLayer {
	result := make([]ApprovedAuthorityLayer, len(layers))
	for index, layer := range layers {
		result[index] = layer
		result[index].Bytes = append([]byte(nil), layer.Bytes...)
	}
	return result
}

func cloneDeterministicOperations(document *speccompiler.DeterministicOperationsDocument) *speccompiler.DeterministicOperationsDocument {
	if document == nil {
		return nil
	}
	clone := *document
	clone.Operations = make([]speccompiler.DeterministicOperation, len(document.Operations))
	copy(clone.Operations, document.Operations)
	for index := range clone.Operations {
		clone.Operations[index].Implementation = document.Operations[index].Implementation
		clone.Operations[index].Implementation.Changes = append([]speccompiler.DeterministicChange(nil), document.Operations[index].Implementation.Changes...)
	}
	return &clone
}

func (s *Service) loadApprovedDeliveryTicketSource(ctx context.Context, closure workflowstore.SourceVaultClosure, workspace workflowstore.FeatureWorkspace, ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision) (ApprovedSourceDocument, error) {
	if s.sourceVaults == nil {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: source-vault reader is not configured", ErrApprovedAuthorityInvalid)
	}
	if err := validateDeliveryTicketSourcePath(revision.SourcePath); err != nil {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: %v", ErrApprovedAuthorityInvalid, err)
	}
	if revision.SourceClosureRowID != closure.ID {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: selected Ticket revision source closure row ID does not match approved closure", ErrApprovedAuthorityInvalid)
	}
	result, err := s.sourceVaults.ReadPath(ctx, sourcevault.ReadPathRequest{
		ClosureID: closure.ClosureID,
		Path:      revision.SourcePath,
		MaxBytes:  approvedAuthorityReadLimit,
	})
	if err != nil {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document is unavailable: %v", ErrApprovedAuthorityInvalid, err)
	}
	if len(result.Bytes) == 0 {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document is empty", ErrApprovedAuthorityInvalid)
	}
	if !utf8.Valid(result.Bytes) {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document is not valid UTF-8", ErrApprovedAuthorityInvalid)
	}
	var topObject map[string]json.RawMessage
	if err := json.Unmarshal(result.Bytes, &topObject); err != nil || topObject == nil {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document is not a top-level JSON object", ErrApprovedAuthorityInvalid)
	}
	var document speccompiler.DeliveryTicketDocument
	if err := json.Unmarshal(result.Bytes, &document); err != nil {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document is not valid JSON: %v", ErrApprovedAuthorityInvalid, err)
	}
	if document.FeatureSlug != workspace.FeatureSlug || document.TicketID != ticket.TicketID || document.Revision != revision.RevisionNumber || document.RepoTarget != revision.RepoTarget || document.Branch != revision.Branch || document.BaseCommit != revision.BaseCommit {
		return ApprovedSourceDocument{}, fmt.Errorf("%w: Delivery Ticket source document identity does not match selected revision: feature_slug=%q/%q ticket_id=%q/%q revision=%d/%d repo_target=%q/%q branch=%q/%q base_commit=%q/%q", ErrApprovedAuthorityInvalid, document.FeatureSlug, workspace.FeatureSlug, document.TicketID, ticket.TicketID, document.Revision, revision.RevisionNumber, document.RepoTarget, revision.RepoTarget, document.Branch, revision.Branch, document.BaseCommit, revision.BaseCommit)
	}
	digest := sha256Hex(result.Bytes)
	doc := ApprovedSourceDocument{
		DisplayName:  filepath.Base(revision.SourcePath),
		RelativePath: revision.SourcePath,
		MediaType:    "application/json",
		SHA256:       digest,
		ObjectOID:    result.ObjectOID,
		SizeBytes:    int64(len(result.Bytes)),
		Bytes:        append([]byte(nil), result.Bytes...),
	}
	return doc, nil
}

func validateDeliveryTicketSourcePath(path string) error {
	if path == "" || strings.TrimSpace(path) != path {
		return fmt.Errorf("Delivery Ticket source path must be nonblank and free of outer whitespace")
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return fmt.Errorf("Delivery Ticket source path must be repository-relative")
	}
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return fmt.Errorf("Delivery Ticket source path must not include a Windows drive prefix")
		}
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("Delivery Ticket source path must not contain backslashes")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("Delivery Ticket source path must not contain empty, current, or parent segments")
		}
	}
	if !utf8.ValidString(path) {
		return fmt.Errorf("Delivery Ticket source path must be valid UTF-8")
	}
	for _, r := range path {
		if r < 0x20 {
			return fmt.Errorf("Delivery Ticket source path must not contain control characters")
		}
	}
	base := filepath.Base(path)
	if base == "" || strings.TrimSpace(base) != base {
		return fmt.Errorf("Delivery Ticket source path basename must be nonblank")
	}
	if strings.ContainsAny(base, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f") {
		return fmt.Errorf("Delivery Ticket source path basename is not safe for packet embedding")
	}
	return nil
}
