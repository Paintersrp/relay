package packages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	workflowruns "relay/internal/app/runs/workflow"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/planningartifacts"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidPackageInput   = errors.New("invalid execution package input")
	ErrSelectionNotFound     = errors.New("delivery ticket selection not found")
	ErrSelectionNotActive    = errors.New("delivery ticket selection is not active")
	ErrSelectionInvalid      = errors.New("delivery ticket selection cardinality is invalid")
	ErrPackageNotFound       = errors.New("execution package not found")
	ErrPackageAlreadyRun     = errors.New("execution package already has a Run")
	ErrPackageBasisChanged   = errors.New("execution package basis changed")
)

var packageSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	store *workflowstore.Store
	runs  *workflowruns.Service
}

type validatedInput struct {
	specIdentity speccompiler.FilenameInfo
	spec         *speccompiler.ExecutionDocument
	rendered     []byte
	briefs       map[string]validatedBrief
}

type validatedBrief struct {
	input    ArtifactInput
	identity speccompiler.FilenameInfo
	sha256   string
}

type packageMemberBasis struct {
	selectionMember workflowstore.DeliveryTicketSelectionMember
	revision        workflowstore.DeliveryTicketRevision
	ticket          workflowstore.DeliveryTicket
	approval        workflowstore.DeliveryTicketRevisionApproval
	brief           validatedBrief
	packageMember   workflowstore.ExecutionPackageMember
}

type packageBasis struct {
	selection workflowstore.DeliveryTicketSelection
	workspace workflowstore.FeatureWorkspace
	authority workflowstore.FeatureWorkspaceAuthorityRevision
	closure   workflowstore.SourceVaultClosure
	members   []packageMemberBasis

	sourceSHA256      string
	authoritySHA256   string
	designBriefSHA256 string
	packageSHA256     string
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, runs: runs}, nil
}

func (s *Service) Prepare(ctx context.Context, input PrepareInput) (PrepareResult, error) {
	validated, err := validateInput(input)
	if err != nil {
		return PrepareResult{}, err
	}
	packageID := workflowstore.NewExecutionPackageID()
	batch, err := s.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("packages", packageID)))
	if err != nil {
		return PrepareResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = batch.Rollback()
		}
	}()

	briefFiles := make([]workflowartifacts.File, 0, len(input.TicketDesignBriefs))
	for _, brief := range input.TicketDesignBriefs {
		file, stageErr := batch.Stage("ticket_design_brief", brief.DisplayName, "text/markdown", brief.Bytes)
		if stageErr != nil {
			return PrepareResult{}, stageErr
		}
		briefFiles = append(briefFiles, file)
	}
	specFile, err := batch.Stage("execution_spec", input.ExecutionSpec.DisplayName, "application/json", input.ExecutionSpec.Bytes)
	if err != nil {
		return PrepareResult{}, err
	}

	result := PrepareResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		basis, basisErr := s.validateBasis(ctx, tx, input, validated, nil)
		if basisErr != nil {
			return basisErr
		}
		packageRow, createErr := tx.CreateExecutionPackage(ctx, workflowstore.CreateExecutionPackageParams{
			PackageID:              packageID,
			SelectionRowID:         basis.selection.ID,
			WorkspaceRowID:         basis.workspace.ID,
			RepoTarget:             validated.spec.RepoTarget,
			Branch:                 validated.spec.Branch,
			BaseCommit:             validated.spec.BaseCommit,
			SourceClosureRowID:     basis.closure.ID,
			AuthorityRevisionRowID: basis.authority.ID,
			PackageSha256:          basis.packageSHA256,
			AuthoritySha256:        basis.authoritySHA256,
			SourceSha256:           basis.sourceSHA256,
			DesignBriefSha256:      basis.designBriefSHA256,
			ExecutionSpecSha256:    validated.specIdentityHash(input.ExecutionSpec),
		})
		if createErr != nil {
			return fmt.Errorf("create execution package: %w", createErr)
		}
		result.Package = packageRow
		result.Members = make([]workflowstore.ExecutionPackageMember, 0, len(basis.members))
		for _, member := range basis.members {
			packageMember, memberErr := tx.CreateExecutionPackageMember(ctx, workflowstore.CreateExecutionPackageMemberParams{
				PackageRowID:         packageRow.ID,
				SelectionMemberRowID: member.selectionMember.ID,
				Sequence:             member.selectionMember.Sequence,
				RevisionRowID:        member.revision.ID,
				MemberSha256:         member.brief.sha256,
			})
			if memberErr != nil {
				return fmt.Errorf("create execution package member: %w", memberErr)
			}
			result.Members = append(result.Members, packageMember)
		}
		result.Briefs = packageArtifactsFromFiles(briefFiles)
		result.ExecutionSpec = packageArtifactFromFile(specFile)
		return nil
	})
	if err != nil {
		return PrepareResult{}, err
	}
	committed = true
	return result, nil
}

func (s *Service) Approve(ctx context.Context, input ApproveInput) (ApproveResult, error) {
	if input.PackageID == "" || strings.TrimSpace(input.PackageID) != input.PackageID {
		return ApproveResult{}, fmt.Errorf("%w: package ID must be nonblank without outer whitespace", ErrInvalidPackageInput)
	}
	if !packageSHA256.MatchString(input.ExpectedPackageSha256) {
		return ApproveResult{}, fmt.Errorf("%w: expected package SHA-256 must be 64 lowercase hexadecimal characters", ErrInvalidPackageInput)
	}
	evidence := strings.TrimSpace(input.OperatorConfirmationEvidence)
	if evidence == "" || len(evidence) > 4096 {
		return ApproveResult{}, fmt.Errorf("%w: operator confirmation evidence must be 1-4096 non-whitespace characters", ErrInvalidPackageInput)
	}

	packageRow, err := s.store.GetExecutionPackageByPackageID(ctx, input.PackageID)
	if errors.Is(err, sql.ErrNoRows) {
		return ApproveResult{}, fmt.Errorf("%w: %s", ErrPackageNotFound, input.PackageID)
	}
	if err != nil {
		return ApproveResult{}, err
	}
	if _, runErr := s.store.GetRunByExecutionPackageRowID(ctx, packageRow.ID); runErr == nil {
		return ApproveResult{}, fmt.Errorf("%w: %s", ErrPackageAlreadyRun, input.PackageID)
	} else if !errors.Is(runErr, sql.ErrNoRows) {
		return ApproveResult{}, runErr
	}
	if _, approvalErr := s.store.GetExecutionPackageApprovalByPackageRowID(ctx, packageRow.ID); approvalErr == nil {
		return ApproveResult{}, fmt.Errorf("%w: %s", ErrPackageAlreadyRun, input.PackageID)
	} else if !errors.Is(approvalErr, sql.ErrNoRows) {
		return ApproveResult{}, approvalErr
	}

	prepareInput, err := s.readPackageInput(ctx, packageRow)
	if err != nil {
		return ApproveResult{}, err
	}
	validated, err := validateInput(prepareInput)
	if err != nil {
		return ApproveResult{}, err
	}
	var (
		approvalID       = workflowstore.NewExecutionPackageApprovalID()
		approvalRowID    int64
	)

	created, err := s.runs.CreatePackageRun(ctx, workflowruns.CreatePackageRunInput{
		FeatureSlug:              validated.spec.FeatureSlug,
		RepoTarget:               validated.spec.RepoTarget,
		Branch:                   validated.spec.Branch,
		BaseCommit:               validated.spec.BaseCommit,
		CanonicalJSON:            prepareInput.ExecutionSpec.Bytes,
		RenderedMarkdown:         validated.rendered,
		ExecutionPackageRowID:    packageRow.ID,
		PackageApprovalRowIDRef:  &approvalRowID,
		Preflight: func(ctx context.Context, tx *workflowstore.Tx) error {
			freshInput, readErr := s.rereadPackageInput(packageRow.PackageID, prepareInput)
			if readErr != nil {
				return readErr
			}
			freshValidated, validateErr := validateInput(freshInput)
			if validateErr != nil {
				return validateErr
			}
			if !samePackageInput(prepareInput, freshInput) {
				return fmt.Errorf("%w: package bytes changed during approval", ErrPackageBasisChanged)
			}
			basis, basisErr := s.validateBasis(ctx, tx, freshInput, freshValidated, &packageRow)
			if basisErr != nil {
				return basisErr
			}
			if input.ExpectedPackageSha256 != basis.packageSHA256 {
				return fmt.Errorf("%w: expected package SHA does not match the current package basis", ErrPackageBasisChanged)
			}
			bindings, listErr := tx.ListExecutionPackageApprovalBindings(ctx, packageRow.ID)
			if listErr != nil {
				return listErr
			}
			if len(bindings) != 0 {
				return fmt.Errorf("%w: approval bindings already exist", ErrPackageAlreadyRun)
			}
			packageMembers, listErr := tx.ListExecutionPackageMembers(ctx, packageRow.ID)
			if listErr != nil {
				return listErr
			}
			if len(packageMembers) != len(basis.members) {
				return fmt.Errorf("%w: package member count changed", ErrPackageBasisChanged)
			}
			memberByRevision := make(map[int64]workflowstore.ExecutionPackageMember, len(packageMembers))
			for _, member := range packageMembers {
				memberByRevision[member.RevisionRowID] = member
			}
			for index := range basis.members {
				member := &basis.members[index]
				packageMember, ok := memberByRevision[member.revision.ID]
				if !ok || packageMember.Sequence != member.selectionMember.Sequence || packageMember.MemberSha256 != member.brief.sha256 {
					return fmt.Errorf("%w: package member %d changed", ErrPackageBasisChanged, member.selectionMember.Sequence)
				}
				member.packageMember = packageMember
				approvalBasis := compoundSHA256(
					"approval-basis-v1", packageRow.PackageSha256, member.approval.ApprovalID,
					strconv.FormatInt(member.packageMember.ID, 10), member.brief.sha256,
					strconv.FormatInt(member.approval.AuthorityRevisionRowID.Int64, 10),
					strconv.FormatInt(member.approval.SourceClosureRowID, 10),
				)
				if _, createErr := tx.CreateExecutionPackageApprovalBinding(ctx, workflowstore.CreateExecutionPackageApprovalBindingParams{
					PackageRowID:           packageRow.ID,
					PackageMemberRowID:     member.packageMember.ID,
					ApprovalRowID:          member.approval.ID,
					AuthorityRevisionRowID: member.approval.AuthorityRevisionRowID.Int64,
					SourceClosureRowID:     member.approval.SourceClosureRowID,
					ApprovalBasisSha256:    approvalBasis,
				}); createErr != nil {
					return fmt.Errorf("create execution package approval binding: %w", createErr)
				}
			}
			packageApproval, createApprovalErr := tx.CreateExecutionPackageApproval(ctx, workflowstore.CreateExecutionPackageApprovalParams{
				PackageRowID:                 packageRow.ID,
				ApprovalID:                   approvalID,
				PackageSha256:                basis.packageSHA256,
				OperatorConfirmationEvidence: evidence,
			})
			if createApprovalErr != nil {
				return fmt.Errorf("create execution package approval: %w", createApprovalErr)
			}
			approvalRowID = packageApproval.ID
			if _, consumeErr := tx.ConsumeDeliveryTicketSelection(ctx, basis.selection.SelectionID); consumeErr != nil {
				return fmt.Errorf("consume delivery ticket selection: %w", consumeErr)
			}
			return nil
		},
	})
	if err != nil {
		return ApproveResult{}, err
	}
	packageApproval, err := s.store.GetExecutionPackageApprovalByApprovalID(ctx, approvalID)
	if err != nil {
		return ApproveResult{}, err
	}
	packageRow, err = s.store.GetExecutionPackageByPackageID(ctx, input.PackageID)
	if err != nil {
		return ApproveResult{}, err
	}
	return ApproveResult{Package: packageRow, Run: created.Run, RunArtifacts: created.Artifacts, PackageApproval: packageApproval}, nil
}

func (s *Service) Get(ctx context.Context, packageID string) (Detail, error) {
	if packageID == "" || strings.TrimSpace(packageID) != packageID {
		return Detail{}, fmt.Errorf("%w: package ID must be nonblank without outer whitespace", ErrInvalidPackageInput)
	}
	packageRow, err := s.store.GetExecutionPackageByPackageID(ctx, packageID)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, fmt.Errorf("%w: %s", ErrPackageNotFound, packageID)
	}
	if err != nil {
		return Detail{}, err
	}
	members, err := s.store.ListExecutionPackageMembers(ctx, packageRow.ID)
	if err != nil {
		return Detail{}, err
	}
	bindings, err := s.store.ListExecutionPackageApprovalBindings(ctx, packageRow.ID)
	if err != nil {
		return Detail{}, err
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packageRow.WorkspaceRowID)
	if err != nil {
		return Detail{}, err
	}
	briefs := make([]PackageArtifact, 0, len(members))
	for _, member := range members {
		revision, revisionErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if revisionErr != nil {
			return Detail{}, revisionErr
		}
		memberTicket, ticketErr := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if ticketErr != nil {
			return Detail{}, ticketErr
		}
		filename := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, memberTicket.TicketID, revision.RevisionNumber)
		bytes, readErr := s.readPackageFile(packageRow.PackageID, filename)
		if readErr != nil {
			return Detail{}, readErr
		}
		sha := sha256Hex(bytes)
		if sha != member.MemberSha256 {
			return Detail{}, fmt.Errorf("%w: design brief %s no longer matches its package member", ErrPackageBasisChanged, filename)
		}
		briefs = append(briefs, PackageArtifact{DisplayName: filename, RelativePath: filepath.ToSlash(filepath.Join("packages", packageRow.PackageID, filename)), SHA256: sha, SizeBytes: int64(len(bytes))})
	}
	specName := workspace.FeatureSlug + ".execution-spec.json"
	specBytes, err := s.readPackageFile(packageRow.PackageID, specName)
	if err != nil {
		return Detail{}, err
	}
	specSHA := sha256Hex(specBytes)
	if specSHA != packageRow.ExecutionSpecSha256 {
		return Detail{}, fmt.Errorf("%w: execution spec no longer matches its package basis", ErrPackageBasisChanged)
	}
	detail := Detail{
		Package:          packageRow,
		Members:          members,
		ApprovalBindings: bindings,
		Briefs:           briefs,
		ExecutionSpec:    PackageArtifact{DisplayName: specName, RelativePath: filepath.ToSlash(filepath.Join("packages", packageRow.PackageID, specName)), SHA256: specSHA, SizeBytes: int64(len(specBytes))},
	}
	if run, runErr := s.store.GetRunByExecutionPackageRowID(ctx, packageRow.ID); runErr == nil {
		detail.Run = &run
		if run.PackageApprovalRowID.Valid {
			approval, approvalErr := s.store.GetExecutionPackageApprovalByPackageRowID(ctx, packageRow.ID)
			if approvalErr == nil {
				detail.PackageApprovalID = approval.ApprovalID
			}
		}
	} else if !errors.Is(runErr, sql.ErrNoRows) {
		return Detail{}, runErr
	}
	return detail, nil
}

func validateInput(input PrepareInput) (validatedInput, error) {
	if input.SelectionID == "" || strings.TrimSpace(input.SelectionID) != input.SelectionID {
		return validatedInput{}, fmt.Errorf("%w: selection ID must be nonblank without outer whitespace", ErrInvalidPackageInput)
	}
	if len(input.TicketDesignBriefs) == 0 {
		return validatedInput{}, fmt.Errorf("%w: at least one Ticket Design Brief is required", ErrInvalidPackageInput)
	}
	specIdentity, diagnostics := speccompiler.ParseFilename(input.ExecutionSpec.DisplayName)
	if len(diagnostics) != 0 || specIdentity.Kind != speccompiler.ArtifactExecutionSpec || specIdentity.HasPassQualifier {
		return validatedInput{}, fmt.Errorf("%w: Execution Spec filename must be one unqualified canonical basename", ErrInvalidPackageInput)
	}
	if err := validateArtifactHash(input.ExecutionSpec); err != nil {
		return validatedInput{}, err
	}
	compiled, document := speccompiler.CompileExecutionSpec(input.ExecutionSpec.DisplayName, input.ExecutionSpec.Bytes)
	if len(compiled.Errors) != 0 || document == nil || compiled.Markdown == nil {
		return validatedInput{}, fmt.Errorf("%w: Execution Spec compiler rejected exact bytes: %v", ErrInvalidPackageInput, compiled.Errors)
	}
	if document.FeatureSlug != specIdentity.FeatureSlug || document.RepoTarget == "" || document.Branch == "" || document.BaseCommit == "" {
		return validatedInput{}, fmt.Errorf("%w: Execution Spec identity is incomplete or disagrees with its filename", ErrInvalidPackageInput)
	}
	briefs := make(map[string]validatedBrief, len(input.TicketDesignBriefs))
	for _, brief := range input.TicketDesignBriefs {
		identity, briefDiagnostics := speccompiler.ParseFilename(brief.DisplayName)
		if len(briefDiagnostics) != 0 || identity.Kind != speccompiler.ArtifactTicketDesignBrief {
			return validatedInput{}, fmt.Errorf("%w: invalid Ticket Design Brief filename %q", ErrInvalidPackageInput, brief.DisplayName)
		}
		if err := validateArtifactHash(brief); err != nil {
			return validatedInput{}, err
		}
		if diagnostics := planningartifacts.Validate(speccompiler.ArtifactTicketDesignBrief, brief.Bytes); len(diagnostics) != 0 {
			return validatedInput{}, fmt.Errorf("%w: Ticket Design Brief %q has invalid structure", ErrInvalidPackageInput, brief.DisplayName)
		}
		if identity.FeatureSlug != document.FeatureSlug {
			return validatedInput{}, fmt.Errorf("%w: Ticket Design Brief %q has a different feature slug", ErrInvalidPackageInput, brief.DisplayName)
		}
		key := briefKey(identity.TicketID, identity.Revision)
		if _, exists := briefs[key]; exists {
			return validatedInput{}, fmt.Errorf("%w: duplicate Ticket Design Brief %q", ErrInvalidPackageInput, brief.DisplayName)
		}
		briefs[key] = validatedBrief{input: brief, identity: identity, sha256: sha256Hex(brief.Bytes)}
	}
	return validatedInput{specIdentity: specIdentity, spec: document, rendered: []byte(*compiled.Markdown), briefs: briefs}, nil
}

func (s *Service) validateBasis(ctx context.Context, tx *workflowstore.Tx, input PrepareInput, validated validatedInput, packageRow *workflowstore.ExecutionPackage) (packageBasis, error) {
	selection, err := tx.GetDeliveryTicketSelectionBySelectionID(ctx, input.SelectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return packageBasis{}, fmt.Errorf("%w: %s", ErrSelectionNotFound, input.SelectionID)
	}
	if err != nil {
		return packageBasis{}, err
	}
	if selection.State != "active" {
		return packageBasis{}, fmt.Errorf("%w: %s is %s", ErrSelectionNotActive, input.SelectionID, selection.State)
	}
	if !selection.SourceClosureRowID.Valid {
		return packageBasis{}, fmt.Errorf("%w: selection has no source closure", ErrPackageBasisChanged)
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, selection.WorkspaceRowID)
	if err != nil {
		return packageBasis{}, err
	}
	if workspace.FeatureSlug != validated.spec.FeatureSlug || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return packageBasis{}, fmt.Errorf("%w: current workspace authority does not match the unqualified Execution Spec", ErrPackageBasisChanged)
	}
	authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil {
		return packageBasis{}, err
	}
	if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != selection.SourceClosureRowID.Int64 {
		return packageBasis{}, fmt.Errorf("%w: current authority is not bound to the selected source closure", ErrPackageBasisChanged)
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, selection.SourceClosureRowID.Int64)
	if err != nil {
		return packageBasis{}, err
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != validated.spec.BaseCommit {
		return packageBasis{}, fmt.Errorf("%w: source closure is not the exact ready Execution Spec base", ErrPackageBasisChanged)
	}
	target, err := tx.GetRepositoryTarget(ctx, validated.spec.RepoTarget)
	if err != nil {
		return packageBasis{}, err
	}
	if target.RepoTarget != validated.spec.RepoTarget || !target.ConfiguredBranchRef.Valid || target.ConfiguredBranchRef.String != "refs/heads/"+validated.spec.Branch {
		return packageBasis{}, fmt.Errorf("%w: repository target and configured branch do not match the package", ErrPackageBasisChanged)
	}
	selectionMembers, err := tx.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return packageBasis{}, err
	}
	if len(selectionMembers) != 1 {
		return packageBasis{}, fmt.Errorf("%w: selection must have exactly one member, found %d", ErrSelectionInvalid, len(selectionMembers))
	}
	members := make([]packageMemberBasis, 0, len(selectionMembers))
	for _, selectionMember := range selectionMembers {
		revision, revisionErr := tx.GetDeliveryTicketRevisionByRowID(ctx, selectionMember.RevisionRowID)
		if revisionErr != nil {
			return packageBasis{}, revisionErr
		}
		ticket, ticketErr := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if ticketErr != nil {
			return packageBasis{}, ticketErr
		}
		if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID ||
			revision.RepoTarget != validated.spec.RepoTarget || revision.Branch != validated.spec.Branch || revision.BaseCommit != validated.spec.BaseCommit ||
			revision.SourceClosureRowID != closure.ID {
			return packageBasis{}, fmt.Errorf("%w: selected ticket %s is not current on the exact package target/source", ErrPackageBasisChanged, ticket.TicketID)
		}
		approvals, approvalErr := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
		if approvalErr != nil {
			return packageBasis{}, approvalErr
		}
		var approval workflowstore.DeliveryTicketRevisionApproval
		foundApproval := false
		for _, candidate := range approvals {
			if candidate.ID == selectionMember.ApprovalRowID {
				approval = candidate
				foundApproval = true
				break
			}
		}
		if !foundApproval || approval.ApprovalKind != "delivery" || approval.ApprovalState != "approved" ||
			approval.SourceClosureRowID != closure.ID || !approval.AuthorityRevisionRowID.Valid || approval.AuthorityRevisionRowID.Int64 != authority.ID {
			return packageBasis{}, fmt.Errorf("%w: selected ticket %s approval is not current", ErrPackageBasisChanged, ticket.TicketID)
		}
		brief, ok := validated.briefs[briefKey(ticket.TicketID, revision.RevisionNumber)]
		if !ok || brief.identity.FeatureSlug != workspace.FeatureSlug {
			return packageBasis{}, fmt.Errorf("%w: selected ticket %s has no exact current Ticket Design Brief", ErrPackageBasisChanged, ticket.TicketID)
		}
		members = append(members, packageMemberBasis{selectionMember: selectionMember, revision: revision, ticket: ticket, approval: approval, brief: brief})
	}
	sourceSHA := sourceBasisSHA256(closure)
	authoritySHA, err := authorityBasisSHA256(ctx, tx, workspace, authority, closure)
	if err != nil {
		return packageBasis{}, err
	}
	designParts := []string{"design-briefs-v1"}
	for _, member := range members {
		designParts = append(designParts, strconv.FormatInt(member.selectionMember.Sequence, 10), member.ticket.TicketID, strconv.FormatInt(member.revision.RevisionNumber, 10), member.brief.input.DisplayName, member.brief.sha256)
	}
	designSHA := compoundSHA256(designParts...)
	specSHA := validated.specIdentityHash(input.ExecutionSpec)
	packageParts := []string{"execution-package-v1", input.SelectionID, strconv.FormatInt(selection.ID, 10), workspace.WorkspaceID, strconv.FormatInt(workspace.ID, 10), workspace.FeatureSlug, validated.spec.RepoTarget, validated.spec.Branch, validated.spec.BaseCommit, strconv.FormatInt(authority.ID, 10), authoritySHA, sourceSHA, designSHA, specSHA}
	for _, member := range members {
		packageParts = append(packageParts, strconv.FormatInt(member.selectionMember.Sequence, 10), strconv.FormatInt(member.selectionMember.ID, 10), strconv.FormatInt(member.revision.ID, 10), strconv.FormatInt(member.approval.ID, 10), member.brief.input.DisplayName, member.brief.sha256)
	}
	packageSHA := compoundSHA256(packageParts...)
	basis := packageBasis{selection: selection, workspace: workspace, authority: authority, closure: closure, members: members, sourceSHA256: sourceSHA, authoritySHA256: authoritySHA, designBriefSHA256: designSHA, packageSHA256: packageSHA}
	if packageRow != nil {
		if packageRow.SelectionRowID != selection.ID || packageRow.WorkspaceRowID != workspace.ID || packageRow.RepoTarget != validated.spec.RepoTarget || packageRow.Branch != validated.spec.Branch || packageRow.BaseCommit != validated.spec.BaseCommit || packageRow.SourceClosureRowID != closure.ID || packageRow.AuthorityRevisionRowID != authority.ID || packageRow.PackageSha256 != packageSHA || packageRow.AuthoritySha256 != authoritySHA || packageRow.SourceSha256 != sourceSHA || packageRow.DesignBriefSha256 != designSHA || packageRow.ExecutionSpecSha256 != specSHA {
			return packageBasis{}, fmt.Errorf("%w: immutable package identity no longer matches current ticket, authority, source, or bytes", ErrPackageBasisChanged)
		}
	}
	return basis, nil
}

func (s *Service) readPackageInput(ctx context.Context, packageRow workflowstore.ExecutionPackage) (PrepareInput, error) {
	selection, err := s.store.GetDeliveryTicketSelectionByRowID(ctx, packageRow.SelectionRowID)
	if err != nil {
		return PrepareInput{}, err
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packageRow.WorkspaceRowID)
	if err != nil {
		return PrepareInput{}, err
	}
	selectionMembers, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return PrepareInput{}, err
	}
	packageMembers, err := s.store.ListExecutionPackageMembers(ctx, packageRow.ID)
	if err != nil {
		return PrepareInput{}, err
	}
	memberHashes := make(map[int64]string, len(packageMembers))
	for _, member := range packageMembers {
		memberHashes[member.RevisionRowID] = member.MemberSha256
	}
	briefs := make([]ArtifactInput, 0, len(selectionMembers))
	for _, member := range selectionMembers {
		revision, revisionErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if revisionErr != nil {
			return PrepareInput{}, revisionErr
		}
		ticket, ticketErr := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if ticketErr != nil {
			return PrepareInput{}, ticketErr
		}
		filename := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
		bytes, readErr := s.readPackageFile(packageRow.PackageID, filename)
		if readErr != nil {
			return PrepareInput{}, readErr
		}
		expectedSHA, ok := memberHashes[member.RevisionRowID]
		if !ok {
			return PrepareInput{}, fmt.Errorf("%w: package member for revision %d is missing", ErrPackageBasisChanged, member.RevisionRowID)
		}
		briefs = append(briefs, ArtifactInput{DisplayName: filename, ExpectedSHA256: expectedSHA, Bytes: bytes})
	}
	specName := workspace.FeatureSlug + ".execution-spec.json"
	specBytes, err := s.readPackageFile(packageRow.PackageID, specName)
	if err != nil {
		return PrepareInput{}, err
	}
	return PrepareInput{SelectionID: selection.SelectionID, TicketDesignBriefs: briefs, ExecutionSpec: ArtifactInput{DisplayName: specName, ExpectedSHA256: packageRow.ExecutionSpecSha256, Bytes: specBytes}}, nil
}

func (s *Service) rereadPackageInput(packageID string, input PrepareInput) (PrepareInput, error) {
	fresh := input
	fresh.TicketDesignBriefs = make([]ArtifactInput, len(input.TicketDesignBriefs))
	for index, brief := range input.TicketDesignBriefs {
		bytes, err := s.readPackageFile(packageID, brief.DisplayName)
		if err != nil {
			return PrepareInput{}, err
		}
		fresh.TicketDesignBriefs[index] = ArtifactInput{DisplayName: brief.DisplayName, ExpectedSHA256: brief.ExpectedSHA256, Bytes: bytes}
	}
	bytes, err := s.readPackageFile(packageID, input.ExecutionSpec.DisplayName)
	if err != nil {
		return PrepareInput{}, err
	}
	fresh.ExecutionSpec = ArtifactInput{DisplayName: input.ExecutionSpec.DisplayName, ExpectedSHA256: input.ExecutionSpec.ExpectedSHA256, Bytes: bytes}
	return fresh, nil
}

func (s *Service) readPackageFile(packageID, filename string) ([]byte, error) {
	if packageID == "" || strings.TrimSpace(packageID) != packageID || filename == "" || filepath.Base(filename) != filename {
		return nil, fmt.Errorf("%w: unsafe package artifact path", ErrPackageBasisChanged)
	}
	bytes, err := os.ReadFile(filepath.Join(s.store.ArtifactStore().Root(), "packages", packageID, filename))
	if err != nil {
		return nil, fmt.Errorf("%w: package artifact %s is unavailable: %v", ErrPackageBasisChanged, filename, err)
	}
	return bytes, nil
}

func validateArtifactHash(input ArtifactInput) error {
	if !packageSHA256.MatchString(input.ExpectedSHA256) {
		return fmt.Errorf("%w: expected SHA-256 for %q must be 64 lowercase hexadecimal characters", ErrInvalidPackageInput, input.DisplayName)
	}
	if sha256Hex(input.Bytes) != input.ExpectedSHA256 {
		return fmt.Errorf("%w: exact bytes for %q do not match the expected SHA-256", ErrInvalidPackageInput, input.DisplayName)
	}
	return nil
}

func samePackageInput(left, right PrepareInput) bool {
	if left.SelectionID != right.SelectionID || left.ExecutionSpec.DisplayName != right.ExecutionSpec.DisplayName || left.ExecutionSpec.ExpectedSHA256 != right.ExecutionSpec.ExpectedSHA256 || !bytes.Equal(left.ExecutionSpec.Bytes, right.ExecutionSpec.Bytes) || len(left.TicketDesignBriefs) != len(right.TicketDesignBriefs) {
		return false
	}
	for index := range left.TicketDesignBriefs {
		first, second := left.TicketDesignBriefs[index], right.TicketDesignBriefs[index]
		if first.DisplayName != second.DisplayName || first.ExpectedSHA256 != second.ExpectedSHA256 || !bytes.Equal(first.Bytes, second.Bytes) {
			return false
		}
	}
	return true
}

func packageArtifactsFromFiles(files []workflowartifacts.File) []PackageArtifact {
	result := make([]PackageArtifact, 0, len(files))
	for _, file := range files {
		result = append(result, packageArtifactFromFile(file))
	}
	return result
}

func packageArtifactFromFile(file workflowartifacts.File) PackageArtifact {
	return PackageArtifact{DisplayName: filepath.Base(file.RelativePath), RelativePath: file.RelativePath, SHA256: file.SHA256, SizeBytes: file.SizeBytes}
}

func (v validatedInput) specIdentityHash(input ArtifactInput) string {
	return sha256Hex(input.Bytes)
}

func briefKey(ticketID string, revision int64) string {
	return ticketID + "\x00" + strconv.FormatInt(revision, 10)
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func compoundSHA256(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(strconv.FormatInt(int64(len(part)), 10)))
		_, _ = hash.Write([]byte(":"))
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte("\x00"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sourceBasisSHA256(closure workflowstore.SourceVaultClosure) string {
	return compoundSHA256("source-v1", closure.ClosureID, strconv.FormatInt(closure.ID, 10), closure.CommitOID, closure.TreeOID, strconv.FormatInt(closure.Generation, 10), closure.RefName, closure.State)
}

func authorityBasisSHA256(ctx context.Context, tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace, authority workflowstore.FeatureWorkspaceAuthorityRevision, closure workflowstore.SourceVaultClosure) (string, error) {
	layers, err := tx.ListFeatureWorkspaceAuthorityLayers(ctx, authority.ID)
	if err != nil {
		return "", err
	}
	parts := []string{"authority-v1", workspace.WorkspaceID, strconv.FormatInt(workspace.ID, 10), authority.AuthorityRevisionID, strconv.FormatInt(authority.ID, 10), strconv.FormatInt(authority.RevisionNumber, 10), strconv.FormatInt(closure.ID, 10)}
	for _, layer := range layers {
		parts = append(parts, layer.LayerKind, strconv.FormatInt(layer.Sequence, 10), nullInt64Text(layer.ArtifactRowID), nullInt64Text(layer.RetainedArtifactRowID), layer.ArtifactSha256, nullInt64Text(layer.SourceClosureRowID))
	}
	return compoundSHA256(parts...), nil
}

func nullInt64Text(value sql.NullInt64) string {
	if !value.Valid {
		return "null"
	}
	return strconv.FormatInt(value.Int64, 10)
}
