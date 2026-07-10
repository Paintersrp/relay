package workflowrepos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	workflowstore "relay/internal/store/workflow"
)

type Registry struct {
	store *workflowstore.Store
}

func NewRegistry(store *workflowstore.Store) (*Registry, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Registry{store: store}, nil
}

func (r *Registry) Register(ctx context.Context, repoTarget, localPath string) (workflowstore.RepositoryTarget, error) {
	if err := validateRepoTarget(repoTarget); err != nil {
		return workflowstore.RepositoryTarget{}, err
	}
	resolvedPath, err := resolveDirectory(localPath)
	if err != nil {
		return workflowstore.RepositoryTarget{}, err
	}
	var created workflowstore.RepositoryTarget
	if err := r.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		created, createErr = tx.CreateRepositoryTarget(ctx, repoTarget, resolvedPath)
		return createErr
	}); err != nil {
		return workflowstore.RepositoryTarget{}, fmt.Errorf("register repository target %q: %w", repoTarget, err)
	}
	return created, nil
}

func (r *Registry) Resolve(ctx context.Context, repoTarget string) (workflowstore.RepositoryTarget, error) {
	if err := validateRepoTarget(repoTarget); err != nil {
		return workflowstore.RepositoryTarget{}, err
	}
	target, err := r.store.GetRepositoryTarget(ctx, repoTarget)
	if err != nil {
		return workflowstore.RepositoryTarget{}, fmt.Errorf("resolve repository target %q: %w", repoTarget, err)
	}
	return target, nil
}

func validateRepoTarget(value string) error {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("repository target must be a nonblank global key without path separators or outer whitespace")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("repository target contains whitespace or control characters")
		}
	}
	return nil
}

func resolveDirectory(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("repository path must be nonblank without outer whitespace")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve repository path symlinks: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect repository path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository path is not a directory")
	}
	return filepath.Clean(canonical), nil
}
