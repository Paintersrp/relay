package intake

import (
	"fmt"
	"path/filepath"
	"strings"

	"relay/internal/executor"
	"relay/internal/store"
)

func deriveRunTitleFromMarkdown(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return "Untitled Run"
}

func resolveIntakeExecutorAdapter(input IntakeInput, metadata map[string]string) (string, bool, error) {
	candidates := []string{
		input.ExecutorAdapter,
		input.ExecutorAdapter2,
		metadata["executor_adapter"],
		metadata["executorAdapter"],
	}
	for _, cand := range candidates {
		if strings.TrimSpace(cand) != "" {
			adapter, err := executor.NormalizeKnownAdapterID(cand)
			if err != nil {
				return "", true, err
			}
			return adapter, true, nil
		}
	}

	targetExec := metadata["target_executor"]
	if strings.TrimSpace(targetExec) != "" {
		adapter, err := executor.NormalizeKnownAdapterID(targetExec)
		if err == nil {
			return adapter, true, nil
		}
	}

	return "opencode_go", false, nil
}

func resolveIntakePlanInputs(input IntakeInput) (string, string) {
	planID := strings.TrimSpace(input.PlanID)
	if planID == "" {
		planID = strings.TrimSpace(input.PlanIDSnake)
	}
	passID := strings.TrimSpace(input.PassID)
	if passID == "" {
		passID = strings.TrimSpace(input.PassIDSnake)
	}
	return planID, passID
}

func resolveIntakeSourceContextInputs(input IntakeInput) (string, string) {
	contextPacketID := strings.TrimSpace(input.ContextPacketID)
	if contextPacketID == "" {
		contextPacketID = strings.TrimSpace(input.ContextPacketIDSnake)
	}
	sourceSnapshotID := strings.TrimSpace(input.SourceSnapshotID)
	if sourceSnapshotID == "" {
		sourceSnapshotID = strings.TrimSpace(input.SourceSnapshotIDSnake)
	}
	return contextPacketID, sourceSnapshotID
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// resolveRepo finds or creates a repo by name or path, mirroring legacy behavior.
func (s *Service) resolveRepo(repoNameOrPath string) (*store.Repo, error) {
	if repoNameOrPath == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if repo, err := s.store.GetRepoByName(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	if repo, err := s.store.GetRepoByPath(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	baseName := filepath.Base(repoNameOrPath)
	if repo, err := s.store.GetRepoByName(baseName); err == nil && repo != nil {
		return repo, nil
	}
	normalized := filepath.Clean(repoNameOrPath)
	if repo, err := s.store.GetRepoByPath(normalized); err == nil && repo != nil {
		return repo, nil
	}
	if repos, err := s.store.ListRepos(); err == nil {
		for _, r := range repos {
			if strings.EqualFold(r.Name, repoNameOrPath) || strings.EqualFold(r.Name, baseName) {
				rCopy := r
				return &rCopy, nil
			}
		}
	}
	return s.store.CreateRepo(baseName, repoNameOrPath)
}
