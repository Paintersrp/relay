package pipeline

import (
	"fmt"
	"strings"
)

const (
	maxAuditOriginalHandoffChars = 6000
	maxAuditResultRawChars       = 2000
	maxAuditValidationExcerpt    = 1200
	maxAuditPatchChars           = 80000
	maxAuditPerFilePatchChars    = 20000
)

type AuditHandoffInput struct {
	RunID      int64
	Title      string
	RepoName   string
	BranchName string
	Status     string

	SelectedModel    string
	RecommendedModel string

	OriginalHandoff   string
	AgentResultStatus string
	BuildStatus       string
	TestStatus        string
	LOCChanged        string
	ResultRaw         string

	ValidationStatus   string
	ValidationRepoPath string
	ValidationCommands []CommandRunResult

	GitStatusText     string
	GitDiffStat       string
	GitDiffNumstat    string
	GitDiffNameStatus string
	GitDiffPatch      string
}

func BuildAuditHandoff(input AuditHandoffInput) string {
	patchFiles := ParseUnifiedDiffPatch(input.GitDiffPatch)
	patchFitsInline := len(input.GitDiffPatch) > 0 && len(input.GitDiffPatch) <= maxAuditPatchChars
	patchTruncated := len(input.GitDiffPatch) > 0 && !patchFitsInline

	var b strings.Builder

	b.WriteString("# Relay Run Audit Handoff\n\n")

	b.WriteString("## Run\n\n")
	b.WriteString(fmt.Sprintf("- Run ID: %d\n", input.RunID))
	b.WriteString(fmt.Sprintf("- Title: %s\n", input.Title))
	b.WriteString(fmt.Sprintf("- Repo: %s\n", input.RepoName))
	b.WriteString(fmt.Sprintf("- Branch: %s\n", input.BranchName))
	b.WriteString(fmt.Sprintf("- Status: %s\n", input.Status))
	b.WriteString(fmt.Sprintf("- Selected model: %s\n", orEmpty(input.SelectedModel, "N/A")))
	b.WriteString(fmt.Sprintf("- Recommended model: %s\n", orEmpty(input.RecommendedModel, "N/A")))

	b.WriteString("\n## Original Handoff\n\n")
	if input.OriginalHandoff != "" {
		writeTextBlock(&b, input.OriginalHandoff, maxAuditOriginalHandoffChars, "[truncated; full artifact available in Relay]")
	} else {
		b.WriteString("Not available.\n")
	}

	b.WriteString("\n## Agent Result\n\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", orEmpty(input.AgentResultStatus, "unknown")))
	b.WriteString(fmt.Sprintf("- Build status: %s\n", orEmpty(input.BuildStatus, "N/A")))
	b.WriteString(fmt.Sprintf("- Test status: %s\n", orEmpty(input.TestStatus, "N/A")))
	b.WriteString(fmt.Sprintf("- LOC changed: %s\n", orEmpty(input.LOCChanged, "N/A")))
	if input.ResultRaw != "" {
		b.WriteString("\nRaw result excerpt:\n")
		writeTextBlock(&b, input.ResultRaw, maxAuditResultRawChars, "[truncated]")
	}

	b.WriteString("\n## Relay Validation\n\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", orEmpty(input.ValidationStatus, "unknown")))
	b.WriteString(fmt.Sprintf("- Repo path: %s\n", orEmpty(input.ValidationRepoPath, "N/A")))
	if len(input.ValidationCommands) == 0 {
		b.WriteString("\nNo commands executed.\n")
	} else {
		b.WriteString("\n### Command Results\n\n")
		for _, cmd := range input.ValidationCommands {
			b.WriteString(fmt.Sprintf("#### `%s`\n\n", cmd.Command))
			status := validationCommandStatus(cmd)
			b.WriteString(fmt.Sprintf("- Status: %s\n", status))
			b.WriteString(fmt.Sprintf("- Exit code: %d\n", cmd.ExitCode))
			b.WriteString(fmt.Sprintf("- Duration: %dms\n", cmd.DurationMS))
			b.WriteString(fmt.Sprintf("- Timed out: %t\n", cmd.TimedOut))
			b.WriteString(fmt.Sprintf("- Stdout present: %t\n", cmd.Stdout != ""))
			b.WriteString(fmt.Sprintf("- Stderr present: %t\n", cmd.Stderr != ""))
			if status != "pass" {
				excerpt := cmd.Stderr
				if excerpt == "" {
					excerpt = cmd.Stdout
				}
				if excerpt != "" {
					b.WriteString("- Failure excerpt:\n")
					writeTextBlock(&b, excerpt, maxAuditValidationExcerpt, "[truncated]")
				}
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Changed Files\n\n")
	writeArtifactSection(&b, "git status --short", input.GitStatusText)
	writeArtifactSection(&b, "git diff --name-status", input.GitDiffNameStatus)
	writeArtifactSection(&b, "git diff --stat", input.GitDiffStat)
	writeArtifactSection(&b, "git diff --numstat", input.GitDiffNumstat)

	b.WriteString("## Full Patch For Review\n\n")
	if input.GitDiffPatch == "" {
		b.WriteString("No git diff patch artifact was available. Run Inspect Git Diff in Step 7 before audit for stronger evidence.\n")
	} else if patchFitsInline {
		b.WriteString("```diff\n")
		b.WriteString(input.GitDiffPatch)
		if !strings.HasSuffix(input.GitDiffPatch, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	} else {
		b.WriteString("TRUNCATED: The full git diff patch exceeded the inline audit budget. Review the per-file sections below for structured excerpts, and attach or paste the full git_diff_patch artifact for a complete audit.\n")
	}

	b.WriteString("\n## Per-file Review Notes\n\n")
	if len(patchFiles) == 0 {
		b.WriteString("No parsed file-level diff evidence was available.\n")
	} else {
		for _, file := range patchFiles {
			path := auditPatchFilePath(file)
			added, deleted, context := countAuditPatchLineKinds(file)
			b.WriteString(fmt.Sprintf("### `%s`\n\n", path))
			b.WriteString(fmt.Sprintf("- Change type: %s\n", file.ChangeType))
			if file.OldPath != "" && file.OldPath != path {
				b.WriteString(fmt.Sprintf("- Old path: %s\n", file.OldPath))
			}
			if file.NewPath != "" && file.NewPath != path {
				b.WriteString(fmt.Sprintf("- New path: %s\n", file.NewPath))
			}
			b.WriteString(fmt.Sprintf("- Added lines: %d\n", added))
			b.WriteString(fmt.Sprintf("- Deleted lines: %d\n", deleted))
			b.WriteString(fmt.Sprintf("- Context lines: %d\n", context))
			b.WriteString(fmt.Sprintf("- Binary: %t\n", file.Binary))
			b.WriteString(fmt.Sprintf("- Created: %t\n", file.Created))
			b.WriteString(fmt.Sprintf("- Deleted: %t\n", file.Deleted))
			b.WriteString(fmt.Sprintf("- Renamed: %t\n", file.Renamed))
			if patchFitsInline {
				b.WriteString("- Patch included inline above: yes\n")
			} else {
				b.WriteString("- Patch included inline above: truncated\n")
				excerpt, truncated := renderAuditPatchFileExcerpt(file, maxAuditPerFilePatchChars)
				if excerpt != "" {
					b.WriteString("\n```diff\n")
					b.WriteString(excerpt)
					if !strings.HasSuffix(excerpt, "\n") {
						b.WriteString("\n")
					}
					b.WriteString("```\n")
				}
				if truncated {
					b.WriteString("TRUNCATED: This file's patch exceeded the audit handoff inline budget. Attach or paste the full git_diff_patch artifact for complete review.\n")
				}
			}
			b.WriteString("\n")
		}
	}

	if patchTruncated {
		b.WriteString("TRUNCATION NOTE: The global patch was shortened for the inline audit handoff. Attach or paste the full git_diff_patch artifact if you need a complete review packet.\n\n")
	}

	b.WriteString("## Audit Request\n\n")
	b.WriteString("Please assess correctness, risks, missing tests, and whether this run should be accepted.\n")

	return b.String()
}

func writeTextBlock(b *strings.Builder, text string, maxChars int, truncatedNote string) {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return
	}
	truncated := false
	if maxChars > 0 {
		if truncatedText, ok := truncateText(text, maxChars); ok {
			text = truncatedText
			truncated = true
		}
	}
	b.WriteString("```text\n")
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	if truncated && truncatedNote != "" {
		b.WriteString(truncatedNote)
		b.WriteString("\n")
	}
}

func truncateText(text string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return text, false
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	return string(runes[:maxChars]), true
}

func writeArtifactSection(b *strings.Builder, label, content string) {
	b.WriteString(fmt.Sprintf("### %s\n\n", label))
	content = strings.TrimRight(content, "\r\n")
	if content == "" {
		b.WriteString("Not available.\n\n")
		return
	}
	b.WriteString("```text\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")
}

func validationCommandStatus(cmd CommandRunResult) string {
	switch {
	case cmd.TimedOut:
		return "timed out"
	case cmd.ExitCode == 0:
		return "pass"
	default:
		return "fail"
	}
}

func orEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
