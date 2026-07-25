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
	ErrInvalidPackageInput = errors.New("invalid execution package input")
	ErrSelectionNotFound   = errors.New("delivery ticket selection not found")
	ErrSelectionNotActive  = errors.New("delivery ticket selection is not active")
	ErrSelectionInvalid    = errors.New("delivery ticket selection cardinality is invalid")
	ErrPackageNotFound     = errors.New("execution package not found")
	ErrPackageAlreadyRun   = errors.New("execution package already has a Run")
	ErrPackageBasisChanged = errors.New("execution package basis changed")
)

var packageSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	store *workflowstore.Store
	runs  *workflowruns.Service
}

type validatedInput struct {
	brief              validatedBrief
	operations         *validatedOperations
	operationsSHA256   string
	operationsCoverage string
}

type validatedBrief struct {
	input    ArtifactInput
	identity speccompiler.FilenameInfo
	sha256   string
}

type validatedOperations struct {
	input    ArtifactInput
	identity speccompiler.FilenameInfo
	document *speccompiler.DeterministicOperationsDocument
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

	sourceSHA256       string
	authoritySHA256    string
	designBriefSHA256  string
	operationsSHA256   string
	operationsCoverage string
	packageSHA256      string
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

	briefFile, err := batch.Stage("ticket_design_brief", input.TicketDesignBrief.DisplayName, "text/markdown", input.TicketDesignBrief.Bytes)
	if err != nil {
		return PrepareResult{}, err
	}
	var operationsFile *workflowartifacts.File
	if input.DeterministicOperations != nil {
		file, stageErr := batch.Stage("deterministic_operations", input.DeterministicOperations.DisplayName, "application/json", input.DeterministicOperations.Bytes)
		if stageErr != nil {
			return PrepareResult{}, stageErr
		}
		operationsFile = &file
	}

	result := PrepareResult{}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		basis, basisErr := s.validateBasis(ctx, tx, input, validated, nil)
		if basisErr != nil {
			return basisErr
		}
		packageRow, createErr := tx.CreateExecutionPackage(ctx, workflowstore.CreateExecutionPackageParams{
			PackageID:                       packageID,
			SelectionRowID:                  basis.selection.ID,
			WorkspaceRowID:                  basis.workspace.ID,
			RepoTarget:                      basis.members[0].revision.RepoTarget,
			Branch:                          basis.members[0].revision.Branch,
			BaseCommit:                      basis.members[0].revision.BaseCommit,
			SourceClosureRowID:              basis.closure.ID,
			AuthorityRevisionRowID:          basis.authority.ID,
			PackageSha256:                   basis.packageSHA256,
			AuthoritySha256:                 basis.authoritySHA256,
			SourceSha256:                    basis.sourceSHA256,
			DesignBriefSha256:               basis.designBriefSHA256,
			DeterministicOperationsSha256:   nullableString(validated.operationsSHA256),
			DeterministicOperationsCoverage: nullableString(validated.operationsCoverage),
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
		result.TicketDesignBrief = packageArtifactFromFile(briefFile)
		if operationsFile != nil {
			artifact := packageArtifactFromFile(*operationsFile)
			result.DeterministicOperations = &artifact
		}
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
		approvalID    = workflowstore.NewExecutionPackageApprovalID()
		approvalRowID int64
	)

	created, err := s.runs.CreatePackageRun(ctx, workflowruns.CreatePackageRunInput{
		FeatureSlug:             validated.brief.identity.FeatureSlug,
		RepoTarget:              packageRow.RepoTarget,
		Branch:                  packageRow.Branch,
		BaseCommit:              packageRow.BaseCommit,
		ExecutionPackageRowID:   packageRow.ID,
		PackageApprovalRowIDRef: &approvalRowID,
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
	var briefArtifact PackageArtifact
	var selectedTicket workflowstore.DeliveryTicket
	var selectedRevision workflowstore.DeliveryTicketRevision
	for _, member := range members {
		revision, revisionErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if revisionErr != nil {
			return Detail{}, revisionErr
		}
		memberTicket, ticketErr := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if ticketErr != nil {
			return Detail{}, ticketErr
		}
		selectedTicket, selectedRevision = memberTicket, revision
		filename := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, memberTicket.TicketID, revision.RevisionNumber)
		bytes, readErr := s.readPackageFile(packageRow.PackageID, filename)
		if readErr != nil {
			return Detail{}, readErr
		}
		sha := sha256Hex(bytes)
		if sha != member.MemberSha256 {
			return Detail{}, fmt.Errorf("%w: design brief %s no longer matches its package member", ErrPackageBasisChanged, filename)
		}
		briefArtifact = PackageArtifact{DisplayName: filename, RelativePath: filepath.ToSlash(filepath.Join("packages", packageRow.PackageID, filename)), SHA256: sha, SizeBytes: int64(len(bytes))}
	}
	var operationsArtifact *PackageArtifact
	if packageRow.DeterministicOperationsSha256.Valid {
		operationsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", workspace.FeatureSlug, selectedTicket.TicketID, selectedRevision.RevisionNumber)
		operationsBytes, readErr := s.readPackageFile(packageRow.PackageID, operationsName)
		if readErr != nil {
			return Detail{}, readErr
		}
		operationsSHA := sha256Hex(operationsBytes)
		if operationsSHA != packageRow.DeterministicOperationsSha256.String {
			return Detail{}, fmt.Errorf("%w: deterministic operations no longer matches its package basis", ErrPackageBasisChanged)
		}
		artifact := PackageArtifact{DisplayName: operationsName, RelativePath: filepath.ToSlash(filepath.Join("packages", packageRow.PackageID, operationsName)), SHA256: operationsSHA, SizeBytes: int64(len(operationsBytes))}
		operationsArtifact = &artifact
	}
	detail := Detail{
		Package:                 packageRow,
		Members:                 members,
		ApprovalBindings:        bindings,
		TicketDesignBrief:       briefArtifact,
		DeterministicOperations: operationsArtifact,
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
	identity, diagnostics := speccompiler.ParseFilename(input.TicketDesignBrief.DisplayName)
	if len(diagnostics) != 0 || identity.Kind != speccompiler.ArtifactTicketDesignBrief {
		return validatedInput{}, fmt.Errorf("%w: invalid Ticket Design Brief filename %q", ErrInvalidPackageInput, input.TicketDesignBrief.DisplayName)
	}
	if err := validateArtifactHash(input.TicketDesignBrief); err != nil {
		return validatedInput{}, err
	}
	if diagnostics := planningartifacts.Validate(speccompiler.ArtifactTicketDesignBrief, input.TicketDesignBrief.Bytes); len(diagnostics) != 0 {
		return validatedInput{}, fmt.Errorf("%w: Ticket Design Brief %q has invalid structure", ErrInvalidPackageInput, input.TicketDesignBrief.DisplayName)
	}
	validated := validatedInput{brief: validatedBrief{input: input.TicketDesignBrief, identity: identity, sha256: sha256Hex(input.TicketDesignBrief.Bytes)}}
	if input.DeterministicOperations != nil {
		operationsIdentity, operationsDiagnostics := speccompiler.ParseFilename(input.DeterministicOperations.DisplayName)
		if len(operationsDiagnostics) != 0 || operationsIdentity.Kind != speccompiler.ArtifactDeterministicOperations {
			return validatedInput{}, fmt.Errorf("%w: invalid Deterministic Operations filename %q", ErrInvalidPackageInput, input.DeterministicOperations.DisplayName)
		}
		if err := validateArtifactHash(*input.DeterministicOperations); err != nil {
			return validatedInput{}, err
		}
		compiled, document := speccompiler.CompileDeterministicOperations(input.DeterministicOperations.DisplayName, input.DeterministicOperations.Bytes)
		if len(compiled.Errors) != 0 || document == nil {
			return validatedInput{}, fmt.Errorf("%w: Deterministic Operations compiler rejected exact bytes: %v", ErrInvalidPackageInput, compiled.Errors)
		}
		validated.operations = &validatedOperations{input: *input.DeterministicOperations, identity: operationsIdentity, document: document, sha256: sha256Hex(input.DeterministicOperations.Bytes)}
		validated.operationsSHA256 = validated.operations.sha256
		validated.operationsCoverage = document.Coverage
	}
	return validated, nil
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
	if workspace.FeatureSlug != validated.brief.identity.FeatureSlug || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return packageBasis{}, fmt.Errorf("%w: current workspace authority does not match the Ticket Design Brief", ErrPackageBasisChanged)
	}
	authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil {
		return packageBasis{}, err
	}
	if authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != selection.SourceClosureRowID.Int64 {
		return packageBasis{}, fmt.Errorf("%w: current authority is not bound to the selected source closure", ErrPackageBasisChanged)
	}
	selectionMembers, err := tx.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil {
		return packageBasis{}, err
	}
	if len(selectionMembers) != 1 {
		return packageBasis{}, fmt.Errorf("%w: selection must have exactly one member, found %d", ErrSelectionInvalid, len(selectionMembers))
	}
	selectionMember := selectionMembers[0]
	revision, err := tx.GetDeliveryTicketRevisionByRowID(ctx, selectionMember.RevisionRowID)
	if err != nil {
		return packageBasis{}, err
	}
	ticket, err := tx.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return packageBasis{}, err
	}
	if validated.brief.identity.FeatureSlug != workspace.FeatureSlug || validated.brief.identity.TicketID != ticket.TicketID || validated.brief.identity.Revision != revision.RevisionNumber {
		return packageBasis{}, fmt.Errorf("%w: Ticket Design Brief does not match selected Ticket revision", ErrPackageBasisChanged)
	}
	if !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != revision.ID || revision.SourceClosureRowID != selection.SourceClosureRowID.Int64 {
		return packageBasis{}, fmt.Errorf("%w: selected Ticket is not current on the exact package source", ErrPackageBasisChanged)
	}
	closure, err := tx.GetSourceVaultClosureByRowID(ctx, selection.SourceClosureRowID.Int64)
	if err != nil {
		return packageBasis{}, err
	}
	if closure.State != workflowstore.SourceVaultClosureStateReady || closure.CommitOID != revision.BaseCommit {
		return packageBasis{}, fmt.Errorf("%w: source closure is not the exact ready Ticket base", ErrPackageBasisChanged)
	}
	target, err := tx.GetRepositoryTarget(ctx, revision.RepoTarget)
	if err != nil {
		return packageBasis{}, err
	}
	if target.RepoTarget != revision.RepoTarget || !target.ConfiguredBranchRef.Valid || target.ConfiguredBranchRef.String != "refs/heads/"+revision.Branch {
		return packageBasis{}, fmt.Errorf("%w: repository target and configured branch do not match the selected Ticket", ErrPackageBasisChanged)
	}
	if validated.operations != nil {
		if validated.operations.identity.FeatureSlug != workspace.FeatureSlug || validated.operations.identity.TicketID != ticket.TicketID || validated.operations.identity.Revision != revision.RevisionNumber ||
			validated.operations.document.RepoTarget != revision.RepoTarget || validated.operations.document.Branch != revision.Branch || validated.operations.document.BaseCommit != revision.BaseCommit {
			return packageBasis{}, fmt.Errorf("%w: Deterministic Operations does not match the selected Ticket basis", ErrPackageBasisChanged)
		}
	}
	approvals, err := tx.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return packageBasis{}, err
	}
	var approval workflowstore.DeliveryTicketRevisionApproval
	foundApproval := false
	for _, candidate := range approvals {
		if candidate.ID == selectionMember.ApprovalRowID {
			approval, foundApproval = candidate, true
			break
		}
	}
	if !foundApproval || approval.ApprovalKind != "delivery" || approval.ApprovalState != "approved" ||
		approval.SourceClosureRowID != closure.ID || !approval.AuthorityRevisionRowID.Valid || approval.AuthorityRevisionRowID.Int64 != authority.ID {
		return packageBasis{}, fmt.Errorf("%w: selected Ticket approval is not current", ErrPackageBasisChanged)
	}
	members := []packageMemberBasis{{selectionMember: selectionMember, revision: revision, ticket: ticket, approval: approval, brief: validated.brief}}
	sourceSHA := sourceBasisSHA256(closure)
	authoritySHA, err := authorityBasisSHA256(ctx, tx, workspace, authority, closure)
	if err != nil {
		return packageBasis{}, err
	}
	designSHA := compoundSHA256("ticket-design-brief-v2", strconv.FormatInt(selectionMember.Sequence, 10), ticket.TicketID, strconv.FormatInt(revision.RevisionNumber, 10), validated.brief.input.DisplayName, validated.brief.sha256)
	operationsSHA, operationsCoverage, operationsMarker := "", "", "operations absent"
	if validated.operations != nil {
		operationsSHA, operationsCoverage, operationsMarker = validated.operations.sha256, validated.operations.document.Coverage, "operations present"
	}
	packageParts := []string{"selected-package-v2", input.SelectionID, strconv.FormatInt(selection.ID, 10), strconv.FormatInt(selectionMember.ID, 10), strconv.FormatInt(revision.ID, 10), strconv.FormatInt(approval.ID, 10), workspace.WorkspaceID, strconv.FormatInt(workspace.ID, 10), workspace.FeatureSlug, revision.RepoTarget, revision.Branch, revision.BaseCommit, strconv.FormatInt(authority.ID, 10), authoritySHA, sourceSHA, designSHA, validated.brief.input.DisplayName, validated.brief.sha256, operationsMarker, operationsSHA, operationsCoverage}
	packageSHA := compoundSHA256(packageParts...)
	basis := packageBasis{selection: selection, workspace: workspace, authority: authority, closure: closure, members: members, sourceSHA256: sourceSHA, authoritySHA256: authoritySHA, designBriefSHA256: designSHA, operationsSHA256: operationsSHA, operationsCoverage: operationsCoverage, packageSHA256: packageSHA}
	if packageRow != nil && (packageRow.SelectionRowID != selection.ID || packageRow.WorkspaceRowID != workspace.ID || packageRow.RepoTarget != revision.RepoTarget || packageRow.Branch != revision.Branch || packageRow.BaseCommit != revision.BaseCommit || packageRow.SourceClosureRowID != closure.ID || packageRow.AuthorityRevisionRowID != authority.ID || packageRow.PackageSha256 != packageSHA || packageRow.AuthoritySha256 != authoritySHA || packageRow.SourceSha256 != sourceSHA || packageRow.DesignBriefSha256 != designSHA || nullStringValue(packageRow.DeterministicOperationsSha256) != nullableValue(operationsSHA) || nullStringValue(packageRow.DeterministicOperationsCoverage) != nullableValue(operationsCoverage)) {
		return packageBasis{}, fmt.Errorf("%w: immutable package identity no longer matches current Ticket, authority, source, or bytes", ErrPackageBasisChanged)
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
	if len(selectionMembers) != 1 {
		return PrepareInput{}, fmt.Errorf("%w: package selection must have exactly one member", ErrPackageBasisChanged)
	}
	member := selectionMembers[0]
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
	if err != nil {
		return PrepareInput{}, err
	}
	ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return PrepareInput{}, err
	}
	filename := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	briefBytes, err := s.readPackageFile(packageRow.PackageID, filename)
	if err != nil {
		return PrepareInput{}, err
	}
	expectedSHA, ok := memberHashes[member.RevisionRowID]
	if !ok {
		return PrepareInput{}, fmt.Errorf("%w: package member for revision %d is missing", ErrPackageBasisChanged, member.RevisionRowID)
	}
	input := PrepareInput{SelectionID: selection.SelectionID, TicketDesignBrief: ArtifactInput{DisplayName: filename, ExpectedSHA256: expectedSHA, Bytes: briefBytes}}
	if packageRow.DeterministicOperationsSha256.Valid {
		operationsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
		operationsBytes, readErr := s.readPackageFile(packageRow.PackageID, operationsName)
		if readErr != nil {
			return PrepareInput{}, readErr
		}
		input.DeterministicOperations = &ArtifactInput{DisplayName: operationsName, ExpectedSHA256: packageRow.DeterministicOperationsSha256.String, Bytes: operationsBytes}
	}
	return input, nil
}

func (s *Service) rereadPackageInput(packageID string, input PrepareInput) (PrepareInput, error) {
	fresh := input
	bytes, err := s.readPackageFile(packageID, input.TicketDesignBrief.DisplayName)
	if err != nil {
		return PrepareInput{}, err
	}
	fresh.TicketDesignBrief = ArtifactInput{DisplayName: input.TicketDesignBrief.DisplayName, ExpectedSHA256: input.TicketDesignBrief.ExpectedSHA256, Bytes: bytes}
	if input.DeterministicOperations != nil {
		bytes, err := s.readPackageFile(packageID, input.DeterministicOperations.DisplayName)
		if err != nil {
			return PrepareInput{}, err
		}
		operations := ArtifactInput{DisplayName: input.DeterministicOperations.DisplayName, ExpectedSHA256: input.DeterministicOperations.ExpectedSHA256, Bytes: bytes}
		fresh.DeterministicOperations = &operations
	}
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
	if left.SelectionID != right.SelectionID || left.TicketDesignBrief.DisplayName != right.TicketDesignBrief.DisplayName || left.TicketDesignBrief.ExpectedSHA256 != right.TicketDesignBrief.ExpectedSHA256 || !bytes.Equal(left.TicketDesignBrief.Bytes, right.TicketDesignBrief.Bytes) {
		return false
	}
	if (left.DeterministicOperations == nil) != (right.DeterministicOperations == nil) {
		return false
	}
	if left.DeterministicOperations != nil {
		first, second := *left.DeterministicOperations, *right.DeterministicOperations
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

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableValue(value string) string {
	return value
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
