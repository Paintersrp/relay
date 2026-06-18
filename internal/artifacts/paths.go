package artifacts

import (
	"fmt"
	"os"
	"path/filepath"
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
	"audit_revision":                     true,
	"mcp_audit_handback":                 true,
	"repair_request_json":                true,
	"repair_prompt":                      true,
	"repair_output":                      true,
	"repaired_packet":                    true,
	"repair_validation_report":           true,
}

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
