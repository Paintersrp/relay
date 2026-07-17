package workflowrepos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	workflowstore "relay/internal/store/workflow"
)

const (
	ConfigurationDispositionPreserve  = "preserve"
	ConfigurationDispositionConfigure = "configure"
	ConfigurationDispositionChange    = "change"
)

var (
	ErrInvalidConfiguredBranch      = errors.New("invalid configured branch")
	ErrConfiguredBranchUnavailable  = errors.New("configured branch is unavailable")
	ErrStaleRepositoryConfiguration = errors.New("repository configuration is stale")
)

type branchObservation struct {
	Ref       string
	CommitOID string
	TreeOID   string
}

func (r *Registry) inspectBranchConfiguration(
	ctx context.Context,
	inspection *Inspection,
	proposed string,
) error {
	if inspection == nil {
		return fmt.Errorf("inspection is required")
	}

	var currentRef sql.NullString
	var currentVersion int64
	if inspection.ExistingRepository != nil {
		currentRef = inspection.ExistingRepository.ConfiguredBranchRef
		currentVersion = inspection.ExistingRepository.ConfigurationVersion
		if currentVersion < 1 {
			return fmt.Errorf("%w: current configuration version must be positive", ErrStaleRepositoryConfiguration)
		}
	}

	finalRef := currentRef
	switch {
	case proposed != "":
		finalRef = sql.NullString{String: proposed, Valid: true}
	case inspection.ExistingRepository == nil:
		finalRef = sql.NullString{}
	}

	disposition := ConfigurationDispositionPreserve
	proposedVersion := int64(1)
	switch {
	case inspection.ExistingRepository == nil && finalRef.Valid:
		disposition = ConfigurationDispositionConfigure
	case inspection.ExistingRepository == nil:
		disposition = ConfigurationDispositionPreserve
	case sameNullableString(currentRef, finalRef):
		proposedVersion = currentVersion
	case !currentRef.Valid && finalRef.Valid:
		disposition = ConfigurationDispositionConfigure
		proposedVersion = currentVersion + 1
	default:
		disposition = ConfigurationDispositionChange
		proposedVersion = currentVersion + 1
	}

	var observation branchObservation
	var err error
	if finalRef.Valid {
		observation, err = r.resolveConfiguredBranch(ctx, inspection.ResolvedLocalPath, finalRef.String)
		if err != nil {
			return err
		}
	}

	inspection.CurrentConfiguredBranchRef = currentRef
	inspection.ExpectedConfigurationVersion = currentVersion
	inspection.ProposedConfiguredBranchRef = finalRef
	inspection.ProposedConfigurationVersion = proposedVersion
	inspection.ProposedBranchCommitOID = observation.CommitOID
	inspection.ProposedBranchTreeOID = observation.TreeOID
	inspection.ConfigurationDisposition = disposition
	return nil
}

func (r *Registry) resolveConfiguredBranch(
	ctx context.Context,
	localPath string,
	ref string,
) (branchObservation, error) {
	if err := validateConfiguredBranchRef(ref); err != nil {
		return branchObservation{}, err
	}
	if _, err := r.runner.Run(ctx, localPath, "check-ref-format", ref); err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return branchObservation{}, err
		}
		return branchObservation{}, fmt.Errorf("%w: Git rejected %q", ErrInvalidConfiguredBranch, ref)
	}
	commitResult, err := r.runner.Run(
		ctx,
		localPath,
		"rev-parse",
		"--verify",
		"--end-of-options",
		ref+"^{commit}",
	)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return branchObservation{}, err
		}
		return branchObservation{}, fmt.Errorf("%w: %q does not resolve to a local commit", ErrConfiguredBranchUnavailable, ref)
	}
	commitOID := strings.TrimSpace(commitResult.Stdout)
	if !validFullOID(commitOID) {
		return branchObservation{}, fmt.Errorf("%w: branch commit identity is invalid", ErrConfiguredBranchUnavailable)
	}
	treeResult, err := r.runner.Run(
		ctx,
		localPath,
		"rev-parse",
		"--verify",
		"--end-of-options",
		commitOID+"^{tree}",
	)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return branchObservation{}, err
		}
		return branchObservation{}, fmt.Errorf("%w: branch tree is unavailable", ErrConfiguredBranchUnavailable)
	}
	treeOID := strings.TrimSpace(treeResult.Stdout)
	if !validFullOID(treeOID) {
		return branchObservation{}, fmt.Errorf("%w: branch tree identity is invalid", ErrConfiguredBranchUnavailable)
	}
	if err := r.requireObjectType(ctx, localPath, commitOID, "commit"); err != nil {
		return branchObservation{}, err
	}
	if err := r.requireObjectType(ctx, localPath, treeOID, "tree"); err != nil {
		return branchObservation{}, err
	}
	return branchObservation{Ref: ref, CommitOID: commitOID, TreeOID: treeOID}, nil
}

func validateConfiguredBranchRef(ref string) error {
	switch {
	case ref == "":
		return fmt.Errorf("%w: branch ref is required", ErrInvalidConfiguredBranch)
	case !utf8.ValidString(ref):
		return fmt.Errorf("%w: branch ref must be valid UTF-8", ErrInvalidConfiguredBranch)
	case strings.TrimSpace(ref) != ref:
		return fmt.Errorf("%w: branch ref must not contain outer whitespace", ErrInvalidConfiguredBranch)
	case len(ref) > 1024:
		return fmt.Errorf("%w: branch ref exceeds 1024 bytes", ErrInvalidConfiguredBranch)
	case !strings.HasPrefix(ref, "refs/heads/"):
		return fmt.Errorf("%w: branch ref must use refs/heads/", ErrInvalidConfiguredBranch)
	case ref == "refs/heads/":
		return fmt.Errorf("%w: branch name is required", ErrInvalidConfiguredBranch)
	case strings.Contains(ref, "@{") || strings.ContainsAny(ref, "~^:?[\\*"):
		return fmt.Errorf("%w: revision expressions are prohibited", ErrInvalidConfiguredBranch)
	}
	for _, value := range ref {
		if unicode.IsControl(value) {
			return fmt.Errorf("%w: control characters are prohibited", ErrInvalidConfiguredBranch)
		}
	}
	return nil
}

func sameNullableString(left, right sql.NullString) bool {
	if left.Valid != right.Valid {
		return false
	}
	return !left.Valid || left.String == right.String
}

func repositoryMatchesInspection(
	target workflowstore.RepositoryTarget,
	inspection Inspection,
) bool {
	return target.ConfigurationVersion == inspection.ProposedConfigurationVersion &&
		sameNullableString(target.ConfiguredBranchRef, inspection.ProposedConfiguredBranchRef)
}
