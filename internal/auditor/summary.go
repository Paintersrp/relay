package auditor

import (
	"fmt"
	"strings"
	"time"
)

// GenerateInputSummary produces an audit_input_summary.md from the collected evidence.
// The summary is structured as a concise Audit Agent Handoff.
// Generated pipeline artifacts are listed separately from implementation/source files.
// The full audit_packet.md is the authoritative document.
func GenerateInputSummary(ev *Evidence) string {
	var b strings.Builder

	b.WriteString("# Audit Agent Handoff\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString("> This is a preliminary audit input summary for the Audit Agent. ")
	b.WriteString("It does **not** constitute final human acceptance.\n")
	b.WriteString("> A separate explicit approval action is required to accept or close the run.\n\n")

	// ── Run summary and preliminary decision ──
	b.WriteString("## Run Summary\n\n")
	b.WriteString(fmt.Sprintf("- Run ID: %d\n", ev.RunID))
	b.WriteString(fmt.Sprintf("- Title: %s\n", ev.RunTitle))
	b.WriteString(fmt.Sprintf("- Status at collection: %s\n", ev.RunStatus))
	// decision not yet computed here, caller passes it separately
	b.WriteString("\n")

	// ── Packet scope ──
	b.WriteString("## Packet Scope\n\n")
	b.WriteString(fmt.Sprintf("- Packet ID: %s\n", ev.Packet.PacketID))
	if ev.Packet.Goal != "" {
		b.WriteString("\n### Goal\n\n")
		b.WriteString(ev.Packet.Goal)
		b.WriteString("\n")
	}
	if ev.Packet.Scope != "" {
		b.WriteString("\n### Scope\n\n")
		b.WriteString(ev.Packet.Scope)
		b.WriteString("\n")
	}
	if ev.Packet.NonGoals != "" {
		b.WriteString("\n### Non-goals\n\n")
		for _, l := range strings.Split(strings.TrimSpace(ev.Packet.NonGoals), "\n") {
			l = strings.TrimSpace(l)
			if l != "" {
				if !strings.HasPrefix(l, "-") {
					b.WriteString(fmt.Sprintf("- %s\n", l))
				} else {
					b.WriteString(l + "\n")
				}
			}
		}
	}
	if len(ev.Packet.FileTargets) > 0 {
		b.WriteString("\n### File Targets\n\n")
		for _, ft := range ev.Packet.FileTargets {
			b.WriteString(fmt.Sprintf("- `%s`\n", ft))
		}
	}
	if len(ev.Packet.MissingFields) > 0 {
		b.WriteString(fmt.Sprintf("\n⚠ Missing packet fields: %s\n", strings.Join(ev.Packet.MissingFields, ", ")))
	}
	b.WriteString("\n")

	// ── Evidence map ──
	b.WriteString("## Evidence Map\n\n")
	b.WriteString("| Evidence Section | Artifact Kind | Path |\n")
	b.WriteString("|---|---|---|\n")
	if ev.ExecutorResult.Present {
		b.WriteString(fmt.Sprintf("| Executor Result | `executor_result` | `%s` |\n", ev.ExecutorResult.RawArtifactPath))
	} else {
		b.WriteString("| Executor Result | `executor_result` | ⚠ not present |\n")
	}
	for _, vr := range ev.ValidationResults {
		kind := vr.RawArtifactKind
		if kind == "" {
			kind = "validation_stdout"
		}
		if vr.RawArtifactPath != "" {
			b.WriteString(fmt.Sprintf("| Validation (%s) | `%s` | `%s` |\n", vr.ID, kind, vr.RawArtifactPath))
		} else {
			b.WriteString(fmt.Sprintf("| Validation (%s) | `%s` | ⚠ not present |\n", vr.ID, kind))
		}
	}
	if ev.ChangedFiles.Present {
		b.WriteString(fmt.Sprintf("| Changed Files | `%s` | `%s` |\n", ev.ChangedFiles.SourceKind, ev.ChangedFiles.RawArtifactPath))
	} else {
		b.WriteString("| Changed Files | `git_diff_name_status` | ⚠ not present |\n")
	}
	if ev.GitDiff.Present {
		b.WriteString(fmt.Sprintf("| Git Diff Patch | `git_diff_patch` | `%s` |\n", ev.GitDiff.RawArtifactPath))
	} else {
		b.WriteString("| Git Diff Patch | `git_diff_patch` | ⚠ not present |\n")
	}
	if ev.AcceptanceEvidence.Present {
		b.WriteString(fmt.Sprintf("| Acceptance Evidence | `validation_failure_acceptance_json` | `%s` |\n", ev.AcceptanceEvidence.RawArtifactPath))
	}
	// Audit evidence manifest and audit packet are always present
	b.WriteString("| Audit Evidence Manifest | `audit_evidence_manifest_json` | See run artifacts |\n")
	b.WriteString("| Audit Packet | `audit_packet` | See run artifacts |\n")
	b.WriteString("\n")

	// ── Implementation changed files ──
	b.WriteString("## Implementation Changed Files\n\n")
	if ev.ChangedFiles.Present && len(ev.ChangedFiles.ImplementationFiles) > 0 {
		b.WriteString("```\n")
		for _, f := range ev.ChangedFiles.ImplementationFiles {
			b.WriteString(fmt.Sprintf("%s\t%s\n", f.Status, f.Path))
		}
		b.WriteString("```\n")
	} else if ev.ChangedFiles.Present {
		b.WriteString("_No implementation/source files changed in this run — only generated pipeline artifacts._\n")
	} else {
		b.WriteString("⚠ Changed-files artifact not available.\n")
	}
	b.WriteString("\n")

	// ── Generated pipeline artifacts ──
	b.WriteString("## Generated Pipeline Artifacts\n\n")
	b.WriteString("These are generated pipeline artifacts produced during the run. ")
	b.WriteString("They are evidence inputs, not implementation files for file-target enforcement.\n\n")
	if len(ev.ChangedFiles.GeneratedArtifactFiles) > 0 {
		b.WriteString("| Status | Path | Inferred Artifact Kind | Recognized |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, ga := range ev.ChangedFiles.GeneratedArtifactFiles {
			rec := "yes"
			if !ga.Recognized {
				rec = "⚠ no"
			}
			b.WriteString(fmt.Sprintf("| %s | `%s` | `%s` | %s |\n", ga.Status, ga.Path, ga.InferredArtifactKind, rec))
		}
	} else {
		b.WriteString("_No generated pipeline artifacts were changed in this run._\n")
	}
	b.WriteString("\n")

	// ── Automated findings summary ──
	b.WriteString("## Automated Findings Summary\n\n")

	hasUnknown := false

	// File scope results
	b.WriteString("### File Scope\n\n")
	if len(ev.FileScopeResults) == 0 {
		b.WriteString("_No file scope results._\n\n")
	} else {
		b.WriteString("| ID | Result | Rationale |\n")
		b.WriteString("|---|---|---|\n")
		for _, r := range ev.FileScopeResults {
			emoji := ""
			switch r.Result {
			case CheckPass:
				emoji = "✅ pass"
			case CheckFail:
				emoji = "❌ fail"
			case CheckUnknown:
				emoji = "❓ unknown"
				hasUnknown = true
			case CheckNotApplicable:
				emoji = "➖ n/a"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.ID, emoji, r.Rationale))
		}
		b.WriteString("\n")
	}

	// Validation results
	b.WriteString("### Validation\n\n")
	if len(ev.ValidationResults) == 0 {
		b.WriteString("_No validation results._\n\n")
	} else {
		b.WriteString("| ID | Command | Status | Exit |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, vr := range ev.ValidationResults {
			emoji := ""
			switch vr.Status {
			case CheckPass:
				emoji = "✅ pass"
			case CheckFail:
				emoji = "❌ fail"
			case CheckUnknown:
				emoji = "❓ unknown"
				hasUnknown = true
			}
			b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n", vr.ID, vr.Command, emoji, vr.ExitResult))
		}
		b.WriteString("\n")
	}

	// Checklist results
	b.WriteString("### Checklist\n\n")
	if len(ev.ChecklistResults) == 0 {
		b.WriteString("_No checklist results._\n\n")
	} else {
		b.WriteString("| ID | Result | Severity if Failed | Rationale |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, cr := range ev.ChecklistResults {
			emoji := ""
			switch cr.Result {
			case CheckPass:
				emoji = "✅ pass"
			case CheckFail:
				emoji = "❌ fail"
			case CheckUnknown:
				emoji = "❓ unknown"
				hasUnknown = true
			case CheckNotApplicable:
				emoji = "➖ n/a"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", cr.ID, emoji, string(cr.SeverityIfFailed), cr.Rationale))
		}
		b.WriteString("\n")
	}

	// Non-goal results
	b.WriteString("### Non-goal Checks\n\n")
	if len(ev.NonGoalResults) == 0 {
		b.WriteString("_No non-goal results._\n\n")
	} else {
		b.WriteString("| ID | Result | Rationale |\n")
		b.WriteString("|---|---|---|\n")
		for _, r := range ev.NonGoalResults {
			emoji := ""
			switch r.Result {
			case CheckPass:
				emoji = "✅ pass"
			case CheckFail:
				emoji = "❌ fail"
			case CheckUnknown:
				emoji = "❓ unknown"
				hasUnknown = true
			case CheckNotApplicable:
				emoji = "➖ n/a"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.ID, emoji, r.Rationale))
		}
		b.WriteString("\n")
	}

	// Evidence warnings
	if len(ev.Warnings) > 0 {
		b.WriteString("### Evidence Warnings\n\n")
		for _, w := range ev.Warnings {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", string(w.Severity), w.Message))
		}
		b.WriteString("\n")
	}

	// ── Manual review focus ──
	b.WriteString("## Manual Review Focus\n\n")
	if hasUnknown {
		b.WriteString("The following items have `unknown` results requiring auditor judgment:\n\n")
		for _, r := range ev.FileScopeResults {
			if r.Result == CheckUnknown {
				b.WriteString(fmt.Sprintf("- [file-scope] %s: %s\n", r.ID, r.Rationale))
			}
		}
		for _, cr := range ev.ChecklistResults {
			if cr.Result == CheckUnknown {
				b.WriteString(fmt.Sprintf("- [checklist] %s (%s): %s\n", cr.ID, cr.Check, cr.Rationale))
			}
		}
		for _, vr := range ev.ValidationResults {
			if vr.Status == CheckUnknown {
				b.WriteString(fmt.Sprintf("- [validation] %s (%s): %s\n", vr.ID, vr.Command, vr.EvidenceSummary))
			}
		}
		for _, r := range ev.NonGoalResults {
			if r.Result == CheckUnknown {
				b.WriteString(fmt.Sprintf("- [non-goal] %s: %s\n", r.ID, r.Rationale))
			}
		}
		b.WriteString("\n")
	} else {
		b.WriteString("_All checks have automated pass/fail results._\n\n")
	}

	// ── Decision guidance ──
	b.WriteString("## Decision Guidance\n\n")
	b.WriteString("This summary is provided to support the Audit Agent's preliminary recommendation. ")
	b.WriteString("The final decision requires explicit human approval.\n\n")
	b.WriteString("The preliminary decision logic evaluates:\n\n")
	b.WriteString("- **accepted**: All evidence present, no warnings, no failures.\n")
	b.WriteString("- **accepted_with_warnings**: Non-blocking warnings or informational notes exist; evidence is substantially present.\n")
	b.WriteString("- **revision_required**: Concrete failures (validation, checklist, file-scope) require executor correction.\n")
	b.WriteString("- **blocked**: Critical evidence missing or blocking failures found.\n")
	b.WriteString("- **manual_review_required**: Error-severity evidence gaps exist that need human resolution.\n\n")

	b.WriteString("> Generated pipeline artifacts listed above are evidence inputs, not implementation files for file-target enforcement. ")
	b.WriteString("They do not trigger file-scope failures unless they contain unsafe or unrecognized paths.\n\n")

	b.WriteString("---\n")
	b.WriteString("*Generated by Relay audit service. Not final human acceptance.*\n")

	return b.String()
}
