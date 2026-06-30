package closeout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"relay/internal/artifacts"
)

const (
	defaultValidationCommand = "make validate-full"
	validationReportJSON     = "handoffs/validation/latest.validation-report.json"
	validationReportMarkdown = "handoffs/validation/latest.validation-summary.md"
)

type Options struct {
	Message           string
	Slug              string
	DryRun            bool
	ValidationCommand string
	Now               func() time.Time
	Runner            CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) CommandResult
}

type CommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"-"`
	Stderr   string `json:"-"`
	Err      error  `json:"-"`
}

type Report struct {
	SchemaVersion     string             `json:"schema_version"`
	ReportKind        string             `json:"report_kind"`
	Status            string             `json:"status"`
	CreatedAt         string             `json:"created_at"`
	Message           string             `json:"message"`
	Slug              string             `json:"slug"`
	Branch            string             `json:"branch"`
	HeadSHA           string             `json:"head_sha"`
	DryRun            bool               `json:"dry_run"`
	Validation        ValidationEvidence `json:"validation"`
	EvidencePaths     EvidencePaths      `json:"evidence_paths"`
	StagedFiles       []string           `json:"staged_files"`
	CommitStatus      string             `json:"commit_status"`
	CommitSHA         string             `json:"commit_sha,omitempty"`
	PushStatus        string             `json:"push_status"`
	MechanicalBlocker *MechanicalBlocker `json:"mechanical_blocker,omitempty"`
}

type ValidationEvidence struct {
	Command     string   `json:"command"`
	ExitCode    int      `json:"exit_code"`
	Status      string   `json:"status"`
	OutputTail  string   `json:"output_tail"`
	ReportPaths []string `json:"report_paths"`
}

type EvidencePaths struct {
	JSON     string `json:"json"`
	Markdown string `json:"markdown"`
}

type MechanicalBlocker struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type MechanicalBlockerError struct {
	Stage   string
	Message string
}

func (e *MechanicalBlockerError) Error() string {
	return fmt.Sprintf("%s: %s", e.Stage, e.Message)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return CommandResult{
		Command:  strings.TrimSpace(name + " " + strings.Join(args, " ")),
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Err:      err,
	}
}

func Run(ctx context.Context, opts Options) (Report, error) {
	message := strings.TrimSpace(opts.Message)
	if message == "" {
		return blockedReport("commit_message", "commit message is required"), blockerError("commit_message", "commit message is required")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	runner := opts.Runner
	if runner == nil {
		runner = execRunner{}
	}
	validationCommand := strings.TrimSpace(opts.ValidationCommand)
	if validationCommand == "" {
		validationCommand = defaultValidationCommand
	}
	dryRun := opts.DryRun || os.Getenv("RELAY_CLOSEOUT_DRY_RUN") == "1"

	branch := runner.Run(ctx, "git", "branch", "--show-current")
	head := runner.Run(ctx, "git", "rev-parse", "HEAD")
	if !commandOK(branch) || !commandOK(head) {
		return blockedReport("git_metadata", "failed to resolve branch or HEAD"), blockerError("git_metadata", "failed to resolve branch or HEAD")
	}

	createdAt := now().UTC()
	slug := safeSlug(opts.Slug)
	if slug == "" {
		slug = safeSlug(message)
	}
	if slug == "" {
		slug = "relay-closeout"
	}

	previousBaseDir := artifacts.BaseDir
	artifacts.SetBaseDir(".")
	defer artifacts.SetBaseDir(previousBaseDir)

	finalSlug, evidencePaths, err := reserveEvidencePaths(createdAt.Format("2006-01-02"), slug)
	if err != nil {
		report := baseReport(message, slug, createdAt, dryRun, branch.Stdout, head.Stdout)
		return block(report, "evidence_write", err.Error())
	}

	report := baseReport(message, finalSlug, createdAt, dryRun, branch.Stdout, head.Stdout)
	report.EvidencePaths = evidencePaths
	report.CommitStatus = "pending"
	report.PushStatus = "pending"

	validation := runValidation(ctx, runner, validationCommand)
	report.Validation = validation
	report.Status = validation.Status

	if err := writeEvidence(&report); err != nil {
		return block(report, "evidence_write", err.Error())
	}

	stage := runner.Run(ctx, "git", "add", "-A")
	if !commandOK(stage) {
		return blockAndWrite(report, "git_stage", resultMessage(stage))
	}

	staged := runner.Run(ctx, "git", "diff", "--cached", "--name-only")
	if !commandOK(staged) {
		return blockAndWrite(report, "git_stage", resultMessage(staged))
	}
	report.StagedFiles = parseLines(staged.Stdout)

	if dryRun {
		report.CommitStatus = "skipped_dry_run"
		report.PushStatus = "skipped_dry_run"
		if err := writeEvidence(&report); err != nil {
			return block(report, "evidence_write", err.Error())
		}
		return report, nil
	}

	commit := runner.Run(ctx, "git", "commit", "-m", message)
	if !commandOK(commit) {
		return blockAndWrite(report, "git_commit", resultMessage(commit))
	}
	report.CommitStatus = "committed"

	commitSHA := runner.Run(ctx, "git", "rev-parse", "HEAD")
	if !commandOK(commitSHA) {
		return blockAndWrite(report, "git_commit", resultMessage(commitSHA))
	}
	report.CommitSHA = strings.TrimSpace(commitSHA.Stdout)

	push := runner.Run(ctx, "git", "push")
	if !commandOK(push) {
		return blockAndWrite(report, "git_push", resultMessage(push))
	}
	report.PushStatus = "pushed"

	if err := writeEvidence(&report); err != nil {
		return block(report, "evidence_write", err.Error())
	}
	return report, nil
}

func blockedReport(stage, message string) Report {
	return Report{
		SchemaVersion: "1.0.0",
		ReportKind:    "relay_closeout_evidence",
		Status:        "blocked",
		MechanicalBlocker: &MechanicalBlocker{
			Stage:   stage,
			Message: message,
		},
	}
}

func baseReport(message, slug string, createdAt time.Time, dryRun bool, branchOut, headOut string) Report {
	return Report{
		SchemaVersion: "1.0.0",
		ReportKind:    "relay_closeout_evidence",
		Status:        "pending",
		CreatedAt:     createdAt.Format(time.RFC3339),
		Message:       message,
		Slug:          slug,
		Branch:        strings.TrimSpace(branchOut),
		HeadSHA:       strings.TrimSpace(headOut),
		DryRun:        dryRun,
	}
}

func block(report Report, stage, message string) (Report, error) {
	report.Status = "blocked"
	report.MechanicalBlocker = &MechanicalBlocker{Stage: stage, Message: message}
	return report, blockerError(stage, message)
}

func blockAndWrite(report Report, stage, message string) (Report, error) {
	report.Status = "blocked"
	report.MechanicalBlocker = &MechanicalBlocker{Stage: stage, Message: message}
	_ = writeEvidence(&report)
	return report, blockerError(stage, message)
}

func blockerError(stage, message string) error {
	return &MechanicalBlockerError{Stage: stage, Message: message}
}

func runValidation(ctx context.Context, runner CommandRunner, command string) ValidationEvidence {
	result := runner.Run(ctx, "bash", "-lc", command)
	status := "passed"
	if result.ExitCode != 0 || result.Err != nil {
		status = "failed"
	}
	return ValidationEvidence{
		Command:     command,
		ExitCode:    result.ExitCode,
		Status:      status,
		OutputTail:  outputTail(command, result.Stdout, result.Stderr, result.ExitCode),
		ReportPaths: []string{validationReportJSON, validationReportMarkdown},
	}
}

func writeEvidence(report *Report) error {
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')

	jsonPath, err := artifacts.WriteCloseout(report.CreatedAt[:10], report.Slug, "closeout_evidence_json", jsonData)
	if err != nil {
		return err
	}
	mdPath, err := artifacts.WriteCloseout(report.CreatedAt[:10], report.Slug, "closeout_evidence_markdown", []byte(renderMarkdown(*report)))
	if err != nil {
		return err
	}
	report.EvidencePaths = EvidencePaths{JSON: jsonPath, Markdown: mdPath}
	return nil
}

func reserveEvidencePaths(date, slug string) (string, EvidencePaths, error) {
	for i := 1; i < 1000; i++ {
		candidate := slug
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", slug, i)
		}
		jsonPath, err := artifacts.CloseoutPath(date, candidate, "closeout_evidence_json")
		if err != nil {
			return "", EvidencePaths{}, err
		}
		mdPath, err := artifacts.CloseoutPath(date, candidate, "closeout_evidence_markdown")
		if err != nil {
			return "", EvidencePaths{}, err
		}
		if !pathExists(jsonPath) && !pathExists(mdPath) {
			return candidate, EvidencePaths{JSON: jsonPath, Markdown: mdPath}, nil
		}
	}
	return "", EvidencePaths{}, fmt.Errorf("could not reserve closeout evidence path for slug %q", slug)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func renderMarkdown(report Report) string {
	lines := []string{
		"# Relay Closeout Evidence",
		"",
		"- status: " + report.Status,
		"- validation_status: " + report.Validation.Status,
		"- validation_exit_code: " + fmt.Sprint(report.Validation.ExitCode),
		"- commit_status: " + report.CommitStatus,
		"- push_status: " + report.PushStatus,
		"- branch: " + report.Branch,
		"- head_sha: " + report.HeadSHA,
		"- dry_run: " + fmt.Sprint(report.DryRun),
		"",
		"## Evidence",
		"",
		"- json: " + filepath.ToSlash(report.EvidencePaths.JSON),
		"- markdown: " + filepath.ToSlash(report.EvidencePaths.Markdown),
		"- validation_json: " + validationReportJSON,
		"- validation_markdown: " + validationReportMarkdown,
		"",
	}
	if report.MechanicalBlocker != nil {
		lines = append(lines,
			"## Mechanical Blocker",
			"",
			"- stage: "+report.MechanicalBlocker.Stage,
			"- message: "+report.MechanicalBlocker.Message,
			"",
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func commandOK(result CommandResult) bool {
	return result.Err == nil && result.ExitCode == 0
}

func resultMessage(result CommandResult) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	output := strings.TrimSpace(result.Stderr)
	if output == "" {
		output = strings.TrimSpace(result.Stdout)
	}
	if output == "" {
		output = fmt.Sprintf("command exited with code %d", result.ExitCode)
	}
	return filterOutput(output)
}

func parseLines(raw string) []string {
	lines := []string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func safeSlug(raw string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 80 {
		slug = strings.TrimRight(slug[:80], "-")
	}
	return slug
}

func outputTail(command, stdout, stderr string, exitCode int) string {
	combined := fmt.Sprintf("$ %s\n%s%sexit_code: %d\n", command, stdout, stderr, exitCode)
	lines := strings.Split(combined, "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return filterOutput(strings.Join(lines, "\n"))
}

var redactors = []struct {
	pattern *regexp.Regexp
	repl    string
}{
	{regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`), `${1}[REDACTED_TOKEN]`},
	{regexp.MustCompile(`(?i)([?&](?:token|access_token|auth|signature|X-Amz-Signature)=)[^&\s]+`), `${1}[REDACTED_TOKEN]`},
	{regexp.MustCompile(`\b[A-Za-z0-9+_-]{48,}={0,2}\b`), `[REDACTED_SECRET]`},
	{regexp.MustCompile(`(?i)([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASS|API_KEY|ACCESS_KEY|PRIVATE_KEY|AUTH|COOKIE|SESSION|CSRF|JWT)[A-Z0-9_]*=)[^\s]+`), `${1}[REDACTED_SECRET]`},
}

func filterOutput(value string) string {
	filtered := value
	for _, redactor := range redactors {
		filtered = redactor.pattern.ReplaceAllString(filtered, redactor.repl)
	}
	return filtered
}
