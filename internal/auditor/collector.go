package auditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
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

	c.generateRevisionRequirements(ev)

	return ev, nil
}

func (c *Collector) generateRevisionRequirements(ev *Evidence) {
	// 1. Map blocker/error warnings to revision requirements
	for _, w := range ev.Warnings {
		if w.Severity == SeverityBlocker || w.Severity == SeverityError {
			ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
				Reason:   fmt.Sprintf("Resolve warning: %s", w.Message),
				Severity: w.Severity,
			})
		}
	}

	// 2. Map validation failures or missing evidence of required commands to revision requirements
	for _, vr := range ev.ValidationResults {
		if vr.Required {
			if vr.Status == CheckFail {
				ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
					Reason:   fmt.Sprintf("Validation command %s (%s) failed with exit: %s", vr.ID, vr.Command, vr.ExitResult),
					Severity: SeverityError,
				})
			} else if vr.Status == CheckUnknown {
				ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
					Reason:   fmt.Sprintf("Validation command %s (%s) has unknown result — evidence missing or incomplete: %s", vr.ID, vr.Command, vr.EvidenceSummary),
					Severity: SeverityError,
				})
			}
		}
	}

	// 3. Map checklist failures to revision requirements
	for _, cr := range ev.ChecklistResults {
		if cr.Result == CheckFail {
			ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
				Reason:   fmt.Sprintf("Checklist item %s failed: %s", cr.ID, cr.Check),
				Severity: cr.SeverityIfFailed,
			})
		}
	}

	// 4. Map file scope failures to revision requirements
	for _, fsr := range ev.FileScopeResults {
		if fsr.Result == CheckFail {
			ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
				Reason:   fmt.Sprintf("File scope check %s failed: %s (Rationale: %s)", fsr.ID, fsr.Check, fsr.Rationale),
				Severity: fsr.SeverityIfFailed,
			})
		}
	}

	// 5. Map checklist-parser structural failures (malformed entries) to revision requirements
	var checklistParseWarnings []EvidenceWarning
	for _, w := range ev.Warnings {
		if strings.Contains(w.Message, "Malformed checklist entry") || strings.Contains(w.Message, "parse error") {
			checklistParseWarnings = append(checklistParseWarnings, w)
		}
	}
	if len(checklistParseWarnings) > 0 {
		// Deduplicate: only add one revision requirement for checklist-parser structural issues
		alreadyMapped := false
		for _, rr := range ev.RevisionRequirements {
			if strings.Contains(rr.Reason, "checklist parser") {
				alreadyMapped = true
				break
			}
		}
		if !alreadyMapped {
			messages := make([]string, len(checklistParseWarnings))
			for i, w := range checklistParseWarnings {
				messages[i] = w.Message
			}
			ev.RevisionRequirements = append(ev.RevisionRequirements, RevisionRequirement{
				Reason:   fmt.Sprintf("Checklist parser structural issues: %s", strings.Join(messages, "; ")),
				Severity: SeverityError,
			})
		}
	}
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
	Goal               json.RawMessage        `json:"goal"`
	Scope              json.RawMessage        `json:"scope"`
	NonGoals           json.RawMessage        `json:"non_goals"`
	FileTargets        json.RawMessage        `json:"file_targets"`
	ValidationCommands []validationCommandRaw `json:"validation_commands"`
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

func parseFileTargets(raw json.RawMessage) ([]string, []EvidenceWarning) {
	if len(raw) == 0 {
		return nil, nil
	}
	// 1. Try a string array
	var strArr []string
	if err := json.Unmarshal(raw, &strArr); err == nil {
		return strArr, nil
	}
	// 2. Try a single string
	var singleStr string
	if err := json.Unmarshal(raw, &singleStr); err == nil {
		if strings.TrimSpace(singleStr) != "" {
			return []string{strings.TrimSpace(singleStr)}, nil
		}
		return nil, nil
	}
	// 3. Try an array of interface{} which might be strings or objects (schema-valid canonical form)
	var genericArr []interface{}
	if err := json.Unmarshal(raw, &genericArr); err == nil {
		var targets []string
		var warnings []EvidenceWarning
		for i, item := range genericArr {
			switch v := item.(type) {
			case string:
				targets = append(targets, v)
			case map[string]interface{}:
				pathVal, hasPath := v["path"]
				if hasPath {
					if pathStr, ok := pathVal.(string); ok && strings.TrimSpace(pathStr) != "" {
						targets = append(targets, strings.TrimSpace(pathStr))
					} else {
						warnings = append(warnings, EvidenceWarning{
							Message:  fmt.Sprintf("file_targets[%d] has path field but it is not a non-empty string", i),
							Severity: SeverityWarning,
						})
					}
				} else {
					warnings = append(warnings, EvidenceWarning{
						Message:  fmt.Sprintf("file_targets[%d] is an object without a path field — skipping", i),
						Severity: SeverityWarning,
					})
				}
			default:
				warnings = append(warnings, EvidenceWarning{
					Message:  fmt.Sprintf("file_targets[%d] has unsupported type — skipping", i),
					Severity: SeverityWarning,
				})
			}
		}
		return targets, warnings
	}
	// 4. Try a single object with a "path" field or multiple fields
	var singleObj map[string]interface{}
	if err := json.Unmarshal(raw, &singleObj); err == nil {
		if pathVal, ok := singleObj["path"]; ok {
			if pathStr, ok := pathVal.(string); ok && strings.TrimSpace(pathStr) != "" {
				return []string{strings.TrimSpace(pathStr)}, nil
			}
		}
	}
	return nil, []EvidenceWarning{{
		Message:  "unsupported file_targets format — cannot parse file targets",
		Severity: SeverityError,
	}}
}

// parseChecklistItems handles old flat-string and new typed-object audit checklist formats.
// Returns parsed items and any warnings for malformed entries.
func parseChecklistItems(raw json.RawMessage) ([]ChecklistItem, []EvidenceWarning) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Determine the format by inspecting the first non-null element.
	isObjectFormat := false
	var rawArr []json.RawMessage
	if err := json.Unmarshal(raw, &rawArr); err == nil && len(rawArr) > 0 {
		for _, el := range rawArr {
			if len(el) == 0 || string(el) == "null" {
				continue
			}
			trim := strings.TrimSpace(string(el))
			if strings.HasPrefix(trim, "{") {
				isObjectFormat = true
			}
			break
		}
	}

	if isObjectFormat {
		// Typed-object format: [{"id":..., "check":..., "severity_if_failed":...}]
		var typed []map[string]interface{}
		if err := json.Unmarshal(raw, &typed); err != nil {
			return nil, []EvidenceWarning{{
				Message:  fmt.Sprintf("audit_checklist typed-object parse error: %v", err),
				Severity: SeverityError,
			}}
		}
		items := make([]ChecklistItem, 0, len(typed))
		var warnings []EvidenceWarning
		idx := 0
		for _, t := range typed {
			check, _ := t["check"].(string)
			check = strings.TrimSpace(check)
			if check == "" {
				warnings = append(warnings, EvidenceWarning{
					Message:  "audit_checklist contains an object with empty or missing check field — skipping",
					Severity: SeverityWarning,
				})
				continue
			}
			idx++
			id, _ := t["id"].(string)
			id = strings.TrimSpace(id)
			if id == "" {
				id = fmt.Sprintf("A%d", idx)
			}
			sev := SeverityWarning
			if sevStr, ok := t["severity_if_failed"].(string); ok {
				sevStr = strings.ToLower(strings.TrimSpace(sevStr))
				switch sevStr {
				case "info":
					sev = SeverityInfo
				case "warning":
					sev = SeverityWarning
				case "error":
					sev = SeverityError
				case "blocker":
					sev = SeverityBlocker
				}
			}
			items = append(items, ChecklistItem{
				ID:               id,
				Check:            check,
				SeverityIfFailed: sev,
			})
		}
		return items, warnings
	}

	// Fall back to old flat-string format: must be an array of strings
	var flat []string
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, []EvidenceWarning{{
			Message:  fmt.Sprintf("audit_checklist flat-array parse error: %v — checklist items unavailable", err),
			Severity: SeverityError,
		}}
	}
	items := make([]ChecklistItem, 0)
	var warnings []EvidenceWarning
	var idx int
	var pendingSeverity CheckSeverity

	// JSON-residue field labels to skip in flat parsing when objects were serialized as strings
	isJSONResidue := func(lower string) bool {
		residuePrefixes := []string{
			"\"severity_if_failed\"",
			"\"id\"",
			"\"check\"",
			"\"severity\"",
			"\"evidence_source\"",
			"\"rationale\"",
			"\"result\"",
		}
		for _, p := range residuePrefixes {
			if strings.HasPrefix(lower, p) {
				return true
			}
		}
		return false
	}

	for _, line := range flat {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		// Skip JSON-residue field-label lines — they are object-serialization artifacts
		if isJSONResidue(lower) {
			warnings = append(warnings, EvidenceWarning{
				Message:  fmt.Sprintf("Malformed checklist entry (JSON residue): %s — skipping", line),
				Severity: SeverityWarning,
			})
			continue
		}

		// Skip section headers (lines ending with colon not starting with a check prefix)
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
	return items, warnings
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
	targets, ftWarnings := parseFileTargets(ep.FileTargets)
	ev.Warnings = append(ev.Warnings, ftWarnings...)
	if len(targets) == 0 {
		meta.MissingFields = append(meta.MissingFields, "execution_payload.file_targets")
		hasWarning := false
		for _, w := range ftWarnings {
			if w.Severity == SeverityError {
				hasWarning = true
				break
			}
		}
		if !hasWarning {
			ev.Warnings = append(ev.Warnings, EvidenceWarning{
				Message:  "execution_payload.file_targets missing or empty in canonical_packet.json",
				Severity: SeverityError,
			})
		}
	} else {
		meta.FileTargets = targets
	}

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
		items, checklistWarnings := parseChecklistItems(pkt.AuditSeed.AuditChecklist)
		meta.AuditChecklist = items
		ev.Warnings = append(ev.Warnings, checklistWarnings...)
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

type validationRunJSON struct {
	RunID    int64  `json:"runId"`
	Status   string `json:"status"`
	Commands []struct {
		ID           string `json:"id"`
		Command      string `json:"command"`
		Required     bool   `json:"required"`
		Status       string `json:"status"`
		ExitCode     int    `json:"exitCode"`
		StdoutKind   string `json:"stdoutKind"`
		StderrKind   string `json:"stderrKind"`
		NotRunReason string `json:"notRunReason,omitempty"`
	} `json:"commands"`
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

	// Try parsing validation_run.json as validation runner's ValidationRun first
	var valRun validationRunJSON
	hasValRun := false
	var progress pipeline.ValidationProgress
	hasProgress := false
	var progressPath string
	for _, a := range collected {
		if a.kind == "validation_run_json" {
			if err := json.Unmarshal(a.data, &valRun); err == nil && len(valRun.Commands) > 0 {
				hasValRun = true
				progressPath = a.path
				break
			}
			if err := json.Unmarshal(a.data, &progress); err == nil {
				hasProgress = true
				progressPath = a.path
				break
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
			ev.ValidationResults = append(ev.ValidationResults, ValidationCommandResult{
				ID:              spec.ID,
				Command:         spec.Command,
				Required:        spec.Required,
				Status:          CheckUnknown,
				ExitResult:      "not_run",
				EvidenceSummary: "No validation output artifact found",
			})
		}
		return
	}

	// Determine the best available validation artifact path for commands that lack a specific match
	var bestAvailablePath string
	for _, a := range collected {
		if a.path != "" && bestAvailablePath == "" {
			bestAvailablePath = a.path
		}
	}

	// For each spec, find the result
	if len(specByID) > 0 {
		for _, spec := range ev.Packet.ValidationCommands {
			var bestPath string
			status := CheckUnknown
			exitResult := "unknown"
			summary := "No validation artifact matched"

			matchedFromProgress := false
			if hasValRun {
				for _, vc := range valRun.Commands {
					if strings.EqualFold(vc.ID, spec.ID) {
						matchedFromProgress = true
						bestPath = progressPath
						exitResult = fmt.Sprintf("exit %d", vc.ExitCode)
						summary = fmt.Sprintf("Command exit code: %d, status: %s", vc.ExitCode, vc.Status)
						if vc.Status == "pass" {
							status = CheckPass
						} else if vc.Status == "fail" {
							status = CheckFail
						} else if vc.NotRunReason != "" {
							status = CheckUnknown
							summary = fmt.Sprintf("Not run: %s", vc.NotRunReason)
						} else {
							status = CheckUnknown
						}
						break
					}
				}
			}

			if !matchedFromProgress && hasProgress {
				for _, pc := range progress.Commands {
					if strings.Contains(strings.ToLower(pc.Command), strings.ToLower(spec.Command)) ||
						strings.Contains(strings.ToLower(spec.Command), strings.ToLower(pc.Command)) {
						matchedFromProgress = true
						bestPath = progressPath
						exitResult = fmt.Sprintf("exit %d", pc.ExitCode)
						summary = fmt.Sprintf("Command exit code: %d, progress status: %s", pc.ExitCode, pc.Status)
						if pc.Status == "pass" {
							status = CheckPass
						} else if pc.Status == "fail" {
							status = CheckFail
						} else {
							status = CheckUnknown
						}
						break
					}
				}
			}

			if !matchedFromProgress {
				// Fallback to heuristic in validation_stdout / validation_stderr
				var best valArtifact
				for _, a := range collected {
					if a.kind != "validation_run_json" && len(a.data) > len(best.data) {
						best = a
					}
				}
				if len(best.data) > 0 {
					bestPath = best.path
					summary = fmt.Sprintf("sourced from %s artifact (%d bytes)", best.kind, len(best.data))
					redacted := redactSecrets(string(best.data))
					lower := strings.ToLower(redacted)
					// Match Go test output (ok\tpkg) or generic pass
					okLineMatch := false
					for _, line := range strings.Split(lower, "\n") {
						trimmed := strings.TrimSpace(line)
						if strings.HasPrefix(trimmed, "ok") && (len(trimmed) == 2 || trimmed[2] == ' ' || trimmed[2] == '\t') {
							okLineMatch = true
							break
						}
					}
					if okLineMatch || strings.Contains(lower, "pass") || strings.Contains(lower, "exit 0") {
						status = CheckPass
						exitResult = "exit 0 (inferred)"
					} else if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "exit 1") {
						status = CheckFail
						exitResult = "non-zero exit (inferred)"
					}
				}
			}

			// If the specific command was not matched to an artifact but we have available artifacts,
			// still report the best available path so required commands show evidence
			if bestPath == "" && bestAvailablePath != "" {
				bestPath = bestAvailablePath
			}

			// Check for contradiction with executor result
			if ev.ExecutorResult.Present && ev.ExecutorResult.Summary != "" {
				execLower := strings.ToLower(ev.ExecutorResult.Summary)
				if strings.Contains(execLower, "done") && status == CheckFail {
					ev.Warnings = append(ev.Warnings, EvidenceWarning{
						Message:  fmt.Sprintf("Executor reports DONE but validation command %s (%s) shows FAIL: %s — evidence contradiction, manual review required", spec.ID, spec.Command, summary),
						Severity: SeverityError,
					})
				}
			}

			ev.ValidationResults = append(ev.ValidationResults, ValidationCommandResult{
				ID:              spec.ID,
				Command:         spec.Command,
				Required:        spec.Required,
				Status:          status,
				ExitResult:      exitResult,
				EvidenceSummary: summary,
				RawArtifactPath: bestPath,
			})
		}
	} else {
		// No packet commands — produce one generic validation result from the best available artifact
		var best valArtifact
		for _, a := range collected {
			if len(a.data) > len(best.data) {
				best = a
			}
		}
		status := CheckUnknown
		exitResult := "unknown"
		summary := fmt.Sprintf("sourced from %s artifact (%d bytes)", best.kind, len(best.data))
		if best.kind == "validation_run_json" && hasValRun {
			status = CheckPass
			for _, vc := range valRun.Commands {
				if vc.Status == "fail" {
					status = CheckFail
					break
				}
			}
			exitResult = fmt.Sprintf("validation status: %s", valRun.Status)
		} else if best.kind == "validation_run_json" && hasProgress {
			status = CheckPass
			for _, pc := range progress.Commands {
				if pc.Status == "fail" {
					status = CheckFail
					break
				}
			}
			exitResult = fmt.Sprintf("progress status: %s", progress.Status)
		} else {
			redacted := redactSecrets(string(best.data))
			lower := strings.ToLower(redacted)
			okLineMatch := false
			for _, line := range strings.Split(lower, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "ok") && (len(trimmed) == 2 || trimmed[2] == ' ' || trimmed[2] == '\t') {
					okLineMatch = true
					break
				}
			}
			if okLineMatch || strings.Contains(lower, "pass") {
				status = CheckPass
				exitResult = "exit 0 (inferred)"
			} else if strings.Contains(lower, "fail") || strings.Contains(lower, "error") {
				status = CheckFail
				exitResult = "non-zero exit (inferred)"
			}
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

		// ── Content / section checks (use diff evidence) ──
		case strings.Contains(lowerCheck, "section") && (strings.Contains(lowerCheck, "includes") || strings.Contains(lowerCheck, "present") || strings.Contains(lowerCheck, "required")):
			headingMatched := false
			diffContent := ""
			if ev.GitDiff.Present {
				diffContent = ev.GitDiff.Preview
				evidenceSource = fmt.Sprintf("git_diff_patch: %s", ev.GitDiff.RawArtifactPath)
			}
			if diffContent != "" {
				for _, line := range strings.Split(diffContent, "\n") {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "+#") {
						headingMatched = true
						break
					}
				}
				if headingMatched {
					result = CheckPass
					rationale = "Diff evidence shows added Markdown headings — section likely present"
				} else {
					result = CheckUnknown
					rationale = "Diff evidence present but no added headings detected — manual review required to confirm section content"
				}
			} else {
				result = CheckUnknown
				rationale = "No diff evidence — cannot confirm section presence"
				evidenceSource = "none"
			}

		// ── Documentation-only diff checks ──
		case strings.Contains(lowerCheck, "documentation-only") || strings.Contains(lowerCheck, "documentation only") || strings.Contains(lowerCheck, "doc-only") || strings.Contains(lowerCheck, "docs-only"):
			if hasChangedFiles {
				evidenceSource = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
				isDocOnly := true
				for _, f := range ev.ChangedFiles.Files {
					ext := strings.ToLower(filepath.Ext(f.Path))
					if ext != ".md" && ext != ".txt" && ext != ".json" {
						isDocOnly = false
						break
					}
				}
				if isDocOnly {
					result = CheckPass
					rationale = "All changed files are documentation-only (.md, .txt, .json)"
				} else {
					result = CheckFail
					rationale = "Changed files include non-documentation files"
				}
			} else {
				result = CheckUnknown
				rationale = "No changed-files evidence available — cannot confirm diff is documentation-only"
				evidenceSource = "none"
			}

		// ── File scope / changed-files checks (use changed_files evidence) ──
		case strings.Contains(lowerCheck, "changed file") || strings.Contains(lowerCheck, "file scope") ||
			strings.Contains(lowerCheck, "only expected file") || strings.Contains(lowerCheck, "only") && strings.Contains(lowerCheck, "edit") ||
			strings.Contains(lowerCheck, "runtime code") || strings.Contains(lowerCheck, "no runtime") ||
			strings.Contains(lowerCheck, "test files were deleted") || strings.Contains(lowerCheck, "no test file") ||
			strings.Contains(lowerCheck, "security-sensitive") || strings.Contains(lowerCheck, "auth") && strings.Contains(lowerCheck, "file") ||
			strings.Contains(lowerCheck, "scope") || strings.Contains(lowerCheck, "outside expected"):

			if hasChangedFiles {
				evidenceSource = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
				targetSet := map[string]bool{}
				for _, t := range ev.Packet.FileTargets {
					targetSet[t] = true
				}
				var outOfScope []string
				for _, f := range ev.ChangedFiles.Files {
					if !targetSet[f.Path] {
						outOfScope = append(outOfScope, f.Path)
					}
				}
				if len(outOfScope) == 0 {
					result = CheckPass
					rationale = fmt.Sprintf("All %d changed files are within expected targets — evidence from changed_files artifact", len(ev.ChangedFiles.Files))
				} else {
					result = CheckFail
					rationale = fmt.Sprintf("Out-of-scope files detected: %s — evidence from changed_files artifact", strings.Join(outOfScope, ", "))
				}
			} else if hasDiff {
				evidenceSource = fmt.Sprintf("git_diff_patch: %s", ev.GitDiff.RawArtifactPath)
				result = CheckUnknown
				rationale = "Diff artifact present but no explicit changed-files list — scope cannot be confirmed automatically"
			} else {
				result = CheckUnknown
				rationale = "No changed files or diff artifact found — scope cannot be confirmed"
				evidenceSource = "none"
			}

		// ── Validation / test command checks (use validation evidence) ──
		case strings.Contains(lowerCheck, "go vet") || strings.Contains(lowerCheck, "go test") ||
			strings.Contains(lowerCheck, "go build") || strings.Contains(lowerCheck, "make ") ||
			strings.Contains(lowerCheck, "templ generate") || strings.Contains(lowerCheck, "npm run") ||
			strings.Contains(lowerCheck, "validation") && strings.Contains(lowerCheck, "command") ||
			strings.Contains(lowerCheck, "validation") && strings.Contains(lowerCheck, "pass"):
			if hasValidation {
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

		// ── Executor result checks ──
		case strings.Contains(lowerCheck, "executor") || strings.Contains(lowerCheck, "result") && strings.Contains(lowerCheck, "done") ||
			strings.Contains(lowerCheck, "executor") && strings.Contains(lowerCheck, "status"):
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

	// FS-TARGETS check (original name/check string for test compatibility)
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot confirm scope against expected targets"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			targetSet := map[string]bool{}
			for _, t := range fileTargets {
				targetSet[t] = true
			}
			var outOfScope []string
			for _, f := range ev.ChangedFiles.Files {
				if !targetSet[f.Path] {
					outOfScope = append(outOfScope, f.Path)
				}
			}
			if len(outOfScope) == 0 {
				res = CheckPass
				rat = fmt.Sprintf("All %d changed files are within expected targets", len(ev.ChangedFiles.Files))
			} else {
				res = CheckFail
				rat = fmt.Sprintf("Out-of-scope files detected: %s", strings.Join(outOfScope, ", "))
			}
		} else if len(fileTargets) > 0 && len(checks) == 0 {
			res = CheckUnknown
			rat = "No changed-files artifact — cannot confirm scope against expected targets"
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-TARGETS",
			Check:            fmt.Sprintf("Changed files limited to: %s", strings.Join(fileTargets, ", ")),
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	// 1. Only expected files check
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify if only expected files changed"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			targetSet := map[string]bool{}
			for _, t := range fileTargets {
				targetSet[t] = true
			}
			var outOfScope []string
			for _, f := range ev.ChangedFiles.Files {
				if !targetSet[f.Path] {
					outOfScope = append(outOfScope, f.Path)
				}
			}
			if len(outOfScope) == 0 {
				res = CheckPass
				rat = fmt.Sprintf("All %d changed files are within expected targets", len(ev.ChangedFiles.Files))
			} else {
				res = CheckFail
				rat = fmt.Sprintf("Out-of-scope files detected: %s", strings.Join(outOfScope, ", "))
			}
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-EXPECTED-ONLY",
			Check:            "Only expected files changed",
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	// 2. Runtime files check
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify if runtime files changed"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			targetSet := map[string]bool{}
			for _, t := range fileTargets {
				targetSet[t] = true
			}
			var unexpectedCode []string
			for _, f := range ev.ChangedFiles.Files {
				if !targetSet[f.Path] {
					ext := filepath.Ext(f.Path)
					if ext == ".go" || ext == ".templ" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" {
						unexpectedCode = append(unexpectedCode, f.Path)
					}
				}
			}
			if len(unexpectedCode) == 0 {
				res = CheckPass
				rat = "No unexpected runtime code files changed outside targets"
			} else {
				res = CheckFail
				rat = fmt.Sprintf("Unexpected runtime code files changed outside targets: %s", strings.Join(unexpectedCode, ", "))
			}
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-RUNTIME-FILES",
			Check:            "No unexpected runtime code files changed outside expected targets",
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	// 3. Tests removed/weakened check
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify if tests were deleted"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			var deletedTests []string
			for _, f := range ev.ChangedFiles.Files {
				if (strings.Contains(f.Path, "test") || strings.Contains(f.Path, "_test.go")) && f.Status == "D" {
					deletedTests = append(deletedTests, f.Path)
				}
			}
			if len(deletedTests) == 0 {
				res = CheckPass
				rat = "No test files were deleted"
			} else {
				res = CheckFail
				rat = fmt.Sprintf("Deleted test files detected: %s", strings.Join(deletedTests, ", "))
			}
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-TESTS-PRESERVED",
			Check:            "No test files were deleted",
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	// 4. Security-sensitive / MCP / auth files check
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify MCP/auth/security file changes"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			targetSet := map[string]bool{}
			for _, t := range fileTargets {
				targetSet[t] = true
			}
			var sensitiveChanges []string
			sensitiveKeywords := []string{"mcp", "auth", "security", "credentials", "token", "password", "secret", "private_key"}
			for _, f := range ev.ChangedFiles.Files {
				if !targetSet[f.Path] {
					lowerPath := strings.ToLower(f.Path)
					matched := false
					for _, kw := range sensitiveKeywords {
						if strings.Contains(lowerPath, kw) {
							matched = true
							break
						}
					}
					if matched {
						sensitiveChanges = append(sensitiveChanges, f.Path)
					}
				}
			}
			if len(sensitiveChanges) == 0 {
				res = CheckPass
				rat = "No security-sensitive, auth, or MCP files changed outside expected targets"
			} else {
				res = CheckFail
				rat = fmt.Sprintf("Security-sensitive/auth/MCP files changed outside targets: %s", strings.Join(sensitiveChanges, ", "))
			}
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-SECURITY-MCP",
			Check:            "No security-sensitive, auth, or MCP files changed outside expected targets",
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	// 5. Documentation-only run check
	{
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify if doc-only task touched code"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			isDocOnly := true
			for _, t := range fileTargets {
				ext := filepath.Ext(t)
				if ext != ".md" && ext != ".txt" && ext != ".json" && !strings.Contains(t, "docs/") && !strings.Contains(t, "handoffs/") {
					isDocOnly = false
					break
				}
			}
			if isDocOnly && len(fileTargets) > 0 {
				var touchedCode []string
				for _, f := range ev.ChangedFiles.Files {
					ext := filepath.Ext(f.Path)
					if ext == ".go" || ext == ".templ" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" {
						touchedCode = append(touchedCode, f.Path)
					}
				}
				if len(touchedCode) == 0 {
					res = CheckPass
					rat = "Documentation-only task changed only documentation files"
				} else {
					res = CheckFail
					rat = fmt.Sprintf("Documentation-only task touched runtime code files: %s", strings.Join(touchedCode, ", "))
				}
			} else {
				res = CheckNotApplicable
				rat = "Task is not documentation-only"
			}
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               "FS-DOC-ONLY",
			Check:            "Documentation-only tasks touched only documentation files",
			Result:           res,
			SeverityIfFailed: SeverityError,
			EvidenceSource:   src,
			Rationale:        rat,
		})
	}

	if !ev.ChangedFiles.Present {
		ev.Warnings = append(ev.Warnings, EvidenceWarning{
			Message:  "File scope checks cannot be evaluated without changed-files artifact",
			Severity: SeverityError,
		})
	}

	// Also evaluate any explicit file_scope_checks (from audit_seed)
	for i, chk := range checks {
		res := CheckUnknown
		rat := "No changed-files artifact — cannot verify manually"
		src := "none"
		if ev.ChangedFiles.Present {
			src = fmt.Sprintf("changed_files artifact: %s", ev.ChangedFiles.RawArtifactPath)
			rat = fmt.Sprintf("Changed files present (%d files) — auditor must verify manually", len(ev.ChangedFiles.Files))
		}
		ev.FileScopeResults = append(ev.FileScopeResults, PerCheckResult{
			ID:               fmt.Sprintf("FS%d", i+1),
			Check:            chk,
			Result:           res,
			SeverityIfFailed: SeverityWarning,
			EvidenceSource:   src,
			Rationale:        rat,
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
