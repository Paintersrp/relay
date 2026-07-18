package operations

import (
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
	byKey      map[string]packet.WorkflowReference
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

	workflow, err := s.prepareWorkflowReferences(ctx, request.workflowReferences)
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
	inputs = append(inputs, derived...)
	attestations := materializeAttestations(request.attestations)
	if err := s.revalidateRepositoryAuthority(ctx, operation, repositories, governanceRevision); err != nil {
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
			return repositoryPreparation{}, err
		}
		imported, err := s.vaults.ImportClosure(ctx, sourcevault.ImportRequest{Revision: revision})
		if err != nil {
			return repositoryPreparation{}, err
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

func (s *LifecycleService) prepareGovernance(ctx context.Context, packetID string, operation registry.OperationDefinition, request lifecycleRequest, repositories repositoryPreparation) (packet.GovernanceBinding, packet.ManifestDomainBinding, []PublicationVaultInput, workflowrepos.ResolvedRevision, error) {
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
			reference, data, err := s.materializeWorkflowRecord(ctx, *source.Source.WorkflowRecord, workflow)
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

func (s *LifecycleService) materializeDerivedInputs(ctx context.Context, operation registry.OperationDefinition, workflow workflowPreparation, builder *retainedBuilder) ([]packet.InputBinding, error) {
	if len(operation.DerivedInputs) == 0 {
		return nil, nil
	}
	if operation.OperationID != "auditor.audit" {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var runReference packet.WorkflowReference
	for _, value := range workflow.references {
		if value.Kind == "run" {
			runReference = value
			break
		}
	}
	if runReference.RunID == "" {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	run, err := s.store.GetRunByRunID(ctx, runReference.RunID)
	if err != nil {
		return nil, err
	}
	auditPacket, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	artifact, err := s.store.GetArtifactByRowID(ctx, auditPacket.ArtifactRowID)
	if err != nil {
		return nil, err
	}
	data, err := readWorkflowArtifact(s.store, artifact)
	if err != nil {
		return nil, err
	}
	sections, err := auditDerivedSections(data)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	inputs := make([]packet.InputBinding, 0, len(operation.DerivedInputs))
	for _, slot := range operation.DerivedInputs {
		section, ok := sections[slot.InputName]
		if !ok {
			return nil, &Error{Code: CodeInvalidPacketDocument}
		}
		artifactID := builder.add(workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot, "application/json", section, workflowstore.OperationPacketDependencyWorkflowSnapshot, slot.InputName)
		inputs = append(inputs, packet.InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: packet.InputSourceInlineText, DisplayName: slot.InputName + ".json", MediaType: "application/json", SHA256: digestBytes(section), SizeBytes: int64(len(section)), AttestationKind: slot.AttestationKind, Source: packet.InputSource{Kind: packet.InputSourceInlineText, ArtifactID: artifactID}})
	}
	return inputs, nil
}

func (s *LifecycleService) prepareWorkflowReferences(ctx context.Context, requests []semanticidentity.WorkflowReferenceRequest) (workflowPreparation, error) {
	result := workflowPreparation{byKey: make(map[string]packet.WorkflowReference)}
	for _, request := range requests {
		reference, err := s.resolveWorkflowReference(ctx, request)
		if err != nil {
			return workflowPreparation{}, err
		}
		result.references = append(result.references, reference)
		result.byKey[workflowReferenceKey(reference)] = reference
	}
	return result, nil
}

func (s *LifecycleService) resolveWorkflowReference(ctx context.Context, request semanticidentity.WorkflowReferenceRequest) (packet.WorkflowReference, error) {
	switch request.Kind {
	case "plan":
		plan, err := s.store.GetPlanByPlanID(ctx, request.PlanID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		artifacts, listErr := s.store.ListArtifactsByPlan(ctx, plan.ID)
		artifact, err := findArtifactBySHA(artifacts, plan.CanonicalSHA256, listErr)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		return packet.WorkflowReference{Kind: "plan", PlanID: plan.PlanID, CanonicalArtifactID: artifact.ArtifactID, CanonicalArtifactSHA256: artifact.SHA256}, nil
	case "pass":
		plan, err := s.store.GetPlanByPlanID(ctx, request.PlanID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		pass, err := s.store.GetPlanPassByPassID(ctx, request.PassID)
		if err != nil || pass.PlanRowID != plan.ID {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "pass", PlanID: plan.PlanID, PassID: pass.PassID, PassNumber: pass.PassNumber}, nil
	case "run":
		run, err := s.store.GetRunByRunID(ctx, request.RunID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		artifacts, listErr := s.store.ListArtifactsByRun(ctx, run.ID)
		artifact, err := findArtifactBySHA(artifacts, run.CanonicalSHA256, listErr)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		return packet.WorkflowReference{Kind: "run", RunID: run.RunID, ExecutionSpecArtifactID: artifact.ArtifactID, ExecutionSpecSHA256: artifact.SHA256}, nil
	case "audit_packet":
		run, err := s.store.GetRunByRunID(ctx, request.RunID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		value, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
		if err != nil || value.AuditPacketID != request.AuditPacketID || value.PacketSHA256 != request.ExpectedAuditPacketSHA256 {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "audit_packet", RunID: run.RunID, AuditPacketID: value.AuditPacketID, AuditPacketSHA256: value.PacketSHA256}, nil
	case "audit_decision":
		run, err := s.store.GetRunByRunID(ctx, request.RunID)
		if err != nil {
			return packet.WorkflowReference{}, err
		}
		value, err := s.store.GetAuditDecisionByDecisionID(ctx, request.AuditDecisionID)
		if err != nil || value.RunRowID != run.ID {
			return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
		}
		return packet.WorkflowReference{Kind: "audit_decision", RunID: run.RunID, AuditDecisionID: value.AuditDecisionID, Decision: value.Decision, RecordedAt: value.CreatedAt}, nil
	default:
		return packet.WorkflowReference{}, &Error{Code: CodeInvalidPacketDocument}
	}
}

func (s *LifecycleService) materializeWorkflowRecord(ctx context.Context, source semanticidentity.WorkflowRecordInputReference, workflow workflowPreparation) (packet.WorkflowReference, []byte, error) {
	var reference packet.WorkflowReference
	var data []byte
	var err error
	switch source.Kind {
	case "plan_artifact":
		plan, loadErr := s.store.GetPlanByPlanID(ctx, source.PlanID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		artifact, loadErr := s.store.GetArtifactByArtifactID(ctx, source.ArtifactID)
		if loadErr != nil || !artifact.PlanRowID.Valid || artifact.PlanRowID.Int64 != plan.ID || artifact.SHA256 != source.ExpectedSHA256 {
			return packet.WorkflowReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = workflow.byKey["plan\x00"+plan.PlanID]
	case "pass_record":
		plan, loadErr := s.store.GetPlanByPlanID(ctx, source.PlanID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		pass, loadErr := s.store.GetPlanPassByPassID(ctx, source.PassID)
		if loadErr != nil || pass.PlanRowID != plan.ID {
			return packet.WorkflowReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		data, err = canonicalJSON(struct {
			PlanID     string `json:"plan_id"`
			PassID     string `json:"pass_id"`
			PassNumber int64  `json:"pass_number"`
			Name       string `json:"name"`
			RepoTarget string `json:"repo_target"`
			Status     string `json:"status"`
		}{plan.PlanID, pass.PassID, pass.PassNumber, pass.Name, pass.RepoTarget, pass.Status})
		reference = workflow.byKey["pass\x00"+plan.PlanID+"\x00"+pass.PassID]
	case "run_execution_spec":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		artifact, loadErr := s.store.GetArtifactByArtifactID(ctx, source.ArtifactID)
		if loadErr != nil || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.SHA256 != source.ExpectedSHA256 {
			return packet.WorkflowReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = workflow.byKey["run\x00"+run.RunID]
	case "audit_packet":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		value, loadErr := s.store.GetCurrentAuditPacketByRun(ctx, run.ID)
		if loadErr != nil || value.AuditPacketID != source.AuditPacketID || value.PacketSHA256 != source.ExpectedSHA256 {
			return packet.WorkflowReference{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyWorkflowSnapshot)
		}
		artifact, loadErr := s.store.GetArtifactByRowID(ctx, value.ArtifactRowID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		data, err = readWorkflowArtifact(s.store, artifact)
		reference = workflow.byKey["audit_packet\x00"+run.RunID+"\x00"+value.AuditPacketID]
	case "audit_decision":
		run, loadErr := s.store.GetRunByRunID(ctx, source.RunID)
		if loadErr != nil {
			return packet.WorkflowReference{}, nil, loadErr
		}
		value, loadErr := s.store.GetAuditDecisionByDecisionID(ctx, source.AuditDecisionID)
		if loadErr != nil || value.RunRowID != run.ID {
			return packet.WorkflowReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
		}
		data, err = canonicalJSON(struct {
			AuditDecisionID string `json:"audit_decision_id"`
			RunID           string `json:"run_id"`
			AuditedCommit   string `json:"audited_commit"`
			PacketSHA256    string `json:"packet_sha256"`
			Decision        string `json:"decision"`
			RecordedAt      string `json:"recorded_at"`
		}{value.AuditDecisionID, run.RunID, value.AuditedCommit, value.PacketSHA256, value.Decision, value.CreatedAt})
		reference = workflow.byKey["audit_decision\x00"+run.RunID+"\x00"+value.AuditDecisionID]
	default:
		return packet.WorkflowReference{}, nil, &Error{Code: CodeInvalidPacketDocument}
	}
	if err != nil || reference.Kind == "" {
		return packet.WorkflowReference{}, nil, errOrInvalid(err)
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
	value, _ := registry.SurfaceManifestSHA256(surface)
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
	case "plan":
		return "plan\x00" + value.PlanID
	case "pass":
		return "pass\x00" + value.PlanID + "\x00" + value.PassID
	case "run":
		return "run\x00" + value.RunID
	case "audit_packet":
		return "audit_packet\x00" + value.RunID + "\x00" + value.AuditPacketID
	case "audit_decision":
		return "audit_decision\x00" + value.RunID + "\x00" + value.AuditDecisionID
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
