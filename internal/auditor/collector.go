package auditor

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
)

// artifactStore is the narrow store interface the Collector requires.
type artifactStore interface {
	GetRun(id int64) (*store.Run, error)
	ListArtifactsByRunKind(runID int64, kind string) ([]store.Artifact, error)
}

// Collector gathers structured evidence for a run's audit.
type Collector struct {
	store artifactStore
}

// NewCollector creates a Collector backed by the given store.
func NewCollector(s *store.Store) *Collector {
	return &Collector{store: s}
}

// boundedPreview truncates data to max bytes and appends an ellipsis.
func boundedPreview(data []byte, max int) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) > max {
		return string(data[:max]) + "\n...[truncated]"
	}
	return string(data)
}

// redactSecrets removes known secret-like patterns from text before rendering.
// Covers env-var-like tokens, bearer tokens, and generic key=value patterns.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-_.~+/]+=*`),
	regexp.MustCompile(`(?i)(Authorization:\s*)[^\r\n]+`),
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)(token\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)(secret\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)\S+`),
}

func redactSecrets(input string) string {
	result := input
	for _, re := range secretPatterns {
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the prefix (group 1), replace the value with [REDACTED]
			locs := re.FindStringSubmatchIndex(match)
			if len(locs) >= 4 {
				prefix := match[locs[2]:locs[3]]
				return prefix + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}

// Collect gathers all evidence for the given run.
func (c *Collector) Collect(runID int64) (*Evidence, error) {
	run, err := c.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	ev := &Evidence{
		RunID:     runID,
		RunTitle:  run.Title,
		RunStatus: run.Status,
	}

	c.collectPacketMetadata(runID, ev)
	c.collectExecutorResult(runID, ev)
	c.collectValidationResults(runID, ev)
	c.collectChangedFiles(runID, ev)
	c.collectGitDiff(runID, ev)
	c.evaluateChecklistResults(ev)
	c.evaluateFileScopeResults(ev)
	c.evaluateNonGoalResults(ev)

	return ev, nil
}

// canonicalPacketRaw is the top-level canonical_packet.json structure we parse.
type canonicalPacketRaw struct {
	AuditSeed        *auditSeedRaw        `json:"audit_seed"`
	ExecutionPayload *executionPayloadRaw `json:"execution_payload"`
}

type auditSeedRaw struct {
	AuditChecklist  json.RawMessage `json:"audit_checklist"`
	NonGoalChecks   []string        `json:"non_goal_checks"`
	FileScopeChecks []string        `json:"file_scope_checks"`
}

type executionPayloadRaw struct {
	Goal               json.RawMessage          `json:"goal"`
	Scope              json.RawMessage          `json:"scope"`
	NonGoals           json.RawMessage          `json:"non_goals"`
	FileTargets        []string                 `json:"file_targets"`
	ValidationCommands []validationCommandRaw   `json:"validation_commands"`
}

type validationCommandRaw struct {
	ID              string `json:"id"`
	Command         string `json:"command"`
	Required        bool   `json:"required"`
	Purpose         string `json:"purpose"`
	SuccessSignal   string `json:"success_signal"`
	FailureHandling string `json:"failure_handling"`
}

// extractStringField handles both string and []string JSON fields.
func extractStringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s), s != ""
	}
	// Try array of strings
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		joined := strings.Join(arr, "\n")
		return strings.TrimSpace(joined), len(arr) > 0
	}
	return "", false
}

// parseChecklistItems handles old flat-string and new typed-object audit checklist formats.
func parseChecklistItems(raw json.RawMessage) []ChecklistItem {
	if len(raw) == 0 {
		return nil
	}
	// Try new typed-object format: [{id, check, severity_if_failed}]
	type checkItemRaw struct {
		ID               string `json:"id"`
		Check            string `json:"check"`
		SeverityIfFailed string `json:"severity_if_failed"`
	}
	var typed []checkItemRaw
	if err := json.Unmarshal(raw, &typed); err == nil && len(typed) > 0 {
		// Only accept if at least one item has a non-empty Check field (objects, not strings)
		if typed[0].Check != "" {
			items := make([]ChecklistItem, 0, len(typed))
			for _, t := range typed {
				if t.Check == "" {
					continue
				}
				sev := CheckSeverity(t.SeverityIfFailed)
				if sev == "" {
					sev = SeverityWarning
				}
				items = append(items, ChecklistItem{
					ID:               t.ID,
					Check:            t.Check,
					SeverityIfFailed: sev,
				})
			}
			return items
		}
	}
	// Fall back to old flat-string format: parse meaningful lines
	var flat []string
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil
	}
	items := make([]ChecklistItem, 0)
	var idx int
	var pendingSeverity CheckSeverity
	for _, line := range flat {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		// Skip section headers
		if strings.HasSuffix(lower, ":") && !strings.HasPrefix(lower, "a") && !strings.HasPrefix(lower, "confirm") && !strings.HasPrefix(lower, "verify") {
			pendingSeverity = ""
			continue
		}
		if strings.HasPrefix(lower, "severity_if_failed:") {
			val := strings.TrimSpace(line[len("severity_if_failed:"):])
			pendingSeverity = CheckSeverity(strings.ToLower(val))
			continue
		}
		// This is a check item line
		idx++
		sev := pendingSeverity
		if sev == "" {
			sev = SeverityWarning
		}
		pendingSeverity = ""
		items = append(items, ChecklistItem{
			ID:               fmt.Sprintf("A%d", idx),
			Check:            line,
			SeverityIfFailed: sev,
		})
	}
	return items
}

func (c *Collector) collectPacketMetadata(runID int64, ev *Evidence) {
	meta := PacketMetadata{
		PacketID: fmt.Sprintf("packet-%d", runID),
	}

	data, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "canonical_packet.json not found — goal, scope, non-goals, checklist unavailable",
			Severity: SeverityBlocker,
		})
		ev.Packet = meta
		return
	}

	var pkt canonicalPacketRaw
	if err := json.Unmarshal(data, &pkt); err != nil {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  fmt.Sprintf("canonical_packet.json parse error: %v — goal, scope, non-goals unavailable", err),
			Severity: SeverityBlocker,
		})
		ev.Packet = meta
		return
	}

	ep := pkt.ExecutionPayload
	if ep == nil {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "canonical_packet.json missing execution_payload — goal, scope, non-goals unavailable",
			Severity: SeverityBlocker,
		})
		meta.MissingFields = append(meta.MissingFields, "execution_payload")
		ev.Packet = meta
		return
	}

	// Parse goal
	if goal, ok := extractStringField(ep.Goal); ok {
		meta.Goal = goal
	} else {
		meta.MissingFields = append(meta.MissingFields, "execution_payload.goal")
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "execution_payload.goal missing or empty in canonical_packet.json",
			Severity: SeverityError,
		})
	}

	// Parse scope
	if scope, ok := extractStringField(ep.Scope); ok {
		meta.Scope = scope
	} else {
		meta.MissingFields = append(meta.MissingFields, "execution_payload.scope")
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "execution_payload.scope missing or empty in canonical_packet.json",
			Severity: SeverityError,
		})
	}

	// Parse non-goals
	if nonGoals, ok := extractStringField(ep.NonGoals); ok {
		meta.NonGoals = nonGoals
	} else {
		meta.MissingFields = append(meta.MissingFields, "execution_payload.non_goals")
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "execution_payload.non_goals missing or empty in canonical_packet.json",
			Severity: SeverityWarning,
		})
	}

	// Parse file targets
	meta.FileTargets = ep.FileTargets

	// Parse validation commands
	for _, vc := range ep.ValidationCommands {
		meta.ValidationCommands = append(meta.ValidationCommands, ValidationCommandSpec{
			ID:              vc.ID,
			Command:         vc.Command,
			Required:        vc.Required,
			Purpose:         vc.Purpose,
			SuccessSignal:   vc.SuccessSignal,
			FailureHandling: vc.FailureHandling,
		})
	}

	// Parse audit checklist and related checks from audit_seed
	if pkt.AuditSeed != nil {
		meta.AuditChecklist = parseChecklistItems(pkt.AuditSeed.AuditChecklist)
		meta.NonGoalChecks = pkt.AuditSeed.NonGoalChecks
		meta.FileScopeChecks = pkt.AuditSeed.FileScopeChecks
	} else {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "canonical_packet.json missing audit_seed — audit checklist unavailable",
			Severity: SeverityError,
		})
	}

	if len(meta.AuditChecklist) == 0 {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "audit_seed.audit_checklist is empty or could not be parsed — checklist items unavailable",
			Severity: SeverityError,
		})
	}

	ev.Packet = meta
}

func (c *Collector) collectExecutorResult(runID int64, ev *Evidence) {
	data, err := artifacts.Read(runID, "executor_result", "executor_result.txt")
	if err != nil {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "executor_result.txt not found — executor result evidence unavailable",
			Severity: SeverityError,
		})
		return
	}

	redacted := redactSecrets(string(data))
	preview := boundedPreview([]byte(redacted), MaxPreviewBytes)

	var summary string
	var exitCode string
	for _, line := range strings.Split(redacted, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "status:") || strings.HasPrefix(strings.ToLower(line), "exit_code:") {
			summary += line + "\n"
		}
		if strings.HasPrefix(strings.ToLower(line), "exit_code:") || strings.HasPrefix(strings.ToLower(line), "exitcode:") {
			exitCode = strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}

	rawPath, _ := artifacts.Path(runID, "executor_result", "executor_result.txt")
	ev.ExecutorResult = ExecutorResultEvidence{
		Present:         true,
		Content:         preview,
		Summary:         strings.TrimSpace(summary),
		ExitCode:        exitCode,
		RawArtifactPath: rawPath,
	}
}

func (c *Collector) collectValidationResults(runID int64, ev *Evidence) {
	// Build per-command results from packet validation_commands + available artifacts
	specByID := map[string]ValidationCommandSpec{}
	for _, vc := range ev.Packet.ValidationCommands {
		specByID[vc.ID] = vc
	}

	// Collect raw validation artifacts for evidence
	type valArtifact struct {
		kind string
		path string
		data []byte
	}
	var collected []valArtifact
	kinds := []string{"validation_stdout", "validation_run_json", "validation_stderr"}
	for _, k := range kinds {
		paths := c.listArtifactPaths(runID, k)
		for _, p := range paths {
			d, err := os.ReadFile(p)
			if err == nil && len(d) > 0 {
				collected = append(collected, valArtifact{kind: k, path: p, data: d})
			}
		}
	}

	if len(specByID) == 0 && len(collected) == 0 {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "No validation commands in packet and no validation output artifacts found — validation evidence unavailable",
			Severity: SeverityError,
		})
		return
	}

	if len(collected) == 0 {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "Validation command specs exist in packet but no validation output artifacts found — validation evidence unavailable",
			Severity: SeverityError,
		})
		// Produce unknown results for each required command
		for _, spec := range ev.Packet.ValidationCommands {
			result := CheckUnknown
			rationale := "No validation output artifact found"
			ev.ValidationResults = append(ev.ValidationResults, ValidationCommandResult{
				ID:              spec.ID,
				Command:         spec.Command,
				Required:        spec.Required,
				Status:          result,
				ExitResult:      "not_run",
				EvidenceSummary: rationale,
			})
		}
		return
	}

	// If we have packet commands, try to match artifacts; otherwise produce one generic result
	if len(specByID) > 0 {
		for _, spec := range ev.Packet.ValidationCommands {
			// Find the best matching artifact (use the largest available)
			var best valArtifact
			for _, a := range collected {
				if len(a.data) > len(best.data) {
					best = a
				}
			}
			summary := fmt.Sprintf("sourced from %s artifact (%d bytes)", best.kind, len(best.data))
			redacted := redactSecrets(string(best.data))
			preview := boundedPreview([]byte(redacted), MaxPreviewBytes)
			_ = preview // available if needed for more detail

			status := CheckUnknown
			exitResult := "unknown"
			// Heuristic: check for common pass indicators in stdout
			lower := strings.ToLower(redacted)
			if strings.Contains(lower, "ok\n") || strings.Contains(lower, "pass") || strings.Contains(lower, "exit 0") {
				status = CheckPass
				exitResult = "exit 0 (inferred)"
			} else if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "exit 1") {
				status = CheckFail
				exitResult = "non-zero exit (inferred)"
			}

			ev.ValidationResults = append(ev.ValidationResults, ValidationCommandResult{
				ID:              spec.ID,
				Command:         spec.Command,
				Required:        spec.Required,
				Status:          status,
				ExitResult:      exitResult,
				EvidenceSummary: summary,
				RawArtifactPath: best.path,
			})
		}
	} else {
		// No packet commands — produce one generic validation result
		best := collected[0]
		for _, a := range collected {
			if len(a.data) > len(best.data) {
				best = a
			}
		}
		redacted := redactSecrets(string(best.data))
		summary := fmt.Sprintf("sourced from %s artifact (%d bytes)", best.kind, len(best.data))
		status := CheckUnknown
		exitResult := "unknown"
		lower := strings.ToLower(redacted)
		if strings.Contains(lower, "ok\n") || strings.Contains(lower, "pass") {
			status = CheckPass
			exitResult = "exit 0 (inferred)"
		} else if strings.Contains(lower, "fail") || strings.Contains(lower, "error") {
			status = CheckFail
			exitResult = "non-zero exit (inferred)"
		}
		ev.ValidationResults = append(ev.ValidationResults, ValidationCommandResult{
			ID:              "V?",
			Command:         "(unknown — not in packet)",
			Required:        false,
			Status:          status,
			ExitResult:      exitResult,
			EvidenceSummary: summary,
			RawArtifactPath: best.path,
		})
	}
}

func (c *Collector) collectChangedFiles(runID int64, ev *Evidence) {
	kinds := []string{"git_diff_name_status", "git_status_text", "git_diff_stat"}
	for _, k := range kinds {
		paths := c.listArtifactPaths(runID, k)
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil || len(data) == 0 {
				continue
			}
			var entries []ChangedFileEntry
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Parse "M\tpath" or "M path" format from git diff --name-status
				parts := strings.Fields(line)
				if len(parts) >= 2 && len(parts[0]) <= 2 {
					entries = append(entries, ChangedFileEntry{Status: parts[0], Path: strings.Join(parts[1:], " ")})
				} else {
					entries = append(entries, ChangedFileEntry{Status: "?", Path: line})
				}
			}
			if len(entries) == 0 {
				continue
			}
			if len(entries) > 100 {
				entries = entries[:100]
			}
			ev.ChangedFiles = ChangedFilesEvidence{
				Present:         true,
				Files:           entries,
				RawArtifactPath: p,
				SourceKind:      k,
			}
			return
		}
	}

	ev.Warnings = append(ev.Warnings, EvidenceWarning{
		Message:  "No changed-files artifact found (git_diff_name_status, git_status_text, git_diff_stat) — changed files evidence unavailable",
		Severity: SeverityError,
	})
}

func (c *Collector) collectGitDiff(runID int64, ev *Evidence) {
	data, err := artifacts.Read(runID, "git_diff_patch", "git_diff.patch")
	if err != nil {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "git_diff.patch not found — diff evidence unavailable",
			Severity: SeverityWarning,
		})
		return
	}
	rawPath, _ := artifacts.Path(runID, "git_diff_patch", "git_diff.patch")
	redacted := redactSecrets(string(data))
	ev.GitDiff = DiffEvidence{
		Present:         true,
		Preview:         boundedPreview([]byte(redacted), MaxDiffPreviewBytes),
		RawArtifactPath: rawPath,
	}
}

// evaluateChecklistResults produces a PerCheckResult for each checklist item.
// Missing evidence always yields unknown, never pass.
func (c *Collector) evaluateChecklistResults(ev *Evidence) {
	if len(ev.Packet.AuditChecklist) == 0 {
		ev.ChecklistResults = []PerCheckResult{{
			ID:               "CL-MISSING",
			Check:            "Audit checklist",
			Result:           CheckUnknown,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   "canonical_packet.json",
			Rationale:        "No checklist items could be parsed from canonical_packet.json — manual review required",
		}}
		return
	}

	hasValidation := len(ev.ValidationResults) > 0
	hasChangedFiles := ev.ChangedFiles.Present
	hasDiff := ev.GitDiff.Present

	for _, item := range ev.Packet.AuditChecklist {
		result := CheckUnknown
		rationale := "Requires manual auditor review — no automated evidence available for this check"
		evidenceSource := "manual"

		lowerCheck := strings.ToLower(item.Check)

		// Heuristic evidence matching
		switch {
		case strings.Contains(lowerCheck, "validation") || strings.Contains(lowerCheck, "test") || strings.Contains(lowerCheck, "go test") || strings.Contains(lowerCheck, "make "):
			if hasValidation {
				// Find matching validation result
				for _, vr := range ev.ValidationResults {
					if strings.Contains(lowerCheck, strings.ToLower(vr.Command)) || vr.Status != CheckUnknown {
						result = vr.Status
						rationale = fmt.Sprintf("Validation evidence: %s — exit: %s", vr.EvidenceSummary, vr.ExitResult)
						evidenceSource = fmt.Sprintf("validation artifact: %s", vr.RawArtifactPath)
						break
					}
				}
				if result == CheckUnknown {
					result = CheckUnknown
					rationale = "Validation artifacts present but could not be matched to this check — manual review required"
					evidenceSource = "validation artifacts"
				}
			} else {
				result = CheckUnknown
				rationale = "No validation output artifacts found — cannot confirm pass/fail without evidence"
				evidenceSource = "none"
			}

		case strings.Contains(lowerCheck, "changed file") || strings.Contains(lowerCheck, "file scope") || strings.Contains(lowerCheck, "only") && strings.Contains(lowerCheck, "edit") || strings.Contains(lowerCheck, "diff"):
			if hasChangedFiles {
				evidenceSource = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
				result = CheckUnknown
				rationale = "Changed files artifact present — auditor must confirm scope manually"
			} else if hasDiff {
				evidenceSource = fmt.Sprintf("git_diff_patch: %s", ev.GitDiff.RawArtifactPath)
				result = CheckUnknown
				rationale = "Diff artifact present — auditor must confirm scope manually"
			} else {
				result = CheckUnknown
				rationale = "No changed files or diff artifact found — scope cannot be confirmed"
				evidenceSource = "none"
			}

		case strings.Contains(lowerCheck, "executor") || strings.Contains(lowerCheck, "result") || strings.Contains(lowerCheck, "status"):
			if ev.ExecutorResult.Present {
				evidenceSource = fmt.Sprintf("executor_result: %s", ev.ExecutorResult.RawArtifactPath)
				result = CheckUnknown
				rationale = "Executor result artifact present — auditor must verify result matches expectation"
			} else {
				result = CheckUnknown
				rationale = "Executor result artifact not found — status cannot be confirmed"
				evidenceSource = "none"
			}
		}

		ev.ChecklistResults = append(ev.ChecklistResults, PerCheckResult{
			ID:               item.ID,
			Check:            item.Check,
			Result:           result,
			SeverityIfFailed: item.SeverityIfFailed,
			EvidenceSource:   evidenceSource,
			Rationale:        rationale,
		})
	}
}

// evaluateFileScopeResults checks whether changed files are within expected scope.
func (c *Collector) evaluateFileScopeResults(ev *Evidence) {
	checks := ev.Packet.FileScopeChecks
	fileTargets := ev.Packet.FileTargets

	if len(checks) == 0 && len(fileTargets) == 0 {
		ev.FileScopeResults = []PerCheckResult{{
			ID:               "FS-MISSING",
			Check:            "File scope enforcement",
			Result:           CheckUnknown,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   "canonical_packet.json",
			Rationale:        "No file_targets or file_scope_checks in canonical_packet.json — file scope cannot be enforced",
		}}
		return
	}

	if !ev.ChangedFiles.Present {
		// Produce unknown results for all checks
		for i, chk := range checks {
			ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
				ID:               fmt.Sprintf("FS%d", i+1),
				Check:            chk,
				Result:           CheckUnknown,
				SeverityIfFailed: SeverityError,
				EvidenceSource:   "none",
				Rationale:        "No changed-files artifact — file scope cannot be confirmed",
			})
		}
		if len(fileTargets) > 0 && len(checks) == 0 {
			ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
				ID:               "FS1",
				Check:            fmt.Sprintf("Changed files limited to expected targets: %s", strings.Join(fileTargets, ", ")),
				Result:           CheckUnknown,
				SeverityIfFailed: SeverityError,
				EvidenceSource:   "none",
				Rationale:        "No changed-files artifact — cannot confirm scope against expected targets",
			})
		}
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "File scope checks cannot be evaluated without changed-files artifact",
			Severity: SeverityError,
		})
		return
	}

	changedPaths := make([]string, len(ev.ChangedFiles.Files))
	for i, f := range ev.ChangedFiles.Files {
		changedPaths[i] = f.Path
	}

	// Check against explicit file_targets from packet
	if len(fileTargets) > 0 {
		targetSet := map[string]bool{}
		for _, t := range fileTargets {
			targetSet[t] = true
		}
		var outOfScope []string
		for _, p := range changedPaths {
			if !targetSet[p] {
				outOfScope = append(outOfScope, p)
			}
		}
		result := CheckPass
		rationale := fmt.Sprintf("All %d changed files are within expected targets", len(changedPaths))
		if len(outOfScope) > 0 {
			result = CheckFail
			rationale = fmt.Sprintf("Out-of-scope files detected: %s", strings.Join(outOfScope, ", "))
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-TARGETS",
			Check:            fmt.Sprintf("Changed files limited to: %s", strings.Join(fileTargets, ", ")),
			Result:           result,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath),
			Rationale:        rationale,
		})
	}

	// Evaluate explicit file_scope_checks (these require manual review)
	for i, chk := range checks {
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               fmt.Sprintf("FS%d", i+1),
			Check:            chk,
			Result:           CheckUnknown,
			SeverityIfFailed: SeverityWarning,
			EvidenceSource:   fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath),
			Rationale:        fmt.Sprintf("Changed files present (%d files) — auditor must verify manually", len(changedPaths)),
		})
	}
}

// evaluateNonGoalResults checks non-goal enforcement.
func (c *Collector) evaluateNonGoalResults(ev *Evidence) {
	checks := ev.Packet.NonGoalChecks
	if len(checks) == 0 {
		ev.NonGoalResults = []PerCheckResult{{
			ID:               "NG-MISSING",
			Check:            "Non-goal enforcement",
			Result:           CheckUnknown,
			SeverityIfFailed: SeverityWarning,
			EvidenceSource:   "canonical_packet.json",
			Rationale:        "No non_goal_checks in canonical_packet.json — non-goal enforcement requires manual review",
		}}
		return
	}

	hasDiff := ev.GitDiff.Present
	hasChangedFiles := ev.ChangedFiles.Present
	evidenceSource := "none"
	if hasChangedFiles {
		evidenceSource = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
	} else if hasDiff {
		evidenceSource = fmt.Sprintf("git_diff_patch: %s", ev.GitDiff.RawArtifactPath)
	}

	for i, chk := range checks {
		result := CheckUnknown
		rationale := "Requires manual auditor review — automated non-goal enforcement not implemented"
		if !hasChangedFiles && !hasDiff {
			rationale = "No diff or changed-files artifact — non-goal cannot be verified"
		}
		ev.NonGoalResults = append(ev.NonGoalResults, PerCheckResult{
			ID:               fmt.Sprintf("NG%d", i+1),
			Check:            chk,
			Result:           result,
			SeverityIfFailed: SeverityWarning,
			EvidenceSource:   evidenceSource,
			Rationale:        rationale,
		})
	}
}

func (c *Collector) listArtifactPaths(runID int64, kind string) []string {
	if c.store == nil {
		return nil
	}
	arts, err := c.store.ListArtifactsByRunKind(runID, kind)
	if err != nil {
		return nil
	}
	var paths []string
	for _, a := range arts {
		paths = append(paths, a.Path)
	}
	return paths
}
