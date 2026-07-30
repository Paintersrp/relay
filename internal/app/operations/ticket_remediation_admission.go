package operations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/app/tickets"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

const (
	remediationPlannerOperation = registry.OperationID("planner.delivery_ticket_remediation")
	remediationPlannerSurface   = registry.SurfaceContractID("planner-authoring.v1")

	remediationSeedInputName   = "remediation_seed"
	currentAuthorityInputName  = "current_approved_authority"
	workflowSnapshotDependency = workflowstore.OperationPacketDependencyWorkflowSnapshot
)

// ticketAuthoringPacketReader is intentionally read-only. It is implemented by
// the application packet service and is optional for ordinary ticket tests and
// publications.
type ticketAuthoringPacketReader interface {
	Get(context.Context, string) (PacketView, error)
	ReadVerifiedRetainedInput(context.Context, string, string) ([]byte, error)
}

func (s *TicketWorkflowService) verifyRemediationAuthoring(ctx context.Context, input tickets.PublishInput, ref RemediationAuthoringReference) error {
	reader, ok := s.packets.(ticketAuthoringPacketReader)
	if !ok {
		return ErrTicketAdmission
	}
	view, err := reader.Get(ctx, ref.PacketID)
	if err != nil {
		return err
	}
	if view.Summary.PacketID != ref.PacketID || view.Summary.PacketSHA256 != ref.ExpectedPacketSHA256 ||
		view.Summary.Role != registry.Role("planner") || view.Summary.SurfaceContract != remediationPlannerSurface ||
		view.Summary.OperationID != remediationPlannerOperation || view.Summary.ReadinessState != workflowstore.OperationPacketReadinessReady ||
		view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || view.Summary.ReplacementPacket != nil ||
		view.Summary.SupersededAt != nil || view.Summary.ClosedAt != nil {
		return ErrTicketAdmission
	}

	document, err := decodeCanonicalAuthoringPacket(view.DocumentBytes, view.Summary.PacketSHA256)
	if err != nil || document.Role != registry.Role("planner") || document.SurfaceContract != remediationPlannerSurface || document.OperationID != remediationPlannerOperation || document.ReadinessState != packet.ReadinessReady {
		return ErrTicketAdmission
	}
	if len(document.WorkflowReferences) != 1 || document.WorkflowReferences[0].Kind != "audit_decision" ||
		document.WorkflowReferences[0].AuditDecisionID == "" || document.WorkflowReferences[0].RunID == "" {
		return ErrTicketAdmission
	}
	if len(document.Inputs) != 2 {
		return ErrTicketAdmission
	}
	seen := make(map[string]struct{}, len(document.Inputs))
	for _, derived := range document.Inputs {
		if derived.InputRole != registry.InputRole("governing") || derived.SourceKind != packet.InputSourceInlineText || derived.Source.Kind != packet.InputSourceInlineText ||
			(derived.InputName != remediationSeedInputName && derived.InputName != currentAuthorityInputName) {
			return ErrTicketAdmission
		}
		if _, duplicate := seen[derived.InputName]; duplicate {
			return ErrTicketAdmission
		}
		seen[derived.InputName] = struct{}{}
	}
	if len(seen) != 2 {
		return ErrTicketAdmission
	}

	seedBytes, err := reader.ReadVerifiedRetainedInput(ctx, ref.PacketID, remediationSeedInputName)
	if err != nil {
		return err
	}
	authorityBytes, err := reader.ReadVerifiedRetainedInput(ctx, ref.PacketID, currentAuthorityInputName)
	if err != nil {
		return err
	}
	seed, err := decodeRemediationSeed(seedBytes)
	if err != nil || !validateRemediationSeed(seed, input.RemediationSeedID, document.WorkflowReferences[0].AuditDecisionID) {
		return ErrTicketAdmission
	}
	authority, err := decodeCurrentApprovedAuthority(authorityBytes)
	if err != nil {
		return ErrTicketAdmission
	}
	return s.validateCurrentApprovedAuthority(ctx, input, authority)
}

func (s *Service) ReadVerifiedRetainedInput(ctx context.Context, packetID, inputName string) ([]byte, error) {
	value, err := s.loadPacket(ctx, strings.TrimSpace(packetID))
	if err != nil {
		return nil, err
	}
	if value.LifecycleState != workflowstore.OperationPacketLifecycleActive {
		return nil, lifecycleMutationError(ctx, s.store, value)
	}
	if value.ReadinessState != workflowstore.OperationPacketReadinessReady {
		return nil, &Error{Code: CodePacketNotReady}
	}
	_, data, err := s.loadVerifiedPacketDocument(ctx, value)
	if err != nil {
		return nil, err
	}
	document, err := decodeCanonicalAuthoringPacket(data, value.PacketSHA256)
	if err != nil {
		return nil, &Error{Code: CodeInvalidPacketDocument}
	}
	var found packet.InputBinding
	count := 0
	for _, input := range document.Inputs {
		if input.InputName == inputName {
			found = input
			count++
		}
	}
	if count != 1 || found.SourceKind != packet.InputSourceInlineText || found.Source.Kind != packet.InputSourceInlineText || found.Source.ArtifactID == "" {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	dependency, err := s.authorizeDependency(ctx, value, workflowSnapshotDependency, inputName)
	if err != nil || !dependency.OwnerIdentity.Valid || dependency.OwnerIdentity.String != found.Source.ArtifactID {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	if !value.CoordinatedPublicationID.Valid || value.CoordinatedPublicationID.String == "" {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	integrity, err := s.store.GetOperationPacketPublicationIntegrity(ctx, value.CoordinatedPublicationID.String)
	if err != nil || integrity.Publication.State != workflowstore.OperationPacketPublicationStateCommitted ||
		integrity.Publication.PacketRowID != value.ID || integrity.Packet.PacketID != value.PacketID ||
		integrity.Packet.PacketSHA256 != value.PacketSHA256 || integrity.Packet.PacketArtifactRowID != integrity.PacketArtifact.ID ||
		integrity.Publication.PacketArtifactRowID != integrity.PacketArtifact.ID {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	var binding workflowstore.OperationPacketArtifactBinding
	bindingCount := 0
	for _, candidate := range integrity.Bindings {
		if candidate.PacketRowID == value.ID && candidate.DependencyClass == workflowSnapshotDependency && candidate.DependencyKey == inputName {
			binding = candidate
			bindingCount++
		}
	}
	if bindingCount != 1 || !binding.RetainedArtifactRowID.Valid || binding.RetainedArtifactRowID.Int64 == 0 {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	var retained workflowstore.OperationPacketRetainedArtifact
	retainedCount := 0
	for _, candidate := range integrity.RetainedArtifacts {
		if candidate.ID == binding.RetainedArtifactRowID.Int64 {
			retained = candidate
			retainedCount++
		}
	}
	if retainedCount != 1 || retained.ArtifactID != found.Source.ArtifactID || retained.Kind != workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot ||
		retained.PublicationID != integrity.Publication.PublicationID || binding.PublicationID != integrity.Publication.PublicationID ||
		retained.MediaType != found.MediaType || retained.SHA256 != found.SHA256 || retained.SizeBytes != found.SizeBytes {
		return nil, retainedAuthorityError(workflowSnapshotDependency)
	}
	bytes, err := s.readRetainedArtifact(retained)
	if err != nil {
		return nil, &Error{Code: CodePacketArtifactMismatch}
	}
	return append([]byte(nil), bytes...), nil
}

func (s *Service) readRetainedArtifact(artifact workflowstore.OperationPacketRetainedArtifact) ([]byte, error) {
	root := s.store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("retained artifact path escapes root")
	}
	data, err := os.ReadFile(path)
	if err != nil || packet.VerifyBytes(data, artifact.SHA256, artifact.SizeBytes) != nil {
		return nil, errors.New("retained artifact identity mismatch")
	}
	return data, nil
}

func decodeRemediationSeed(data []byte) (remediationSeedInput, error) {
	var value remediationSeedInput
	if err := decodeStrictJSON(data, &value); err != nil {
		return remediationSeedInput{}, err
	}
	return value, nil
}

func validateRemediationSeed(value remediationSeedInput, expectedSeedID, expectedDecisionID string) bool {
	if value.RemediationSeedID != expectedSeedID || !exactNonBlank(value.AuditDecisionID) || value.AuditDecisionID != expectedDecisionID ||
		!exactNonBlank(value.AuditPacketID) || !exactNonBlank(value.ApprovedExecutionPackage.PackageID) || !validTicketSHA256(value.ApprovedExecutionPackage.PackageSHA256) ||
		!exactNonBlank(value.AuditedDeliveryTicket.TicketID) || value.AuditedDeliveryTicketRevision.RevisionID < 1 || value.AuditedDeliveryTicketRevision.RevisionNumber < 1 ||
		!validGitOID(value.AuditedCommit) || !exactNonBlank(value.DecisionRationale) || len(value.MaterialFindings) == 0 {
		return false
	}
	for index, finding := range value.MaterialFindings {
		if finding.Sequence != int64(index+1) || (finding.UpstreamClassification != "implementation" && finding.UpstreamClassification != "governing_package" && finding.UpstreamClassification != "both") ||
			!exactNonBlank(finding.Summary) || !exactNonBlank(finding.Evidence) || !exactNonBlank(finding.RequiredRemediation) {
			return false
		}
	}
	return true
}

func decodeCurrentApprovedAuthority(data []byte) (currentApprovedAuthorityInput, error) {
	var value currentApprovedAuthorityInput
	if err := decodeStrictJSON(data, &value); err != nil {
		return currentApprovedAuthorityInput{}, err
	}
	if !exactNonBlank(value.FeatureWorkspaceID) || !exactNonBlank(value.CurrentAuthorityRevisionID) || !exactNonBlank(value.SourceClosureID) ||
		!exactNonBlank(value.SourceClosureCommit) || !validTicketSHA256(value.AuthorityByteDigest) || value.AuthorityBytes == "" {
		return currentApprovedAuthorityInput{}, ErrTicketAdmission
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value.AuthorityBytes)
	if err != nil || digestBytes(decoded) != value.AuthorityByteDigest {
		return currentApprovedAuthorityInput{}, ErrTicketAdmission
	}
	var document retainedAuthorityDocument
	if err := decodeStrictJSON(decoded, &document); err != nil || document.AuthorityRevisionID != value.CurrentAuthorityRevisionID || len(document.Layers) == 0 {
		return currentApprovedAuthorityInput{}, ErrTicketAdmission
	}
	canonical, err := canonicalJSON(document)
	if err != nil || string(canonical) != string(decoded) {
		return currentApprovedAuthorityInput{}, ErrTicketAdmission
	}
	for index, layer := range document.Layers {
		if layer.Sequence != int64(index+1) || !exactNonBlank(layer.LayerKind) || !validTicketSHA256(layer.ArtifactSHA256) || layer.BytesBase64 == "" {
			return currentApprovedAuthorityInput{}, ErrTicketAdmission
		}
		bytes, err := base64.StdEncoding.Strict().DecodeString(layer.BytesBase64)
		if err != nil || digestBytes(bytes) != layer.ArtifactSHA256 {
			return currentApprovedAuthorityInput{}, ErrTicketAdmission
		}
	}
	return value, nil
}

func (s *TicketWorkflowService) validateCurrentApprovedAuthority(ctx context.Context, publish tickets.PublishInput, value currentApprovedAuthorityInput) error {
	if value.FeatureWorkspaceID != publish.WorkspaceID {
		return ErrTicketAdmission
	}
	workspace, err := s.currentWorkspace(ctx, publish.WorkspaceID)
	if err != nil || !workspace.CurrentAuthorityRevisionRowID.Valid {
		return ErrTicketAdmission
	}
	authority, err := s.currentAuthority(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil || authority.WorkspaceRowID != workspace.ID || authority.AuthorityRevisionID != value.CurrentAuthorityRevisionID ||
		!authority.SourceClosureRowID.Valid {
		return ErrTicketAdmission
	}
	store := s.packetStore()
	if store == nil {
		return ErrTicketAdmission
	}
	closure, err := store.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID != value.SourceClosureID || closure.CommitOID != value.SourceClosureCommit ||
		publish.Revision.SourceClosureRowID != authority.SourceClosureRowID.Int64 {
		return ErrTicketAdmission
	}
	return nil
}

func (s *TicketWorkflowService) currentWorkspace(ctx context.Context, workspaceID string) (workflowstore.FeatureWorkspace, error) {
	store := s.packetStore()
	if store == nil {
		return workflowstore.FeatureWorkspace{}, ErrTicketAdmission
	}
	return store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
}

func (s *TicketWorkflowService) currentAuthority(ctx context.Context, rowID int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error) {
	store := s.packetStore()
	if store == nil {
		return workflowstore.FeatureWorkspaceAuthorityRevision{}, ErrTicketAdmission
	}
	return store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, rowID)
}

func (s *TicketWorkflowService) packetStore() *workflowstore.Store {
	provider, ok := s.packets.(ticketWorkflowStoreProvider)
	if ok {
		return provider.ticketWorkflowStore()
	}
	return nil
}

type ticketWorkflowStoreProvider interface {
	ticketWorkflowStore() *workflowstore.Store
}

func (s *Service) ticketWorkflowStore() *workflowstore.Store {
	return s.store
}

func validGitOID(value string) bool {
	if len(value) < 40 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return fmt.Errorf("trailing JSON value")
}

// The packet package deliberately exposes canonical construction but not a
// JSON decoder. Keep this wire representation local to the application read
// boundary so canonical packet verification does not become a filesystem API.
type authoringPacketWire struct {
	SchemaVersion         string                           `json:"schema_version"`
	CreatedAt             string                           `json:"created_at"`
	Role                  string                           `json:"role"`
	OperationID           string                           `json:"operation_id"`
	SurfaceContract       string                           `json:"surface_contract"`
	SurfaceManifestSHA256 string                           `json:"surface_manifest_sha256"`
	PriorPacket           *authoringPriorPacketWire        `json:"prior_packet"`
	Output                authoringOutputWire              `json:"output"`
	Project               authoringProjectWire             `json:"project"`
	WorkflowReferences    []authoringWorkflowReferenceWire `json:"workflow_references"`
	Attestations          []authoringAttestationWire       `json:"attestations"`
	Inputs                []authoringInputWire             `json:"inputs"`
	Repositories          []authoringRepositoryWire        `json:"repositories"`
	RelaySpecs            authoringGovernanceWire          `json:"relay_specs"`
	ManifestDomain        authoringManifestWire            `json:"manifest_domain"`
	SourcePolicy          string                           `json:"source_policy"`
	HistoricalAuthority   string                           `json:"historical_authority"`
	AllowedActions        []string                         `json:"allowed_actions"`
	ReadinessState        string                           `json:"readiness_state"`
}
type authoringPriorPacketWire struct {
	PacketID     string `json:"packet_id"`
	PacketSHA256 string `json:"packet_sha256"`
}
type authoringOutputWire struct {
	OutputKind        string `json:"output_kind"`
	OutputPersistence string `json:"output_persistence"`
}
type authoringProjectWire struct {
	ProjectID string `json:"project_id"`
}
type authoringWorkflowReferenceWire struct {
	Kind                    string `json:"kind"`
	PlanID                  string `json:"plan_id"`
	CanonicalArtifactID     string `json:"canonical_artifact_id"`
	CanonicalArtifactSHA256 string `json:"canonical_artifact_sha256"`
	PassID                  string `json:"pass_id"`
	PassNumber              int64  `json:"pass_number"`
	RunID                   string `json:"run_id"`
	ExecutionSpecArtifactID string `json:"execution_spec_artifact_id"`
	ExecutionSpecSHA256     string `json:"execution_spec_sha256"`
	AuditPacketID           string `json:"audit_packet_id"`
	AuditPacketSHA256       string `json:"audit_packet_sha256"`
	AuditDecisionID         string `json:"audit_decision_id"`
	Decision                string `json:"decision"`
	RecordedAt              string `json:"recorded_at"`
}
type authoringAttestationWire struct {
	Kind                    string                  `json:"kind"`
	InputName               string                  `json:"input_name"`
	SubjectSHA256           string                  `json:"subject_sha256"`
	Confirmed               bool                    `json:"confirmed"`
	Approved                bool                    `json:"approved"`
	CompleteTransfer        bool                    `json:"complete_transfer"`
	SelectedMode            string                  `json:"selected_mode"`
	ReviewedCandidateSHA256 string                  `json:"reviewed_candidate_sha256"`
	ReviewResult            string                  `json:"review_result"`
	Complete                bool                    `json:"complete"`
	Clearance               *authoringClearanceWire `json:"clearance"`
}
type authoringClearanceWire struct {
	PolicyVersion string                   `json:"policy_version"`
	SubjectSHA256 string                   `json:"subject_sha256"`
	Declaration   authoringDeclarationWire `json:"declaration"`
	Confirmed     bool                     `json:"confirmed"`
}
type authoringDeclarationWire struct {
	Password                             bool `json:"password"`
	APIKeyOrAccessToken                  bool `json:"api_key_or_access_token"`
	RefreshTokenOrSessionMaterial        bool `json:"refresh_token_or_session_material"`
	CookieOrAuthorizationHeader          bool `json:"cookie_or_authorization_header"`
	PrivateOrSSHKey                      bool `json:"private_or_ssh_key"`
	Credential                           bool `json:"credential"`
	CompleteSecretBearingEnvironmentFile bool `json:"complete_secret_bearing_environment_file"`
	AvoidableSignedSecretBearingURL      bool `json:"avoidable_signed_secret_bearing_url"`
}
type authoringInputWire struct {
	InputName       string              `json:"input_name"`
	InputRole       string              `json:"input_role"`
	SourceKind      string              `json:"source_kind"`
	DisplayName     string              `json:"display_name"`
	MediaType       string              `json:"media_type"`
	SHA256          string              `json:"sha256"`
	SizeBytes       int64               `json:"size_bytes"`
	AttestationKind string              `json:"attestation_kind"`
	Source          authoringSourceWire `json:"source"`
}
type authoringSourceWire struct {
	Kind                string                         `json:"kind"`
	FileIndex           int64                          `json:"file_index"`
	ArtifactID          string                         `json:"artifact_id"`
	WorkflowReference   authoringWorkflowReferenceWire `json:"workflow_reference"`
	SnapshotArtifactID  string                         `json:"snapshot_artifact_id"`
	SnapshotSHA256      string                         `json:"snapshot_sha256"`
	RepositoryBindingID string                         `json:"repository_binding_id"`
	CommitOID           string                         `json:"commit_oid"`
	TreeOID             string                         `json:"tree_oid"`
	Path                authoringPathWire              `json:"path"`
	BlobOID             string                         `json:"blob_oid"`
}
type authoringPathWire struct {
	PathID          string `json:"path_id"`
	ByteLength      int64  `json:"byte_length"`
	PathBytesBase64 string `json:"path_bytes_base64"`
}
type authoringRepositoryWire struct {
	RepositoryKey                        string                `json:"repository_key"`
	RepositoryTarget                     string                `json:"repository_target"`
	BindingOrder                         int64                 `json:"binding_order"`
	RevisionSource                       string                `json:"revision_source"`
	ConfiguredWorkingBranchRef           string                `json:"configured_working_branch_ref"`
	RepositoryTargetConfigurationVersion int64                 `json:"repository_target_configuration_version"`
	CommitOID                            string                `json:"commit_oid"`
	TreeOID                              string                `json:"tree_oid"`
	Anchors                              []authoringAnchorWire `json:"anchors"`
}
type authoringAnchorWire struct {
	AnchorName string `json:"anchor_name"`
	Purpose    string `json:"purpose"`
	CommitOID  string `json:"commit_oid"`
	TreeOID    string `json:"tree_oid"`
}
type authoringGovernanceWire struct {
	RepositoryKey                        string `json:"repository_key"`
	RepositoryTarget                     string `json:"repository_target"`
	Reserved                             bool   `json:"reserved"`
	RevisionSource                       string `json:"revision_source"`
	ConfiguredWorkingBranchRef           string `json:"configured_working_branch_ref"`
	RepositoryTargetConfigurationVersion int64  `json:"repository_target_configuration_version"`
	CommitOID                            string `json:"commit_oid"`
	TreeOID                              string `json:"tree_oid"`
}
type authoringManifestWire struct {
	ManifestPath    authoringPathWire             `json:"manifest_path"`
	ManifestBlobOID string                        `json:"manifest_blob_oid"`
	ManifestSHA256  string                        `json:"manifest_sha256"`
	Domain          string                        `json:"domain"`
	Members         []authoringManifestMemberWire `json:"members"`
}
type authoringManifestMemberWire struct {
	MemberOrder int64             `json:"member_order"`
	Path        authoringPathWire `json:"path"`
	BlobOID     string            `json:"blob_oid"`
	ByteSize    int64             `json:"byte_size"`
	SHA256      string            `json:"sha256"`
}

func decodeCanonicalAuthoringPacket(data []byte, expectedSHA string) (packet.Document, error) {
	if !validTicketSHA256(expectedSHA) || packet.VerifyBytes(data, expectedSHA, int64(len(data))) != nil {
		return packet.Document{}, ErrTicketAdmission
	}
	var wire authoringPacketWire
	if err := decodeStrictJSON(data, &wire); err != nil {
		return packet.Document{}, err
	}
	document := authoringPacketDocument(wire)
	canonical, err := packet.CanonicalBytes(document)
	if err != nil || string(canonical) != string(data) {
		return packet.Document{}, ErrTicketAdmission
	}
	return document, nil
}

func authoringPacketDocument(w authoringPacketWire) packet.Document {
	document := packet.Document{SchemaVersion: w.SchemaVersion, CreatedAt: w.CreatedAt, Role: registry.Role(w.Role), OperationID: registry.OperationID(w.OperationID), SurfaceContract: registry.SurfaceContractID(w.SurfaceContract), SurfaceManifestSHA256: w.SurfaceManifestSHA256, Output: packet.OutputContract{OutputKind: w.Output.OutputKind, OutputPersistence: w.Output.OutputPersistence}, Project: packet.ProjectBinding{ProjectID: w.Project.ProjectID}, SourcePolicy: registry.SourcePolicy(w.SourcePolicy), HistoricalAuthority: registry.HistoricalAuthorityPolicy(w.HistoricalAuthority), ReadinessState: w.ReadinessState}
	if w.PriorPacket != nil {
		document.PriorPacket = &packet.PriorPacketIdentity{PacketID: w.PriorPacket.PacketID, PacketSHA256: w.PriorPacket.PacketSHA256}
	}
	for _, value := range w.WorkflowReferences {
		document.WorkflowReferences = append(document.WorkflowReferences, authoringWorkflowReference(value))
	}
	for _, value := range w.Attestations {
		document.Attestations = append(document.Attestations, authoringAttestation(value))
	}
	for _, value := range w.Inputs {
		document.Inputs = append(document.Inputs, authoringInput(value))
	}
	for _, value := range w.Repositories {
		repository := packet.RepositoryBinding{RepositoryKey: value.RepositoryKey, RepositoryTarget: value.RepositoryTarget, BindingOrder: value.BindingOrder, RevisionSource: value.RevisionSource, ConfiguredWorkingBranchRef: value.ConfiguredWorkingBranchRef, RepositoryTargetConfigurationVersion: value.RepositoryTargetConfigurationVersion, CommitOID: value.CommitOID, TreeOID: value.TreeOID}
		for _, anchor := range value.Anchors {
			repository.Anchors = append(repository.Anchors, packet.Anchor{AnchorName: anchor.AnchorName, Purpose: registry.AnchorPurpose(anchor.Purpose), CommitOID: anchor.CommitOID, TreeOID: anchor.TreeOID})
		}
		document.Repositories = append(document.Repositories, repository)
	}
	document.RelaySpecs = packet.GovernanceBinding{RepositoryKey: w.RelaySpecs.RepositoryKey, RepositoryTarget: w.RelaySpecs.RepositoryTarget, Reserved: w.RelaySpecs.Reserved, RevisionSource: w.RelaySpecs.RevisionSource, ConfiguredWorkingBranchRef: w.RelaySpecs.ConfiguredWorkingBranchRef, RepositoryTargetConfigurationVersion: w.RelaySpecs.RepositoryTargetConfigurationVersion, CommitOID: w.RelaySpecs.CommitOID, TreeOID: w.RelaySpecs.TreeOID}
	document.ManifestDomain = packet.ManifestDomainBinding{ManifestPath: authoringPath(w.ManifestDomain.ManifestPath), ManifestBlobOID: w.ManifestDomain.ManifestBlobOID, ManifestSHA256: w.ManifestDomain.ManifestSHA256, Domain: registry.ManifestDomain(w.ManifestDomain.Domain)}
	for _, member := range w.ManifestDomain.Members {
		document.ManifestDomain.Members = append(document.ManifestDomain.Members, packet.ManifestMember{MemberOrder: member.MemberOrder, Path: authoringPath(member.Path), BlobOID: member.BlobOID, ByteSize: member.ByteSize, SHA256: member.SHA256})
	}
	for _, action := range w.AllowedActions {
		document.AllowedActions = append(document.AllowedActions, registry.AllowedAction(action))
	}
	return document
}

func authoringWorkflowReference(w authoringWorkflowReferenceWire) packet.WorkflowReference {
	return packet.WorkflowReference{Kind: registry.WorkflowReferenceKind(w.Kind), PlanID: w.PlanID, CanonicalArtifactID: w.CanonicalArtifactID, CanonicalArtifactSHA256: w.CanonicalArtifactSHA256, PassID: w.PassID, PassNumber: w.PassNumber, RunID: w.RunID, ExecutionSpecArtifactID: w.ExecutionSpecArtifactID, ExecutionSpecSHA256: w.ExecutionSpecSHA256, AuditPacketID: w.AuditPacketID, AuditPacketSHA256: w.AuditPacketSHA256, AuditDecisionID: w.AuditDecisionID, Decision: w.Decision, RecordedAt: w.RecordedAt}
}
func authoringAttestation(w authoringAttestationWire) packet.Attestation {
	var clearance *packet.SensitiveDataClearance
	if w.Clearance != nil {
		clearance = &packet.SensitiveDataClearance{PolicyVersion: w.Clearance.PolicyVersion, SubjectSHA256: w.Clearance.SubjectSHA256, Declaration: registry.SensitiveDataDeclaration{Password: w.Clearance.Declaration.Password, APIKeyOrAccessToken: w.Clearance.Declaration.APIKeyOrAccessToken, RefreshTokenOrSessionMaterial: w.Clearance.Declaration.RefreshTokenOrSessionMaterial, CookieOrAuthorizationHeader: w.Clearance.Declaration.CookieOrAuthorizationHeader, PrivateOrSSHKey: w.Clearance.Declaration.PrivateOrSSHKey, Credential: w.Clearance.Declaration.Credential, CompleteSecretBearingEnvironmentFile: w.Clearance.Declaration.CompleteSecretBearingEnvironmentFile, AvoidableSignedSecretBearingURL: w.Clearance.Declaration.AvoidableSignedSecretBearingURL}, Confirmed: w.Clearance.Confirmed}
	}
	return packet.Attestation{Kind: registry.AttestationKind(w.Kind), InputName: w.InputName, SubjectSHA256: w.SubjectSHA256, Confirmed: w.Confirmed, Approved: w.Approved, CompleteTransfer: w.CompleteTransfer, SelectedMode: w.SelectedMode, ReviewedCandidateSHA256: w.ReviewedCandidateSHA256, ReviewResult: w.ReviewResult, Complete: w.Complete, Clearance: clearance}
}
func authoringInput(w authoringInputWire) packet.InputBinding {
	return packet.InputBinding{InputName: w.InputName, InputRole: registry.InputRole(w.InputRole), SourceKind: registry.InputSourceKind(w.SourceKind), DisplayName: w.DisplayName, MediaType: w.MediaType, SHA256: w.SHA256, SizeBytes: w.SizeBytes, AttestationKind: registry.AttestationKind(w.AttestationKind), Source: authoringSource(w.Source)}
}
func authoringSource(w authoringSourceWire) packet.InputSource {
	return packet.InputSource{Kind: registry.InputSourceKind(w.Kind), FileIndex: w.FileIndex, ArtifactID: w.ArtifactID, WorkflowReference: authoringWorkflowReference(w.WorkflowReference), SnapshotArtifactID: w.SnapshotArtifactID, SnapshotSHA256: w.SnapshotSHA256, RepositoryBindingID: w.RepositoryBindingID, CommitOID: w.CommitOID, TreeOID: w.TreeOID, Path: authoringPath(w.Path), BlobOID: w.BlobOID}
}
func authoringPath(w authoringPathWire) packet.PathIdentity {
	return packet.PathIdentity{PathID: w.PathID, ByteLength: w.ByteLength, PathBytesBase64: w.PathBytesBase64}
}
