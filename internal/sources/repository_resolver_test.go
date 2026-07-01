package sources

import (
	"testing"

	"relay/internal/store"
)

func TestResolveProjectRepositoryExactCanonical(t *testing.T) {
	repos := resolverRepos("Paintersrp/relay", "relay-contracts")

	result := ResolveProjectRepository(" Paintersrp/relay ", repos)

	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
	if result.CanonicalRepoID != "Paintersrp/relay" {
		t.Fatalf("expected canonical repo ID, got %+v", result)
	}
	if !stringSliceContains(result.AcceptedAliases, "Paintersrp/relay") || !stringSliceContains(result.AcceptedAliases, "relay") {
		t.Fatalf("expected canonical and suffix aliases, got %+v", result.AcceptedAliases)
	}
}

func TestResolveProjectRepositoryUniqueSuffixAlias(t *testing.T) {
	result := ResolveProjectRepository("relay-contracts", resolverRepos("Paintersrp/relay", "openai/relay-contracts"))

	if len(result.Blockers) != 0 || result.CanonicalRepoID != "openai/relay-contracts" {
		t.Fatalf("expected unique suffix resolution, got %+v", result)
	}
}

func TestResolveProjectRepositoryOwnerQualifiedCanonical(t *testing.T) {
	result := ResolveProjectRepository("openai/relay-contracts", resolverRepos("openai/relay-contracts"))

	if len(result.Blockers) != 0 || result.CanonicalRepoID != "openai/relay-contracts" {
		t.Fatalf("expected owner-qualified canonical resolution, got %+v", result)
	}
}

func TestResolveProjectRepositoryUnknownReturnsRecoverableBlocker(t *testing.T) {
	result := ResolveProjectRepository("missing", resolverRepos("Paintersrp/relay"))

	if len(result.Blockers) != 1 {
		t.Fatalf("expected one blocker, got %+v", result)
	}
	blocker := result.Blockers[0]
	if blocker.Code != SourceBlockerUnknownRepository || !blocker.Recoverable || len(blocker.Evidence) == 0 || len(blocker.NextActions) == 0 {
		t.Fatalf("expected recoverable unknown_repository blocker, got %+v", blocker)
	}
}

func TestResolveProjectRepositoryAmbiguousReturnsCandidates(t *testing.T) {
	result := ResolveProjectRepository("relay", resolverRepos("Paintersrp/relay", "example/relay"))

	if len(result.Blockers) != 1 || result.Blockers[0].Code != SourceBlockerAmbiguousRepository {
		t.Fatalf("expected alias_ambiguous blocker, got %+v", result.Blockers)
	}
	if result.Blockers[0].Code != "alias_ambiguous" {
		t.Fatalf("expected serialized blocker code alias_ambiguous, got %q", result.Blockers[0].Code)
	}
	if result.CanonicalRepoID != "" || len(result.Candidates) != 2 {
		t.Fatalf("expected candidates without canonical selection, got %+v", result)
	}
}

func TestResolveProjectRepositoryDoesNotInferFilesystemPaths(t *testing.T) {
	pathLike := `C:\Users\trist\relay`
	result := ResolveProjectRepository(pathLike, resolverRepos("Paintersrp/relay"))

	if len(result.Blockers) != 1 || result.Blockers[0].Code != SourceBlockerUnknownRepository {
		t.Fatalf("expected path-like input to remain unknown on this platform, got %+v", result)
	}
}

func resolverRepos(repoIDs ...string) map[string]store.ProjectRepository {
	repos := make(map[string]store.ProjectRepository, len(repoIDs))
	for _, repoID := range repoIDs {
		repos[repoID] = store.ProjectRepository{RepoID: repoID, Role: "primary", Enabled: 1}
	}
	return repos
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
