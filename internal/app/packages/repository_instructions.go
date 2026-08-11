package packages

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"relay/internal/sourcevault"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

// ApprovedRepositoryInstruction is the exact identity of one bound repository
// instruction (an applicable AGENTS.md file) resolved from the exact selected
// source closure. The relative path and SHA-256 bind the exact closure bytes;
// execution transports these identities through the immutable
// ExecutionAssignment.
type ApprovedRepositoryInstruction struct {
	RelativePath string
	SHA256       string
	SizeBytes    int64
	ObjectOID    string
}

// inspectedSourcePaths derives the package's exact inspected source basis from
// the selected approved Delivery Ticket: the selected Ticket's source document
// path plus every non-null implementation-obligation source area. The result
// is deduplicated and ordered by repository-relative path. No new source-scope
// concept participates: the basis is entirely existing approved Ticket data.
func inspectedSourcePaths(ticketSourcePath string, sourceAreas []string) []string {
	set := make(map[string]struct{}, 1+len(sourceAreas))
	set[ticketSourcePath] = struct{}{}
	for _, area := range sourceAreas {
		if area == "" {
			continue
		}
		set[area] = struct{}{}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// inspectedSourcePathsFromProjection derives the inspected source basis from
// the deterministic Delivery Ticket projection carried by the approved
// authority, mirroring inspectedSourcePaths.
func inspectedSourcePathsFromProjection(ticketSourcePath string, projection speccompiler.DeliveryTicketProjection) []string {
	areas := make([]string, 0, len(projection.ImplementationObligations))
	for _, obligation := range projection.ImplementationObligations {
		if obligation.SourceArea != nil {
			areas = append(areas, *obligation.SourceArea)
		}
	}
	return inspectedSourcePaths(ticketSourcePath, areas)
}

// applicableInstructionPaths computes the deduplicated, repository-relative
// path-ordered set of AGENTS.md candidate paths that apply to any of the
// inspected source paths. The applicable chain for a source path runs from the
// repository root toward that path; the repository-root AGENTS.md applies to
// every source path. Only files named AGENTS.md are ever candidates, so
// agents/orchestrator.md and every other repository file are excluded by
// construction.
func applicableInstructionPaths(sourcePaths []string) []string {
	set := make(map[string]struct{})
	for _, sourcePath := range sourcePaths {
		set["AGENTS.md"] = struct{}{}
		parts := strings.Split(sourcePath, "/")
		for index := 1; index < len(parts); index++ {
			directory := strings.Join(parts[:index], "/")
			set[directory+"/AGENTS.md"] = struct{}{}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// resolveRepositoryInstructions resolves the exact repository-instruction
// basis for the inspected source paths from the exact selected source closure.
// A path the closure does not contain is not an applicable instruction; any
// other read failure rejects the basis because the instruction basis must be
// resolvable from the exact selected source closure/base commit.
func (s *Service) resolveRepositoryInstructions(ctx context.Context, closure workflowstore.SourceVaultClosure, sourcePaths []string) ([]ApprovedRepositoryInstruction, error) {
	if s.sourceVaults == nil {
		return nil, fmt.Errorf("%w: source-vault reader is not configured", ErrPackageBasisChanged)
	}
	candidates := applicableInstructionPaths(sourcePaths)
	instructions := make([]ApprovedRepositoryInstruction, 0, len(candidates))
	for _, candidate := range candidates {
		result, err := s.sourceVaults.ReadPath(ctx, sourcevault.ReadPathRequest{
			ClosureID: closure.ClosureID,
			Path:      candidate,
			MaxBytes:  approvedAuthorityReadLimit,
		})
		if err != nil {
			var vaultError *sourcevault.Error
			if errors.As(err, &vaultError) && vaultError.Code == sourcevault.CodeObjectUnavailable {
				continue
			}
			return nil, fmt.Errorf("%w: repository instruction %s is unavailable: %w", ErrPackageBasisChanged, candidate, err)
		}
		if !validOID(result.ObjectOID) {
			return nil, fmt.Errorf("%w: repository instruction %s object OID is invalid", ErrPackageBasisChanged, candidate)
		}
		if !utf8.Valid(result.Bytes) {
			return nil, fmt.Errorf("%w: repository instruction %s is not valid UTF-8", ErrPackageBasisChanged, candidate)
		}
		instructions = append(instructions, ApprovedRepositoryInstruction{
			RelativePath: candidate,
			SHA256:       sha256Hex(result.Bytes),
			SizeBytes:    int64(len(result.Bytes)),
			ObjectOID:    result.ObjectOID,
		})
	}
	return instructions, nil
}

// repositoryInstructionsBasisSHA256 binds the ordered repository-instruction
// identities (repository-relative path and exact SHA-256) into the package
// basis digest. The order is the deterministic repository-relative-path order
// produced by resolution, so the digest is stable across recomputation.
func repositoryInstructionsBasisSHA256(instructions []ApprovedRepositoryInstruction) string {
	parts := []string{"repository-instructions-v1"}
	for _, instruction := range instructions {
		parts = append(parts, instruction.RelativePath, instruction.SHA256)
	}
	return compoundSHA256(parts...)
}

// verifyRepositoryInstructionRows verifies that the stored package instruction
// rows are exactly the recomputed instruction basis: same membership, same
// repository-relative-path order, and same exact SHA-256 identities.
func verifyRepositoryInstructionRows(stored []workflowstore.ExecutionPackageRepositoryInstruction, want []ApprovedRepositoryInstruction) error {
	if len(stored) != len(want) {
		return fmt.Errorf("repository instruction row count %d does not match recomputed basis %d", len(stored), len(want))
	}
	for index, instruction := range want {
		row := stored[index]
		if row.PackageRowID < 1 || row.Sequence != int64(index+1) || row.Path != instruction.RelativePath || row.Sha256 != instruction.SHA256 {
			return fmt.Errorf("repository instruction row %d does not match the recomputed basis", index)
		}
	}
	return nil
}

// cloneApprovedRepositoryInstructions deep-copies the identity records so no
// consumer shares slice storage with the loaded authority. Nil and non-nil
// empty slices are preserved.
func cloneApprovedRepositoryInstructions(instructions []ApprovedRepositoryInstruction) []ApprovedRepositoryInstruction {
	if instructions == nil {
		return nil
	}
	return append(make([]ApprovedRepositoryInstruction, 0, len(instructions)), instructions...)
}

// revalidatePackageRepositoryInstructions recomputes the repository-instruction
// basis for an existing immutable package from the exact selected source
// closure and verifies it against the stored package digest and instruction
// rows. The inspected source paths are derived from the selected revision's
// source path and its stored implementation-obligation source areas, which the
// package basis binds to the approved Ticket document.
func (s *Service) revalidatePackageRepositoryInstructions(ctx context.Context, packageRow workflowstore.ExecutionPackage, closure workflowstore.SourceVaultClosure, revisionRowID int64) error {
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, revisionRowID)
	if err != nil {
		return err
	}
	members, err := s.store.ListDeliveryTicketRevisionMembers(ctx, revisionRowID)
	if err != nil {
		return err
	}
	var sourceAreas []string
	for _, member := range members {
		if member.MemberKind == "implementation_obligation" && member.MemberPath.Valid && member.MemberPath.String != "" {
			sourceAreas = append(sourceAreas, member.MemberPath.String)
		}
	}
	sourcePaths := inspectedSourcePaths(revision.SourcePath, sourceAreas)
	instructions, err := s.resolveRepositoryInstructions(ctx, closure, sourcePaths)
	if err != nil {
		return err
	}
	if repositoryInstructionsBasisSHA256(instructions) != packageRow.RepositoryInstructionsSha256 {
		return fmt.Errorf("%w: repository instructions no longer match the immutable package basis", ErrPackageBasisChanged)
	}
	stored, err := s.store.ListExecutionPackageRepositoryInstructions(ctx, packageRow.ID)
	if err != nil {
		return err
	}
	if err := verifyRepositoryInstructionRows(stored, instructions); err != nil {
		return fmt.Errorf("%w: %v", ErrPackageBasisChanged, err)
	}
	return nil
}
