package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	executionpackages "relay/internal/app/packages"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/artifactschema"
	workflowstore "relay/internal/store/workflow"
)

const (
	executionAssignmentKind      = "execution_assignment"
	executionAssignmentMediaType = "application/json"
	standingRoleSourcePath       = "agents/orchestrator.md"
)

var ErrExecutionAssignmentConflict = errors.New("execution assignment conflicts with approved authority")

// validExecutionAssignmentOID reports whether value is a 40-hex Git object OID.
func validExecutionAssignmentOID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// validExecutionAssignmentSHA256 reports whether value is a 64-hex SHA-256.
func validExecutionAssignmentSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// validExecutionAssignmentInstructionPath reports whether value is a safe
// repository-relative AGENTS.md path: no absolute, drive, or backslash
// prefixes, no empty or dot path segments, and the basename is exactly
// AGENTS.md.
func validExecutionAssignmentInstructionPath(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.Contains(value, "\\") {
		return false
	}
	if len(value) >= 2 && value[1] == ':' {
		c := value[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return false
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return filepath.Base(value) == "AGENTS.md"
}

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
	Dependencies            []ExecutionAssignmentDependency        `json:"dependencies"`
	Repository              ExecutionAssignmentRepository          `json:"repository"`
	Source                  ExecutionAssignmentSource              `json:"source"`
	Authority               ExecutionAssignmentAuthority           `json:"authority"`
	AuthorityLayers         []ExecutionAssignmentLayer             `json:"authority_layers"`
	RepositoryInstructions  []ExecutionAssignmentRepositoryInstruction `json:"repository_instructions"`
	DeliveryTicket          ExecutionAssignmentDocument            `json:"delivery_ticket"`
	DeterministicOperations ExecutionAssignmentOperations          `json:"deterministic_operations"`
	ValidationCommands      []ExecutionAssignmentValidationCommand `json:"validation_commands"`
	StandingRole            ExecutionAssignmentStandingRole        `json:"standing_role"`
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

// ExecutionAssignmentDependency is one completed dependency of the selected
// Ticket revision as loaded and verified by ApprovedAuthority: its sequence,
// the depends-on Ticket ID, that Ticket's revision number, and the stored
// completed outcome ("satisfied"). The package SHA transitively binds the same
// outcome; this record carries it directly.
type ExecutionAssignmentDependency struct {
	Sequence int64  `json:"sequence"`
	TicketID string `json:"ticket_id"`
	Revision int64  `json:"revision"`
	Outcome  string `json:"outcome"`
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
	CommitOID    string `json:"commit_oid"`
	TreeOID      string `json:"tree_oid"`
	Generation   int64  `json:"generation"`
	RefName      string `json:"ref_name"`
	State        string `json:"state"`
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

// ExecutionAssignmentRepositoryInstruction is the immutable identity of one
// bound repository instruction (an applicable AGENTS.md file) verified by the
// approved package authority: its repository-relative path and the exact
// SHA-256 of its bytes from the exact selected source closure. Execution knows
// exactly which repository instructions are bound without reinterpreting
// package authority.
type ExecutionAssignmentRepositoryInstruction struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
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

type ExecutionAssignmentStandingRole struct {
	AuthorityRepository string `json:"authority_repository"`
	AuthorityCommit     string `json:"authority_commit"`
	SourcePath          string `json:"source_path"`
}

type ExecutionAssignmentService struct {
	store    *workflowstore.Store
	packages *executionpackages.Service
}

func NewExecutionAssignmentService(
	store *workflowstore.Store,
	sourceVaults executionpackages.SourceVaultReader,
) (*ExecutionAssignmentService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if sourceVaults == nil {
		return nil, fmt.Errorf("source-vault reader is required")
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
		return ExecutionAssignmentResult{}, fmt.Errorf("%w: Run execution_assignment artifact is missing", ErrExecutionAssignmentConflict)
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
	document := authority.DeliveryTicket
	if document.DisplayName == "" || document.RelativePath == "" || filepath.Base(document.RelativePath) != document.DisplayName || document.MediaType != "application/json" || document.SHA256 == "" {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("ticket-qualified source document identity is inconsistent")
	}
	if len(authority.TicketProjection.ValidationCommands) == 0 {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("approved Delivery Ticket has no validation commands")
	}
	if len(authority.AuthorityLayers) == 0 {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("approved authority has no verified layers")
	}
	if !validExecutionAssignmentOID(authority.Source.CommitOID) || !validExecutionAssignmentOID(authority.Source.TreeOID) || authority.Source.Generation < 1 || authority.Source.RefName == "" || authority.Source.State == "" {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("approved source closure identity is inconsistent")
	}
	dependencies := make([]ExecutionAssignmentDependency, 0, len(authority.CompletedDependencies))
	var previousDependencySequence int64
	for index, dep := range authority.CompletedDependencies {
		if dep.Sequence < 1 || (index > 0 && dep.Sequence <= previousDependencySequence) || dep.TicketID == "" || dep.Revision < 1 || dep.Outcome == "" {
			return ExecutionAssignment{}, nil, "", fmt.Errorf("approved completed dependency records are inconsistent")
		}
		previousDependencySequence = dep.Sequence
		dependencies = append(dependencies, ExecutionAssignmentDependency{Sequence: dep.Sequence, TicketID: dep.TicketID, Revision: dep.Revision, Outcome: dep.Outcome})
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
	instructions := make([]ExecutionAssignmentRepositoryInstruction, 0, len(authority.RepositoryInstructions))
	var previousInstructionPath string
	for index, instruction := range authority.RepositoryInstructions {
		if instruction.RelativePath == "" || (index > 0 && instruction.RelativePath <= previousInstructionPath) || !validExecutionAssignmentInstructionPath(instruction.RelativePath) || !validExecutionAssignmentSHA256(instruction.SHA256) {
			return ExecutionAssignment{}, nil, "", fmt.Errorf("approved repository instruction identities are inconsistent")
		}
		previousInstructionPath = instruction.RelativePath
		instructions = append(instructions, ExecutionAssignmentRepositoryInstruction{Path: instruction.RelativePath, SHA256: instruction.SHA256})
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
	commands := make([]ExecutionAssignmentValidationCommand, 0, len(authority.TicketProjection.ValidationCommands))
	for _, command := range authority.TicketProjection.ValidationCommands {
		commands = append(commands, ExecutionAssignmentValidationCommand{WorkingDirectory: command.WorkingDirectory, Command: command.Command, Expected: command.Expected})
	}
	assignment := ExecutionAssignment{
		SchemaVersion:           "1.0",
		Run:                     ExecutionAssignmentRun{RunID: authority.Run.RunID, RunRowID: authority.Run.ID},
		Package:                 ExecutionAssignmentPackage{PackageID: authority.Package.PackageID, PackageRowID: authority.Package.ID, SHA256: authority.Package.PackageSha256},
		PackageApproval:         ExecutionAssignmentApproval{ApprovalID: authority.PackageApproval.ApprovalID, ApprovalRowID: authority.PackageApproval.ID, ApprovedPackageSHA256: authority.PackageApproval.PackageSha256},
		Ticket:                  ExecutionAssignmentTicket{TicketID: ticket.TicketID, TicketRowID: ticket.ID, RevisionRowID: revision.ID, RevisionNumber: revision.RevisionNumber, DeliveryApprovalID: authority.TicketApproval.ApprovalID, DeliveryApprovalRowID: authority.TicketApproval.ID},
		Dependencies:            dependencies,
		Repository:              ExecutionAssignmentRepository{Target: authority.Run.RepoTarget, Branch: authority.Run.Branch, BaseCommit: authority.Run.BaseCommit},
		Source:                  ExecutionAssignmentSource{ClosureID: authority.Source.ClosureID, ClosureRowID: authority.Source.ID, SHA256: authority.Package.SourceSha256, CommitOID: authority.Source.CommitOID, TreeOID: authority.Source.TreeOID, Generation: authority.Source.Generation, RefName: authority.Source.RefName, State: authority.Source.State},
		Authority:               ExecutionAssignmentAuthority{RevisionID: authority.Authority.AuthorityRevisionID, RevisionRowID: authority.Authority.ID, RevisionNumber: authority.Authority.RevisionNumber, AuthorityBasisSHA256: authority.Package.AuthoritySha256, Repository: artifactschema.AuthorityRepository, Commit: artifactschema.AuthorityCommit},
		AuthorityLayers:         layers,
		RepositoryInstructions:  instructions,
		DeliveryTicket:          ExecutionAssignmentDocument{DisplayName: document.DisplayName, RelativePath: document.RelativePath, MediaType: document.MediaType, SHA256: document.SHA256},
		DeterministicOperations: operations,
		ValidationCommands:      commands,
		StandingRole:            ExecutionAssignmentStandingRole{AuthorityRepository: artifactschema.AuthorityRepository, AuthorityCommit: artifactschema.AuthorityCommit, SourcePath: standingRoleSourcePath},
	}
	content, err := json.Marshal(assignment)
	if err != nil {
		return ExecutionAssignment{}, nil, "", fmt.Errorf("marshal execution assignment: %w", err)
	}
	content = append(content, '\n')
	filename := fmt.Sprintf("%s.ticket-%s.r%d.execution-assignment.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	return assignment, content, filename, nil
}
