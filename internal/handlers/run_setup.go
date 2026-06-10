package handlers

import (
	"encoding/json"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
	"relay/internal/store"
)

// Compile-time check that RunsHandler has the required methods.
var _ runsHandlerSetup = (*RunsHandler)(nil)

type runsHandlerSetup interface {
	prepareRunForReview(runID int64) RunSetupResult
}

// RunSetupResult describes the outcome of the automatic setup pipeline.
type RunSetupResult struct {
	ValidationStatus string
	PromptGenerated  bool
	PacketGenerated  bool
	Blocked          bool
	Blockers         []string
}

// promptSetupResult describes the outcome of the prompt generation step.
type promptSetupResult struct {
	Generated bool
	Blocked   bool
	Blockers  []string
}

// prepareRunForReview runs the full setup pipeline: validate → prompt → packet.
// It reuses the same lower-level function calls as the manual action handlers.
// It has no HTTP side effects so it can be called from both handoff creation
// and the manual action handlers.
func (h *RunsHandler) prepareRunForReview(runID int64) RunSetupResult {
	_, err := h.store.GetRun(runID)
	if err != nil {
		h.log.Error("setup: get run", "run_id", runID, "error", err)
		return RunSetupResult{Blocked: true, Blockers: []string{"run not found"}}
	}

	result := RunSetupResult{}
	h.log.Info("setup: starting automatic setup", "run_id", runID)

	// Step 1: Validate handoff
	status, blockers := h.runValidateForSetup(runID)
	result.ValidationStatus = status
	if len(blockers) > 0 {
		result.Blocked = true
		result.Blockers = blockers
		h.log.Info("setup: blocked by intake review", "run_id", runID, "blockers", blockers)
		return result
	}

	// Step 2: Generate Agent Prompt
	prompt := h.runPreparePromptForSetup(runID)
	if prompt.Blocked {
		result.Blocked = true
		result.Blockers = prompt.Blockers
		h.log.Info("setup: blocked by intake review", "run_id", runID, "blockers", prompt.Blockers)
		return result
	}

	result.PromptGenerated = prompt.Generated
	if !prompt.Generated {
		h.log.Warn("setup: prompt generation failed, skipping packet", "run_id", runID)
		return result
	}

	// Step 3: Generate OpenCode packet
	packetOK := h.runGeneratePacketForSetup(runID)
	result.PacketGenerated = packetOK
	if !packetOK {
		h.log.Warn("setup: packet generation failed", "run_id", runID)
	}

	h.log.Info("setup: automatic setup complete", "run_id", runID)
	return result
}

// runValidateForSetup reuses validation logic without HTTP responses.
// Returns (reportStatus, blockers).
func (h *RunsHandler) runValidateForSetup(runID int64) (string, []string) {
	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("setup: read handoff for validation", "run_id", runID, "error", err)
		return "error", []string{"handoff not found on disk"}
	}

	run, err := h.store.GetRun(runID)
	if err != nil {
		return "error", []string{"run not found"}
	}

	report := pipeline.ValidateHandoff(string(handoffData), run.RecommendedModel)

	reportJSON, _ := report.JSON()
	reportPath, err := artifacts.Write(runID, "handoff_validation_json", pipeline.ArtifactFilename("handoff_validation_json"), reportJSON)
	if err == nil {
		h.store.CreateArtifact(runID, "handoff_validation_json", reportPath, "application/json")
	}

	h.store.DeleteChecksByRunKind(runID, "validation")

	for _, c := range report.Checks {
		detailsJSON, _ := json.Marshal(c)
		h.store.CreateCheck(runID, "validation", c.Status, c.Summary, string(detailsJSON))
	}

	newStatus := "draft"
	switch report.Status {
	case "ready":
		newStatus = "validated"
	case "needs_fix":
		newStatus = "needs_cleanup"
	case "needs_review":
		newStatus = "needs_review"
	}
	h.store.UpdateRunStatus(runID, newStatus)

	// Determine blockers
	var blockers []string
	for _, c := range report.Checks {
		if c.Status == "fail" {
			blockers = append(blockers, c.Summary)
		}
	}

	h.store.CreateEvent(runID, "info", "[Auto] Handoff validation completed: "+report.Status)
	h.log.Info("setup: handoff validated", "run_id", runID, "status", report.Status)

	return report.Status, blockers
}

// runPreparePromptForSetup reuses prompt generation logic without HTTP responses.
// Returns a promptSetupResult indicating whether the prompt was generated or
// blocked.
func (h *RunsHandler) runPreparePromptForSetup(runID int64) promptSetupResult {
	handoffData, err := artifacts.Read(runID, "original_handoff", pipeline.ArtifactFilename("original_handoff"))
	if err != nil {
		h.log.Error("setup: read handoff for prompt", "run_id", runID, "error", err)
		return promptSetupResult{Generated: false}
	}

	run, err := h.store.GetRun(runID)
	if err != nil {
		return promptSetupResult{Generated: false}
	}

	repo, _ := h.store.GetRepo(run.RepoID)
	repoPath := ""
	repoDefaults := ""
	if repo != nil {
		repoPath = repo.Path
		repoDefaults = repo.DefaultValidationCommands
	}

	metadata := pipeline.ParseHandoffMetadata(string(handoffData), repoDefaults)
	review := pipeline.BuildIntakeReview(metadata, repoPath)

	if len(review.Blockers) > 0 {
		h.store.UpdateRunStatus(runID, "needs_review")
		h.store.CreateEvent(runID, "warn", "[Auto] Automatic setup stopped: Intake Review has blockers.")
		h.store.CreateEvent(runID, "warn", "[Auto] "+strings.Join(review.Blockers, "; "))
		h.log.Warn("setup: blocked from generating prompt by intake review blockers", "run_id", runID)
		return promptSetupResult{
			Generated: false,
			Blocked:   true,
			Blockers:  review.Blockers,
		}
	}

	prompt := pipeline.PreparePrompt(string(handoffData))

	promptPath, err := artifacts.Write(runID, "agent_prompt", pipeline.ArtifactFilename("agent_prompt"), []byte(prompt))
	if err != nil {
		h.log.Error("setup: write agent prompt", "run_id", runID, "error", err)
		return promptSetupResult{Generated: false}
	}

	h.store.CreateArtifact(runID, "agent_prompt", promptPath, "text/plain")
	h.store.UpdateRunStatus(runID, "ready")
	h.store.CreateEvent(runID, "info", "[Auto] Agent prompt generated")

	h.log.Info("setup: agent prompt generated", "run_id", runID)
	return promptSetupResult{Generated: true}
}

// runGeneratePacketForSetup reuses packet generation logic without HTTP responses.
// Returns true if the packet was generated.
func (h *RunsHandler) runGeneratePacketForSetup(runID int64) bool {
	run, err := h.store.GetRun(runID)
	if err != nil {
		h.log.Error("setup: get run for packet", "run_id", runID, "error", err)
		return false
	}

	repo, err := h.store.GetRepo(run.RepoID)
	if err != nil {
		h.log.Error("setup: get repo for packet", "run_id", runID, "error", err)
		return false
	}

	artifactsList, err := h.store.ListArtifactsByRun(runID)
	if err != nil {
		h.log.Error("setup: list artifacts for packet", "run_id", runID, "error", err)
		return false
	}

	var promptArtifact *store.Artifact
	for i := range artifactsList {
		if artifactsList[i].Kind == "agent_prompt" {
			promptArtifact = &artifactsList[i]
			break
		}
	}
	if promptArtifact == nil {
		for i := range artifactsList {
			if artifactsList[i].Kind == "ready_prompt" {
				promptArtifact = &artifactsList[i]
				break
			}
		}
	}
	if promptArtifact == nil {
		h.log.Warn("setup: no agent prompt artifact found for packet", "run_id", runID)
		return false
	}

	packet := pipeline.NewOpenCodeHandoffPacket(
		run.ID,
		repo.Path,
		run.BranchName,
		run.SelectedModel,
		run.RecommendedModel,
		promptArtifact.Path,
		artifacts.Dir(run.ID),
	)

	packetJSON, err := pipeline.MarshalOpenCodeHandoffPacket(packet)
	if err != nil {
		h.log.Error("setup: marshal packet", "run_id", runID, "error", err)
		return false
	}

	packetPath, err := artifacts.Write(runID, "opencode_handoff_packet", pipeline.ArtifactFilename("opencode_handoff_packet"), packetJSON)
	if err != nil {
		h.log.Error("setup: write packet", "run_id", runID, "error", err)
		return false
	}

	h.store.CreateArtifact(runID, "opencode_handoff_packet", packetPath, "application/json")
	h.store.CreateEvent(runID, "info", "[Auto] OpenCode handoff packet generated")

	h.log.Info("setup: opencode packet generated", "run_id", runID)
	return true
}
