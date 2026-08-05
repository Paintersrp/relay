package operations

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

const (
	wayfinderDiscoveryOperation = registry.OperationID("wayfinder.discovery")
	wayfinderDiscoverySurface   = registry.SurfaceContractID("wayfinder-discovery.v1")
	resolveDiscoveryAction      = registry.AllowedAction("resolve_discovery_ticket")
)

type PacketInputAuthorityRequest struct {
	PacketID        string
	WorkspaceID     string
	InputName       string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	Action          registry.AllowedAction
}

type PacketInputAuthority struct {
	ArtifactRowID    sql.NullInt64
	RetainedArtifact sql.NullInt64
	ArtifactSHA256   string
}

type packetInputDocument struct {
	OperationID     registry.OperationID       `json:"operation_id"`
	SurfaceContract registry.SurfaceContractID `json:"surface_contract"`
	Project         struct {
		ProjectID string `json:"project_id"`
	} `json:"project"`
	AllowedActions []registry.AllowedAction   `json:"allowed_actions"`
	Inputs         []packetInputDocumentInput `json:"inputs"`
}

type packetInputDocumentInput struct {
	InputName  string `json:"input_name"`
	SourceKind string `json:"source_kind"`
	SHA256     string `json:"sha256"`
	Source     struct {
		Kind       string `json:"kind"`
		ArtifactID string `json:"artifact_id"`
	} `json:"source"`
}

func (s *Service) ResolvePacketInputAuthority(ctx context.Context, request PacketInputAuthorityRequest) (PacketInputAuthority, error) {
	if s == nil || strings.TrimSpace(request.PacketID) != request.PacketID || request.PacketID == "" ||
		strings.TrimSpace(request.WorkspaceID) != request.WorkspaceID || request.WorkspaceID == "" ||
		strings.TrimSpace(request.InputName) != request.InputName || request.InputName == "" ||
		request.SurfaceContract != wayfinderDiscoverySurface || request.OperationID != wayfinderDiscoveryOperation || request.Action != resolveDiscoveryAction {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}

	view, err := s.Get(ctx, request.PacketID)
	if err != nil {
		return PacketInputAuthority{}, err
	}
	if view.Summary.SurfaceContract != wayfinderDiscoverySurface || view.Summary.OperationID != wayfinderDiscoveryOperation || view.Summary.ProjectID == "" {
		return PacketInputAuthority{}, &Error{Code: CodePacketRouteMismatch}
	}

	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, request.WorkspaceID)
	if err != nil {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	project, err := s.store.GetProjectByRowID(ctx, workspace.ProjectRowID)
	if err != nil {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	packetProject, err := s.store.GetProjectByProjectID(ctx, view.Summary.ProjectID)
	if err != nil || packetProject.ID != workspace.ProjectRowID || project.ProjectID != view.Summary.ProjectID {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}

	var document packetInputDocument
	if err := json.Unmarshal(view.DocumentBytes, &document); err != nil {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	if document.OperationID != wayfinderDiscoveryOperation || document.SurfaceContract != wayfinderDiscoverySurface || document.Project.ProjectID != view.Summary.ProjectID || !containsAllowedAction(document.AllowedActions, resolveDiscoveryAction) {
		return PacketInputAuthority{}, &Error{Code: CodePacketRouteMismatch}
	}

	var selected packetInputDocumentInput
	matches := 0
	for _, input := range document.Inputs {
		if input.InputName == request.InputName {
			selected = input
			matches++
		}
	}
	if matches != 1 || selected.InputName == "" || !validPacketSHA256(selected.SHA256) || selected.Source.ArtifactID == "" {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}

	publication, err := s.store.GetOperationPacketPublicationByPacketID(ctx, request.PacketID)
	if err != nil {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	integrity, err := s.store.GetOperationPacketPublicationIntegrity(ctx, publication.PublicationID)
	if err != nil || integrity.Packet.PacketID != request.PacketID || integrity.Packet.ID != publication.PacketRowID || integrity.Publication.State != workflowstore.OperationPacketPublicationStateCommitted {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	verified, err := s.store.ArtifactStore().VerifyPublication(publication.PublicationID)
	if err != nil || verified.ManifestSHA256 != integrity.Publication.ManifestSHA256 || verified.Manifest.Namespace != integrity.Publication.Namespace || verifyPublicationIntegrity(verified.Manifest, integrity) != nil {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}

	binding, ok := oneInputArtifactBinding(integrity.Bindings, request.InputName, integrity.Packet.ID)
	if !ok || !matchingInputDependency(integrity.Dependencies, request.InputName) || !binding.RetainedArtifactRowID.Valid || binding.PacketArtifactRowID.Valid {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	retained, err := s.store.GetOperationPacketRetainedArtifactByRowID(ctx, binding.RetainedArtifactRowID.Int64)
	if err != nil || retained.PublicationID != publication.PublicationID || retained.ArtifactID == "" || retained.SHA256 != selected.SHA256 || !verifyRetainedArtifact(s.store, retained) || !matchingInputDependencyOwner(integrity.Dependencies, request.InputName, retained.ArtifactID) {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}

	if selected.Source.Kind == "relay_artifact" {
		artifact, err := s.store.GetArtifactByArtifactID(ctx, selected.Source.ArtifactID)
		if err != nil || artifact.SHA256 != selected.SHA256 {
			return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
		}
		if _, err := readWorkflowArtifact(s.store, artifact); err != nil {
			return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
		}
		return PacketInputAuthority{ArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true}, ArtifactSHA256: selected.SHA256}, nil
	}
	if retained.ArtifactID != selected.Source.ArtifactID {
		return PacketInputAuthority{}, retainedAuthorityError(workflowstore.OperationPacketDependencyInputArtifact)
	}
	return PacketInputAuthority{RetainedArtifact: sql.NullInt64{Int64: retained.ID, Valid: true}, ArtifactSHA256: selected.SHA256}, nil
}

func validPacketSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func containsAllowedAction(values []registry.AllowedAction, wanted registry.AllowedAction) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func oneInputArtifactBinding(values []workflowstore.OperationPacketArtifactBinding, inputName string, packetRowID int64) (workflowstore.OperationPacketArtifactBinding, bool) {
	var result workflowstore.OperationPacketArtifactBinding
	found := false
	for _, value := range values {
		if value.PacketRowID != packetRowID || value.DependencyClass != workflowstore.OperationPacketDependencyInputArtifact || value.DependencyKey != inputName {
			continue
		}
		if found {
			return workflowstore.OperationPacketArtifactBinding{}, false
		}
		result = value
		found = true
	}
	return result, found
}

func matchingInputDependency(values []workflowstore.OperationPacketRetentionDependency, inputName string) bool {
	found := false
	for _, value := range values {
		if value.DependencyClass != workflowstore.OperationPacketDependencyInputArtifact || value.DependencyKey != inputName {
			continue
		}
		if found || !value.Required || !value.Attached || !value.Retained || !value.OwnerIdentity.Valid || value.OwnerIdentity.String == "" {
			return false
		}
		found = true
	}
	return found
}

func matchingInputDependencyOwner(values []workflowstore.OperationPacketRetentionDependency, inputName, owner string) bool {
	found := false
	for _, value := range values {
		if value.DependencyClass != workflowstore.OperationPacketDependencyInputArtifact || value.DependencyKey != inputName {
			continue
		}
		if found || !value.Required || !value.Attached || !value.Retained || !value.OwnerIdentity.Valid || value.OwnerIdentity.String != owner {
			return false
		}
		found = true
	}
	return found
}

func verifyRetainedArtifact(store *workflowstore.Store, artifact workflowstore.OperationPacketRetainedArtifact) bool {
	root := store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && int64(len(data)) == artifact.SizeBytes && digestBytes(data) == artifact.SHA256
}
