package workflowrepos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"strings"
	"unicode"
	"unicode/utf8"

	workflowstore "relay/internal/store/workflow"
)

const (
	RevisionSourceExplicitCommit          = "explicit_commit"
	RevisionSourceConfiguredWorkingBranch = "configured_working_branch"
)

var (
	ErrRepositoryUnconfigured = errors.New("repository target is unconfigured")
	ErrInvalidExplicitCommit  = errors.New("invalid explicit commit")
	ErrRepositoryObject       = errors.New("repository object is unavailable or has the wrong type")
	ErrDirtyProjectWorktree   = errors.New("targeted Project worktree is dirty")
	ErrGovernanceUnavailable  = errors.New("governance authority is unavailable")
)

type RepositoryUsePolicy struct {
	RequireCleanWorktree       bool
	RequireGovernanceAuthority bool
}

type GovernanceRequest struct {
	ManifestPath string
	Domain       string
}

type RevisionRequest struct {
	RepoTarget        string
	ExplicitCommitOID string
	Policy            RepositoryUsePolicy
	Governance        GovernanceRequest
}

type GovernanceMemberAvailability struct {
	Path    string
	BlobOID string
}

type GovernanceAvailability struct {
	ManifestPath    string
	ManifestBlobOID string
	Domain          string
	Members         []GovernanceMemberAvailability
}

type ResolvedRevision struct {
	RepositoryTarget                     workflowstore.RepositoryTarget
	RevisionSource                       string
	ConfiguredWorkingBranchRef           string
	RepositoryTargetConfigurationVersion int64
	CommitOID                            string
	TreeOID                              string
	GovernanceAvailability               *GovernanceAvailability
}

func (r *Registry) ResolveRevision(
	ctx context.Context,
	request RevisionRequest,
) (ResolvedRevision, error) {
	request.RepoTarget = strings.TrimSpace(request.RepoTarget)
	if err := validateRepoTarget(request.RepoTarget); err != nil {
		return ResolvedRevision{}, err
	}
	target, err := r.store.GetRepositoryTarget(ctx, request.RepoTarget)
	if err != nil {
		return ResolvedRevision{}, err
	}
	if target.ConfigurationVersion < 1 {
		return ResolvedRevision{}, fmt.Errorf("%w: configuration version must be positive", ErrRepositoryUnconfigured)
	}

	var revisionSource string
	var configuredRef string
	var commitOID string
	var treeOID string
	switch {
	case request.ExplicitCommitOID != "":
		if !validFullOID(request.ExplicitCommitOID) {
			return ResolvedRevision{}, ErrInvalidExplicitCommit
		}
		revisionSource = RevisionSourceExplicitCommit
		commitOID, treeOID, err = r.resolveCommitAndTree(ctx, target.LocalPath, request.ExplicitCommitOID)
	case !target.ConfiguredBranchRef.Valid:
		return ResolvedRevision{}, ErrRepositoryUnconfigured
	default:
		observation, resolveErr := r.resolveConfiguredBranch(
			ctx,
			target.LocalPath,
			target.ConfiguredBranchRef.String,
		)
		if resolveErr != nil {
			return ResolvedRevision{}, resolveErr
		}
		revisionSource = RevisionSourceConfiguredWorkingBranch
		configuredRef = observation.Ref
		commitOID = observation.CommitOID
		treeOID = observation.TreeOID
	}
	if err != nil {
		return ResolvedRevision{}, err
	}

	if request.Policy.RequireCleanWorktree {
		if err := r.requireCleanWorktree(ctx, target.LocalPath); err != nil {
			return ResolvedRevision{}, err
		}
	}

	var governance *GovernanceAvailability
	if request.Policy.RequireGovernanceAuthority {
		if target.RepoTarget != "relay-specs" {
			return ResolvedRevision{}, fmt.Errorf("%w: governance policy is reserved for relay-specs", ErrGovernanceUnavailable)
		}
		value, governanceErr := r.verifyGovernanceAvailability(
			ctx,
			target.LocalPath,
			commitOID,
			treeOID,
			request.Governance,
		)
		if governanceErr != nil {
			return ResolvedRevision{}, governanceErr
		}
		governance = &value
	}

	return ResolvedRevision{
		RepositoryTarget:                     target,
		RevisionSource:                       revisionSource,
		ConfiguredWorkingBranchRef:           configuredRef,
		RepositoryTargetConfigurationVersion: target.ConfigurationVersion,
		CommitOID:                            commitOID,
		TreeOID:                              treeOID,
		GovernanceAvailability:               governance,
	}, nil
}

func (r *Registry) resolveCommitAndTree(
	ctx context.Context,
	localPath string,
	commitOID string,
) (string, string, error) {
	if !validFullOID(commitOID) {
		return "", "", ErrInvalidExplicitCommit
	}
	if err := r.requireObjectType(ctx, localPath, commitOID, "commit"); err != nil {
		return "", "", err
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
			return "", "", err
		}
		return "", "", fmt.Errorf("%w: commit tree cannot be resolved", ErrRepositoryObject)
	}
	treeOID := strings.TrimSpace(treeResult.Stdout)
	if !validFullOID(treeOID) {
		return "", "", fmt.Errorf("%w: invalid tree identity", ErrRepositoryObject)
	}
	if err := r.requireObjectType(ctx, localPath, treeOID, "tree"); err != nil {
		return "", "", err
	}
	return commitOID, treeOID, nil
}

func (r *Registry) requireObjectType(
	ctx context.Context,
	localPath string,
	oid string,
	want string,
) error {
	result, err := r.runner.Run(ctx, localPath, "cat-file", "-t", oid)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return err
		}
		return fmt.Errorf("%w: %s object is missing", ErrRepositoryObject, want)
	}
	if strings.TrimSpace(result.Stdout) != want {
		return fmt.Errorf("%w: expected %s object", ErrRepositoryObject, want)
	}
	return nil
}

func (r *Registry) requireCleanWorktree(ctx context.Context, localPath string) error {
	result, err := r.runner.Run(
		ctx,
		localPath,
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=all",
	)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return err
		}
		return fmt.Errorf("%w: status could not be inspected", ErrDirtyProjectWorktree)
	}
	if result.Stdout != "" {
		return ErrDirtyProjectWorktree
	}
	return nil
}

func (r *Registry) verifyGovernanceAvailability(
	ctx context.Context,
	localPath string,
	commitOID string,
	treeOID string,
	request GovernanceRequest,
) (GovernanceAvailability, error) {
	if err := validateGovernancePath(request.ManifestPath); err != nil {
		return GovernanceAvailability{}, err
	}
	request.Domain = strings.TrimSpace(request.Domain)
	if request.Domain == "" {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest domain is required", ErrGovernanceUnavailable)
	}
	if err := r.requireObjectType(ctx, localPath, commitOID, "commit"); err != nil {
		return GovernanceAvailability{}, fmt.Errorf("%w: %v", ErrGovernanceUnavailable, err)
	}
	if err := r.requireObjectType(ctx, localPath, treeOID, "tree"); err != nil {
		return GovernanceAvailability{}, fmt.Errorf("%w: %v", ErrGovernanceUnavailable, err)
	}

	manifestOID, err := r.resolveTreeBlob(ctx, localPath, treeOID, request.ManifestPath)
	if err != nil {
		return GovernanceAvailability{}, err
	}
	manifestResult, err := r.runner.Run(ctx, localPath, "cat-file", "blob", manifestOID)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return GovernanceAvailability{}, err
		}
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest blob is unavailable", ErrGovernanceUnavailable)
	}
	if !utf8.ValidString(manifestResult.Stdout) {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest is not valid UTF-8", ErrGovernanceUnavailable)
	}

	var document struct {
		Domains map[string]json.RawMessage `json:"domains"`
	}
	decoder := json.NewDecoder(strings.NewReader(manifestResult.Stdout))
	if err := decoder.Decode(&document); err != nil {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest JSON is invalid", ErrGovernanceUnavailable)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest must contain one JSON object", ErrGovernanceUnavailable)
	}
	rawMembers, ok := document.Domains[request.Domain]
	if !ok {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest domain %q is missing", ErrGovernanceUnavailable, request.Domain)
	}
	var members []string
	if err := json.Unmarshal(rawMembers, &members); err != nil || len(members) == 0 {
		return GovernanceAvailability{}, fmt.Errorf("%w: manifest domain members are invalid", ErrGovernanceUnavailable)
	}
	seen := make(map[string]struct{}, len(members))
	availability := GovernanceAvailability{
		ManifestPath:    request.ManifestPath,
		ManifestBlobOID: manifestOID,
		Domain:          request.Domain,
		Members:         make([]GovernanceMemberAvailability, 0, len(members)),
	}
	for _, member := range members {
		if err := validateGovernancePath(member); err != nil {
			return GovernanceAvailability{}, err
		}
		if _, duplicate := seen[member]; duplicate {
			return GovernanceAvailability{}, fmt.Errorf("%w: duplicate manifest member %q", ErrGovernanceUnavailable, member)
		}
		seen[member] = struct{}{}
		blobOID, err := r.resolveTreeBlob(ctx, localPath, treeOID, member)
		if err != nil {
			return GovernanceAvailability{}, err
		}
		availability.Members = append(availability.Members, GovernanceMemberAvailability{
			Path:    member,
			BlobOID: blobOID,
		})
	}
	return availability, nil
}

func (r *Registry) resolveTreeBlob(
	ctx context.Context,
	localPath string,
	treeOID string,
	path string,
) (string, error) {
	result, err := r.runner.Run(
		ctx,
		localPath,
		"ls-tree",
		"-z",
		"--full-tree",
		treeOID,
		"--",
		path,
	)
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return "", err
		}
		return "", fmt.Errorf("%w: path %q cannot be resolved", ErrGovernanceUnavailable, path)
	}
	value := strings.TrimSuffix(result.Stdout, "\x00")
	if value == "" || strings.Contains(value, "\x00") {
		return "", fmt.Errorf("%w: path %q is missing or ambiguous", ErrGovernanceUnavailable, path)
	}
	tab := strings.IndexByte(value, '\t')
	if tab < 0 || value[tab+1:] != path {
		return "", fmt.Errorf("%w: path %q did not resolve exactly", ErrGovernanceUnavailable, path)
	}
	fields := strings.Fields(value[:tab])
	if len(fields) != 3 || fields[1] != "blob" || !validFullOID(fields[2]) {
		return "", fmt.Errorf("%w: path %q is not a blob", ErrGovernanceUnavailable, path)
	}
	if err := r.requireObjectType(ctx, localPath, fields[2], "blob"); err != nil {
		return "", fmt.Errorf("%w: path %q blob is unavailable", ErrGovernanceUnavailable, path)
	}
	return fields[2], nil
}

func validateGovernancePath(value string) error {
	switch {
	case value == "":
		return fmt.Errorf("%w: governance path is required", ErrGovernanceUnavailable)
	case !utf8.ValidString(value):
		return fmt.Errorf("%w: governance path must be valid UTF-8", ErrGovernanceUnavailable)
	case strings.TrimSpace(value) != value:
		return fmt.Errorf("%w: governance path has outer whitespace", ErrGovernanceUnavailable)
	case strings.HasPrefix(value, "/") || strings.Contains(value, "\\"):
		return fmt.Errorf("%w: governance path must be repository-relative POSIX", ErrGovernanceUnavailable)
	case pathpkg.Clean(value) != value || value == ".":
		return fmt.Errorf("%w: governance path is not canonical", ErrGovernanceUnavailable)
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%w: governance path contains an invalid segment", ErrGovernanceUnavailable)
		}
	}
	for _, value := range value {
		if unicode.IsControl(value) {
			return fmt.Errorf("%w: governance path contains a control character", ErrGovernanceUnavailable)
		}
	}
	return nil
}

func validFullOID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, digit := range value {
		if digit >= '0' && digit <= '9' {
			continue
		}
		if digit < 'a' || digit > 'f' {
			return false
		}
	}
	return true
}

func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
