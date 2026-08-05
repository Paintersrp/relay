package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	featureapp "relay/internal/app/features"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

const lifecycleObjectLimit int64 = 64 << 20

type lifecycleRequest struct {
	surface            registry.SurfaceContractID
	operationID        registry.OperationID
	projectID          string
	inputs             []semanticidentity.InputBinding
	workflowReferences []semanticidentity.WorkflowReferenceRequest
	attestations       []semanticidentity.AttestationRequest
	primaryRevisions   []semanticidentity.PrimaryRevisionRequest
	comparisonAnchors  []semanticidentity.ComparisonAnchorRequest
	relaySpecsRevision string
	declaredFiles      []semanticidentity.DeclaredFile
	inputFileCount     int
	requestIdentity    semanticidentity.RequestIdentity
	prior              *PacketView
}

type repositoryPreparation struct {
	bindings        []packet.RepositoryBinding
	primary         map[string]sourcevault.ImportResult
	primaryRevision map[string]workflowrepos.ResolvedRevision
	anchors         map[string]map[string]sourcevault.ImportResult
	anchorRevision  map[string]map[string]workflowrepos.ResolvedRevision
	direct          map[string]sourcevault.ImportResult
	directRevision  map[string]workflowrepos.ResolvedRevision
	vaultEdges      []PublicationVaultInput
}

type workflowPreparation struct {
	references []packet.WorkflowReference
}

type derivedPreparation struct {
	inputs           []packet.InputBinding
	currentInputName string
	currentBytes     []byte
}

type featureWorkspaceRouteInput struct {
	WorkspaceID           string `json:"workspace_id"`
	FeatureSlug           string `json:"feature_slug"`
	WorkspaceVersion      int64  `json:"workspace_version"`
	WorkspaceState        string `json:"workspace_state"`
	RouteStateID          string `json:"route_state_id"`
	RouteSequence         int64  `json:"route_sequence"`
	RouteWorkspaceVersion int64  `json:"route_workspace_version"`
	RouteState            string `json:"route_state"`
}

type transitionApplicabilityInput struct {
	WorkspaceID             string `json:"workspace_id"`
	FeatureSlug             string `json:"feature_slug"`
	WorkspaceVersion        int64  `json:"workspace_version"`
	TicketID                string `json:"ticket_id"`
	RevisionID              int64  `json:"revision_id"`
	RevisionNumber          int64  `json:"revision_number"`
	SourceClosureID         string `json:"source_closure_id"`
	TransitionApplicability string `json:"transition_applicability"`
}

type selectionIdentityInput struct {
	WorkspaceID         string `json:"workspace_id"`
	FeatureSlug         string `json:"feature_slug"`
	WorkspaceVersion    int64  `json:"workspace_version"`
	SelectionID         string `json:"selection_id"`
	SelectionState      string `json:"selection_state"`
	TicketID            string `json:"ticket_id"`
	RevisionID          int64  `json:"revision_id"`
	RevisionNumber      int64  `json:"revision_number"`
	ApprovalID          string `json:"approval_id"`
	AuthorityRevisionID string `json:"authority_revision_id"`
	SourceClosureID     string `json:"source_closure_id"`
}

type remediationSeedInput struct {
	RemediationSeedID             string                      `json:"remediation_seed_id"`
	AuditDecisionID               string                      `json:"audit_decision_id"`
	AuditPacketID                 string                      `json:"audit_packet_id"`
	ApprovedExecutionPackage      remediationPackageIdentity  `json:"approved_execution_package"`
	AuditedDeliveryTicket         remediationTicketIdentity   `json:"audited_delivery_ticket"`
	AuditedDeliveryTicketRevision remediationRevisionIdentity `json:"audited_delivery_ticket_revision"`
	AuditedCommit                 string                      `json:"audited_commit"`
	DecisionRationale             string                      `json:"decision_rationale"`
	MaterialFindings              []remediationFindingInput   `json:"material_findings"`
}

type remediationPackageIdentity struct {
	PackageID     string `json:"package_id"`
	PackageSHA256 string `json:"package_sha256"`
}

type remediationTicketIdentity struct {
	TicketID string `json:"ticket_id"`
}

type remediationRevisionIdentity struct {
	RevisionID     int64 `json:"revision_id"`
	RevisionNumber int64 `json:"revision_number"`
}

type remediationFindingInput struct {
	Sequence               int64  `json:"sequence"`
	UpstreamClassification string `json:"upstream_classification"`
	Summary                string `json:"summary"`
	Evidence               string `json:"evidence"`
	RequiredRemediation    string `json:"required_remediation"`
}

type retainedAuthorityDocument struct {
	AuthorityRevisionID string                   `json:"authority_revision_id"`
	Layers              []retainedAuthorityLayer `json:"layers"`
}

type retainedAuthorityLayer struct {
	Sequence       int64  `json:"sequence"`
	LayerKind      string `json:"layer_kind"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	BytesBase64    string `json:"bytes_base64"`
}

type currentApprovedAuthorityInput struct {
	FeatureWorkspaceID         string `json:"feature_workspace_id"`
	CurrentAuthorityRevisionID string `json:"current_authority_revision_id"`
	SourceClosureID            string `json:"source_closure_id"`
	AuthorityBytes             string `json:"authority_bytes"`
	AuthorityByteDigest        string `json:"authority_byte_digest"`
	SourceClosureCommit        string `json:"source_closure_commit"`
}

type selectedRemediationTicketInput struct {
	RemediationSeedID         string                                `json:"remediation_seed_id"`
	AuditDecisionID           string                                `json:"audit_decision_id"`
	ReopeningKind             string                                `json:"reopening_kind"`
	AuditedTicketID           string                                `json:"audited_ticket_id"`
	AuditedRevisionRowID      int64                                 `json:"audited_revision_row_id"`
	AuditedRevisionNumber     int64                                 `json:"audited_revision_number"`
	RemediationTicketID       string                                `json:"remediation_ticket_id"`
	RemediationRevisionRowID  int64                                 `json:"remediation_revision_row_id"`
	RemediationRevisionNumber int64                                 `json:"remediation_revision_number"`
	ReplacementRevisionRowID  *int64                                `json:"replacement_revision_row_id,omitempty"`
	WorkspaceID               string                                `json:"workspace_id"`
	ExternalPriority          int64                                 `json:"external_priority"`
	RepoTarget                string                                `json:"repo_target"`
	Branch                    string                                `json:"branch"`
	BaseCommit                string                                `json:"base_commit"`
	SourceClosureRowID        int64                                 `json:"source_closure_row_id"`
	SourceClosureID           string                                `json:"source_closure_id"`
	SourceClosureCommit       string                                `json:"source_closure_commit"`
	SourcePath                string                                `json:"source_path"`
	Goal                      string                                `json:"goal"`
	Context                   string                                `json:"context"`
	TransitionApplicability   string                                `json:"transition_applicability"`
	CancellationReason        string                                `json:"cancellation_reason"`
	Members                   []selectedRemediationTicketMember     `json:"members"`
	Dependencies              []selectedRemediationTicketDependency `json:"dependencies"`
	Canonical                 selectedRemediationTicketArtifact     `json:"canonical_artifact"`
	Rendered                  selectedRemediationTicketArtifact     `json:"rendered_artifact"`
	Approval                  selectedRemediationTicketApproval     `json:"approval"`
	Selection                 selectedRemediationTicketSelection    `json:"selection"`
}

type selectedRemediationTicketMember struct {
	Sequence int64  `json:"sequence"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Text     string `json:"text"`
}

type selectedRemediationTicketDependency struct {
	Sequence                int64  `json:"sequence"`
	DependencyRevisionRowID int64  `json:"dependency_revision_row_id"`
	DependencyOutcome       string `json:"dependency_outcome"`
}

type selectedRemediationTicketArtifact struct {
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	BytesBase64  string `json:"bytes_base64"`
}

type selectedRemediationTicketApproval struct {
	ApprovalRowID          int64  `json:"approval_row_id"`
	ApprovalID             string `json:"approval_id"`
	AuthorityRevisionRowID int64  `json:"authority_revision_row_id"`
	SourceClosureRowID     int64  `json:"source_closure_row_id"`
}

type selectedRemediationTicketSelection struct {
	SelectionRowID      int64  `json:"selection_row_id"`
	SelectionID         string `json:"selection_id"`
	State               string `json:"state"`
	SourceClosureRowID  int64  `json:"source_closure_row_id"`
	MemberRowID         int64  `json:"member_row_id"`
	MemberSequence      int64  `json:"member_sequence"`
	MemberRevisionRowID int64  `json:"member_revision_row_id"`
	MemberApprovalRowID int64  `json:"member_approval_row_id"`
}

type completedDependencyOutcomesInput struct {
	RemediationTicketID       string                       `json:"remediation_ticket_id"`
	RemediationRevisionRowID  int64                        `json:"remediation_revision_row_id"`
	RemediationRevisionNumber int64                        `json:"remediation_revision_number"`
	Dependencies              []completedDependencyOutcome `json:"dependencies"`
}

type completedDependencyOutcome struct {
	Sequence                  int64                         `json:"sequence"`
	DependencyTicketID        string                        `json:"dependency_ticket_id"`
	DependencyRevisionRowID   int64                         `json:"dependency_revision_row_id"`
	DependencyRevisionNumber  int64                         `json:"dependency_revision_number"`
	DeclaredOutcome           string                        `json:"declared_outcome"`
	CurrentDependencyRevision completedDependencyRevision   `json:"current_dependency_revision"`
	Completion                completedDependencyCompletion `json:"completion"`
}

type completedDependencyRevision struct {
	TicketID       string `json:"ticket_id"`
	RevisionRowID  int64  `json:"revision_row_id"`
	RevisionNumber int64  `json:"revision_number"`
}

type completedDependencyCompletion struct {
	SatisfactionRowID                int64 `json:"satisfaction_row_id"`
	AuditTicketRevisionDecisionRowID int64 `json:"audit_ticket_revision_decision_row_id"`
}

type retainedBuilder struct {
	ids       IDGenerator
	artifacts []PublicationArtifactInput
	bindings  []PublicationBindingInput
	sequence  int64
}

func (s *LifecycleService) prepareCreate(ctx context.Context, input CreateLifecycleInput, fingerprint semanticidentity.Fingerprint) (preparedPacketAuthority, error) {
	request := lifecycleRequest{
		surface:            input.Identity.SurfaceContract,
		operationID:        input.Identity.OperationID,
		projectID:          input.Identity.ProjectID,
		inputs:             input.Identity.Inputs,
		workflowReferences: input.Identity.WorkflowReferences,
		attestations:       input.Identity.Attestations,
		primaryRevisions:   input.Identity.PrimaryRevisions,
		comparisonAnchors:  input.Identity.ComparisonAnchors,
		relaySpecsRevision: input.Identity.RelaySpecsRevision,
		declaredFiles:      input.Identity.DeclaredFiles,
		inputFileCount:     input.Identity.InputFileCount,
		requestIdentity:    input.Identity,
	}
	return s.preparePacket(ctx, request, input.Files, fingerprint)
}

func (s *LifecycleService) prepareRefresh(ctx context.Context, input RefreshLifecycleInput, fingerprint semanticidentity.Fingerprint, prior PacketView) (preparedPacketAuthority, error) {
	request := lifecycleRequest{
		surface:            input.Identity.SurfaceContract,
		operationID:        prior.Summary.OperationID,
		projectID:          prior.Summary.ProjectID,
		inputs:             input.Identity.Inputs,
		workflowReferences: input.Identity.WorkflowReferences,
		attestations:       input.Identity.Attestations,
		primaryRevisions:   input.Identity.PrimaryRevisions,
		comparisonAnchors:  input.Identity.ComparisonAnchors,
		relaySpecsRevision: input.Identity.RelaySpecsRevision,
		declaredFiles:      input.Identity.DeclaredFiles,
		inputFileCount:     input.Identity.InputFileCount,
		requestIdentity:    input.Identity,
		prior:              &prior,
	}
	return s.preparePacket(ctx, request, input.Files, fingerprint)
}

func (s *LifecycleService) preparePacket(ctx context.Context, request lifecycleRequest, files []fileacquisition.FileParameter, fingerprint semanticidentity.Fingerprint) (preparedPacketAuthority, error) {
	operation, ok := registry.Lookup(request.operationID)
	if !ok || operation.SurfaceContract != request.surface {
		return preparedPacketAuthority{}, &Error{Code: CodePacketRouteMismatch}
	}
	project, err := s.store.GetProjectByProjectID(ctx, request.projectID)
	if err != nil || project.Status != workflowstore.ProjectStatusActive {
		return preparedPacketAuthority{}, &Error{Code: CodeInvalidPacketDocument}
	}
	packetID := s.ids.PacketID()
	packetArtifactID := s.ids.ArtifactID()

	workflow, err := s.prepareWorkflowReferences(ctx, project, request.workflowReferences)
	if err != nil {
		return preparedPacketAuthority{}, err
	}
	repositories, err := s.prepareRepositories(ctx, project, operation, request)
	if err != nil {
		return preparedPacketAuthority{}, err
	}
	governance, manifest, governanceEdges, governanceRevision, err := s.prepareGovernance(ctx, packetID, operation, request, repositories)
	if err != nil {
		return preparedPacketAuthority{}, err
	}

	acquired, err := s.acquireFiles(ctx, request, files)
	if err != nil {
		return preparedPacketAuthority{}, err
	}
	defer acquired.Release()

	builder := retainedBuilder{ids: s.ids}
	inputs, inputEdges, err := s.materializeInputs(ctx, operation, request, acquired, workflow, repositories, &builder)
	if err != nil {
		return preparedPacketAuthority{}, err
	}
	derived, err := s.materializeDerivedInputs(ctx, operation, workflow, &builder)
	if err != nil {
		return preparedPacketAuthority{}, err
	}
	inputs = append(inputs, derived.inputs...)
	attestations := materializeAttestations(request.attestations)
	if err := s.revalidateRepositoryAuthority(ctx, operation, repositories, governanceRevision); err != nil {
		return preparedPacketAuthority{}, err
	}
	if err := s.revalidateCurrentPlannerDerivedInput(ctx, operation, workflow, derived); err != nil {
		return preparedPacketAuthority{}, err
	}

	document := packet.Document{
		SchemaVersion:         packet.SchemaVersion,
		CreatedAt:             canonicalTime(s.clock.Now()),
		Role:                  operation.Role,
		OperationID:           operation.OperationID,
		SurfaceContract:       operation.SurfaceContract,
		SurfaceManifestSHA256: mustSurfaceManifest(operation.SurfaceContract),
		Output:                packet.OutputContract{OutputKind: operation.OutputKind, OutputPersistence: operation.OutputPersistence},
		Project:               packet.ProjectBinding{ProjectID: project.ProjectID},
		WorkflowReferences:    workflow.references,
		Attestations:          attestations,
		Inputs:                inputs,
		Repositories:          repositories.bindings,
		RelaySpecs:            governance,
		ManifestDomain:        manifest,
		SourcePolicy:          operation.SourcePolicy,
		HistoricalAuthority:   operation.HistoricalAuthority,
		AllowedActions:        append([]registry.AllowedAction(nil), operation.AllowedNonSourceActions...),
		ReadinessState:        packet.ReadinessReady,
	}
	if request.prior != nil {
		document.PriorPacket = &packet.PriorPacketIdentity{PacketID: request.prior.Summary.PacketID, PacketSHA256: request.prior.Summary.PacketSHA256}
	}
	snapshot, err := packet.NewSnapshot(document)
	if err != nil {
		return preparedPacketAuthority{}, &Error{Code: CodeInvalidPacketDocument}
	}

	vaultEdges := append([]PublicationVaultInput(nil), repositories.vaultEdges...)
	vaultEdges = append(vaultEdges, governanceEdges...)
	vaultEdges = append(vaultEdges, inputEdges...)
	if err := validatePreparedEdges(builder.bindings, vaultEdges); err != nil {
		return preparedPacketAuthority{}, ErrAuthorityPublication
	}
	return preparedPacketAuthority{
		PacketID:           packetID,
		PacketArtifactID:   packetArtifactID,
		RequestIdentity:    request.requestIdentity,
		Fingerprint:        fingerprint,
		Snapshot:           snapshot,
		RetainedArtifacts:  builder.artifacts,
		Bindings:           builder.bindings,
		VaultRelationships: vaultEdges,
	}, nil
}

func (s *LifecycleService) acquireFiles(ctx context.Context, request lifecycleRequest, files []fileacquisition.FileParameter) (fileacquisition.Result, error) {
	declarations := make([]fileacquisition.DeclaredFile, 0, len(request.declaredFiles))
	inputByIndex := make(map[int64]semanticidentity.InputBinding)
	for _, input := range request.inputs {
		if input.SourceKind == string(packet.InputSourceUploadedFile) && input.Source.FileIndex != nil {
			inputByIndex[*input.Source.FileIndex] = input
		}
	}
	for _, value := range request.declaredFiles {
		input, ok := inputByIndex[value.FileIndex]
		if !ok {
			return fileacquisition.Result{}, &fileacquisition.Error{Code: fileacquisition.ErrorFileCoverage}
		}
		declarations = append(declarations, fileacquisition.DeclaredFile{FileIndex: value.FileIndex, ExpectedSHA256: value.ExpectedSHA256, DisplayName: input.DisplayName, MediaType: input.MediaType})
	}
	if len(files) != request.inputFileCount {
		return fileacquisition.Result{}, &fileacquisition.Error{Code: fileacquisition.ErrorFileCoverage}
	}
	return fileacquisition.Acquire(ctx, s.fetcher, fileacquisition.Request{Files: files, Declared: declarations})
}

func (s *LifecycleService) prepareRepositories(ctx context.Context, project workflowstore.Project, operation registry.OperationDefinition, request lifecycleRequest) (repositoryPreparation, error) {
	associations, err := s.store.ListProjectRepositoryTargets(ctx, project.ID, 64)
	if err != nil || len(associations) == 0 {
		return repositoryPreparation{}, &Error{Code: CodeInvalidPacketDocument}
	}
	explicit := make(map[string]string, len(request.primaryRevisions))
	for _, value := range request.primaryRevisions {
		explicit[value.RepositoryKey] = value.CommitOID
	}
	anchorRequests := make(map[string][]semanticidentity.ComparisonAnchorRequest)
	for _, value := range request.comparisonAnchors {
		anchorRequests[value.RepositoryKey] = append(anchorRequests[value.RepositoryKey], value)
	}
	result := repositoryPreparation{
		primary:         make(map[string]sourcevault.ImportResult),
		primaryRevision: make(map[string]workflowrepos.ResolvedRevision),
		anchors:         make(map[string]map[string]sourcevault.ImportResult),
		anchorRevision:  make(map[string]map[string]workflowrepos.ResolvedRevision),
		direct:          make(map[string]sourcevault.ImportResult),
		directRevision:  make(map[string]workflowrepos.ResolvedRevision),
	}
	seen := make(map[string]struct{}, len(associations))
	for index, association := range associations {
		key := association.RepoTarget
		seen[key] = struct{}{}
		revision, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{
			RepoTarget:        association.RepoTarget,
			ExplicitCommitOID: explicit[key],
			Policy:            workflowrepos.RepositoryUsePolicy{RequireCleanWorktree: requiresCleanProject(operation.SourcePolicy)},
		})
		if err != nil {
			return repositoryPreparation{}, repositoryAuthorityError(key, err)
		}
		imported, err := s.vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: revision})
		if err != nil {
			return repositoryPreparation{}, repositoryAuthorityError(key, fmt.Errorf("source-vault capture failed: %w", err))
		}
		result.primary[key] = imported
		result.primaryRevision[key] = revision
		binding := repositoryBinding(key, int64(index+1), revision)
		result.vaultEdges = append(result.vaultEdges, PublicationVaultInput{ClosureID: imported.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:" + key + ":primary"})
		for _, anchorRequest := range anchorRequests[key] {
			anchorRevision, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: association.RepoTarget, ExplicitCommitOID: anchorRequest.CommitOID})
			if err != nil || anchorRevision.TreeOID != anchorRequest.ExpectedTreeOID {
				return repositoryPreparation{}, &Error{Code: CodeInvalidPacketDocument}
			}
			anchorImport, err := s.vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: anchorRevision})
			if err != nil {
				return repositoryPreparation{}, err
			}
			if result.anchors[key] == nil {
				result.anchors[key] = make(map[string]sourcevault.ImportResult)
				result.anchorRevision[key] = make(map[string]workflowrepos.ResolvedRevision)
			}
			result.anchors[key][anchorRequest.AnchorName] = anchorImport
			result.anchorRevision[key][anchorRequest.AnchorName] = anchorRevision
			binding.Anchors = append(binding.Anchors, packet.Anchor{AnchorName: anchorRequest.AnchorName, Purpose: registry.AnchorPurpose(anchorRequest.Purpose), CommitOID: anchorRevision.CommitOID, TreeOID: anchorRevision.TreeOID})
			result.vaultEdges = append(result.vaultEdges, PublicationVaultInput{ClosureID: anchorImport.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:" + key + ":anchor:" + anchorRequest.AnchorName})
		}
		sort.Slice(binding.Anchors, func(i, j int) bool { return binding.Anchors[i].AnchorName < binding.Anchors[j].AnchorName })
		result.bindings = append(result.bindings, binding)
	}
	for key := range explicit {
		if _, ok := seen[key]; !ok {
			return repositoryPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
	}
	for key := range anchorRequests {
		if _, ok := seen[key]; !ok {
			return repositoryPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
	}
	return result, nil
}

func repositoryAuthorityError(repoTarget string, err error) error {
	if err == nil {
		return nil
	}
	reason := "authority resolution failed"
	switch {
	case errors.Is(err, workflowrepos.ErrRepositoryUnconfigured):
		reason = "repository registration is incomplete"
	case errors.Is(err, workflowrepos.ErrConfiguredBranchUnavailable):
		reason = "configured branch does not resolve"
	case errors.Is(err, workflowrepos.ErrInvalidConfiguredBranch):
		reason = "configured branch ref is invalid"
	case errors.Is(err, workflowrepos.ErrRepositoryAccess):
		reason = "registered repository cannot be accessed"
	case errors.Is(err, workflowrepos.ErrDetachedRepositoryHead):
		reason = "repository HEAD is detached"
	case errors.Is(err, workflowrepos.ErrRepositoryNoCommit):
		reason = "repository has no commit to resolve"
	case errors.Is(err, workflowrepos.ErrRepositoryObject):
		reason = "configured branch commit or tree cannot be resolved"
	case errors.Is(err, workflowrepos.ErrDirtyProjectWorktree):
		reason = "configured repository worktree is dirty"
	case errors.Is(err, workflowrepos.ErrGovernanceUnavailable):
		reason = "configured governance authority is unavailable"
	case errors.As(err, new(*sourcevault.Error)):
		reason = "source-vault capture failed"
	}
	return &Error{Code: CodeRepositoryAuthorityUnavailable, MissingDependencyClass: fmt.Sprintf("%q", repoTarget), Reason: reason}
}

func (s *LifecycleService) prepareGovernance(ctx context.Context, packetID string, operation registry.OperationDefinition, request lifecycleRequest, repositories repositoryPreparation) (packet.GovernanceBinding, packet.ManifestDomainBinding, []PublicationVaultInput, workflowrepos.ResolvedRevision, error) {
	if operation.ManifestDomain == "" {
		// Published manifestless packets, including Wayfinder bootstrap and the
		// Planner ticket frontier, bind Project repositories without fabricating
		// a Planner or Auditor governance identity.
		return packet.GovernanceBinding{}, packet.ManifestDomainBinding{}, nil, workflowrepos.ResolvedRevision{}, nil
	}
	manifestPath := governanceManifestPath(operation.Role)
	clean := governanceRequiresCleanProject(operation, repositories)
	revision, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{
		RepoTarget:        "relay-specs",
		ExplicitCommitOID: request.relaySpecsRevision,
		Policy:            workflowrepos.RepositoryUsePolicy{RequireCleanWorktree: clean, RequireGovernanceAuthority: true},
		Governance:        workflowrepos.GovernanceRequest{ManifestPath: manifestPath, Domain: string(operation.ManifestDomain)},
	})
	if err != nil || revision.GovernanceAvailability == nil {
		return packet.GovernanceBinding{}, packet.ManifestDomainBinding{}, nil, workflowrepos.ResolvedRevision{}, errOrInvalid(err)
	}
	imported, err := s.vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: revision})
	if err != nil {
		return packet.GovernanceBinding{}, packet.ManifestDomainBinding{}, nil, workflowrepos.ResolvedRevision{}, err
	}
	availability := revision.GovernanceAvailability
	manifestObject, err := s.vaults.ReadPreparedObject(ctx, sourcevault.PreparedObjectReadRequest{Import: imported, ObjectOID: availability.ManifestBlobOID, ExpectedType: "blob", MaxBytes: lifecycleObjectLimit})
	if err != nil {
		return packet.GovernanceBinding{}, packet.ManifestDomainBinding{}, nil, workflowrepos.ResolvedRevision{}, err
	}
	manifestIdentity := pathIdentity([]byte(availability.ManifestPath))
	manifest := packet.ManifestDomainBinding{ManifestPath: manifestIdentity, ManifestBlobOID: availability.ManifestBlobOID, ManifestSHA256: digestBytes(manifestObject.Bytes), Domain: operation.ManifestDomain}
	edges := []PublicationVaultInput{{ClosureID: imported.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "governance:relay-specs"}, {ClosureID: imported.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyManifestMember, DependencyKey: "manifest:" + manifestIdentity.PathID + ":" + availability.ManifestBlobOID}}
	for index, member := range availability.Members {
		object, err := s.vaults.ReadPreparedObject(ctx, sourcevault.PreparedObjectReadRequest{Import: imported, ObjectOID: member.BlobOID, ExpectedType: "blob", MaxBytes: lifecycleObjectLimit})
		if err != nil {
			return packet.GovernanceBinding{}, packet.ManifestDomainBinding{}, nil, workflowrepos.ResolvedRevision{}, err
		}
		identity := pathIdentity([]byte(member.Path))
		manifest.Members = append(manifest.Members, packet.ManifestMember{MemberOrder: int64(index + 1), Path: identity, BlobOID: member.BlobOID, ByteSize: int64(len(object.Bytes)), SHA256: digestBytes(object.Bytes)})
		edges = append(edges, PublicationVaultInput{ClosureID: imported.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyManifestMember, DependencyKey: "member:" + identity.PathID + ":" + member.BlobOID})
	}
	governance := packet.GovernanceBinding{RepositoryKey: "relay-specs", RepositoryTarget: revision.RepositoryTarget.RepoTarget, Reserved: true, RevisionSource: revision.RevisionSource, ConfiguredWorkingBranchRef: revision.ConfiguredWorkingBranchRef, RepositoryTargetConfigurationVersion: revision.RepositoryTargetConfigurationVersion, CommitOID: revision.CommitOID, TreeOID: revision.TreeOID}
	_ = packetID
	return governance, manifest, edges, revision, nil
}

func (s *LifecycleService) revalidateRepositoryAuthority(ctx context.Context, operation registry.OperationDefinition, repositories repositoryPreparation, governance workflowrepos.ResolvedRevision) error {
	for key, expected := range repositories.primaryRevision {
		explicit := ""
		if expected.RevisionSource == workflowrepos.RevisionSourceExplicitCommit {
			explicit = expected.CommitOID
		}
		current, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: expected.RepositoryTarget.RepoTarget, ExplicitCommitOID: explicit, Policy: workflowrepos.RepositoryUsePolicy{RequireCleanWorktree: requiresCleanProject(operation.SourcePolicy)}})
		if err != nil || current.RepositoryTargetConfigurationVersion != expected.RepositoryTargetConfigurationVersion || current.RevisionSource != expected.RevisionSource || current.ConfiguredWorkingBranchRef != expected.ConfiguredWorkingBranchRef || current.CommitOID != expected.CommitOID || current.TreeOID != expected.TreeOID {
			_ = key
			return &sourcevault.Error{Code: sourcevault.CodeStaleConfiguredAuthority}
		}
	}
	if operation.ManifestDomain == "" {
		return nil
	}
	explicit := ""
	if governance.RevisionSource == workflowrepos.RevisionSourceExplicitCommit {
		explicit = governance.CommitOID
	}
	governanceClean := governanceRequiresCleanProject(operation, repositories)
	current, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: governance.RepositoryTarget.RepoTarget, ExplicitCommitOID: explicit, Policy: workflowrepos.RepositoryUsePolicy{RequireCleanWorktree: governanceClean, RequireGovernanceAuthority: true}, Governance: workflowrepos.GovernanceRequest{ManifestPath: governanceManifestPath(operation.Role), Domain: string(operation.ManifestDomain)}})
	if err != nil || current.RepositoryTargetConfigurationVersion != governance.RepositoryTargetConfigurationVersion || current.RevisionSource != governance.RevisionSource || current.ConfiguredWorkingBranchRef != governance.ConfiguredWorkingBranchRef || current.CommitOID != governance.CommitOID || current.TreeOID != governance.TreeOID {
		return &sourcevault.Error{Code: sourcevault.CodeStaleConfiguredAuthority}
	}
	return nil
}

func governanceRequiresCleanProject(operation registry.OperationDefinition, repositories repositoryPreparation) bool {
	if !requiresCleanProject(operation.SourcePolicy) {
		return false
	}
	for _, binding := range repositories.bindings {
		if strings.EqualFold(binding.RepositoryTarget, "relay-specs") {
			return true
		}
	}
	return false
}

func (s *LifecycleService) materializeInputs(ctx context.Context, operation registry.OperationDefinition, request lifecycleRequest, acquired fileacquisition.Result, workflow workflowPreparation, repositories repositoryPreparation, builder *retainedBuilder) ([]packet.InputBinding, []PublicationVaultInput, error) {
	slots := operationSlots(operation, request.prior != nil)
	inputs := make([]packet.InputBinding, 0, len(request.inputs))
	vaultEdges := make([]PublicationVaultInput, 0)
	priorPaths := priorPathIdentities(request.prior)
	for _, source := range request.inputs {
		slot, ok := slots[source.InputName]
		if !ok {
			return nil, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		value := packet.InputBinding{InputName: source.InputName, InputRole: slot.InputRole, SourceKind: registry.InputSourceKind(source.SourceKind), DisplayName: source.DisplayName, MediaType: source.MediaType, SHA256: source.ExpectedSHA256, AttestationKind: slot.AttestationKind}
		switch source.SourceKind {
		case string(packet.InputSourceUploadedFile):
			if source.Source.FileIndex == nil {
				return nil, nil, &Error{Code: CodeInvalidPacketDocument}
			}
			file, ok := acquired.File(*source.Source.FileIndex)
			if !ok || file.SHA256 != source.ExpectedSHA256 || file.DisplayName != source.DisplayName || file.MediaType != source.MediaType {
				return nil, nil, &Error{Code: CodeInvalidPacketDocument}
			}
			artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactDirectUploadedInput, source.MediaType, append([]byte(nil), file.Bytes...), workflowstore.OperationPacketDependencyInputArtifact, source.InputName)
			value.SizeBytes = file.SizeBytes
			value.Source = packet.InputSource{Kind: packet.InputSourceUploadedFile, FileIndex: file.FileIndex, ArtifactID: artifactID}
		case string(packet.InputSourceInlineText):
			data := []byte(source.Source.Text)
			if digestBytes(data) != source.ExpectedSHA256 {
				return nil, nil, &Error{Code: CodeInvalidPacketDocument}
			}
			artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactInlineInput, source.MediaType, data, workflowstore.OperationPacketDependencyInputArtifact, source.InputName)
			value.SizeBytes = int64(len(data))
			value.Source = packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: artifactID}
		case string(packet.InputSourceRelayArtifact):
			artifact, err := s.store.GetArtifactByArtifactID(ctx, source.Source.ArtifactID)
			if err != nil || artifact.SHA256 != source.ExpectedSHA256 {
				return nil, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
			}
			data, err := readWorkflowArtifact(s.store, artifact)
			if err != nil {
				return nil, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
			}
			builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, source.MediaType, data, workflowstore.OperationPacketDependencyInputArtifact, source.InputName)
			value.SizeBytes = int64(len(data))
			value.Source = packet.InputSource{Kind: packet.InputSourceRelayArtifact, ArtifactID: artifact.ArtifactID}
		case string(packet.InputSourceWorkflowRecord):
			if source.Source.WorkflowRecord == nil {
				return nil, nil, &Error{Code: CodeInvalidPacketDocument}
			}
			reference, data, err := s.materializeWorkflowRecord(ctx, *source.Source.WorkflowRecord)
			if err != nil || digestBytes(data) != source.ExpectedSHA256 {
				return nil, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
			}
			artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", data, workflowstore.OperationPacketDependencyWorkflowSnapshot, source.InputName)
			value.SizeBytes = int64(len(data))
			value.Source = packet.InputSource{Kind: packet.InputSourceWorkflowRecord, WorkflowReference: reference, SnapshotArtifactID: artifactID, SnapshotSHA256: source.ExpectedSHA256}
		case string(packet.InputSourceCommittedSource):
			pathValue, err := resolvePathSelector(source.Source.Path, priorPaths)
			if err != nil {
				return nil, nil, err
			}
			prepared, revision, err := s.resolveCommittedRevision(ctx, source.Source.RepositoryKey, source.Source.Revision, repositories)
			if err != nil {
				return nil, nil, err
			}
			resolved, err := s.repositories.ResolvePathBlob(ctx, revision, string(pathValue))
			if err != nil || resolved.BlobOID != source.Source.ExpectedBlobOID {
				return nil, nil, &Error{Code: CodeInvalidPacketDocument}
			}
			object, err := s.vaults.ReadPreparedObject(ctx, sourcevault.PreparedObjectReadRequest{Import: prepared, ObjectOID: resolved.BlobOID, ExpectedType: "blob", MaxBytes: lifecycleObjectLimit})
			if err != nil || digestBytes(object.Bytes) != source.ExpectedSHA256 {
				return nil, nil, errOrInvalid(err)
			}
			identity := pathIdentity(pathValue)
			value.SizeBytes = int64(len(object.Bytes))
			value.Source = packet.InputSource{Kind: packet.InputSourceCommittedSource, RepositoryBindingID: source.Source.RepositoryKey, CommitOID: revision.CommitOID, TreeOID: revision.TreeOID, Path: identity, BlobOID: resolved.BlobOID}
			vaultEdges = append(vaultEdges, PublicationVaultInput{ClosureID: prepared.Closure.ClosureID, DependencyClass: workflowstore.OperationPacketDependencyGitPathObject, DependencyKey: "path:" + source.Source.RepositoryKey + ":" + identity.PathID + ":" + resolved.BlobOID})
		default:
			return nil, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		inputs = append(inputs, value)
	}
	return inputs, vaultEdges, nil
}

func (s *LifecycleService) materializeDerivedInputs(ctx context.Context, operation registry.OperationDefinition, workflow workflowPreparation, builder *retainedBuilder) (derivedPreparation, error) {
	if len(operation.DerivedInputs) == 0 {
		return derivedPreparation{}, nil
	}
	switch operation.OperationID {
	case "planner.delivery_ticket", "planner.ticket_frontier":
		return s.materializeCurrentPlannerDerivedInput(ctx, operation, workflow, builder, "current_feature_workspace_route", s.loadCurrentFeatureWorkspaceRoute)
	case "planner.transition_plan":
		return s.materializeCurrentPlannerDerivedInput(ctx, operation, workflow, builder, "current_transition_applicability", s.loadCurrentTransitionApplicability)
	case "planner.ticket_design_brief":
		return s.materializeCurrentPlannerDerivedInput(ctx, operation, workflow, builder, "current_selection_identity", s.loadCurrentSelectionIdentity)
	case "planner.delivery_ticket_remediation":
		inputs, err := s.materializeRemediationDerivedInputs(ctx, workflow, builder)
		return derivedPreparation{inputs: inputs}, err
	case "planner.ticket_design_brief_remediation":
		inputs, err := s.materializeRemediationBriefDerivedInputs(ctx, workflow, builder)
		return derivedPreparation{inputs: inputs}, err
	case "auditor.audit":
		return s.materializeAuditDerivedInputs(ctx, operation, workflow, builder)
	default:
		return derivedPreparation{}, &Error{Code: CodeInvalidPacketDocument}
	}
}

func (s *LifecycleService) materializeAuditDerivedInputs(ctx context.Context, operation registry.OperationDefinition, workflow workflowPreparation, builder *retainedBuilder) (derivedPreparation, error) {
	var runReference packet.WorkflowReference
	for _, value := range workflow.references {
		if value.Kind == "run" {
			runReference = value
			break
		}
	}
	if runReference.RunID == "" {
		return derivedPreparation{}, &Error{Code: CodeInvalidPacketDocument}
	}
	run, err := s.store.GetRunByRunID(ctx, runReference.RunID)
	if err != nil {
		return derivedPreparation{}, err
	}
	auditPacket, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if err != nil {
		return derivedPreparation{}, err
	}
	artifact, err := s.store.GetArtifactByRowID(ctx, auditPacket.ArtifactRowID)
	if err != nil {
		return derivedPreparation{}, err
	}
	data, err := readWorkflowArtifact(s.store, artifact)
	if err != nil {
		return derivedPreparation{}, err
	}
	sections, err := auditDerivedSections(data)
	if err != nil {
		return derivedPreparation{}, &Error{Code: CodeInvalidPacketDocument}
	}
	inputs := make([]packet.InputBinding, 0, len(operation.DerivedInputs))
	for _, slot := range operation.DerivedInputs {
		section, ok := sections[slot.InputName]
		if !ok {
			return derivedPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
		artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", section, workflowstore.OperationPacketDependencyWorkflowSnapshot, slot.InputName)
		inputs = append(inputs, packet.InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: packet.InputSourceInlineText, DisplayName: slot.InputName + ".json", MediaType: "application/json", SHA256: digestBytes(section), SizeBytes: int64(len(section)), AttestationKind: slot.AttestationKind, Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: artifactID}})
	}
	return derivedPreparation{inputs: inputs}, nil
}

type currentPlannerDerivedLoader func(context.Context, workflowPreparation) (string, []byte, error)

func (s *LifecycleService) materializeCurrentPlannerDerivedInput(ctx context.Context, operation registry.OperationDefinition, workflow workflowPreparation, builder *retainedBuilder, expectedName string, loader currentPlannerDerivedLoader) (derivedPreparation, error) {
	name, data, err := loader(ctx, workflow)
	if err != nil || name != expectedName {
		return derivedPreparation{}, &Error{Code: CodeInvalidPacketDocument}
	}
	input, err := publishCurrentPlannerDerivedInput(operation, expectedName, data, builder)
	if err != nil {
		return derivedPreparation{}, err
	}
	return derivedPreparation{inputs: []packet.InputBinding{input}, currentInputName: name, currentBytes: append([]byte(nil), data...)}, nil
}

func publishCurrentPlannerDerivedInput(operation registry.OperationDefinition, expectedName string, data []byte, builder *retainedBuilder) (packet.InputBinding, error) {
	if len(operation.DerivedInputs) != 1 {
		return packet.InputBinding{}, &Error{Code: CodeInvalidPacketDocument}
	}
	slot := operation.DerivedInputs[0]
	if slot.InputName != expectedName || slot.WorkflowRecordPolicy != "derived" || len(slot.AllowedSourceKinds) != 0 || slot.AttestationKind != "derived_authority" {
		return packet.InputBinding{}, &Error{Code: CodeInvalidPacketDocument}
	}
	artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", data, workflowstore.OperationPacketDependencyWorkflowSnapshot, slot.InputName)
	return packet.InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: packet.InputSourceInlineText, DisplayName: slot.InputName + ".json", MediaType: "application/json", SHA256: digestBytes(data), SizeBytes: int64(len(data)), AttestationKind: slot.AttestationKind, Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: artifactID}}, nil
}

func requiredWorkflowReference(workflow workflowPreparation, kind registry.WorkflowReferenceKind) (packet.WorkflowReference, error) {
	var found packet.WorkflowReference
	count := 0
	for _, reference := range workflow.references {
		if reference.Kind == kind {
			found = reference
			count++
		}
	}
	if count != 1 {
		return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
	}
	var expected packet.WorkflowReference
	switch kind {
	case "feature_workspace":
		if found.WorkspaceID == "" || found.WorkspaceVersion < 1 || found.RouteStateID == "" || found.RouteSequence < 1 || found.RouteWorkspaceVersion < 1 || found.RouteState == "" {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		expected = packet.WorkflowReference{Kind: kind, WorkspaceID: found.WorkspaceID, WorkspaceVersion: found.WorkspaceVersion, RouteStateID: found.RouteStateID, RouteSequence: found.RouteSequence, RouteWorkspaceVersion: found.RouteWorkspaceVersion, RouteState: found.RouteState}
	case "delivery_ticket":
		if found.WorkspaceID == "" || found.TicketID == "" || found.RevisionID < 1 || found.RevisionNumber < 1 || found.SourceClosureID == "" {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		expected = packet.WorkflowReference{Kind: kind, WorkspaceID: found.WorkspaceID, TicketID: found.TicketID, RevisionID: found.RevisionID, RevisionNumber: found.RevisionNumber, SourceClosureID: found.SourceClosureID}
	default:
		return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
	}
	if found != expected {
		return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return found, nil
}

func (s *LifecycleService) loadCurrentFeatureWorkspaceRoute(ctx context.Context, workflow workflowPreparation) (string, []byte, error) {
	reference, err := requiredWorkflowReference(workflow, "feature_workspace")
	if err != nil {
		return "", nil, err
	}
	workspace, route, err := s.loadReferencedReadyWorkspaceRoute(ctx, reference)
	if err != nil {
		return "", nil, err
	}
	data, err := canonicalJSON(featureWorkspaceRouteInput{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, WorkspaceVersion: workspace.Version, WorkspaceState: workspace.State, RouteStateID: route.RouteStateID, RouteSequence: route.Sequence, RouteWorkspaceVersion: route.WorkspaceVersion, RouteState: route.State})
	return "current_feature_workspace_route", data, err
}

func (s *LifecycleService) loadReferencedReadyWorkspaceRoute(ctx context.Context, reference packet.WorkflowReference) (workflowstore.FeatureWorkspace, workflowstore.FeatureWorkspaceRouteState, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, reference.WorkspaceID)
	if err != nil || workspace.WorkspaceID != reference.WorkspaceID || workspace.FeatureSlug == "" || workspace.State != "open" || workspace.Version < 1 || !workspace.CurrentRouteStateRowID.Valid {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument}
	}
	route, err := s.store.GetFeatureWorkspaceRouteStateByRowID(ctx, workspace.CurrentRouteStateRowID.Int64)
	if err != nil || route.WorkspaceRowID != workspace.ID || route.ID != workspace.CurrentRouteStateRowID.Int64 {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument}
	}
	if route.RouteStateID != reference.RouteStateID {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument, Reason: "route_id_mismatch"}
	}
	if route.Sequence != reference.RouteSequence {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument, Reason: "route_sequence_mismatch"}
	}
	if route.WorkspaceVersion != reference.RouteWorkspaceVersion {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument, Reason: "route_workspace_version_mismatch"}
	}
	if route.State != reference.RouteState || workspace.Version != reference.WorkspaceVersion {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument, Reason: "workspace_version_mismatch"}
	}
	if route.WorkspaceVersion != workspace.Version || route.State != "ready" {
		return workflowstore.FeatureWorkspace{}, workflowstore.FeatureWorkspaceRouteState{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return workspace, route, nil
}

func (s *LifecycleService) loadCurrentTransitionApplicability(ctx context.Context, workflow workflowPreparation) (string, []byte, error) {
	workspaceReference, ticketReference, workspace, ticket, revision, closure, err := s.loadCurrentReferencedTicket(ctx, workflow)
	_ = workspaceReference
	if err != nil || revision.TransitionApplicability != "required" {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	data, err := canonicalJSON(transitionApplicabilityInput{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, WorkspaceVersion: workspace.Version, TicketID: ticket.TicketID, RevisionID: revision.ID, RevisionNumber: revision.RevisionNumber, SourceClosureID: closure.ClosureID, TransitionApplicability: revision.TransitionApplicability})
	if ticketReference.TicketID != ticket.TicketID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	return "current_transition_applicability", data, err
}

func (s *LifecycleService) loadCurrentReferencedTicket(ctx context.Context, workflow workflowPreparation) (packet.WorkflowReference, packet.WorkflowReference, workflowstore.FeatureWorkspace, workflowstore.DeliveryTicket, workflowstore.DeliveryTicketRevision, workflowstore.SourceVaultClosure, error) {
	workspaceReference, err := requiredWorkflowReference(workflow, "feature_workspace")
	if err != nil {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, err
	}
	ticketReference, err := requiredWorkflowReference(workflow, "delivery_ticket")
	if err != nil || workspaceReference.WorkspaceID != ticketReference.WorkspaceID {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, &Error{Code: CodeInvalidPacketDocument}
	}
	workspace, _, err := s.loadReferencedReadyWorkspaceRoute(ctx, workspaceReference)
	if err != nil {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, err
	}
	ticket, err := s.store.GetDeliveryTicketByTicketID(ctx, ticketReference.TicketID)
	if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != ticketReference.RevisionID {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, &Error{Code: CodeInvalidPacketDocument}
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, ticketReference.RevisionID)
	if err != nil || revision.DeliveryTicketRowID != ticket.ID || revision.ID != ticketReference.RevisionID || revision.RevisionNumber != ticketReference.RevisionNumber || revision.CancellationReason.Valid || revision.SourceClosureRowID < 1 {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, &Error{Code: CodeInvalidPacketDocument}
	}
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, revision.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID == "" || closure.ClosureID != ticketReference.SourceClosureID {
		return packet.WorkflowReference{}, packet.WorkflowReference{}, workflowstore.FeatureWorkspace{}, workflowstore.DeliveryTicket{}, workflowstore.DeliveryTicketRevision{}, workflowstore.SourceVaultClosure{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return workspaceReference, ticketReference, workspace, ticket, revision, closure, nil
}

func (s *LifecycleService) loadCurrentSelectionIdentity(ctx context.Context, workflow workflowPreparation) (string, []byte, error) {
	_, _, workspace, ticket, revision, closure, err := s.loadCurrentReferencedTicket(ctx, workflow)
	if err != nil || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	authority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil || authority.WorkspaceRowID != workspace.ID || authority.AuthorityRevisionID == "" || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != revision.SourceClosureRowID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var selection workflowstore.DeliveryTicketSelection
	for _, candidate := range selections {
		if candidate.State == "active" {
			if selection.ID != 0 {
				return "", nil, &Error{Code: CodeInvalidPacketDocument}
			}
			selection = candidate
		}
	}
	if selection.ID == 0 || selection.WorkspaceRowID != workspace.ID || selection.SelectionID == "" || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != revision.SourceClosureRowID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	members, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil || len(members) != 1 || members[0].SelectionRowID != selection.ID || members[0].Sequence != 1 || members[0].ApprovalRowID < 1 {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if members[0].RevisionRowID != revision.ID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument, Reason: "selection_target_mismatch"}
	}
	approval, err := s.store.GetDeliveryTicketRevisionApprovalByRowID(ctx, members[0].ApprovalRowID)
	if err != nil || approval.RevisionRowID != revision.ID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument, Reason: "approval_revision_mismatch"}
	}
	if approval.ApprovalKind != "delivery" || approval.ApprovalState != "approved" || approval.ApprovalID == "" || approval.SourceClosureRowID != revision.SourceClosureRowID || !approval.AuthorityRevisionRowID.Valid || approval.AuthorityRevisionRowID.Int64 != authority.ID {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	dependencies, err := s.store.ListDeliveryTicketRevisionDependencies(ctx, revision.ID)
	if err != nil {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	for _, dependency := range dependencies {
		if dependency.Outcome != "satisfied" {
			return "", nil, &Error{Code: CodeInvalidPacketDocument}
		}
		dependencyRevision, loadErr := s.store.GetDeliveryTicketRevisionByRowID(ctx, dependency.DependsOnRevisionRowID)
		if loadErr != nil {
			return "", nil, &Error{Code: CodeInvalidPacketDocument}
		}
		dependencyTicket, loadErr := s.store.GetDeliveryTicketByRowID(ctx, dependencyRevision.DeliveryTicketRowID)
		if loadErr != nil || dependencyTicket.WorkspaceRowID != workspace.ID {
			return "", nil, &Error{Code: CodeInvalidPacketDocument, Reason: "dependency_workspace_mismatch"}
		}
		if !dependencyTicket.CurrentRevisionRowID.Valid || dependencyTicket.CurrentRevisionRowID.Int64 != dependencyRevision.ID {
			return "", nil, &Error{Code: CodeInvalidPacketDocument}
		}
		if _, loadErr = s.store.GetDeliveryTicketRevisionSatisfaction(ctx, dependencyRevision.ID); loadErr != nil {
			return "", nil, &Error{Code: CodeInvalidPacketDocument, Reason: "dependency_satisfaction_missing"}
		}
	}
	if _, err = s.store.GetDeliveryTicketRevisionSatisfaction(ctx, revision.ID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		return "", nil, &Error{Code: CodeInvalidPacketDocument}
	}
	data, err := canonicalJSON(selectionIdentityInput{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, WorkspaceVersion: workspace.Version, SelectionID: selection.SelectionID, SelectionState: selection.State, TicketID: ticket.TicketID, RevisionID: revision.ID, RevisionNumber: revision.RevisionNumber, ApprovalID: approval.ApprovalID, AuthorityRevisionID: authority.AuthorityRevisionID, SourceClosureID: closure.ClosureID})
	return "current_selection_identity", data, err
}

func (s *LifecycleService) revalidateCurrentPlannerDerivedInput(ctx context.Context, operation registry.OperationDefinition, workflow workflowPreparation, prepared derivedPreparation) error {
	if prepared.currentInputName == "" {
		return nil
	}
	var loader currentPlannerDerivedLoader
	switch operation.OperationID {
	case "planner.delivery_ticket", "planner.ticket_frontier":
		loader = s.loadCurrentFeatureWorkspaceRoute
	case "planner.transition_plan":
		loader = s.loadCurrentTransitionApplicability
	case "planner.ticket_design_brief":
		loader = s.loadCurrentSelectionIdentity
	default:
		return &Error{Code: CodeInvalidPacketDocument}
	}
	name, data, err := loader(ctx, workflow)
	if err != nil || name != prepared.currentInputName || !bytes.Equal(data, prepared.currentBytes) {
		return &Error{Code: CodeInvalidPacketDocument}
	}
	return nil
}

func (s *LifecycleService) materializeRemediationDerivedInputs(ctx context.Context, workflow workflowPreparation, builder *retainedBuilder) ([]packet.InputBinding, error) {
	var auditReference packet.WorkflowReference
	for _, reference := range workflow.references {
		if reference.Kind == "audit_decision" {
			if auditReference.AuditDecisionID != "" {
				return nil, &Error{Code: CodeInvalidPacketDocument}
			}
			auditReference = reference
		}
	}
	if auditReference.AuditDecisionID == "" || auditReference.RunID == "" || auditReference.Decision != workflowstore.AuditDecisionNeedsRevision {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	decision, err := s.store.GetAuditDecisionByDecisionID(ctx, auditReference.AuditDecisionID)
	if err != nil || decision.RunRowID == 0 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	run, err := s.store.GetRunByRunID(ctx, auditReference.RunID)
	if err != nil || run.ID != decision.RunRowID || decision.Decision != workflowstore.AuditDecisionNeedsRevision {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	revisionDecisions, err := s.store.ListAuditTicketRevisionDecisions(ctx, decision.ID)
	if err != nil || len(revisionDecisions) != 1 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	revisionDecision := revisionDecisions[0]
	seed, err := s.store.GetAuditRemediationSeedByRevisionDecisionRowID(ctx, revisionDecision.ID)
	if err != nil || seed.AuditTicketRevisionDecisionRowID != revisionDecision.ID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if _, err := s.store.GetAuditRemediationSeedReopening(ctx, seed.ID); err == nil || !errors.Is(err, sql.ErrNoRows) {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	obligation, err := s.store.GetAuditPacketTicketObligationByRowID(ctx, revisionDecision.AuditPacketTicketObligationRowID)
	if err != nil || seed.AuditPacketRowID != obligation.AuditPacketRowID || seed.ExecutionPackageRowID != obligation.ExecutionPackageRowID ||
		obligation.PackageApprovalRowID.Valid != revisionDecision.PackageApprovalRowID.Valid ||
		(obligation.PackageApprovalRowID.Valid && obligation.PackageApprovalRowID.Int64 != revisionDecision.PackageApprovalRowID.Int64) ||
		!obligation.ApprovedPackageSha256.Valid || !revisionDecision.ApprovedPackageSha256.Valid ||
		obligation.ApprovedPackageSha256.String != revisionDecision.ApprovedPackageSha256.String {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	auditPacket, err := s.store.GetAuditPacketByRowID(ctx, seed.AuditPacketRowID)
	if err != nil || auditPacket.RunRowID != run.ID || auditPacket.AuditPacketID == "" || auditPacket.PacketSHA256 != decision.PacketSHA256 ||
		auditPacket.ArtifactRowID != decision.AuditPacketArtifactRowID || auditPacket.AuditedCommit != decision.AuditedCommit ||
		seed.AuditedCommit != decision.AuditedCommit || auditPacket.Status != workflowstore.AuditPacketStatusCurrent {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	pkg, err := s.store.GetExecutionPackageByRowID(ctx, obligation.ExecutionPackageRowID)
	if err != nil || pkg.ID != seed.ExecutionPackageRowID || !obligation.PackageApprovalRowID.Valid || !obligation.ApprovedPackageSha256.Valid ||
		obligation.ApprovedPackageSha256.String != pkg.PackageSha256 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	members, err := s.store.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || !containsExecutionPackageMember(members, obligation.ExecutionPackageMemberRowID, obligation.DeliveryTicketRevisionRowID) {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	ticket, err := s.store.GetDeliveryTicketByRowID(ctx, obligation.DeliveryTicketRowID)
	if err != nil || ticket.WorkspaceRowID != pkg.WorkspaceRowID || !ticket.CurrentRevisionRowID.Valid || ticket.CurrentRevisionRowID.Int64 != obligation.DeliveryTicketRevisionRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
	if err != nil || revision.DeliveryTicketRowID != ticket.ID || revision.CancellationReason.Valid || revision.SourceClosureRowID != pkg.SourceClosureRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if pkg.AuthorityRevisionRowID != obligation.AuthorityRevisionRowID || pkg.SourceClosureRowID != obligation.SourceClosureRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	findings, err := s.store.ListAuditRemediationSeedFindings(ctx, seed.ID)
	if err != nil || len(findings) == 0 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	seedInput := remediationSeedInput{
		RemediationSeedID:             seed.RemediationSeedID,
		AuditDecisionID:               decision.AuditDecisionID,
		AuditPacketID:                 auditPacket.AuditPacketID,
		ApprovedExecutionPackage:      remediationPackageIdentity{PackageID: pkg.PackageID, PackageSHA256: pkg.PackageSha256},
		AuditedDeliveryTicket:         remediationTicketIdentity{TicketID: ticket.TicketID},
		AuditedDeliveryTicketRevision: remediationRevisionIdentity{RevisionID: revision.ID, RevisionNumber: revision.RevisionNumber},
		AuditedCommit:                 seed.AuditedCommit,
		DecisionRationale:             seed.DecisionRationale,
		MaterialFindings:              make([]remediationFindingInput, len(findings)),
	}
	for index, finding := range findings {
		if finding.UpstreamClassification != "implementation" && finding.UpstreamClassification != "governing_package" && finding.UpstreamClassification != "both" {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		seedInput.MaterialFindings[index] = remediationFindingInput{
			Sequence: finding.Sequence, UpstreamClassification: finding.UpstreamClassification,
			Summary: finding.Summary, Evidence: finding.Evidence, RequiredRemediation: finding.RequiredRemediation,
		}
	}
	seedBytes, err := canonicalJSON(seedInput)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	if auditedAuthority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, obligation.AuthorityRevisionRowID); err != nil || auditedAuthority.WorkspaceRowID != pkg.WorkspaceRowID || !auditedAuthority.SourceClosureRowID.Valid || auditedAuthority.SourceClosureRowID.Int64 != pkg.SourceClosureRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	_, authorityInput, err := s.currentApprovedAuthority(ctx, ticket.WorkspaceRowID)
	if err != nil {
		return nil, err
	}
	authorityInputBytes, err := canonicalJSON(authorityInput)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	seedArtifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", seedBytes, workflowstore.OperationPacketDependencyWorkflowSnapshot, "remediation_seed")
	authorityArtifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", authorityInputBytes, workflowstore.OperationPacketDependencyWorkflowSnapshot, "current_approved_authority")
	return []packet.InputBinding{
		{InputName: "remediation_seed", InputRole: "governing", SourceKind: packet.InputSourceInlineText, DisplayName: "remediation_seed.json", MediaType: "application/json", SHA256: digestBytes(seedBytes), SizeBytes: int64(len(seedBytes)), AttestationKind: "derived_authority", Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: seedArtifactID}},
		{InputName: "current_approved_authority", InputRole: "governing", SourceKind: packet.InputSourceInlineText, DisplayName: "current_approved_authority.json", MediaType: "application/json", SHA256: digestBytes(authorityInputBytes), SizeBytes: int64(len(authorityInputBytes)), AttestationKind: "derived_authority", Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: authorityArtifactID}},
	}, nil
}

func (s *LifecycleService) materializeRemediationBriefDerivedInputs(ctx context.Context, workflow workflowPreparation, builder *retainedBuilder) ([]packet.InputBinding, error) {
	var auditReference packet.WorkflowReference
	for _, reference := range workflow.references {
		if reference.Kind != "audit_decision" {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		if auditReference.AuditDecisionID != "" {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		auditReference = reference
	}
	if auditReference.AuditDecisionID == "" || auditReference.RunID == "" || auditReference.Decision != workflowstore.AuditDecisionNeedsRevision {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	decision, err := s.store.GetAuditDecisionByDecisionID(ctx, auditReference.AuditDecisionID)
	if err != nil || decision.RunRowID == 0 || decision.Decision != workflowstore.AuditDecisionNeedsRevision {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	run, err := s.store.GetRunByRunID(ctx, auditReference.RunID)
	if err != nil || run.ID != decision.RunRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	revisionDecisions, err := s.store.ListAuditTicketRevisionDecisions(ctx, decision.ID)
	if err != nil || len(revisionDecisions) != 1 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	revisionDecision := revisionDecisions[0]
	seed, err := s.store.GetAuditRemediationSeedByRevisionDecisionRowID(ctx, revisionDecision.ID)
	if err != nil || seed.AuditTicketRevisionDecisionRowID != revisionDecision.ID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	reopening, err := s.store.GetAuditRemediationSeedReopening(ctx, seed.ID)
	if err != nil || reopening.RemediationSeedRowID != seed.ID || reopening.ReopeningRevisionRowID < 1 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	obligation, err := s.store.GetAuditPacketTicketObligationByRowID(ctx, revisionDecision.AuditPacketTicketObligationRowID)
	if err != nil || seed.AuditPacketRowID != obligation.AuditPacketRowID || seed.ExecutionPackageRowID != obligation.ExecutionPackageRowID ||
		obligation.PackageApprovalRowID.Valid != revisionDecision.PackageApprovalRowID.Valid ||
		(obligation.PackageApprovalRowID.Valid && obligation.PackageApprovalRowID.Int64 != revisionDecision.PackageApprovalRowID.Int64) ||
		!obligation.ApprovedPackageSha256.Valid || !revisionDecision.ApprovedPackageSha256.Valid ||
		obligation.ApprovedPackageSha256.String != revisionDecision.ApprovedPackageSha256.String {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	auditPacket, err := s.store.GetAuditPacketByRowID(ctx, seed.AuditPacketRowID)
	if err != nil || auditPacket.RunRowID != run.ID || auditPacket.AuditPacketID == "" || auditPacket.PacketSHA256 != decision.PacketSHA256 ||
		auditPacket.ArtifactRowID != decision.AuditPacketArtifactRowID || auditPacket.AuditedCommit != decision.AuditedCommit ||
		seed.AuditedCommit != decision.AuditedCommit || auditPacket.Status != workflowstore.AuditPacketStatusCurrent {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	pkg, err := s.store.GetExecutionPackageByRowID(ctx, obligation.ExecutionPackageRowID)
	if err != nil || pkg.ID != seed.ExecutionPackageRowID || !obligation.PackageApprovalRowID.Valid || !obligation.ApprovedPackageSha256.Valid || obligation.ApprovedPackageSha256.String != pkg.PackageSha256 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	members, err := s.store.ListExecutionPackageMembers(ctx, pkg.ID)
	if err != nil || !containsExecutionPackageMember(members, obligation.ExecutionPackageMemberRowID, obligation.DeliveryTicketRevisionRowID) {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	auditedTicket, err := s.store.GetDeliveryTicketByRowID(ctx, obligation.DeliveryTicketRowID)
	if err != nil || auditedTicket.WorkspaceRowID != pkg.WorkspaceRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	auditedRevision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, obligation.DeliveryTicketRevisionRowID)
	if err != nil || auditedRevision.DeliveryTicketRowID != auditedTicket.ID || auditedRevision.SourceClosureRowID != pkg.SourceClosureRowID || auditedRevision.CancellationReason.Valid {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	remediationRevision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, reopening.ReopeningRevisionRowID)
	if err != nil || remediationRevision.ID != reopening.ReopeningRevisionRowID || remediationRevision.CancellationReason.Valid {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	remediationTicket, err := s.store.GetDeliveryTicketByRowID(ctx, remediationRevision.DeliveryTicketRowID)
	if err != nil || remediationTicket.WorkspaceRowID != auditedTicket.WorkspaceRowID || !remediationTicket.CurrentRevisionRowID.Valid || remediationTicket.CurrentRevisionRowID.Int64 != remediationRevision.ID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if reopening.ReopeningKind != "replacement_ticket_revision" && reopening.ReopeningKind != "remediation_ticket" {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var replacementRevisionRowID *int64
	switch reopening.ReopeningKind {
	case "replacement_ticket_revision":
		if remediationTicket.ID != auditedTicket.ID || !remediationRevision.ReplacesRevisionRowID.Valid || remediationRevision.ReplacesRevisionRowID.Int64 != auditedRevision.ID {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		replacementRevisionRowID = remediationReplacementRevisionRowID(reopening.ReopeningKind, remediationRevision)
	case "remediation_ticket":
		if remediationTicket.ID == auditedTicket.ID {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
	}

	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, remediationRevision.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID == "" || closure.CommitOID == "" {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	seedFindings, err := s.store.ListAuditRemediationSeedFindings(ctx, seed.ID)
	if err != nil || len(seedFindings) == 0 {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	seedInput := remediationSeedInput{
		RemediationSeedID: seed.RemediationSeedID, AuditDecisionID: decision.AuditDecisionID, AuditPacketID: auditPacket.AuditPacketID,
		ApprovedExecutionPackage:      remediationPackageIdentity{PackageID: pkg.PackageID, PackageSHA256: pkg.PackageSha256},
		AuditedDeliveryTicket:         remediationTicketIdentity{TicketID: auditedTicket.TicketID},
		AuditedDeliveryTicketRevision: remediationRevisionIdentity{RevisionID: auditedRevision.ID, RevisionNumber: auditedRevision.RevisionNumber},
		AuditedCommit:                 seed.AuditedCommit, DecisionRationale: seed.DecisionRationale, MaterialFindings: make([]remediationFindingInput, len(seedFindings)),
	}
	for index, finding := range seedFindings {
		if finding.UpstreamClassification != "implementation" && finding.UpstreamClassification != "governing_package" && finding.UpstreamClassification != "both" {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		seedInput.MaterialFindings[index] = remediationFindingInput{Sequence: finding.Sequence, UpstreamClassification: finding.UpstreamClassification, Summary: finding.Summary, Evidence: finding.Evidence, RequiredRemediation: finding.RequiredRemediation}
	}
	seedBytes, err := canonicalJSON(seedInput)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, remediationTicket.WorkspaceRowID)
	if err != nil || workspace.ID != auditedTicket.WorkspaceRowID || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	_, authorityInput, err := s.currentApprovedAuthority(ctx, remediationTicket.WorkspaceRowID)
	if err != nil {
		return nil, err
	}
	if authorityInput.SourceClosureID != closure.ClosureID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	currentAuthority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil || currentAuthority.WorkspaceRowID != workspace.ID || !currentAuthority.SourceClosureRowID.Valid || currentAuthority.SourceClosureRowID.Int64 != remediationRevision.SourceClosureRowID {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}

	selected, dependencies, err := s.materializeSelectedRemediationTicket(ctx, remediationTicket, remediationRevision, workspace, closure, currentAuthority.ID)
	if err != nil {
		return nil, err
	}
	dependencyDocument := completedDependencyOutcomesInput{RemediationTicketID: remediationTicket.TicketID, RemediationRevisionRowID: remediationRevision.ID, RemediationRevisionNumber: remediationRevision.RevisionNumber, Dependencies: dependencies}
	selected.RemediationSeedID = seed.RemediationSeedID
	selected.AuditDecisionID = decision.AuditDecisionID
	selected.ReopeningKind = reopening.ReopeningKind
	selected.AuditedTicketID = auditedTicket.TicketID
	selected.AuditedRevisionRowID = auditedRevision.ID
	selected.AuditedRevisionNumber = auditedRevision.RevisionNumber
	selected.ReplacementRevisionRowID = replacementRevisionRowID
	selected.WorkspaceID = workspace.WorkspaceID
	selected.SourceClosureID = closure.ClosureID
	selected.SourceClosureCommit = closure.CommitOID
	selected.Canonical, err = s.readSelectedRemediationTicketArtifact(remediationTicket, remediationRevision, "delivery-ticket.json", "application/json")
	if err != nil {
		return nil, err
	}
	selected.Rendered, err = s.readSelectedRemediationTicketArtifact(remediationTicket, remediationRevision, "delivery-ticket.md", "text/markdown")
	if err != nil {
		return nil, err
	}

	selectedBytes, err := canonicalJSON(selected)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	dependencyBytes, err := canonicalJSON(dependencyDocument)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	authorityInputBytes, err := canonicalJSON(authorityInput)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	values := map[string][]byte{
		"remediation_seed":              seedBytes,
		"selected_remediation_ticket":   selectedBytes,
		"completed_dependency_outcomes": dependencyBytes,
		"current_approved_authority":    authorityInputBytes,
	}
	inputs := make([]packet.InputBinding, 0, len(values))
	for _, slot := range []string{"remediation_seed", "selected_remediation_ticket", "completed_dependency_outcomes", "current_approved_authority"} {
		data, ok := values[slot]
		if !ok {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", data, workflowstore.OperationPacketDependencyWorkflowSnapshot, slot)
		inputs = append(inputs, packet.InputBinding{InputName: slot, InputRole: "governing", SourceKind: packet.InputSourceInlineText, DisplayName: slot + ".json", MediaType: "application/json", SHA256: digestBytes(data), SizeBytes: int64(len(data)), AttestationKind: "derived_authority", Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: artifactID}})
	}
	return inputs, nil
}

func remediationReplacementRevisionRowID(reopeningKind string, remediationRevision workflowstore.DeliveryTicketRevision) *int64 {
	if reopeningKind != "replacement_ticket_revision" || !remediationRevision.ReplacesRevisionRowID.Valid {
		return nil
	}
	value := remediationRevision.ReplacesRevisionRowID.Int64
	return &value
}

func (s *LifecycleService) materializeSelectedRemediationTicket(ctx context.Context, ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision, workspace workflowstore.FeatureWorkspace, closure workflowstore.SourceVaultClosure, authorityRevisionRowID int64) (selectedRemediationTicketInput, []completedDependencyOutcome, error) {
	members, err := s.store.ListDeliveryTicketRevisionMembers(ctx, revision.ID)
	if err != nil {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	cancellationReason := ""
	if revision.CancellationReason.Valid {
		cancellationReason = revision.CancellationReason.String
	}
	selected := selectedRemediationTicketInput{
		RemediationTicketID:       ticket.TicketID,
		RemediationRevisionRowID:  revision.ID,
		RemediationRevisionNumber: revision.RevisionNumber,
		WorkspaceID:               workspace.WorkspaceID,
		ExternalPriority:          ticket.ExternalPriority,
		RepoTarget:                revision.RepoTarget,
		Branch:                    revision.Branch,
		BaseCommit:                revision.BaseCommit,
		SourceClosureRowID:        revision.SourceClosureRowID,
		SourceClosureID:           closure.ClosureID,
		SourceClosureCommit:       closure.CommitOID,
		SourcePath:                revision.SourcePath,
		Goal:                      revision.Goal,
		Context:                   revision.Context,
		TransitionApplicability:   revision.TransitionApplicability,
		CancellationReason:        cancellationReason,
		Members:                   make([]selectedRemediationTicketMember, len(members)),
		Dependencies:              make([]selectedRemediationTicketDependency, 0),
	}
	for index, member := range members {
		path := ""
		if member.MemberPath.Valid {
			path = member.MemberPath.String
		}
		selected.Members[index] = selectedRemediationTicketMember{Sequence: member.Sequence, Kind: member.MemberKind, Path: path, Text: member.MemberText}
	}

	approvals, err := s.store.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var currentApprovals []workflowstore.DeliveryTicketRevisionApproval
	for _, approval := range approvals {
		if approval.ApprovalKind == "delivery" && approval.ApprovalState == "approved" && approval.SourceClosureRowID == revision.SourceClosureRowID && approval.AuthorityRevisionRowID.Valid && approval.AuthorityRevisionRowID.Int64 == authorityRevisionRowID {
			currentApprovals = append(currentApprovals, approval)
		}
	}
	if len(currentApprovals) != 1 {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	approval := currentApprovals[0]
	selected.Approval = selectedRemediationTicketApproval{ApprovalRowID: approval.ID, ApprovalID: approval.ApprovalID, AuthorityRevisionRowID: authorityRevisionRowID, SourceClosureRowID: approval.SourceClosureRowID}

	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var active []workflowstore.DeliveryTicketSelection
	for _, selection := range selections {
		if selection.State == "active" {
			active = append(active, selection)
		}
	}
	if len(active) != 1 {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	selection := active[0]
	if selection.WorkspaceRowID != workspace.ID || !selection.SourceClosureRowID.Valid || selection.SourceClosureRowID.Int64 != revision.SourceClosureRowID {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	selectionMembers, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
	if err != nil || len(selectionMembers) != 1 {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	selectionMember := selectionMembers[0]
	if selectionMember.RevisionRowID != revision.ID || selectionMember.ApprovalRowID != approval.ID {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	selected.Selection = selectedRemediationTicketSelection{SelectionRowID: selection.ID, SelectionID: selection.SelectionID, State: selection.State, SourceClosureRowID: selection.SourceClosureRowID.Int64, MemberRowID: selectionMember.ID, MemberSequence: selectionMember.Sequence, MemberRevisionRowID: selectionMember.RevisionRowID, MemberApprovalRowID: selectionMember.ApprovalRowID}

	dependencies, err := s.store.ListDeliveryTicketRevisionDependencies(ctx, revision.ID)
	if err != nil {
		return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	completed := make([]completedDependencyOutcome, len(dependencies))
	for index, dependency := range dependencies {
		if dependency.Outcome != "satisfied" {
			return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		dependencyRevision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, dependency.DependsOnRevisionRowID)
		if err != nil {
			return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		dependencyTicket, err := s.store.GetDeliveryTicketByRowID(ctx, dependencyRevision.DeliveryTicketRowID)
		if err != nil || dependencyTicket.WorkspaceRowID != workspace.ID || !dependencyTicket.CurrentRevisionRowID.Valid || dependencyTicket.CurrentRevisionRowID.Int64 != dependencyRevision.ID {
			return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		satisfaction, err := s.store.GetDeliveryTicketRevisionSatisfaction(ctx, dependencyRevision.ID)
		if err != nil {
			return selectedRemediationTicketInput{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		selected.Dependencies = append(selected.Dependencies, selectedRemediationTicketDependency{Sequence: dependency.Sequence, DependencyRevisionRowID: dependency.DependsOnRevisionRowID, DependencyOutcome: dependency.Outcome})
		completed[index] = completedDependencyOutcome{Sequence: dependency.Sequence, DependencyTicketID: dependencyTicket.TicketID, DependencyRevisionRowID: dependencyRevision.ID, DependencyRevisionNumber: dependencyRevision.RevisionNumber, DeclaredOutcome: dependency.Outcome, CurrentDependencyRevision: completedDependencyRevision{TicketID: dependencyTicket.TicketID, RevisionRowID: dependencyRevision.ID, RevisionNumber: dependencyRevision.RevisionNumber}, Completion: completedDependencyCompletion{SatisfactionRowID: satisfaction.ID, AuditTicketRevisionDecisionRowID: satisfaction.AuditTicketRevisionDecisionRowID}}
	}
	return selected, completed, nil
}

func (s *LifecycleService) readSelectedRemediationTicketArtifact(ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision, filename, mediaType string) (selectedRemediationTicketArtifact, error) {
	relative := filepath.ToSlash(filepath.Join("delivery-tickets", ticket.TicketID, "revisions", fmt.Sprint(revision.RevisionNumber), filename))
	root := s.store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return selectedRemediationTicketArtifact{}, &Error{Code: CodeInvalidPacketDocument}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return selectedRemediationTicketArtifact{}, &Error{Code: CodeInvalidPacketDocument}
	}
	if filename == "delivery-ticket.json" && mediaType != "application/json" || filename == "delivery-ticket.md" && mediaType != "text/markdown" {
		return selectedRemediationTicketArtifact{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return selectedRemediationTicketArtifact{RelativePath: relative, SHA256: digestBytes(data), SizeBytes: int64(len(data)), BytesBase64: base64.StdEncoding.EncodeToString(data)}, nil
}

func containsExecutionPackageMember(values []workflowstore.ExecutionPackageMember, memberID, revisionID int64) bool {
	for _, value := range values {
		if value.ID == memberID && value.RevisionRowID == revisionID {
			return true
		}
	}
	return false
}

func (s *LifecycleService) currentApprovedAuthority(ctx context.Context, workspaceRowID int64) ([]byte, currentApprovedAuthorityInput, error) {
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, workspaceRowID)
	if err != nil || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	authorityService, err := featureapp.NewService(s.store)
	if err != nil {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	history, err := authorityService.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	var current *featureapp.AuthorityRevisionDetail
	for index := range history {
		if history[index].Revision.ID == workspace.CurrentAuthorityRevisionRowID.Int64 {
			if current != nil {
				return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
			}
			current = &history[index]
		}
	}
	if current == nil || current.Revision.WorkspaceRowID != workspace.ID || current.Revision.ID == 0 {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	if !current.Revision.SourceClosureRowID.Valid {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, current.Revision.SourceClosureRowID.Int64)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID == "" || closure.CommitOID == "" {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	if len(current.Layers) == 0 {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	authorityDocument := retainedAuthorityDocument{AuthorityRevisionID: current.Revision.AuthorityRevisionID, Layers: make([]retainedAuthorityLayer, len(current.Layers))}
	for index, layer := range current.Layers {
		data, err := s.readFeatureAuthorityLayer(ctx, layer)
		if err != nil || digestBytes(data) != layer.ArtifactSha256 {
			return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
		}
		authorityDocument.Layers[index] = retainedAuthorityLayer{Sequence: layer.Sequence, LayerKind: layer.LayerKind, ArtifactSHA256: layer.ArtifactSha256, BytesBase64: base64.StdEncoding.EncodeToString(data)}
	}
	authorityBytes, err := canonicalJSON(authorityDocument)
	if err != nil {
		return nil, currentApprovedAuthorityInput{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return authorityBytes, currentApprovedAuthorityInput{
		FeatureWorkspaceID: workspace.WorkspaceID, CurrentAuthorityRevisionID: current.Revision.AuthorityRevisionID,
		SourceClosureID: closure.ClosureID, AuthorityBytes: base64.StdEncoding.EncodeToString(authorityBytes),
		AuthorityByteDigest: digestBytes(authorityBytes), SourceClosureCommit: closure.CommitOID,
	}, nil
}

func (s *LifecycleService) readFeatureAuthorityLayer(ctx context.Context, layer workflowstore.FeatureWorkspaceAuthorityLayer) ([]byte, error) {
	if layer.ArtifactRowID.Valid == layer.RetainedArtifactRowID.Valid || layer.ArtifactSha256 == "" {
		return nil, errors.New("authority layer artifact identity is invalid")
	}
	if layer.ArtifactRowID.Valid {
		artifact, err := s.store.GetArtifactByRowID(ctx, layer.ArtifactRowID.Int64)
		if err != nil || artifact.SHA256 != layer.ArtifactSha256 {
			return nil, errors.New("authority layer artifact is invalid")
		}
		return readWorkflowArtifact(s.store, artifact)
	}
	retained, err := s.store.GetOperationPacketRetainedArtifactByRowID(ctx, layer.RetainedArtifactRowID.Int64)
	if err != nil || retained.SHA256 != layer.ArtifactSha256 {
		return nil, errors.New("authority layer retained artifact is invalid")
	}
	root := s.store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(retained.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("authority layer retained artifact path is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != retained.SizeBytes || digestBytes(data) != retained.SHA256 {
		return nil, errors.New("authority layer retained artifact is unavailable")
	}
	return data, nil
}

func (s *LifecycleService) prepareWorkflowReferences(ctx context.Context, project workflowstore.Project, requests []semanticidentity.WorkflowReferenceRequest) (workflowPreparation, error) {
	result := workflowPreparation{}
	seenKinds := make(map[registry.WorkflowReferenceKind]struct{}, len(requests))
	seenIdentities := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		reference, err := s.resolveWorkflowReference(ctx, project, request)
		if err != nil {
			return workflowPreparation{}, err
		}
		if _, duplicate := seenKinds[reference.Kind]; duplicate {
			return workflowPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
		seenKinds[reference.Kind] = struct{}{}
		key := workflowReferenceKey(reference)
		if key == "" {
			return workflowPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
		identityKey := string(reference.Kind) + "\x00" + key
		if _, duplicate := seenIdentities[identityKey]; duplicate {
			return workflowPreparation{}, &Error{Code: CodeInvalidPacketDocument}
		}
		seenIdentities[identityKey] = struct{}{}
		result.references = append(result.references, reference)
	}
	return result, nil
}

func (s *LifecycleService) resolveWorkflowReference(ctx context.Context, project workflowstore.Project, request semanticidentity.WorkflowReferenceRequest) (packet.WorkflowReference, error) {
	switch request.Kind {
	case "feature_workspace":
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, request.WorkspaceID)
		if err != nil || workspace.ProjectRowID != project.ID || !workspace.CurrentRouteStateRowID.Valid {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		route, err := s.store.GetFeatureWorkspaceRouteStateByRowID(ctx, workspace.CurrentRouteStateRowID.Int64)
		if err != nil || route.ID != workspace.CurrentRouteStateRowID.Int64 || route.WorkspaceRowID != workspace.ID || route.RouteStateID == "" || route.Sequence < 1 || route.WorkspaceVersion < 1 || route.State == "" {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "feature_workspace", WorkspaceID: workspace.WorkspaceID, WorkspaceVersion: workspace.Version, RouteStateID: route.RouteStateID, RouteSequence: route.Sequence, RouteWorkspaceVersion: route.WorkspaceVersion, RouteState: route.State}, nil
	case "delivery_ticket":
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, request.WorkspaceID)
		if err != nil || workspace.ProjectRowID != project.ID {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		ticket, err := s.store.GetDeliveryTicketByTicketID(ctx, request.TicketID)
		if err != nil || ticket.WorkspaceRowID != workspace.ID || !ticket.CurrentRevisionRowID.Valid {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
		if err != nil || revision.ID != ticket.CurrentRevisionRowID.Int64 || revision.DeliveryTicketRowID != ticket.ID || revision.CancellationReason.Valid || revision.ID < 1 || revision.RevisionNumber < 1 {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		closure, err := s.store.GetSourceVaultClosureByRowID(ctx, revision.SourceClosureRowID)
		if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID == "" {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "delivery_ticket", WorkspaceID: workspace.WorkspaceID, TicketID: ticket.TicketID, RevisionID: revision.ID, RevisionNumber: revision.RevisionNumber, SourceClosureID: closure.ClosureID}, nil
	case "run":
		run, err := s.store.GetRunByRunID(ctx, request.RunID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		artifacts, listErr := s.store.ListArtifactsByRun(ctx, run.ID)
		if !run.CanonicalSHA256.Valid {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		artifact, err := findArtifactBySHA(artifacts, run.CanonicalSHA256.String, listErr)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		return packet.WorkflowReference{Kind: "run", RunID: run.RunID, ExecutionSpecArtifactID: artifact.ArtifactID, ExecutionSpecSHA256: artifact.SHA256}, nil
	case "audit_decision":
		run, err := s.store.GetRunByRunID(ctx, request.RunID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		value, err := s.store.GetAuditDecisionByDecisionID(ctx, request.AuditDecisionID)
		if err != nil || value.RunRowID != run.ID {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "audit_decision", RunID: run.RunID, AuditDecisionID: value.AuditDecisionID, Decision: value.Decision, RecordedAt: canonicalPersistedTime(value.CreatedAt)}, nil
	default:
		return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
	}
}

func canonicalPersistedTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return canonicalTime(parsed)
}

func (s *LifecycleService) materializeWorkflowRecord(ctx context.Context, source semanticidentity.WorkflowRecordInputReference) (packet.WorkflowRecordReference, []byte, error) {
	var reference packet.WorkflowRecordReference
	var data []byte
	var err error
	switch source.Kind {
	case "plan_artifact":
		plan, loadErr := s.store.GetPlanByPlanID(ctx, source.PlanID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		artifact, loadErr := s.store.GetArtifactByArtifactID(ctx, source.ArtifactID)
		if loadErr != nil || !artifact.PlanRowID.Valid || artifact.PlanRowID.Int64 != plan.ID || artifact.SHA256 != source.ExpectedSHA256 {
			return packet.WorkflowRecordReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = packet.WorkflowRecordReference{Kind: source.Kind, PlanID: plan.PlanID, ArtifactID: artifact.ArtifactID, ArtifactSHA256: artifact.SHA256}
	case "pass_record":
		plan, loadErr := s.store.GetPlanByPlanID(ctx, source.PlanID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		pass, loadErr := s.store.GetPlanPassByPassID(ctx, source.PassID)
		if loadErr != nil || pass.PlanRowID != plan.ID {
			return packet.WorkflowRecordReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		data, err = canonicalJSON(struct {
			PlanID     string `json:"plan_id"`
			PassID     string `json:"pass_id"`
			PassNumber int64  `json:"pass_number"`
			Name       string `json:"name"`
			RepoTarget string `json:"repo_target"`
			Status     string `json:"status"`
		}{plan.PlanID, pass.PassID, pass.PassNumber, pass.Name, pass.RepoTarget, pass.Status})
		reference = packet.WorkflowRecordReference{Kind: source.Kind, PlanID: plan.PlanID, PassID: pass.PassID, PassNumber: pass.PassNumber}
	case "run_execution_spec":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		artifact, loadErr := s.store.GetArtifactByArtifactID(ctx, source.ArtifactID)
		if loadErr != nil || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.SHA256 != source.ExpectedSHA256 {
			return packet.WorkflowRecordReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = packet.WorkflowRecordReference{Kind: source.Kind, RunID: run.RunID, ArtifactID: artifact.ArtifactID, ArtifactSHA256: artifact.SHA256}
	case "audit_packet":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		value, loadErr := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
		if loadErr != nil || value.AuditPacketID != source.AuditPacketID || value.PacketSHA256 != source.ExpectedSHA256 {
			return packet.WorkflowRecordReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		artifact, loadErr := s.store.GetArtifactByRowID(ctx, value.ArtifactRowID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = packet.WorkflowRecordReference{Kind: source.Kind, RunID: run.RunID, AuditPacketID: value.AuditPacketID, AuditPacketSHA256: value.PacketSHA256}
	case "audit_decision":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowRecordReference{}, nil, loadErr
		}
		value, loadErr := s.store.GetAuditDecisionByDecisionID(ctx, source.AuditDecisionID)
		if loadErr != nil || value.RunRowID != run.ID {
			return packet.WorkflowRecordReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		data, err = canonicalJSON(struct {
			AuditDecisionID string `json:"audit_decision_id"`
			RunID           string `json:"run_id"`
			AuditedCommit   string `json:"audited_commit"`
			PacketSHA256    string `json:"packet_sha256"`
			Decision        string `json:"decision"`
			RecordedAt      string `json:"recorded_at"`
		}{value.AuditDecisionID, run.RunID, value.AuditedCommit, value.PacketSHA256, value.Decision, value.CreatedAt})
		reference = packet.WorkflowRecordReference{Kind: source.Kind, RunID: run.RunID, AuditDecisionID: value.AuditDecisionID, Decision: value.Decision, RecordedAt: canonicalPersistedTime(value.CreatedAt)}
	default:
		return packet.WorkflowRecordReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if err != nil || reference.Kind == "" {
		return packet.WorkflowRecordReference{}, nil, errOrInvalid(err)
	}
	return reference, data, nil
}

func (s *LifecycleService) resolveCommittedRevision(ctx context.Context, repositoryKey, selector string, repositories repositoryPreparation) (sourcevault.ImportResult, workflowrepos.ResolvedRevision, error) {
	for _, binding := range repositories.bindings {
		if binding.RepositoryKey != repositoryKey {
			continue
		}
		switch {
		case selector == "primary":
			prepared, ok := repositories.primary[repositoryKey]
			if !ok {
				return sourcevault.ImportResult{}, workflowrepos.ResolvedRevision{}, &Error{Code: CodeInvalidPacketDocument}
			}
			return prepared, repositories.primaryRevision[repositoryKey], nil
		case strings.HasPrefix(selector, "anchor:"):
			name := strings.TrimPrefix(selector, "anchor:")
			prepared, ok := repositories.anchors[repositoryKey][name]
			if !ok {
				return sourcevault.ImportResult{}, workflowrepos.ResolvedRevision{}, &Error{Code: CodeInvalidPacketDocument}
			}
			return prepared, repositories.anchorRevision[repositoryKey][name], nil
		case strings.HasPrefix(selector, "commit:"):
			commit := strings.TrimPrefix(selector, "commit:")
			cacheKey := repositoryKey + "\x00" + commit
			if prepared, ok := repositories.direct[cacheKey]; ok {
				return prepared, repositories.directRevision[cacheKey], nil
			}
			revision, err := s.repositories.ResolveRevision(ctx, workflowrepos.RevisionRequest{RepoTarget: binding.RepositoryTarget, ExplicitCommitOID: commit})
			if err != nil {
				return sourcevault.ImportResult{}, workflowrepos.ResolvedRevision{}, err
			}
			prepared, err := s.vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: revision})
			if err != nil {
				return sourcevault.ImportResult{}, workflowrepos.ResolvedRevision{}, err
			}
			repositories.direct[cacheKey] = prepared
			repositories.directRevision[cacheKey] = revision
			return prepared, revision, nil
		}
	}
	return sourcevault.ImportResult{}, workflowrepos.ResolvedRevision{}, &Error{Code: CodeInvalidPacketDocument}
}

func (b *retainedBuilder) add(kind, mediaType string, data []byte, dependencyClass, dependencyKey string) string {
	artifactID := b.ids.ArtifactID()
	index := len(b.artifacts) + 1
	relativePath := fmt.Sprintf("retained/%04d.bin", index)
	b.artifacts = append(b.artifacts, PublicationArtifactInput{ArtifactID: artifactID, Kind: kind, RelativePath: relativePath, MediaType: mediaType, Bytes: append([]byte(nil), data...)})
	b.sequence++
	b.bindings = append(b.bindings, PublicationBindingInput{Sequence: b.sequence, DependencyClass: dependencyClass, DependencyKey: dependencyKey, ArtifactID: artifactID})
	return artifactID
}

func materializeAttestations(values []semanticidentity.AttestationRequest) []packet.Attestation {
	out := make([]packet.Attestation, 0, len(values))
	for _, value := range values {
		var clearance *packet.SensitiveDataClearance
		if value.Clearance != nil {
			copy := *value.Clearance
			clearance = &copy
		}
		out = append(out, packet.Attestation{Kind: registry.AttestationKind(value.Kind), InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Confirmed: value.Confirmed, Approved: value.Approved, CompleteTransfer: value.CompleteTransfer, SelectedMode: value.SelectedMode, ReviewedCandidateSHA256: value.ReviewedCandidateSHA256, ReviewResult: value.ReviewResult, Complete: value.Complete, Clearance: clearance})
	}
	return out
}

func operationSlots(operation registry.OperationDefinition, refreshing bool) map[string]registry.InputSlotDefinition {
	out := make(map[string]registry.InputSlotDefinition)
	for _, value := range operation.RequiredInputs {
		out[value.InputName] = value
	}
	if refreshing {
		for _, value := range operation.ConditionalRefreshInputs {
			out[value.InputName] = value
		}
	}
	return out
}

func repositoryBinding(key string, order int64, revision workflowrepos.ResolvedRevision) packet.RepositoryBinding {
	return packet.RepositoryBinding{RepositoryKey: key, RepositoryTarget: revision.RepositoryTarget.RepoTarget, BindingOrder: order, RevisionSource: revision.RevisionSource, ConfiguredWorkingBranchRef: revision.ConfiguredWorkingBranchRef, RepositoryTargetConfigurationVersion: revision.RepositoryTargetConfigurationVersion, CommitOID: revision.CommitOID, TreeOID: revision.TreeOID}
}

func requiresCleanProject(policy registry.SourcePolicy) bool {
	return strings.Contains(string(policy), "current_clean_project_required_source")
}

func governanceManifestPath(role registry.Role) string {
	if role == "auditor" {
		return "auditor-source-manifest.json"
	}
	return "planner-source-manifest.json"
}

func mustSurfaceManifest(surface registry.SurfaceContractID) string {
	value, _ := registry.RouteContractSHA256(surface)
	return value
}

func pathIdentity(value []byte) packet.PathIdentity {
	digest := sha256.New()
	_, _ = digest.Write([]byte("relay.git-path.v1"))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(value)
	identity := packet.PathIdentity{PathID: hex.EncodeToString(digest.Sum(nil)), ByteLength: int64(len(value))}
	if len(value) <= 8192 {
		identity.PathBytesBase64 = base64.StdEncoding.EncodeToString(value)
	}
	return identity
}

func resolvePathSelector(value *semanticidentity.SourcePathSelector, prior map[string][]byte) ([]byte, error) {
	if value == nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if value.PathBytesBase64 != "" {
		decoded, err := base64.StdEncoding.Strict().DecodeString(value.PathBytesBase64)
		if err != nil || base64.StdEncoding.EncodeToString(decoded) != value.PathBytesBase64 || len(decoded) == 0 || strings.IndexByte(string(decoded), 0) >= 0 {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		return decoded, nil
	}
	if value.PathID != "" {
		decoded, ok := prior[value.PathID]
		if !ok {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		return append([]byte(nil), decoded...), nil
	}
	return nil, &Error{Code: CodeInvalidPacketDocument}
}

func priorPathIdentities(prior *PacketView) map[string][]byte {
	result := make(map[string][]byte)
	if prior == nil {
		return result
	}
	var envelope struct {
		Inputs []struct {
			Source struct {
				Path struct {
					PathID          string `json:"path_id"`
					ByteLength      int64  `json:"byte_length"`
					PathBytesBase64 string `json:"path_bytes_base64"`
				} `json:"path"`
			} `json:"source"`
		} `json:"inputs"`
	}
	if json.Unmarshal(prior.DocumentBytes, &envelope) != nil {
		return result
	}
	for _, input := range envelope.Inputs {
		path := input.Source.Path
		if path.PathID == "" || path.PathBytesBase64 == "" || path.ByteLength < 0 {
			continue
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(path.PathBytesBase64)
		if err == nil && int64(len(decoded)) == path.ByteLength && pathIdentity(decoded).PathID == path.PathID {
			result[path.PathID] = decoded
		}
	}
	return result
}

func readWorkflowArtifact(store *workflowstore.Store, artifact workflowstore.Artifact) ([]byte, error) {
	root := store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("artifact unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != artifact.SizeBytes || digestBytes(data) != artifact.SHA256 {
		return nil, errors.New("artifact unavailable")
	}
	return data, nil
}

func findArtifactBySHA(values []workflowstore.Artifact, sha string, err error) (workflowstore.Artifact, error) {
	if err != nil {
		return workflowstore.Artifact{}, err
	}
	var found workflowstore.Artifact
	for _, value := range values {
		if value.SHA256 == sha {
			if found.ID != 0 {
				return workflowstore.Artifact{}, &Error{Code: CodeInvalidPacketDocument}
			}
			found = value
		}
	}
	if found.ID == 0 {
		return workflowstore.Artifact{}, sql.ErrNoRows
	}
	return found, nil
}

func workflowReferenceKey(value packet.WorkflowReference) string {
	switch value.Kind {
	case "feature_workspace":
		return value.WorkspaceID
	case "delivery_ticket":
		return value.WorkspaceID + "\x00" + value.TicketID
	case "run":
		return value.RunID
	case "audit_decision":
		return value.RunID + "\x00" + value.AuditDecisionID
	default:
		return ""
	}
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func auditDerivedSections(data []byte) (map[string][]byte, error) {
	var packetValue map[string]json.RawMessage
	if err := json.Unmarshal(data, &packetValue); err != nil {
		return nil, err
	}
	var authority map[string]json.RawMessage
	if err := json.Unmarshal(packetValue["authority"], &authority); err != nil {
		return nil, err
	}
	var executionSpec struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(authority["execution_spec"], &executionSpec); err != nil {
		return nil, err
	}
	var executorBrief struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(authority["executor_brief"], &executorBrief); err != nil {
		return nil, err
	}
	values := map[string]any{
		"current_audit_packet":    json.RawMessage(data),
		"packet_intent":           json.RawMessage(authority["managed_context"]),
		"original_execution_spec": executionSpec.Content,
		"derived_executor_brief":  map[string]string{"content": executorBrief.Content},
		"implementation_evidence": json.RawMessage(packetValue["execution"]),
		"validation_evidence":     json.RawMessage(packetValue["validation"]),
	}
	out := make(map[string][]byte, len(values))
	for key, value := range values {
		encoded, err := canonicalJSON(value)
		if err != nil {
			return nil, err
		}
		out[key] = encoded
	}
	return out, nil
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func validatePreparedEdges(bindings []PublicationBindingInput, vaults []PublicationVaultInput) error {
	seen := make(map[string]struct{})
	for _, value := range bindings {
		key := value.DependencyClass + "\x00" + value.DependencyKey
		if _, ok := seen[key]; ok {
			return errors.New("duplicate dependency edge")
		}
		seen[key] = struct{}{}
	}
	for _, value := range vaults {
		key := value.DependencyClass + "\x00" + value.DependencyKey
		if _, ok := seen[key]; ok {
			return errors.New("duplicate dependency edge")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func errOrInvalid(err error) error {
	if err != nil {
		return err
	}
	return &Error{Code: CodeInvalidPacketDocument}
}
