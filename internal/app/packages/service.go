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

	featureapp "relay/internal/app/features"
	workflowruns "relay/internal/app/runs/workflow"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/sourcevault"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidPackageInput      = errors.New("invalid execution package input")
	ErrSelectionNotFound        = errors.New("delivery ticket selection not found")
	ErrSelectionNotActive       = errors.New("delivery ticket selection is not active")
	ErrSelectionInvalid         = errors.New("delivery ticket selection cardinality is invalid")
	ErrPackageNotFound          = errors.New("execution package not found")
	ErrPackageAlreadyRun        = errors.New("execution package already has a Run")
	ErrPackageBasisChanged      = errors.New("execution package basis changed")
	ErrRunNotFound              = errors.New("package Run not found")
	ErrRunNotPackage            = errors.New("Run is not package-linked")
	ErrPackageApprovalMissing   = errors.New("package approval is missing")
	ErrApprovedAuthorityInvalid = errors.New("approved package authority is invalid")
)

var packageSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	store        *workflowstore.Store
	runs         *workflowruns.Service
	sourceVaults SourceVaultReader
}

// SourceVaultReader is the narrow source-vault surface required to resolve the
// exact retained Delivery Ticket source document. It is implemented by
// *sourcevault.Manager.
type SourceVaultReader interface {
	ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error)
}

type validatedInput struct {
	operations         *validatedOperations
	operationsSHA256   string
	operationsCoverage string
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
	source          selectedTicketBasis
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
	ticketSHA256       string
	dependenciesSHA256 string
	operationsSHA256   string
	operationsCoverage string
	packageSHA256      string
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return newService(store, nil)
}

// NewServiceWithSourceVaults creates a Service that can resolve the exact
// retained Delivery Ticket source document from the supplied source-vault
// manager. Preparation, approval revalidation, and approved-authority loading
// all require the source-vault reader because the selected approved Delivery
// Ticket is the sole ticket semantic authority.
func NewServiceWithSourceVaults(store *workflowstore.Store, sourceVaults SourceVaultReader) (*Service, error) {
	if sourceVaults == nil {
		return nil, fmt.Errorf("source-vault reader is required")
	}
	return newService(store, sourceVaults)
}

func newService(store *workflowstore.Store, sourceVaults SourceVaultReader) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	runs, err := workflowruns.NewService(store)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, runs: runs, sourceVaults: sourceVaults}, nil
}

// Prepare creates the immutable execution package for the selected approved
// Delivery Ticket. The selection identifies the exact approved Ticket
// revision; the server resolves its exact source-vault bytes and deterministic
// projection. The package basis binds the Ticket revision+approval, governing
// authority layers, completed dependencies, exact source closure, repository
// target/branch/base commit, and the optional Deterministic Operations. No
// Brief identity, bytes, digest, or projection participates.
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
		basis, basisErr := s.validateBasis(ctx, tx, input, validated, nil, "active")
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
				MemberSha256:         member.source.sourceSHA256,
			})
			if memberErr != nil {
				return fmt.Errorf("create execution package member: %w", memberErr)
			}
			result.Members = append(result.Members, packageMember)
		}
		result.Ticket = basis.members[0].ticket
		result.TicketRevision = basis.members[0].revision
		result.TicketDocument = PackageArtifact{
			DisplayName:  filepath.Base(basis.members[0].revision.SourcePath),
			RelativePath: basis.members[0].revision.SourcePath,
			SHA256:       basis.members[0].source.sourceSHA256,
			SizeBytes:    int64(len(basis.members[0].source.sourceBytes)),
		}
		result.TicketProjection = basis.members[0].source.projection
		if operationsFile != nil {
			artifact := packageArtifactFromFile(*operationsFile)
			result.Operations = &artifact
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
	if _, err := validateInput(prepareInput); err != nil {
		return ApproveResult{}, err
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packageRow.WorkspaceRowID)
	if err != nil {
		return ApproveResult{}, err
	}
	var (
		approvalID    = workflowstore.NewExecutionPackageApprovalID()
		approvalRowID int64
	)

	created, err := s.runs.CreatePackageRun(ctx, workflowruns.CreatePackageRunInput{
		FeatureSlug:             workspace.FeatureSlug,
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
			basis, basisErr := s.validateBasis(ctx, tx, freshInput, freshValidated, &packageRow, "active")
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
				if !ok || packageMember.Sequence != member.selectionMember.Sequence || packageMember.MemberSha256 != member.source.sourceSHA256 {
					return fmt.Errorf("%w: package member %d changed", ErrPackageBasisChanged, member.selectionMember.Sequence)
				}
				member.packageMember = packageMember
				approvalBasis := compoundSHA256(
					"approval-basis-v1", packageRow.PackageSha256, member.approval.ApprovalID,
					strconv.FormatInt(member.packageMember.ID, 10), member.source.sourceSHA256,
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
	selection, err := s.store.GetDeliveryTicketSelectionByRowID(ctx, packageRow.SelectionRowID)
	if err != nil {
		return Detail{}, err
	}
	selectionMembers, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil || len(selectionMembers) != 1 {
		if err != nil {
			return Detail{}, err
		}
		return Detail{}, fmt.Errorf("%w: package selection member cardinality is invalid", ErrPackageBasisChanged)
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, selectionMembers[0].RevisionRowID)
	if err != nil {
		return Detail{}, err
	}
	ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
	if err != nil {
		return Detail{}, err
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, packageRow.WorkspaceRowID)
	if err != nil {
		return Detail{}, err
	}
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, packageRow.SourceClosureRowID)
	if err != nil {
		return Detail{}, err
	}
	var ticketDocument PackageArtifact
	for _, member := range members {
		memberRevision, revisionErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, member.RevisionRowID)
		if revisionErr != nil {
			return Detail{}, revisionErr
		}
		source, sourceErr := s.readSelectedTicketDocument(ctx, closure, memberRevision.SourcePath)
		if sourceErr != nil {
			return Detail{}, sourceErr
		}
		if source.sha256 != member.MemberSha256 {
			return Detail{}, fmt.Errorf("%w: selected Ticket source no longer matches its package member", ErrPackageBasisChanged)
		}
		ticketDocument = PackageArtifact{DisplayName: filepath.Base(memberRevision.SourcePath), RelativePath: memberRevision.SourcePath, SHA256: source.sha256, SizeBytes: int64(len(source.bytes))}
	}
	var operationsArtifact *PackageArtifact
	if packageRow.DeterministicOperationsSha256.Valid {
		operationsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
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
		Ticket:                  ticket,
		TicketRevision:          revision,
		TicketDocument:          ticketDocument,
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
	validated := validatedInput{}
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

func (s *Service) validateBasis(ctx context.Context, tx *workflowstore.Tx, input PrepareInput, validated validatedInput, packageRow *workflowstore.ExecutionPackage, expectedSelectionState string) (packageBasis, error) {
	selection, err := tx.GetDeliveryTicketSelectionBySelectionID(ctx, input.SelectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return packageBasis{}, fmt.Errorf("%w: %s", ErrSelectionNotFound, input.SelectionID)
	}
	if err != nil {
		return packageBasis{}, err
	}
	if selection.State != expectedSelectionState {
		if expectedSelectionState == "active" {
			return packageBasis{}, fmt.Errorf("%w: %s is %s", ErrSelectionNotActive, input.SelectionID, selection.State)
		}
		return packageBasis{}, fmt.Errorf("%w: selection %s must be %s, found %s", ErrApprovedAuthorityInvalid, input.SelectionID, expectedSelectionState, selection.State)
	}
	if !selection.SourceClosureRowID.Valid {
		return packageBasis{}, fmt.Errorf("%w: selection has no source closure", ErrPackageBasisChanged)
	}
	workspace, err := tx.GetFeatureWorkspaceByRowID(ctx, selection.WorkspaceRowID)
	if err != nil {
		return packageBasis{}, err
	}
	currentness, currentnessErr := featureapp.EvaluateCurrentness(ctx, tx, workspace.WorkspaceID)
	if currentnessErr != nil {
		return packageBasis{}, currentnessErr
	}
	if currentness.Readiness != featureapp.FeatureCurrent || currentness.WorkspaceVersion != workspace.Version ||
		!currentness.AuthorityRevisionRowID.Valid || !workspace.CurrentAuthorityRevisionRowID.Valid ||
		currentness.AuthorityRevisionRowID.Int64 != workspace.CurrentAuthorityRevisionRowID.Int64 {
		return packageBasis{}, fmt.Errorf("%w: Feature currentness is not current for package progression", ErrPackageBasisChanged)
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
	source, err := s.resolveSelectedTicketBasis(ctx, tx, selection, workspace, selectionMember)
	if err != nil {
		return packageBasis{}, err
	}
	if source.approval.ApprovalKind != "delivery" || source.approval.ApprovalState != "approved" ||
		source.approval.SourceClosureRowID != source.closure.ID || !source.approval.AuthorityRevisionRowID.Valid || source.approval.AuthorityRevisionRowID.Int64 != authority.ID {
		return packageBasis{}, fmt.Errorf("%w: selected Ticket approval is not current", ErrPackageBasisChanged)
	}
	revision := source.revision
	closure := source.closure
	target, err := tx.GetRepositoryTarget(ctx, revision.RepoTarget)
	if err != nil {
		return packageBasis{}, err
	}
	if target.RepoTarget != revision.RepoTarget || !target.ConfiguredBranchRef.Valid || target.ConfiguredBranchRef.String != "refs/heads/"+revision.Branch {
		return packageBasis{}, fmt.Errorf("%w: repository target and configured branch do not match the selected Ticket", ErrPackageBasisChanged)
	}
	if validated.operations != nil {
		if validated.operations.identity.FeatureSlug != workspace.FeatureSlug || validated.operations.identity.TicketID != source.ticket.TicketID || validated.operations.identity.Revision != revision.RevisionNumber ||
			validated.operations.document.RepoTarget != revision.RepoTarget || validated.operations.document.Branch != revision.Branch || validated.operations.document.BaseCommit != revision.BaseCommit {
			return packageBasis{}, fmt.Errorf("%w: Deterministic Operations does not match the selected Ticket basis", ErrPackageBasisChanged)
		}
	}
	dependencies, err := tx.ListDeliveryTicketRevisionDependencies(ctx, revision.ID)
	if err != nil {
		return packageBasis{}, err
	}
	dependenciesSHA, err := dependenciesBasisSHA256(ctx, tx, revision.ID, dependencies)
	if err != nil {
		return packageBasis{}, err
	}
	members := []packageMemberBasis{{selectionMember: selectionMember, revision: revision, ticket: source.ticket, approval: source.approval, source: source}}
	sourceSHA := sourceBasisSHA256(closure)
	authoritySHA, err := authorityBasisSHA256(ctx, tx, workspace, authority, closure)
	if err != nil {
		return packageBasis{}, err
	}
	operationsSHA, operationsCoverage := "", ""
	if validated.operations != nil {
		operationsSHA, operationsCoverage = validated.operations.sha256, validated.operations.document.Coverage
	}
	packageParts := []string{"selected-package-v4", input.SelectionID, strconv.FormatInt(selection.ID, 10), strconv.FormatInt(selectionMember.ID, 10), strconv.FormatInt(revision.ID, 10), strconv.FormatInt(source.approval.ID, 10), workspace.WorkspaceID, strconv.FormatInt(workspace.ID, 10), workspace.FeatureSlug, revision.RepoTarget, revision.Branch, revision.BaseCommit, strconv.FormatInt(authority.ID, 10), authoritySHA, sourceSHA, source.sourceSHA256, dependenciesSHA}
	packageParts = append(packageParts, selectedPackageOperationsDigestParts(validated.operations)...)
	packageSHA := compoundSHA256(packageParts...)
	basis := packageBasis{selection: selection, workspace: workspace, authority: authority, closure: closure, members: members, sourceSHA256: sourceSHA, authoritySHA256: authoritySHA, ticketSHA256: source.sourceSHA256, dependenciesSHA256: dependenciesSHA, operationsSHA256: operationsSHA, operationsCoverage: operationsCoverage, packageSHA256: packageSHA}
	if packageRow != nil && (packageRow.SelectionRowID != selection.ID || packageRow.WorkspaceRowID != workspace.ID || packageRow.RepoTarget != revision.RepoTarget || packageRow.Branch != revision.Branch || packageRow.BaseCommit != revision.BaseCommit || packageRow.SourceClosureRowID != closure.ID || packageRow.AuthorityRevisionRowID != authority.ID || packageRow.PackageSha256 != packageSHA || packageRow.AuthoritySha256 != authoritySHA || packageRow.SourceSha256 != sourceSHA || nullStringValue(packageRow.DeterministicOperationsSha256) != nullableValue(operationsSHA) || nullStringValue(packageRow.DeterministicOperationsCoverage) != nullableValue(operationsCoverage)) {
		return packageBasis{}, fmt.Errorf("%w: immutable package identity no longer matches current Ticket, authority, source, or bytes", ErrPackageBasisChanged)
	}
	return basis, nil
}

// readPackageInput reconstructs the PrepareInput for an existing immutable
// package. The selected Ticket source bytes are not read here: they are
// resolved from the source vault during basis validation. Only the package's
// staged Deterministic Operations artifact bytes are read from the managed
// artifact store.
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
	input := PrepareInput{SelectionID: selection.SelectionID}
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
	if left.SelectionID != right.SelectionID {
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

func selectedPackageOperationsDigestParts(operations *validatedOperations) []string {
	if operations == nil {
		return []string{"operations absent"}
	}
	return []string{"operations present", operations.input.DisplayName, operations.sha256, operations.document.Coverage}
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
