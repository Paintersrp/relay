package repos

import (
	"fmt"
	"os/exec"
	"strings"
)

type BranchInfo struct {
	Name      string
	IsCurrent bool
}

func ListLocalBranches(repoPath string) ([]BranchInfo, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return nil, fmt.Errorf("repo path is required")
	}

	currentBytes, errCurrent := exec.Command("git", "-C", repoPath, "branch", "--show-current").Output()
	currentBranch := ""
	if errCurrent == nil {
		currentBranch = strings.TrimSpace(string(currentBytes))
	}

	cmd := exec.Command("git", "-C", repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	out, err := cmd.Output()
	if err != nil {
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
