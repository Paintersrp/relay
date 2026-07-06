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
	"runtime"
	"strings"
	"time"
	"unicode"

	"relay/internal/artifacts"

	"github.com/xeipuuv/gojsonschema"
)

const (
	defaultValidationCommand   = "make validate-full"
	defaultAgentRefsGenerate   = "make agentrefs-generate"
	defaultAgentRefsCheck      = "make agentrefs-check"
	validationReportJSON       = "handoffs/validation/latest.validation-report.json"
	validationReportMarkdown   = "handoffs/validation/latest.validation-summary.md"
	closeoutEvidenceSchemaPath = "relay-specs/schema/closeout_evidence.schema.json"

	defaultProjectID  = "relay"
	defaultRepoTarget = "Paintersrp/relay"
	defaultRunID      = "local-closeout"
)

// Options configures a closeout run. Metadata fields (ProjectID, RepoTarget,
// RunID, PlanID, PassID, BaseRef, HeadRef) default to safe local-closeout
// values when empty and may be supplied via CLI flags or env vars.
type Options struct {
	Message                    string
	Slug                       string
	DryRun                     bool
	ValidationCommand          string
	AgentRefsGenerate          string
	AgentRefsCheck             string
	ProjectID                  string
	RepoTarget                 string
	RunID                      string
	PlanID                     string
	PassID                     string
	BaseRef                    string
	HeadRef                    string
	PromoteRuntimeEvidence     bool
	Now                        func() time.Time
	Runner                     CommandRunner
	CloseoutEvidenceSchemaPath string
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

// Report is the schema-conformant closeout evidence document. It conforms to
// relay-specs/schema/closeout_evidence.schema.json and intentionally
// avoids the legacy custom report-shape top-level fields.
type Report struct {
	EvidenceKind       string                `json:"evidence_kind"`
	SchemaVersion      string                `json:"schema_version"`
	CreatedAt          string                `json:"created_at"`
	ProjectID          string                `json:"project_id"`
	PlanID             *string               `json:"plan_id"`
	PassID             *string               `json:"pass_id"`
	RunID              string                `json:"run_id"`
	RepoTarget         string                `json:"repo_target"`
	BranchContext      BranchContext         `json:"branch_context"`
	Status             string                `json:"status"`
	ValidationEvidence ValidationEvidenceSec `json:"validation_evidence"`
	AuditEvidence      AuditEvidenceSec      `json:"audit_evidence"`
	RepositoryEvidence RepositoryEvidenceSec `json:"repository_evidence"`
	ArtifactReferences []ArtifactReference   `json:"artifact_references"`
	Issues             []CloseoutIssue       `json:"issues"`

	// Hidden accumulators kept out of the schema-conformant JSON output.
	evidencePaths EvidencePaths `json:"-"`
	stagedFiles   []string      `json:"-"`
	schemaPath    string        `json:"-"`
}

type BranchContext struct {
	BranchName   string  `json:"branch_name"`
	BaseRef      *string `json:"base_ref"`
	HeadRef      *string `json:"head_ref"`
	WorktreePath *string `json:"worktree_path,omitempty"`
}

type ValidationEvidenceSec struct {
	ValidationReports []ArtifactReference `json:"validation_reports"`
	Summary           string              `json:"summary"`
}

type AuditEvidenceSec struct {
	AuditPackets      []ArtifactReference `json:"audit_packets"`
	AuditStatus       string              `json:"audit_status"`
	AuditDecisionPath *string             `json:"audit_decision_path,omitempty"`
}

type RepositoryEvidenceSec struct {
	GitStatus RepositoryEvidenceReference `json:"git_status"`
	Commit    RepositoryEvidenceReference `json:"commit"`
	Push      RepositoryEvidenceReference `json:"push"`
}

type RepositoryEvidenceReference struct {
	State        string  `json:"state"`
	ArtifactPath *string `json:"artifact_path"`
	Summary      string  `json:"summary,omitempty"`
}

type ArtifactReference struct {
	Kind           string `json:"kind"`
	Path           string `json:"path"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
}

type CloseoutIssue struct {
	Severity     string  `json:"severity"`
	Message      string  `json:"message"`
	ArtifactPath *string `json:"artifact_path,omitempty"`
}

// MechanicalBlockerError is returned when a mechanical (evidence-write or git)
// step fails. It is not persisted into the schema-conformant Report; instead
// the blocker stage/message is recorded as a blocker-severity issue and the
// Report status is set to "blocked".
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
		return blockedReport(opts, "commit_message", "commit message is required"), blockerError("commit_message", "commit message is required")
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
	agentRefsGenerate := strings.TrimSpace(opts.AgentRefsGenerate)
	if agentRefsGenerate == "" {
		agentRefsGenerate = defaultAgentRefsGenerate
	}
	agentRefsCheck := strings.TrimSpace(opts.AgentRefsCheck)
	if agentRefsCheck == "" {
		agentRefsCheck = defaultAgentRefsCheck
	}
	dryRun := opts.DryRun || os.Getenv("RELAY_CLOSEOUT_DRY_RUN") == "1"
	promoteRuntimeEvidence := opts.PromoteRuntimeEvidence || os.Getenv("RELAY_CLOSEOUT_PROMOTE_RUNTIME_EVIDENCE") == "1"

	metadata := resolveMetadata(opts)

	branch := runner.Run(ctx, "git", "branch", "--show-current")
	head := runner.Run(ctx, "git", "rev-parse", "HEAD")
	if !commandOK(branch) || !commandOK(head) {
		report := newReport(metadata, branchContext(metadata, branch.Stdout, head.Stdout), "blocked")
		report.addIssue("blocker", "git_metadata: failed to resolve branch or HEAD")
		return report, blockerError("git_metadata", "failed to resolve branch or HEAD")
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

	_, evidencePaths, err := reserveEvidencePaths(createdAt.Format("2006-01-02"), slug)
	if err != nil {
		report := newReport(metadata, branchContext(metadata, branch.Stdout, head.Stdout), "blocked")
		report.addIssue("blocker", "evidence_write: "+err.Error())
		return report, blockerError("evidence_write", err.Error())
	}

	branchCtx := branchContext(metadata, branch.Stdout, head.Stdout)
	report := newReport(metadata, branchCtx, "ready_for_closeout")
	report.CreatedAt = createdAt.Format(time.RFC3339)
	report.schemaPath = resolveCloseoutEvidenceSchemaPath(opts.CloseoutEvidenceSchemaPath)
	if err := report.setEvidencePaths(evidencePaths); err != nil {
		report.addIssue("blocker", "evidence_path_normalization: "+err.Error())
		return report, blockerError("evidence_path_normalization", err.Error())
	}
	report.RepositoryEvidence = RepositoryEvidenceSec{
		GitStatus: RepositoryEvidenceReference{State: "captured", ArtifactPath: nil, Summary: filterOutput(strings.TrimSpace(branch.Stdout))},
		Commit:    RepositoryEvidenceReference{State: "not_run"},
		Push:      RepositoryEvidenceReference{State: "not_run"},
	}

	// Closeout-owned deterministic generated-artifact step runs before final
	// validation and before staging. Failures are recorded as evidence and
	// do not block closeout unless git mechanics cannot proceed.
	report.recordGeneratedArtifactStep(ctx, runner, agentRefsGenerate)
	report.recordGeneratedArtifactStep(ctx, runner, agentRefsCheck)

	validation := runValidation(ctx, runner, validationCommand)
	report.ValidationEvidence = validation
	if validation.summaryFailed() {
		report.addIssue("error", "validation failure: "+validation.Summary)
	}

	if err := report.writeEvidence(); err != nil {
		return report.blockFromError("evidence_write", err)
	}

	stagePaths, err := sourceStagePaths(ctx, runner, promoteRuntimeEvidence)
	if err != nil {
		return report.blockAndWrite("git_stage", err.Error())
	}
	report.recordStagedFiles(stagePaths)

	if dryRun {
		report.addIssue("info", "dry_run_would_stage: "+stageSummary(stagePaths))
		report.RepositoryEvidence.Commit = RepositoryEvidenceReference{State: "not_run", Summary: "dry_run"}
		report.RepositoryEvidence.Push = RepositoryEvidenceReference{State: "not_run", Summary: "dry_run"}
		if err := report.writeEvidence(); err != nil {
			return report.blockFromError("evidence_write", err)
		}
		return report, nil
	}

	if len(stagePaths) > 0 {
		stageArgs := append([]string{"add", "--"}, stagePaths...)
		stage := runner.Run(ctx, "git", stageArgs...)
		if !commandOK(stage) {
			return report.blockAndWrite("git_stage", resultMessage(stage))
		}
	}

	staged := runner.Run(ctx, "git", "diff", "--cached", "--name-only")
	if !commandOK(staged) {
		return report.blockAndWrite("git_stage", resultMessage(staged))
	}
	report.recordStagedFiles(parseLines(staged.Stdout))

	commit := runner.Run(ctx, "git", "commit", "-m", message)
	if !commandOK(commit) {
		return report.blockAndWrite("git_commit", resultMessage(commit))
	}
	commitSHA := runner.Run(ctx, "git", "rev-parse", "HEAD")
	if !commandOK(commitSHA) {
		return report.blockAndWrite("git_commit", resultMessage(commitSHA))
	}
	report.RepositoryEvidence.Commit = RepositoryEvidenceReference{
		State:   "captured",
		Summary: strings.TrimSpace(commitSHA.Stdout),
	}

	push := runner.Run(ctx, "git", "push")
	if !commandOK(push) {
		return report.blockAndWrite("git_push", resultMessage(push))
	}
	report.RepositoryEvidence.Push = RepositoryEvidenceReference{State: "captured"}

	report.Status = "closed_out"
	if err := report.writeEvidence(); err != nil {
		return report.blockFromError("evidence_write", err)
	}
	return report, nil
}

type closeoutMetadata struct {
	ProjectID  string
	PlanID     *string
	PassID     *string
	RunID      string
	RepoTarget string
	BaseRef    *string
	HeadRef    *string
}

func resolveMetadata(opts Options) closeoutMetadata {
	projectID := strings.TrimSpace(opts.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_PROJECT_ID"))
	}
	if projectID == "" {
		projectID = defaultProjectID
	}
	repoTarget := strings.TrimSpace(opts.RepoTarget)
	if repoTarget == "" {
		repoTarget = strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_REPO_TARGET"))
	}
	if repoTarget == "" {
		repoTarget = defaultRepoTarget
	}
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		runID = strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_RUN_ID"))
	}
	if runID == "" {
		runID = defaultRunID
	}
	var planID *string
	if v := strings.TrimSpace(opts.PlanID); v != "" {
		planID = ptr(v)
	} else if v := strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_PLAN_ID")); v != "" {
		planID = ptr(v)
	}
	var passID *string
	if v := strings.TrimSpace(opts.PassID); v != "" {
		passID = ptr(v)
	} else if v := strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_PASS_ID")); v != "" {
		passID = ptr(v)
	}
	var baseRef *string
	if v := strings.TrimSpace(opts.BaseRef); v != "" {
		baseRef = ptr(v)
	}
	var headRef *string
	if v := strings.TrimSpace(opts.HeadRef); v != "" {
		headRef = ptr(v)
	}
	return closeoutMetadata{
		ProjectID:  projectID,
		PlanID:     planID,
		PassID:     passID,
		RunID:      runID,
		RepoTarget: repoTarget,
		BaseRef:    baseRef,
		HeadRef:    headRef,
	}
}

func branchContext(meta closeoutMetadata, branchOut, headOut string) BranchContext {
	branchName := strings.TrimSpace(branchOut)
	if branchName == "" {
		branchName = "unknown"
	}
	baseRef := meta.BaseRef
	headRef := meta.HeadRef
	if headRef == nil {
		headRef = ptr(branchName)
	}
	_ = headOut
	return BranchContext{
		BranchName: branchName,
		BaseRef:    baseRef,
		HeadRef:    headRef,
	}
}

func newReport(meta closeoutMetadata, branchCtx BranchContext, status string) Report {
	return Report{
		EvidenceKind:  "closeout_evidence",
		SchemaVersion: "1.0.0",
		ProjectID:     meta.ProjectID,
		PlanID:        meta.PlanID,
		PassID:        meta.PassID,
		RunID:         meta.RunID,
		RepoTarget:    meta.RepoTarget,
		BranchContext: branchCtx,
		Status:        status,
		ValidationEvidence: ValidationEvidenceSec{
			ValidationReports: []ArtifactReference{},
			Summary:           "",
		},
		AuditEvidence: AuditEvidenceSec{
			AuditPackets: []ArtifactReference{},
			AuditStatus:  "not_run",
		},
		RepositoryEvidence: RepositoryEvidenceSec{
			GitStatus: RepositoryEvidenceReference{State: "not_run"},
			Commit:    RepositoryEvidenceReference{State: "not_run"},
			Push:      RepositoryEvidenceReference{State: "not_run"},
		},
		ArtifactReferences: []ArtifactReference{},
		Issues:             []CloseoutIssue{},
	}
}

// setEvidencePaths records the persisted evidence paths and adds artifact
// references for the JSON and Markdown outputs. All paths are normalized to
// repo-relative forward-slash form before assignment.
func (r *Report) setEvidencePaths(paths EvidencePaths) error {
	jsonPath, err := normalizeEvidencePath(paths.JSON)
	if err != nil {
		return err
	}
	markdownPath, err := normalizeEvidencePath(paths.Markdown)
	if err != nil {
		return err
	}
	r.ArtifactReferences = appendUniqueArtifact(r.ArtifactReferences, ArtifactReference{
		Kind: "closeout_evidence",
		Path: jsonPath,
	})
	r.ArtifactReferences = appendUniqueArtifact(r.ArtifactReferences, ArtifactReference{
		Kind: "closeout_evidence_markdown",
		Path: markdownPath,
	})
	r.evidencePaths = paths
	return nil
}

func (r *Report) recordStagedFiles(files []string) {
	r.stagedFiles = files
}

// recordGeneratedArtifactStep runs a closeout-owned deterministic
// generated-artifact command and records the result as a closeout issue. The
// command output tail is redacted. Failure is recorded as evidence and does
// not block closeout.
func (r *Report) recordGeneratedArtifactStep(ctx context.Context, runner CommandRunner, command string) {
	result := runner.Run(ctx, "bash", "-lc", command)
	tail := outputTail(command, result.Stdout, result.Stderr, result.ExitCode)
	severity := "info"
	if result.ExitCode != 0 || result.Err != nil {
		severity = "error"
	}
	r.addIssue(severity, "generated_artifact: "+tail)
}

// ValidationEvidenceSec.summaryFailed reports whether validation evidence
// indicates a failed command.
func (v ValidationEvidenceSec) summaryFailed() bool {
	return strings.Contains(v.Summary, "status=failed") || strings.Contains(v.Summary, "failed")
}

func (r *Report) addIssue(severity, message string) {
	if message == "" {
		return
	}
	r.Issues = append(r.Issues, CloseoutIssue{Severity: severity, Message: message})
}

func (r *Report) block(stage, message string) (Report, error) {
	r.Status = "blocked"
	r.addIssue("blocker", stage+": "+message)
	return *r, blockerError(stage, message)
}

func (r *Report) blockAndWrite(stage, message string) (Report, error) {
	r.Status = "blocked"
	r.addIssue("blocker", stage+": "+message)
	_ = r.writeEvidence()
	return *r, blockerError(stage, message)
}

func (r *Report) blockFromError(defaultStage string, err error) (Report, error) {
	stage := defaultStage
	message := err.Error()
	var blocker *MechanicalBlockerError
	if errors.As(err, &blocker) {
		stage = blocker.Stage
		message = blocker.Message
	}
	return r.block(stage, message)
}

func blockedReport(opts Options, stage, message string) Report {
	meta := resolveMetadata(opts)
	report := newReport(meta, BranchContext{BranchName: "unknown"}, "blocked")
	report.addIssue("blocker", stage+": "+message)
	return report
}

// EvidencePaths are the persisted evidence output paths. They are not part of
// the schema-conformant Report JSON; they are exposed via Report.evidencePaths
// for callers (e.g. the CLI) and used to populate ArtifactReferences.
type EvidencePaths struct {
	JSON     string
	Markdown string
}

// EvidenceJSONPath and EvidenceMarkdownPath expose the persisted evidence
// output paths to callers (e.g. the CLI).
func (r Report) EvidenceJSONPath() string     { return r.evidencePaths.JSON }
func (r Report) EvidenceMarkdownPath() string { return r.evidencePaths.Markdown }
func (r Report) CommitStatus() string {
	switch r.RepositoryEvidence.Commit.State {
	case "captured":
		return "committed"
	case "not_run":
		if r.isDryRun() {
			return "skipped_dry_run"
		}
		return "not_run"
	default:
		return r.RepositoryEvidence.Commit.State
	}
}
func (r Report) PushStatus() string {
	switch r.RepositoryEvidence.Push.State {
	case "captured":
		return "pushed"
	case "not_run":
		if r.isDryRun() {
			return "skipped_dry_run"
		}
		return "not_run"
	default:
		return r.RepositoryEvidence.Push.State
	}
}
func (r Report) ValidationStatus() string {
	if strings.Contains(r.ValidationEvidence.Summary, "status=failed") {
		return "failed"
	}
	if r.ValidationEvidence.Summary != "" {
		return "passed"
	}
	return "not_run"
}
func (r Report) isDryRun() bool {
	return r.RepositoryEvidence.Commit.Summary == "dry_run" || r.RepositoryEvidence.Push.Summary == "dry_run"
}

// StagedFiles exposes staged file paths for callers/tests.
func (r Report) StagedFiles() []string {
	return r.stagedFiles
}

// internal accumulators kept off the JSON output.
func (r *Report) writeEvidence() error {
	jsonData, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	jsonData = append(jsonData, '\n')
	if err := validateCloseoutEvidence(r.schemaPath, jsonData); err != nil {
		return err
	}

	date := r.CreatedAt
	if len(date) >= 10 {
		date = date[:10]
	}
	slug := slugFromEvidencePath(r.evidencePaths.JSON)
	jsonPath, err := artifacts.WriteCloseout(date, slug, "closeout_evidence_json", jsonData)
	if err != nil {
		return err
	}
	mdPath, err := artifacts.WriteCloseout(date, slug, "closeout_evidence_markdown", []byte(renderMarkdown(*r)))
	if err != nil {
		return err
	}
	r.evidencePaths = EvidencePaths{JSON: jsonPath, Markdown: mdPath}
	if err := r.setEvidencePaths(r.evidencePaths); err != nil {
		return blockerError("evidence_path_normalization", err.Error())
	}
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

func runValidation(ctx context.Context, runner CommandRunner, command string) ValidationEvidenceSec {
	result := runner.Run(ctx, "bash", "-lc", command)
	status := "passed"
	if result.ExitCode != 0 || result.Err != nil {
		status = "failed"
	}
	tail := outputTail(command, result.Stdout, result.Stderr, result.ExitCode)
	return ValidationEvidenceSec{
		ValidationReports: []ArtifactReference{
			{Kind: "validation_report", Path: mustNormalizeEvidencePath(validationReportJSON)},
			{Kind: "validation_report", Path: mustNormalizeEvidencePath(validationReportMarkdown)},
		},
		Summary: fmt.Sprintf("command=%s exit_code=%d status=%s\n%s", command, result.ExitCode, status, tail),
	}
}

func renderMarkdown(report Report) string {
	lines := []string{
		"# Relay Closeout Evidence",
		"",
		"- evidence_kind: " + report.EvidenceKind,
		"- schema_version: " + report.SchemaVersion,
		"- status: " + report.Status,
		"- project_id: " + report.ProjectID,
		"- run_id: " + report.RunID,
		"- repo_target: " + report.RepoTarget,
		"- branch_name: " + report.BranchContext.BranchName,
		"",
		"## Repository Evidence",
		"",
		"- git_status: " + report.RepositoryEvidence.GitStatus.State,
		"- commit: " + report.RepositoryEvidence.Commit.State,
		"- push: " + report.RepositoryEvidence.Push.State,
		"",
		"## Validation Evidence",
		"",
		"- summary:",
		"```text",
		report.ValidationEvidence.Summary,
		"```",
		"",
		"## Artifact References",
		"",
	}
	if len(report.ArtifactReferences) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, ref := range report.ArtifactReferences {
			lines = append(lines, fmt.Sprintf("- %s: %s", ref.Kind, ref.Path))
		}
	}
	lines = append(lines, "", "## Issues", "")
	if len(report.Issues) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, issue := range report.Issues {
			lines = append(lines, fmt.Sprintf("- [%s] %s", issue.Severity, issue.Message))
		}
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

func sourceStagePaths(ctx context.Context, runner CommandRunner, promoteRuntimeEvidence bool) ([]string, error) {
	status := runner.Run(ctx, "git", "status", "--porcelain=v1", "--untracked-files=normal")
	if !commandOK(status) {
		return nil, fmt.Errorf("%s", resultMessage(status))
	}
	paths := make([]string, 0)
	seen := map[string]bool{}
	for _, p := range parseStatusPaths(status.Stdout) {
		if !promoteRuntimeEvidence && isRuntimeEvidencePath(p) {
			continue
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func parseStatusPaths(raw string) []string {
	paths := []string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || len(line) < 4 {
			continue
		}
		pathPart := strings.TrimSpace(line[3:])
		if strings.Contains(pathPart, " -> ") {
			parts := strings.Split(pathPart, " -> ")
			pathPart = parts[len(parts)-1]
		}
		pathPart = strings.Trim(pathPart, `"`)
		if pathPart != "" {
			paths = append(paths, filepath.ToSlash(pathPart))
		}
	}
	return paths
}

func stageSummary(paths []string) string {
	if len(paths) == 0 {
		return "(none)"
	}
	return strings.Join(paths, ", ")
}

func isRuntimeEvidencePath(p string) bool {
	normalized := filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(p), "./"))
	for _, prefix := range []string{
		"handoffs/validation/",
		"handoffs/closeout/",
		"handoffs/audits/",
		"handoffs/results/",
		"data/artifacts/",
	} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
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
	{regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`), "${1}[REDACTED_TOKEN]"},
	{regexp.MustCompile(`(?i)([?&](?:token|access_token|auth|signature|X-Amz-Signature)=)[^&\s]+`), "${1}[REDACTED_TOKEN]"},
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

// normalizeEvidencePath converts an OS-specific closeout artifact path to a
// repo-relative forward-slash path suitable for durable closeout evidence.
func normalizeEvidencePath(p string) (string, error) {
	return artifacts.NormalizeCloseoutPath(p)
}

func mustNormalizeEvidencePath(p string) string {
	normalized, err := normalizeEvidencePath(p)
	if err != nil {
		panic(err)
	}
	return normalized
}

func appendUniqueArtifact(refs []ArtifactReference, ref ArtifactReference) []ArtifactReference {
	for _, existing := range refs {
		if existing.Kind == ref.Kind && existing.Path == ref.Path {
			return refs
		}
	}
	return append(refs, ref)
}

func slugFromEvidencePath(p string) string {
	base := filepath.Base(p)
	if idx := strings.Index(base, ".closeout-evidence"); idx >= 0 {
		base = base[:idx]
	} else if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	if und := strings.Index(base, "_"); und >= 0 {
		return base[und+1:]
	}
	return base
}

func ptr(v string) *string { return &v }

func blockerError(stage, message string) error {
	return &MechanicalBlockerError{Stage: stage, Message: message}
}

func resolveCloseoutEvidenceSchemaPath(optionPath string) string {
	if p := strings.TrimSpace(optionPath); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("RELAY_CLOSEOUT_EVIDENCE_SCHEMA_PATH")); p != "" {
		return p
	}
	if _, err := os.Stat(closeoutEvidenceSchemaPath); err == nil {
		return closeoutEvidenceSchemaPath
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return closeoutEvidenceSchemaPath
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", closeoutEvidenceSchemaPath))
}

func validateCloseoutEvidence(schemaPath string, evidenceJSON []byte) error {
	if err := validateCloseoutEvidencePaths(evidenceJSON); err != nil {
		return blockerError("evidence_path_normalization", err.Error())
	}
	if strings.TrimSpace(schemaPath) == "" {
		schemaPath = resolveCloseoutEvidenceSchemaPath("")
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return blockerError("evidence_schema_validation", fmt.Sprintf("failed to read closeout evidence schema %q: %v", schemaPath, err))
	}
	schemaLoader := gojsonschema.NewStringLoader(sanitizeCloseoutSchemaRegexes(string(schemaBytes)))
	documentLoader := gojsonschema.NewBytesLoader(evidenceJSON)
	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return blockerError("evidence_schema_validation", fmt.Sprintf("closeout evidence schema validation error: %v", err))
	}
	if result.Valid() {
		return nil
	}
	var sb strings.Builder
	for _, schemaErr := range result.Errors() {
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(schemaErr.String())
	}
	return blockerError("evidence_schema_validation", sb.String())
}

func sanitizeCloseoutSchemaRegexes(schemaContent string) string {
	schemaContent = strings.ReplaceAll(schemaContent, `(?!/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\\.\\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\\\)`, "")
	return schemaContent
}

func validateCloseoutEvidencePaths(evidenceJSON []byte) error {
	var report Report
	if err := json.Unmarshal(evidenceJSON, &report); err != nil {
		return err
	}
	for _, ref := range report.ArtifactReferences {
		if _, err := normalizeEvidencePath(ref.Path); err != nil {
			return fmt.Errorf("artifact_references path %q: %w", ref.Path, err)
		}
	}
	for _, ref := range report.ValidationEvidence.ValidationReports {
		if _, err := normalizeEvidencePath(ref.Path); err != nil {
			return fmt.Errorf("validation_evidence path %q: %w", ref.Path, err)
		}
	}
	for _, ref := range report.AuditEvidence.AuditPackets {
		if _, err := normalizeEvidencePath(ref.Path); err != nil {
			return fmt.Errorf("audit_evidence path %q: %w", ref.Path, err)
		}
	}
	if report.AuditEvidence.AuditDecisionPath != nil {
		if _, err := normalizeEvidencePath(*report.AuditEvidence.AuditDecisionPath); err != nil {
			return fmt.Errorf("audit_decision_path %q: %w", *report.AuditEvidence.AuditDecisionPath, err)
		}
	}
	for _, ref := range []RepositoryEvidenceReference{
		report.RepositoryEvidence.GitStatus,
		report.RepositoryEvidence.Commit,
		report.RepositoryEvidence.Push,
	} {
		if ref.ArtifactPath != nil {
			if _, err := normalizeEvidencePath(*ref.ArtifactPath); err != nil {
				return fmt.Errorf("repository_evidence path %q: %w", *ref.ArtifactPath, err)
			}
		}
	}
	for _, issue := range report.Issues {
		if issue.ArtifactPath != nil {
			if _, err := normalizeEvidencePath(*issue.ArtifactPath); err != nil {
				return fmt.Errorf("issue artifact_path %q: %w", *issue.ArtifactPath, err)
			}
		}
	}
	return nil
}
