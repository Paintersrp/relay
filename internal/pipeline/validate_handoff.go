package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"
)

type CheckResult struct {
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type DetectedInfo struct {
	Title              string   `json:"title"`
	ScopePaths         []string `json:"scope_paths"`
	ValidationCommands []string `json:"validation_commands"`
	RecommendedModel   string   `json:"recommended_model"`
}

type ValidationReport struct {
	Status   string        `json:"status"`
	Checks   []CheckResult `json:"checks"`
	Warnings []string      `json:"warnings"`
	Detected DetectedInfo  `json:"detected"`
}

func ValidateHandoff(text string, recommendedModel string) *ValidationReport {
	report := &ValidationReport{
		Status:   "needs_review",
		Checks:   make([]CheckResult, 0),
		Warnings: []string{},
		Detected: DetectedInfo{
			ScopePaths:         []string{},
			ValidationCommands: []string{},
		},
	}

	lines := strings.Split(text, "\n")

	check := func(kind, status, summary string) {
		report.Checks = append(report.Checks, CheckResult{Kind: kind, Status: status, Summary: summary})
	}

	// title line starting with "# "
	hasTitle := false
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			hasTitle = true
			report.Detected.Title = strings.TrimPrefix(line, "# ")
			break
		}
	}
	if hasTitle {
		check("title_line", "pass", "Title line found")
	} else {
		check("title_line", "fail", "No title line starting with '# ' found")
	}

	// required sections
	requiredSections := map[string]string{
		"goal":           "## Goal",
		"scope":          "## Scope",
		"do_not_change":  "## Do not change",
		"task_checklist": "## Task checklist",
		"validation":     "## Validation",
		"output":         "## Output",
	}

	allSectionsPresent := true
	for key, heading := range requiredSections {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), heading) {
				found = true
				break
			}
		}
		if found {
			check(key+"_section", "pass", heading+" section found")
		} else {
			check(key+"_section", "fail", heading+" section missing")
			allSectionsPresent = false
		}
	}

	// markdown checkbox
	hasCheckbox := false
	checkboxRe := regexp.MustCompile(`- \[[ x]\] `)
	for _, line := range lines {
		if checkboxRe.MatchString(line) {
			hasCheckbox = true
			break
		}
	}
	if hasCheckbox {
		check("checkbox_items", "pass", "Markdown checkbox items found")
	} else {
		check("checkbox_items", "fail", "No markdown checkbox items found")
	}

	// DONE / BLOCKED in output section
	hasDoneBlocked := strings.Contains(text, "DONE") && strings.Contains(text, "BLOCKED")
	if hasDoneBlocked {
		check("output_terms", "pass", "DONE and BLOCKED present in text")
	} else {
		check("output_terms", "warn", "DONE or BLOCKED not clearly present")
	}

	// validation commands
	cmdPrefixRe := regexp.MustCompile(`^\s*(` + "`" + `{0,3})\s*(go |npm |cargo |make |pytest|python |zig |rustc )`)
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				report.Detected.ValidationCommands = append(report.Detected.ValidationCommands, trimmed)
			}
			continue
		}
		if cmdPrefixRe.MatchString(line) {
			report.Detected.ValidationCommands = append(report.Detected.ValidationCommands, trimmed)
		}
	}
	if len(report.Detected.ValidationCommands) > 0 {
		check("validation_commands", "pass", "Validation commands detected")
	} else {
		check("validation_commands", "warn", "No validation commands detected")
	}

	// scope paths
	pathRe := regexp.MustCompile("`[a-zA-Z0-9_/\\.*-]+\\.[a-zA-Z]+`")
	matches := pathRe.FindAllString(text, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		clean := strings.Trim(m, "`")
		if !seen[clean] {
			seen[clean] = true
			report.Detected.ScopePaths = append(report.Detected.ScopePaths, clean)
		}
	}
	if len(report.Detected.ScopePaths) > 0 {
		check("scope_paths", "pass", "Scope paths detected")
	} else {
		check("scope_paths", "warn", "No scope paths detected")
	}

	// recommended model
	if recommendedModel != "" {
		report.Detected.RecommendedModel = recommendedModel
		check("recommended_model", "pass", "Recommended model provided: "+recommendedModel)
	} else {
		modelRe := regexp.MustCompile(`(?i)Recommended Model:\s*([^\n]+)`)
		if m := modelRe.FindStringSubmatch(text); len(m) > 1 {
			report.Detected.RecommendedModel = strings.TrimSpace(m[1])
			check("recommended_model", "pass", "Recommended model detected in text: "+report.Detected.RecommendedModel)
		} else {
			check("recommended_model", "warn", "No recommended model detected")
		}
	}

	// determine status
	hasFail := false
	for _, c := range report.Checks {
		if c.Status == "fail" {
			hasFail = true
			break
		}
	}
	if hasFail {
		report.Status = "needs_fix"
	} else if allSectionsPresent && hasCheckbox && hasDoneBlocked && len(report.Detected.ValidationCommands) > 0 {
		report.Status = "ready"
	} else {
		report.Status = "needs_review"
	}

	return report
}

func (r *ValidationReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
