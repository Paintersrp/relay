package sources

import (
	"fmt"
	"sort"
	"strings"

	"relay/internal/store"
)

type RepositoryResolutionCandidate struct {
	RepoID           string   `json:"repo_id"`
	AcceptedAliases  []string `json:"accepted_aliases"`
	RepositoryRole   string   `json:"repository_role,omitempty"`
	RepositoryStatus string   `json:"repository_status,omitempty"`
}

type RepositoryResolutionResult struct {
	Input           string                          `json:"input"`
	CanonicalRepoID string                          `json:"canonical_repo_id,omitempty"`
	AcceptedAliases []string                        `json:"accepted_aliases,omitempty"`
	Candidates      []RepositoryResolutionCandidate `json:"candidates,omitempty"`
	Blockers        []SourceBlocker                 `json:"blockers,omitempty"`
}

func ResolveProjectRepository(raw string, repos map[string]store.ProjectRepository) RepositoryResolutionResult {
	input := strings.TrimSpace(raw)
	result := RepositoryResolutionResult{Input: input}
	if input == "" {
		result.Blockers = []SourceBlocker{unknownRepositoryBlocker(input, repos)}
		return result
	}
	if repo, ok := repos[input]; ok {
		result.CanonicalRepoID = repo.RepoID
		result.AcceptedAliases = acceptedRepositoryAliases(repo.RepoID)
		return result
	}

	var matches []RepositoryResolutionCandidate
	for _, repoID := range sortedRegisteredRepoIDs(repos) {
		aliases := acceptedRepositoryAliases(repoID)
		if repositoryAliasMatches(input, aliases) {
			repo := repos[repoID]
			matches = append(matches, repositoryResolutionCandidate(repo, aliases))
		}
	}
	switch len(matches) {
	case 0:
		result.Blockers = []SourceBlocker{unknownRepositoryBlocker(input, repos)}
	case 1:
		result.CanonicalRepoID = matches[0].RepoID
		result.AcceptedAliases = matches[0].AcceptedAliases
	default:
		result.Candidates = matches
		result.Blockers = []SourceBlocker{ambiguousRepositoryBlocker(input, matches)}
	}
	return result
}

func acceptedRepositoryAliases(repoID string) []string {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return nil
	}
	aliases := []string{repoID}
	if idx := strings.LastIndex(repoID, "/"); idx >= 0 && idx+1 < len(repoID) {
		suffix := repoID[idx+1:]
		if suffix != "" && suffix != repoID {
			aliases = append(aliases, suffix)
		}
	}
	return aliases
}

func repositoryAliasMatches(input string, aliases []string) bool {
	for _, alias := range aliases {
		if input == alias {
			return true
		}
	}
	return false
}

func sortedRegisteredRepoIDs(repos map[string]store.ProjectRepository) []string {
	repoIDs := make([]string, 0, len(repos))
	for repoID := range repos {
		repoID = strings.TrimSpace(repoID)
		if repoID != "" {
			repoIDs = append(repoIDs, repoID)
		}
	}
	sort.Strings(repoIDs)
	return repoIDs
}

func repositoryResolutionCandidate(repo store.ProjectRepository, aliases []string) RepositoryResolutionCandidate {
	status := "enabled"
	if repo.Enabled == 0 {
		status = "disabled"
	}
	return RepositoryResolutionCandidate{
		RepoID:           repo.RepoID,
		AcceptedAliases:  append([]string(nil), aliases...),
		RepositoryRole:   repo.Role,
		RepositoryStatus: status,
	}
}

func unknownRepositoryBlocker(input string, repos map[string]store.ProjectRepository) SourceBlocker {
	evidence := []string{fmt.Sprintf("repository alias %q did not match any registered project repository ID or accepted suffix alias", input)}
	registered := sortedRegisteredRepoIDs(repos)
	if len(registered) > 0 {
		evidence = append(evidence, "registered repository IDs: "+strings.Join(registered, ", "))
	}
	return SourceBlocker{
		RepoID:      input,
		Code:        SourceBlockerUnknownRepository,
		Message:     fmt.Sprintf("repository alias %q is not registered for this project", input),
		Recoverable: true,
		Evidence:    evidence,
		NextActions: []string{
			"Use a canonical registered repository ID from the project repository list.",
			"Use a unique suffix alias only when it maps to exactly one registered repository.",
			"Register the repository before resolving it if it belongs to this project.",
		},
	}
}

func ambiguousRepositoryBlocker(input string, candidates []RepositoryResolutionCandidate) SourceBlocker {
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.RepoID)
	}
	sort.Strings(candidateIDs)
	return SourceBlocker{
		RepoID:      input,
		Code:        SourceBlockerAmbiguousRepository,
		Message:     fmt.Sprintf("repository alias %q matches multiple registered project repositories", input),
		Recoverable: true,
		Evidence: []string{
			"candidate repository IDs: " + strings.Join(candidateIDs, ", "),
		},
		NextActions: []string{
			"Use the full canonical repository ID for the intended repository.",
		},
	}
}
