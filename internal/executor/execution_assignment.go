package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	executionpackages "relay/internal/app/packages"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/artifactschema"
	workflowstore "relay/internal/store/workflow"
)

const (
	executionAssignmentKind      = "execution_assignment"
	executionAssignmentMediaType = "application/json"
	executorInstructionsPath     = "agents/executor.md"
)

var ErrExecutionAssignmentConflict = errors.New("execution assignment conflicts with approved authority")

type ExecutionAssignmentResult struct {
	Artifact   workflowstore.Artifact
	Assignment ExecutionAssignment
	Bytes      []byte
}

// ExecutionAssignment is the immutable, canonical runtime identity for one
// approved package-linked Run. Its field order is the canonical JSON order.
type ExecutionAssignment struct {
	SchemaVersion           string                                 `json:"schema_version"`
	Run                     ExecutionAssignmentRun                 `json:"run"`
	Package                 ExecutionAssignmentPackage             `json:"package"`
	PackageApproval         ExecutionAssignmentApproval            `json:"package_approval"`
	Ticket                  ExecutionAssignmentTicket              `json:"ticket"`
	Repository              ExecutionAssignmentRepository          `json:"repository"`
	Source                  ExecutionAssignmentSource              `json:"source"`
	Authority               ExecutionAssignmentAuthority           `json:"authority"`
	AuthorityLayers         []ExecutionAssignmentLayer             `json:"authority_layers"`
	TicketDesignBrief       ExecutionAssignmentDocument            `json:"ticket_design_brief"`
	DeterministicOperations ExecutionAssignmentOperations          `json:"deterministic_operations"`
	ValidationCommands      []ExecutionAssignmentValidationCommand `json:"validation_commands"`
	ExecutorInstructions    ExecutionAssignmentInstructions        `json:"executor_instructions"`
}

type ExecutionAssignmentRun struct {
	RunID    string `json:"run_id"`
	RunRowID int64  `json:"run_row_id"`
}

type ExecutionAssignmentPackage struct {
	PackageID    string `json:"package_id"`
	PackageRowID int64  `json:"package_row_id"`
	SHA256       string `json:"sha256"`
}

type ExecutionAssignmentApproval struct {
	ApprovalID            string `json:"approval_id"`
	ApprovalRowID         int64  `json:"approval_row_id"`
	ApprovedPackageSHA256 string `json:"approved_package_sha256"`
}

type ExecutionAssignmentTicket struct {
	TicketID              string `json:"ticket_id"`
	TicketRowID           int64  `json:"ticket_row_id"`
	RevisionRowID         int64  `json:"revision_row_id"`
	RevisionNumber        int64  `json:"revision_number"`
	DeliveryApprovalID    string `json:"delivery_approval_id"`
	DeliveryApprovalRowID int64  `json:"delivery_approval_row_id"`
}

type ExecutionAssignmentRepository struct {
	Target     string `json:"target"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"base_commit"`
}

type ExecutionAssignmentSource struct {
	ClosureID    string `json:"closure_id"`
	ClosureRowID int64  `json:"closure_row_id"`
	SHA256       string `json:"sha256"`
}

type ExecutionAssignmentAuthority struct {
	RevisionID           string `json:"revision_id"`
	RevisionRowID        int64  `json:"revision_row_id"`
	RevisionNumber       int64  `json:"revision_number"`
	AuthorityBasisSHA256 string `json:"authority_basis_sha256"`
	Repository           string `json:"repository"`
	Commit               string `json:"commit"`
}

type ExecutionAssignmentLayer struct {
	Sequence     int64  `json:"sequence"`
	LayerKind    string `json:"layer_kind"`
	RelativePath string `json:"relative_path"`
	MediaType    string `json:"media_type"`
	SHA256       string `json:"sha256"`
}

type ExecutionAssignmentDocument struct {
	DisplayName  string `json:"display_name"`
	RelativePath string `json:"relative_path"`
	MediaType    string `json:"media_type"`
	SHA256       string `json:"sha256"`
}

type ExecutionAssignmentOperations struct {
	Presence     string `json:"presence"`
	DisplayName  string `json:"display_name,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Coverage     string `json:"coverage,omitempty"`
}

type ExecutionAssignmentValidationCommand struct {
	WorkingDirectory string `json:"working_directory"`
	Command          string `json:"command"`
	Expected         string `json:"expected"`
}

type ExecutionAssignmentInstructions struct {
	AuthorityRepository string `json:"authority_repository"`
	AuthorityCommit     string `json:"authority_commit"`
	SourcePath          string `json:"source_path"`
}

type ExecutionAssignmentService struct {
	store    *workflowstore.Store
	packages *executionpackages.Service
}

func NewExecutionAssignmentService(store *workflowstore.Store, sourceVaults executionpackages.SourceVaultReader) (*ExecutionAssignmentService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	packages, err := executionpackages.NewServiceWithSourceVaults(store, sourceVaults)
	if err != nil {
		return nil, err
	}
	return &ExecutionAssignmentService{store: store, packages: packages}, nil
}

func (s *ExecutionAssignmentService) PrepareExecutionAssignment(ctx context.Context, runID string) (ExecutionAssignmentResult, error) {
	if s == nil || s.store == nil || s.packages == nil {
		return ExecutionAssignmentResult{}, fmt.Errorf("execution assignment service is unavailable")
	}

	authority, err := s.packages.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	assignment, content, filename, err := buildExecutionAssignment(authority)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}

	artifacts, err := s.store.ListArtifactsByRun(ctx, authority.Run.ID)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	var existing *workflowstore.Artifact
	for index := range artifacts {
		if artifacts[index].Kind != executionAssignmentKind {
			continue
		}
		if existing != nil || artifacts[index].OwnerType != workflowstore.ArtifactOwnerRun || !artifacts[index].RunRowID.Valid || artifacts[index].RunRowID.Int64 != authority.Run.ID {
			return ExecutionAssignmentResult{}, ErrExecutionAssignmentConflict
		}
		candidate := artifacts[index]
		existing = &candidate
	}
	if existing != nil {
		return s.resolveExistingAssignment(*existing, assignment, content, authority.Run.RunID, filename)
	}
	if authority.Run.Status != workflowstore.RunStatusSetupReady {
		return ExecutionAssignmentResult{}, fmt.Errorf("execution assignment requires a setup_ready Run")
	}

	batch, err := s.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("runs", authority.Run.RunID)))
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	staged, err := batch.Stage(executionAssignmentKind, filename, executionAssignmentMediaType, content)
	if err != nil {
		_ = batch.Rollback()
		return ExecutionAssignmentResult{}, err
	}
	var created workflowstore.Artifact
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:   workflowstore.NewArtifactID(),
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: authority.Run.ID, Valid: true},
			Kind:         staged.Kind,
			RelativePath: staged.RelativePath,
			MediaType:    staged.MediaType,
			SHA256:       staged.SHA256,
			SizeBytes:    staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		created = artifact
		return nil
	})
	if err == nil {
		return ExecutionAssignmentResult{Artifact: created, Assignment: assignment, Bytes: append([]byte(nil), content...)}, nil
	}
	// A concurrent creator may have won the unique Run assignment race. Only
	// return it when its managed file is the exact approved assignment.
	if artifacts, listErr := s.store.ListArtifactsByRun(ctx, authority.Run.ID); listErr == nil {
		for index := range artifacts {
			if artifacts[index].Kind == executionAssignmentKind {
				return s.resolveExistingAssignment(artifacts[index], assignment, content, authority.Run.RunID, filename)
			}
		}
	}
	return ExecutionAssignmentResult{}, err
}

// LoadExecutionAssignment resolves an existing verified assignment without
// creating one. It keeps outcome recording bound to the same approved
// authority and byte-verification path as assignment preparation.
func (s *ExecutionAssignmentService) LoadExecutionAssignment(ctx context.Context, runID string) (ExecutionAssignmentResult, error) {
	if s == nil || s.store == nil || s.packages == nil {
		return ExecutionAssignmentResult{}, fmt.Errorf("execution assignment service is unavailable")
	}
	authority, err := s.packages.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	assignment, content, filename, err := buildExecutionAssignment(authority)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	artifacts, err := s.store.ListArtifactsByRun(ctx, authority.Run.ID)
	if err != nil {
		return ExecutionAssignmentResult{}, err
	}
	var existing *workflowstore.Artifact
	for index := range artifacts {
		if artifacts[index].Kind != executionAssignmentKind {
			continue
		}
		if existing != nil || artifacts[index].OwnerType != workflowstore.ArtifactOwnerRun || !artifacts[index].RunRowID.Valid || artifacts[index].RunRowID.Int64 != authority.Run.ID {
			return ExecutionAssignmentResult{}, ErrExecutionAssignmentConflict
		}
		candidate := artifacts[index]
		existing = &candidate
	}
	if existing == nil {
		return ExecutionAssignmentResult{}, fmt.Errorf("Run execution_assignment artifact is missing")
	}
	return s.resolveExistingAssignment(*existing, assignment, content, authority.Run.RunID, filename)
}

func (s *ExecutionAssignmentService) resolveExistingAssignment(artifact workflowstore.Artifact, assignment ExecutionAssignment, expected []byte, runID, filename string) (ExecutionAssignmentResult, error) {
	wantPath := filepath.ToSlash(filepath.Join("runs", runID, filename))
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.Kind != executionAssignmentKind || artifact.MediaType != executionAssignmentMediaType || artifact.RelativePath != wantPath {
		return ExecutionAssignmentResult{}, ErrExecutionAssignmentConflict
	}
	verified, content, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{
		Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes,
	}, len(expected))
	if err != nil || !bytes.Equal(content, expected) || verified.RelativePath != artifact.RelativePath {
		return ExecutionAssignmentResult{}, ErrExecutionAssignmentConflict
	}
	return ExecutionAssignmentResult{Artifact: artifact, Assignment: assignment, Bytes: append([]byte(nil), content...)}, nil
}

func buildExecutionAssignment(authority executionpackages.ApprovedAuthority) (ExecutionAssignment, []byte, string, error) {
	workspace := authority.Workspace
	ticket := authority.Ticket
	revision := authority.TicketRevision
	wantBriefName := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	if authority.TicketDesignBrief.DisplayName != wantBriefName || filepath.Base(authority.TicketDesignBrief.RelativePath) != wantBriefName || authority.TicketDesignBrief.MediaType != "text/markdown" || authority.TicketDesignBrief.SHA256 == "" {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("ticket-qualified filename identity is inconsistent")
	}
	if len(authority.BriefProjection.ValidationCommands) == 0 {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("validated Brief has no validation commands")
	}
	if len(authority.AuthorityLayers) == 0 {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("approved authority has no verified layers")
	}
	layers := make([]ExecutionAssignmentLayer, 0, len(authority.AuthorityLayers))
	var previousSequence int64
	for index, layer := range authority.AuthorityLayers {
		if layer.Sequence < 1 || (index > 0 && layer.Sequence <= previousSequence) || layer.Kind == "" || layer.RelativePath == "" || layer.MediaType == "" || layer.SHA256 == "" {
			return ExecutionAssignment{}, nil, "", fmt.Errorf("approved authority layers are inconsistent")
		}
		previousSequence = layer.Sequence
		layers = append(layers, ExecutionAssignmentLayer{Sequence: layer.Sequence, LayerKind: layer.Kind, RelativePath: layer.RelativePath, MediaType: layer.MediaType, SHA256: layer.SHA256})
	}
	operations := ExecutionAssignmentOperations{Presence: "absent"}
	if authority.DeterministicOperations != nil {
		operation := authority.DeterministicOperations
		if operation.DisplayName == "" || filepath.Base(operation.RelativePath) != operation.DisplayName || operation.MediaType != executionAssignmentMediaType || operation.SHA256 == "" || (operation.Coverage != "complete" && operation.Coverage != "partial") {
			return ExecutionAssignment{}, nil, "", fmt.Errorf("deterministic operations presence or coverage is inconsistent")
		}
		wantOperationsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
		if operation.DisplayName != wantOperationsName {
			return ExecutionAssignment{}, nil, "", fmt.Errorf("ticket-qualified operations filename identity is inconsistent")
		}
		operations = ExecutionAssignmentOperations{Presence: "present", DisplayName: operation.DisplayName, RelativePath: operation.RelativePath, MediaType: operation.MediaType, SHA256: operation.SHA256, Coverage: operation.Coverage}
	}
	commands := make([]ExecutionAssignmentValidationCommand, 0, len(authority.BriefProjection.ValidationCommands))
	for _, command := range authority.BriefProjection.ValidationCommands {
		commands = append(commands, ExecutionAssignmentValidationCommand{WorkingDirectory: command.WorkingDirectory, Command: command.Command, Expected: command.Expected})
	}
	assignment := ExecutionAssignment{
		SchemaVersion:           "1.0",
		Run:                     ExecutionAssignmentRun{RunID: authority.Run.RunID, RunRowID: authority.Run.ID},
		Package:                 ExecutionAssignmentPackage{PackageID: authority.Package.PackageID, PackageRowID: authority.Package.ID, SHA256: authority.Package.PackageSha256},
		PackageApproval:         ExecutionAssignmentApproval{ApprovalID: authority.PackageApproval.ApprovalID, ApprovalRowID: authority.PackageApproval.ID, ApprovedPackageSHA256: authority.PackageApproval.PackageSha256},
		Ticket:                  ExecutionAssignmentTicket{TicketID: ticket.TicketID, TicketRowID: ticket.ID, RevisionRowID: revision.ID, RevisionNumber: revision.RevisionNumber, DeliveryApprovalID: authority.TicketApproval.ApprovalID, DeliveryApprovalRowID: authority.TicketApproval.ID},
		Repository:              ExecutionAssignmentRepository{Target: authority.Run.RepoTarget, Branch: authority.Run.Branch, BaseCommit: authority.Run.BaseCommit},
		Source:                  ExecutionAssignmentSource{ClosureID: authority.Source.ClosureID, ClosureRowID: authority.Source.ID, SHA256: authority.Package.SourceSha256},
		Authority:               ExecutionAssignmentAuthority{RevisionID: authority.Authority.AuthorityRevisionID, RevisionRowID: authority.Authority.ID, RevisionNumber: authority.Authority.RevisionNumber, AuthorityBasisSHA256: authority.Package.AuthoritySha256, Repository: artifactschema.AuthorityRepository, Commit: artifactschema.AuthorityCommit},
		AuthorityLayers:         layers,
		TicketDesignBrief:       ExecutionAssignmentDocument{DisplayName: authority.TicketDesignBrief.DisplayName, RelativePath: authority.TicketDesignBrief.RelativePath, MediaType: authority.TicketDesignBrief.MediaType, SHA256: authority.TicketDesignBrief.SHA256},
		DeterministicOperations: operations,
		ValidationCommands:      commands,
		ExecutorInstructions:    ExecutionAssignmentInstructions{AuthorityRepository: artifactschema.AuthorityRepository, AuthorityCommit: artifactschema.AuthorityCommit, SourcePath: executorInstructionsPath},
	}
	content, err := json.Marshal(assignment)
	if err != nil {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("marshal execution assignment: %w", err)
	}
	content = append(content, '\n')
	filename := fmt.Sprintf("%s.ticket-%s.r%d.execution-assignment.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	return assignment, content, filename, nil
}
