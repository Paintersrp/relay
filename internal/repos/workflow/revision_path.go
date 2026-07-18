package workflowrepos

import (
	"context"
	"fmt"
	"strings"
)

type ResolvedPathBlob struct {
	Path    string
	BlobOID string
}

func (r *Registry) ResolvePathBlob(ctx context.Context, revision ResolvedRevision, path string) (ResolvedPathBlob, error) {
	if r == nil || strings.TrimSpace(revision.RepositoryTarget.RepoTarget) == "" || revision.CommitOID == "" || revision.TreeOID == "" || path == "" || len(path) > 8192 || strings.IndexByte(path, 0) >= 0 || strings.HasPrefix(path, "/") {
		return ResolvedPathBlob{}, ErrRepositoryObject
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ResolvedPathBlob{}, ErrRepositoryObject
		}
	}
	current, err := r.store.GetRepositoryTarget(ctx, revision.RepositoryTarget.RepoTarget)
	if err != nil {
		return ResolvedPathBlob{}, err
	}
	if current.LocalPath != revision.RepositoryTarget.LocalPath || current.ConfigurationVersion != revision.RepositoryTargetConfigurationVersion {
		return ResolvedPathBlob{}, ErrRepositoryUnconfigured
	}
	if revision.RevisionSource == RevisionSourceConfiguredWorkingBranch {
		if !current.ConfiguredBranchRef.Valid || current.ConfiguredBranchRef.String != revision.ConfiguredWorkingBranchRef {
			return ResolvedPathBlob{}, ErrRepositoryUnconfigured
		}
	} else if revision.RevisionSource != RevisionSourceExplicitCommit {
		return ResolvedPathBlob{}, ErrInvalidExplicitCommit
	}
	if err := r.requireObjectType(ctx, current.LocalPath, revision.CommitOID, "commit"); err != nil {
		return ResolvedPathBlob{}, err
	}
	if err := r.requireObjectType(ctx, current.LocalPath, revision.TreeOID, "tree"); err != nil {
		return ResolvedPathBlob{}, err
	}
	blobOID, err := r.resolveTreeBlob(ctx, current.LocalPath, revision.TreeOID, path)
	if err != nil {
		return ResolvedPathBlob{}, fmt.Errorf("%w: path cannot be resolved", ErrRepositoryObject)
	}
	return ResolvedPathBlob{Path: path, BlobOID: blobOID}, nil
}
