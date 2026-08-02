package sourcevaultpolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrOverlap = errors.New("repository overlaps source-vault root")

func ValidateRepositorySeparation(ctx context.Context, root, localPath string) (string, error) {
	canonicalRoot, err := CanonicalPathForCreation(root)
	if err != nil {
		return "", err
	}
	worktree, err := canonicalExistingPath(localPath)
	if err != nil {
		return "", err
	}
	gitDir, commonDir, err := ResolveGitDirectories(ctx, worktree)
	if err != nil {
		return "", err
	}
	for _, path := range []string{worktree, gitDir, commonDir} {
		if PathsOverlap(canonicalRoot, path) {
			return worktree, fmt.Errorf("%w: repository %q overlaps source-vault root %q", ErrOverlap, worktree, canonicalRoot)
		}
	}
	return worktree, nil
}

func CanonicalPathForCreation(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	current := abs
	missing := []string{}
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("path has no existing ancestor")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func canonicalExistingPath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	return filepath.Clean(resolved), err
}

func PathsOverlap(left, right string) bool { return pathWithin(left, right) || pathWithin(right, left) }
func pathWithin(candidate, protected string) bool {
	rel, err := filepath.Rel(protected, candidate)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	if runtime.GOOS == "windows" {
		rel = strings.ToLower(rel)
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
