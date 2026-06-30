package artifacts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var BaseDir = "data/artifacts"

var allowedKinds = map[string]bool{
	"original_handoff":                   true,
	"handoff_validation_json":            true,
	"ready_prompt":                       true,
	"agent_prompt":                       true,
	"audit_packet":                       true,
	"audit_input_summary":                true,
	"audit_evidence_manifest_json":       true,
	"audit_decision_json":                true,
	"opencode_handoff_packet":            true,
	"agent_result_raw":                   true,
	"agent_result_json":                  true,
	"opencode_lifecycle_diagnostic_json": true,
	"validation_run_json":                true,
	"validation_failure_acceptance_json": true,
	"validation_stdout":                  true,
	"validation_stderr":                  true,
	"validation_progress_json":           true,
	"opencode_stdout":                    true,
	"opencode_stderr":                    true,
	"opencode_combined_log":              true,
	"opencode_stream_progress_json":      true,
	"opencode_dry_run_json":              true,
	"opencode_cli_check_json":            true,
	"intake_remediation_handoff":         true,
	"audit_handoff":                      true,
	"git_status_text":                    true,
	"git_diff_stat":                      true,
	"git_diff_numstat":                   true,
	"git_diff_patch":                     true,
	"git_diff_name_status":               true,
	"commit_message_text":                true,
	"commit_suggestion_json":             true,
	"git_baseline_json":                  true,
	"git_change_evidence_json":           true,
	"audit_clearance_json":               true,
	"git_commit_state_json":              true,
	"git_commit_result_json":             true,
	"git_push_dry_run_json":              true,
	"git_push_result_json":               true,
	"planner_handoff":                    true,
	"planner_handoff_provenance_json":    true,
	"parsed_frontmatter":                 true,
	"run_config":                         true,
	"intake_validation_report":           true,
	"canonical_packet":                   true,
	"packet_validation_report":           true,
	"executor_brief":                     true,
	"brief_validation_report":            true,
	"executor_stdout":                    true,
	"executor_stderr":                    true,
	"command_log":                        true,
	"executor_result":                    true,
	"codex_last_message":                 true,
	"kiro_parse_fixture_json":            true,
	"audit_revision":                     true,
	"mcp_audit_handback":                 true,
	"repair_request_json":                true,
	"repair_prompt":                      true,
	"repair_output":                      true,
	"repaired_packet":                    true,
	"repair_validation_report":           true,
	"context_packet_json":                true,
	"context_packet_markdown":            true,
	"context_coverage_report_json":       true,
	"local_audit_manifest_json":          true,
	"local_audit_packet":                 true,
	"local_audit_input_summary":          true,
	"planner_pass_plan_json":             true,
	"planner_pass_plan_markdown":         true,
	"closeout_evidence_json":             true,
	"closeout_evidence_markdown":         true,
}

var (
	contextDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	contextSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,119}$`)
)

func SetBaseDir(dir string) {
	BaseDir = dir
}

func Dir(runID int64) string {
	return filepath.Join(BaseDir, fmt.Sprintf("%d", runID))
}

func EnsureDir(runID int64) error {
	return os.MkdirAll(Dir(runID), 0755)
}

func Path(runID int64, kind, filename string) (string, error) {
	if !allowedKinds[kind] {
		return "", fmt.Errorf("unknown artifact kind: %s", kind)
	}
	clean := filepath.Clean(filename)
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid artifact filename: %s", filename)
	}
	p := filepath.Join(Dir(runID), clean)
	if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(Dir(runID))+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes run directory: %s", p)
	}
	return p, nil
}

func Write(runID int64, kind, filename string, data []byte) (string, error) {
	p, err := Path(runID, kind, filename)
	if err != nil {
		return "", err
	}
	if err := EnsureDir(runID); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0644)
}

func Read(runID int64, kind, filename string) ([]byte, error) {
	p, err := Path(runID, kind, filename)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func Exists(runID int64, kind, filename string) bool {
	p, err := Path(runID, kind, filename)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func Delete(runID int64, kind, filename string) error {
	p, err := Path(runID, kind, filename)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func ContextDir() string {
	return filepath.Join(BaseDir, "handoffs", "context")
}

func ContextPath(dateYYYYMMDD string, taskSlug string, kind string) (string, error) {
	if !allowedKinds[kind] {
		return "", fmt.Errorf("unknown artifact kind: %s", kind)
	}
	if !contextDatePattern.MatchString(dateYYYYMMDD) {
		return "", fmt.Errorf("invalid context artifact date: %s", dateYYYYMMDD)
	}
	if !contextSlugPattern.MatchString(taskSlug) {
		return "", fmt.Errorf("invalid context artifact slug: %s", taskSlug)
	}
	suffix, ok := contextKindSuffix(kind)
	if !ok {
		return "", fmt.Errorf("artifact kind is not a context artifact: %s", kind)
	}
	filename := dateYYYYMMDD + "_" + taskSlug + suffix
	if strings.Contains(filename, "..") || filepath.IsAbs(filename) || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("invalid context artifact filename: %s", filename)
	}
	dir := ContextDir()
	p := filepath.Join(dir, filename)
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(p)
	if cleanPath != filepath.Join(cleanDir, filepath.Base(cleanPath)) || !strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("context artifact path escapes directory: %s", p)
	}
	return p, nil
}

func WriteContext(dateYYYYMMDD string, taskSlug string, kind string, data []byte) (string, error) {
	p, err := ContextPath(dateYYYYMMDD, taskSlug, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ContextDir(), 0755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0644)
}

func contextKindSuffix(kind string) (string, bool) {
	switch kind {
	case "context_packet_json":
		return ".context-packet.json", true
	case "context_packet_markdown":
		return ".context-packet.md", true
	case "context_coverage_report_json":
		return ".context-coverage-report.json", true
	default:
		return "", false
	}
}

func AuditDir() string {
	return filepath.Join(BaseDir, "handoffs", "audits")
}

func AuditPath(dateYYYYMMDD string, taskSlug string, kind string) (string, error) {
	if !allowedKinds[kind] {
		return "", fmt.Errorf("unknown artifact kind: %s", kind)
	}
	if !contextDatePattern.MatchString(dateYYYYMMDD) {
		return "", fmt.Errorf("invalid audit artifact date: %s", dateYYYYMMDD)
	}
	if !contextSlugPattern.MatchString(taskSlug) {
		return "", fmt.Errorf("invalid audit artifact slug: %s", taskSlug)
	}
	suffix, ok := auditKindSuffix(kind)
	if !ok {
		return "", fmt.Errorf("artifact kind is not a local audit artifact: %s", kind)
	}
	filename := dateYYYYMMDD + "_" + taskSlug + suffix
	if strings.Contains(filename, "..") || filepath.IsAbs(filename) || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("invalid audit artifact filename: %s", filename)
	}
	dir := AuditDir()
	p := filepath.Join(dir, filename)
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(p)
	if cleanPath != filepath.Join(cleanDir, filepath.Base(cleanPath)) || !strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("audit artifact path escapes directory: %s", p)
	}
	return p, nil
}

func WriteAudit(dateYYYYMMDD string, taskSlug string, kind string, data []byte) (string, error) {
	p, err := AuditPath(dateYYYYMMDD, taskSlug, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(AuditDir(), 0755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0644)
}

func auditKindSuffix(kind string) (string, bool) {
	switch kind {
	case "local_audit_manifest_json":
		return ".local-audit-manifest.json", true
	case "local_audit_packet":
		return ".local-audit-packet.md", true
	case "local_audit_input_summary":
		return ".local-audit-input-summary.md", true
	default:
		return "", false
	}
}

// PlanDir is the durable artifact directory for generated Plan of Passes
// artifacts (e.g. reviewable refactor-only plans). Generated plans are local
// review artifacts only; writing one never submits a plan or creates a run.
func PlanDir() string {
	return filepath.Join(BaseDir, "handoffs", "plans")
}

// PlanPath builds a safe, validated path for a generated Plan of Passes artifact
// under PlanDir. The date must be YYYY-MM-DD, the slug must match the shared
// artifact slug pattern, and the kind must be a recognized plan artifact kind.
// The resulting filename cannot escape the plan artifact directory.
func PlanPath(dateYYYYMMDD string, taskSlug string, kind string) (string, error) {
	if !allowedKinds[kind] {
		return "", fmt.Errorf("unknown artifact kind: %s", kind)
	}
	if !contextDatePattern.MatchString(dateYYYYMMDD) {
		return "", fmt.Errorf("invalid plan artifact date: %s", dateYYYYMMDD)
	}
	if !contextSlugPattern.MatchString(taskSlug) {
		return "", fmt.Errorf("invalid plan artifact slug: %s", taskSlug)
	}
	suffix, ok := planKindSuffix(kind)
	if !ok {
		return "", fmt.Errorf("artifact kind is not a plan artifact: %s", kind)
	}
	filename := dateYYYYMMDD + "_" + taskSlug + suffix
	if strings.Contains(filename, "..") || filepath.IsAbs(filename) || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("invalid plan artifact filename: %s", filename)
	}
	dir := PlanDir()
	p := filepath.Join(dir, filename)
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(p)
	if cleanPath != filepath.Join(cleanDir, filepath.Base(cleanPath)) || !strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("plan artifact path escapes directory: %s", p)
	}
	return p, nil
}

// WritePlan writes a generated Plan of Passes artifact under PlanDir using a
// validated, safe path. It does not submit the plan or create any run.
func WritePlan(dateYYYYMMDD string, taskSlug string, kind string, data []byte) (string, error) {
	p, err := PlanPath(dateYYYYMMDD, taskSlug, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(PlanDir(), 0755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0644)
}

func planKindSuffix(kind string) (string, bool) {
	switch kind {
	case "planner_pass_plan_json":
		return ".planner-pass-plan.json", true
	case "planner_pass_plan_markdown":
		return ".planner-pass-plan.md", true
	default:
		return "", false
	}
}

func CloseoutDir() string {
	return filepath.Join(BaseDir, "handoffs", "closeout")
}

// CloseoutPath builds a safe, validated path for repo-owned closeout evidence.
// It only models artifact naming; it does not perform closeout, commit, or push
// behavior.
func CloseoutPath(dateYYYYMMDD string, taskSlug string, kind string) (string, error) {
	if !allowedKinds[kind] {
		return "", fmt.Errorf("unknown artifact kind: %s", kind)
	}
	if !contextDatePattern.MatchString(dateYYYYMMDD) {
		return "", fmt.Errorf("invalid closeout artifact date: %s", dateYYYYMMDD)
	}
	if !contextSlugPattern.MatchString(taskSlug) {
		return "", fmt.Errorf("invalid closeout artifact slug: %s", taskSlug)
	}
	suffix, ok := closeoutKindSuffix(kind)
	if !ok {
		return "", fmt.Errorf("artifact kind is not a closeout artifact: %s", kind)
	}
	filename := dateYYYYMMDD + "_" + taskSlug + suffix
	if strings.Contains(filename, "..") || filepath.IsAbs(filename) || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("invalid closeout artifact filename: %s", filename)
	}
	dir := CloseoutDir()
	p := filepath.Join(dir, filename)
	cleanDir := filepath.Clean(dir)
	cleanPath := filepath.Clean(p)
	if cleanPath != filepath.Join(cleanDir, filepath.Base(cleanPath)) || !strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("closeout artifact path escapes directory: %s", p)
	}
	return p, nil
}

func WriteCloseout(dateYYYYMMDD string, taskSlug string, kind string, data []byte) (string, error) {
	p, err := CloseoutPath(dateYYYYMMDD, taskSlug, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(CloseoutDir(), 0755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0644)
}

func closeoutKindSuffix(kind string) (string, bool) {
	switch kind {
	case "closeout_evidence_json":
		return ".closeout-evidence.json", true
	case "closeout_evidence_markdown":
		return ".closeout-evidence.md", true
	default:
		return "", false
	}
}
