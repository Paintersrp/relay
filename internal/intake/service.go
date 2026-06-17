package intake

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
)

// Service provides shared intake logic for creating Relay runs from planner handoff markdown.
// Both the HTTP API handler (internal/api) and MCP tools (internal/mcp) use this service
// to avoid duplicating run-creation semantics.
type Service struct {
	store *store.Store
}

// NewService constructs an intake Service backed by the given store.
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// CreateRunInput holds the caller-supplied arguments for creating a run from a planner handoff.
type CreateRunInput struct {
	// Markdown is the full planner handoff markdown text. Required.
	Markdown string
	// RepoTarget is an explicit repo name or path. Falls back to frontmatter if empty.
	RepoTarget string
	// BranchContext is an explicit branch name. Falls back to frontmatter, then "main".
	BranchContext string
	// Name is an explicit run title. Falls back to frontmatter title, then markdown H1.
	Name string
	// Source tags the creation origin (e.g., "mcp_chat", "api"). Default "api".
	Source string
}

// ValidationSummary carries intake validation results.
type ValidationSummary struct {
	Warnings []string `json:"warnings"`
	Blockers []string `json:"blockers"`
	Passed   bool     `json:"passed"`
}

// CreateRunOutput holds the result of a successful run creation.
type CreateRunOutput struct {
	RunID             int64             `json:"run_id"`
	Status            string            `json:"status"`
	LifecycleState    string            `json:"lifecycle_state"`
	ReviewURL         string            `json:"review_url"`
	ArtifactKinds     []string          `json:"artifact_kinds"`
	ValidationSummary ValidationSummary `json:"validation_summary"`
}

// CreateRunFromHandoff creates a new Relay run from planner handoff markdown.
// It uses the same intake semantics as POST /api/intake/planner-handoff.
//
// Safety: this function does not read arbitrary files. Markdown must be passed as
// an explicit argument; the caller is responsible for not including secrets, tokens,
// auth headers, private keys, or signed URLs in the markdown content.
func (svc *Service) CreateRunFromHandoff(input CreateRunInput) (*CreateRunOutput, error) {
	if strings.TrimSpace(input.Markdown) == "" {
		return nil, fmt.Errorf("planner_handoff_markdown is required and must not be empty")
	}

	metadata, _, _, _ := ParseFrontmatter(input.Markdown)
	warnings, blockers := ValidateHandoffText(input.Markdown)

	if len(blockers) > 0 {
		return nil, fmt.Errorf("handoff validation blocked: %s", strings.Join(blockers, "; "))
	}

	// Resolve repo target: explicit arg > frontmatter repo > frontmatter repo_target.
	repoTarget := input.RepoTarget
	if repoTarget == "" {
		repoTarget = metadata["repo"]
	}
	if repoTarget == "" {
		repoTarget = metadata["repo_target"]
	}
	if repoTarget == "" {
		return nil, fmt.Errorf("no repository target found in arguments or frontmatter (set repo_target or include repo: in frontmatter)")
	}

	repo, err := resolveRepo(svc.store, repoTarget)
	if err != nil {
		return nil, fmt.Errorf("resolve repository %q: %w", repoTarget, err)
	}

	// Resolve branch: explicit arg > frontmatter branch > frontmatter branch_context > "main".
	branchContext := input.BranchContext
	if branchContext == "" {
		branchContext = metadata["branch"]
	}
	if branchContext == "" {
		branchContext = metadata["branch_context"]
	}
	if branchContext == "" {
		branchContext = "main"
	}

	// Resolve title: explicit name > frontmatter title > markdown H1 > "Untitled Run".
	title := input.Name
	if title == "" {
		title = metadata["title"]
	}
	if title == "" {
		title = deriveTitle(input.Markdown)
	}

	recommendedModel := metadata["recommended_model"]
	if recommendedModel == "" {
		recommendedModel = "deepseek-v4-flash"
	}
	selectedModel := recommendedModel

	status := "intake_received"
	if len(warnings) > 0 {
		status = "intake_needs_review"
	}

	run, err := svc.store.CreateRun(repo.ID, title, status, recommendedModel, selectedModel, branchContext)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Write validation checks.
	_ = svc.store.DeleteChecksByRunKind(run.ID, "validation")
	if len(warnings) > 0 {
		for _, w := range warnings {
			_, _ = svc.store.CreateCheck(run.ID, "validation", "warning", w, "{}")
		}
	} else {
		_, _ = svc.store.CreateCheck(run.ID, "validation", "pass", "Intake validation successful", "{}")
	}

	// Write artifacts through relay artifact conventions.
	artifactKinds := []string{}

	_ = svc.store.DeleteArtifactsByRunKind(run.ID, "planner_handoff")
	if path, werr := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(input.Markdown)); werr == nil {
		_, _ = svc.store.CreateArtifact(run.ID, "planner_handoff", path, "text/markdown")
		artifactKinds = append(artifactKinds, "planner_handoff")
	}

	_ = svc.store.DeleteArtifactsByRunKind(run.ID, "parsed_frontmatter")
	fmJSON, _ := json.MarshalIndent(metadata, "", "  ")
	if path, werr := artifacts.Write(run.ID, "parsed_frontmatter", "parsed_frontmatter.json", fmJSON); werr == nil {
		_, _ = svc.store.CreateArtifact(run.ID, "parsed_frontmatter", path, "application/json")
		artifactKinds = append(artifactKinds, "parsed_frontmatter")
	}

	source := input.Source
	if source == "" {
		source = "api"
	}
	configMap := map[string]string{
		"repo_target":    repo.Path,
		"branch_context": branchContext,
		"source":         source,
		"created_from":   "intake_service",
	}
	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	_ = svc.store.DeleteArtifactsByRunKind(run.ID, "run_config")
	if path, werr := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON); werr == nil {
		_, _ = svc.store.CreateArtifact(run.ID, "run_config", path, "application/json")
		artifactKinds = append(artifactKinds, "run_config")
	}

	report := map[string]interface{}{
		"status":   run.Status,
		"warnings": warnings,
		"blockers": blockers,
	}
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	_ = svc.store.DeleteArtifactsByRunKind(run.ID, "intake_validation_report")
	if path, werr := artifacts.Write(run.ID, "intake_validation_report", "intake_validation_report.json", reportJSON); werr == nil {
		_, _ = svc.store.CreateArtifact(run.ID, "intake_validation_report", path, "application/json")
		artifactKinds = append(artifactKinds, "intake_validation_report")
	}

	_, _ = svc.store.CreateEvent(run.ID, "info",
		fmt.Sprintf("Handoff intake receipt via %s: planner handoff registered", source))

	return &CreateRunOutput{
		RunID:          run.ID,
		Status:         run.Status,
		LifecycleState: "intake",
		ReviewURL:      fmt.Sprintf("/runs/%d/intake", run.ID),
		ArtifactKinds:  artifactKinds,
		ValidationSummary: ValidationSummary{
			Warnings: warnings,
			Blockers: blockers,
			Passed:   len(warnings) == 0 && len(blockers) == 0,
		},
	}, nil
}

// resolveRepo finds or creates a repo by name or path in the store.
// Mirrors the same logic used in internal/api's resolveRepo helper.
func resolveRepo(s *store.Store, repoNameOrPath string) (*store.Repo, error) {
	if repoNameOrPath == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if repo, err := s.GetRepoByName(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	if repo, err := s.GetRepoByPath(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	baseName := filepath.Base(repoNameOrPath)
	if repo, err := s.GetRepoByName(baseName); err == nil && repo != nil {
		return repo, nil
	}
	if repos, err := s.ListRepos(); err == nil {
		for _, r := range repos {
			if strings.EqualFold(r.Name, repoNameOrPath) || strings.EqualFold(r.Name, baseName) {
				rCopy := r
				return &rCopy, nil
			}
		}
	}
	return s.CreateRepo(baseName, repoNameOrPath)
}

// deriveTitle extracts the first H1 heading from markdown, or returns "Untitled Run".
func deriveTitle(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return "Untitled Run"
}
