package pipeline

import (
	"os"
	"path/filepath"
)

// HandoffPreflight holds the overall readiness status and individual checks.
type HandoffPreflight struct {
	Status string                  `json:"status"` // ready, blocked, warning
	Checks []HandoffPreflightCheck `json:"checks"`
}

// HandoffPreflightCheck is a single preflight check for the handoff.
type HandoffPreflightCheck struct {
	Key     string `json:"key"`
	Status  string `json:"status"` // pass, warn, block
	Summary string `json:"summary"`
}

// BuildArtifactManifest builds a HandoffArtifactManifest from an artifact dir
// and a list of known artifact paths indexed by kind.
func BuildArtifactManifest(artifactDir string, kindPaths map[string]string) HandoffArtifactManifest {
	required := []HandoffArtifactItem{}
	optional := []HandoffArtifactItem{}

	// required: agent_prompt
	if path, ok := kindPaths["agent_prompt"]; ok && path != "" {
		required = append(required, HandoffArtifactItem{
			Kind:        "agent_prompt",
			Path:        path,
			Filename:    ArtifactFilename("agent_prompt"),
			MediaType:   "text/plain",
			Description: "Compact prompt for the running repo agent",
		})
	}

	optionalKinds := []struct {
		kind        string
		mediaType   string
		description string
	}{
		{"original_handoff", "text/plain", "Source handoff retained for review/debugging"},
		{"handoff_validation_json", "application/json", "Relay intake review metadata"},
		{"opencode_handoff_packet", "application/json", "This packet"},
	}

	for _, ok := range optionalKinds {
		if path, found := kindPaths[ok.kind]; found && path != "" {
			optional = append(optional, HandoffArtifactItem{
				Kind:        ok.kind,
				Path:        path,
				Filename:    ArtifactFilename(ok.kind),
				MediaType:   ok.mediaType,
				Description: ok.description,
			})
		}
	}

	return HandoffArtifactManifest{
		Dir:      artifactDir,
		Required: required,
		Optional: optional,
	}
}

// BuildHandoffPreflight evaluates readiness for an OpenCode handoff.
// repoPath: the target repo path (may be empty)
// branchName: the selected branch/worktree (may be empty)
// selectedModel: the selected model (may be empty)
// agentPromptPath: path to the agent_prompt artifact (may be empty)
// opencodePacketPath: path to the opencode_handoff_packet artifact (may be empty)
// requiredPaths: a map from check key to artifact path for required artifact existence checks
func BuildHandoffPreflight(repoPath, branchName, selectedModel, agentPromptPath, opencodePacketPath string, requiredPaths map[string]string) HandoffPreflight {
	checks := []HandoffPreflightCheck{}

	// Repo path check
	if repoPath == "" {
		checks = append(checks, HandoffPreflightCheck{
			Key: "repo_path", Status: "block", Summary: "Repo path is empty",
		})
	} else if info, err := os.Stat(repoPath); err != nil || !info.IsDir() {
		checks = append(checks, HandoffPreflightCheck{
			Key: "repo_path", Status: "block", Summary: "Repo path does not exist or is not a directory: " + repoPath,
		})
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "repo_path", Status: "pass", Summary: repoPath,
		})
	}

	// Repo git check
	if repoPath != "" {
		gitPath := filepath.Join(repoPath, ".git")
		if _, err := os.Stat(gitPath); err != nil {
			checks = append(checks, HandoffPreflightCheck{
				Key: "repo_git", Status: "block", Summary: ".git metadata missing at " + gitPath,
			})
		} else {
			checks = append(checks, HandoffPreflightCheck{
				Key: "repo_git", Status: "pass", Summary: ".git metadata present",
			})
		}
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "repo_git", Status: "block", Summary: "Cannot check .git: repo path not set",
		})
	}

	// Branch/worktree check
	if branchName == "" {
		checks = append(checks, HandoffPreflightCheck{
			Key: "branch", Status: "warn", Summary: "Branch/worktree is empty",
		})
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "branch", Status: "pass", Summary: branchName,
		})
	}

	// Selected model check
	if selectedModel == "" {
		checks = append(checks, HandoffPreflightCheck{
			Key: "selected_model", Status: "block", Summary: "Selected model is empty",
		})
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "selected_model", Status: "pass", Summary: selectedModel,
		})
	}

	// Agent prompt check
	if agentPromptPath == "" {
		checks = append(checks, HandoffPreflightCheck{
			Key: "agent_prompt", Status: "block", Summary: "Agent Prompt artifact not generated",
		})
	} else if _, err := os.Stat(agentPromptPath); err != nil {
		checks = append(checks, HandoffPreflightCheck{
			Key: "agent_prompt", Status: "block", Summary: "Agent Prompt file missing or unreadable: " + agentPromptPath,
		})
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "agent_prompt", Status: "pass", Summary: agentPromptPath,
		})
	}

	// OpenCode packet check
	if opencodePacketPath == "" {
		checks = append(checks, HandoffPreflightCheck{
			Key: "opencode_packet", Status: "warn", Summary: "Agent Packet not generated yet",
		})
	} else if _, err := os.Stat(opencodePacketPath); err != nil {
		checks = append(checks, HandoffPreflightCheck{
			Key: "opencode_packet", Status: "warn", Summary: "Agent Packet file missing: " + opencodePacketPath,
		})
	} else {
		checks = append(checks, HandoffPreflightCheck{
			Key: "opencode_packet", Status: "pass", Summary: opencodePacketPath,
		})
	}

	// Required artifacts existence check (from manifest required items)
	for key, path := range requiredPaths {
		if _, err := os.Stat(path); err != nil {
			checks = append(checks, HandoffPreflightCheck{
				Key: "artifact_manifest", Status: "block", Summary: "Required artifact missing or unreadable: " + key + " at " + path,
			})
		}
	}
	if len(requiredPaths) > 0 {
		allPass := true
		for _, c := range checks {
			if c.Status == "block" {
				allPass = false
				break
			}
		}
		// Only add a pass check if we haven't already added a block
		if allPass {
			checks = append(checks, HandoffPreflightCheck{
				Key: "artifact_manifest", Status: "pass", Summary: "All required artifacts exist and are readable",
			})
		}
	}

	// Determine overall status
	status := "ready"
	for _, c := range checks {
		if c.Status == "block" {
			status = "blocked"
			break
		} else if c.Status == "warn" && status != "blocked" {
			status = "warning"
		}
	}

	return HandoffPreflight{
		Status: status,
		Checks: checks,
	}
}
