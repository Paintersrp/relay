package repos

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const branchDiscoveryTimeout = 2 * time.Second

type BranchInfo struct {
	Name      string
	IsCurrent bool
}

func ListLocalBranches(repoPath string) ([]BranchInfo, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return nil, fmt.Errorf("repo path is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), branchDiscoveryTimeout)
	defer cancel()

	currentBytes, errCurrent := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--show-current").Output()
	currentBranch := ""
	if errCurrent == nil {
		currentBranch = strings.TrimSpace(string(currentBytes))
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("branch discovery timed out for %s", repoPath)
		}
		return nil, err
	}

	raw := string(out)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []BranchInfo{}, nil
	}

	lines := strings.Split(raw, "\n")
	var branches []BranchInfo
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		branches = append(branches, BranchInfo{
			Name:      name,
			IsCurrent: name == currentBranch,
		})
	}

	return branches, nil
}
