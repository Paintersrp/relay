package projects

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
)

var allowedRepositoryRoles = map[string]struct{}{
	RepositoryRolePrimary:   {},
	RepositoryRoleReference: {},
	RepositoryRoleContracts: {},
	RepositoryRoleDocs:      {},
}

var allowedProjectStatuses = map[string]struct{}{
	ProjectStatusActive:   {},
	ProjectStatusArchived: {},
}

func ValidateProjectInput(input ProjectInput) []ProjectValidationIssue {
	_, issues := NormalizeProjectInput(input)
	return issues
}

func ValidateProjectRepositoryInput(input ProjectRepositoryInput) []ProjectValidationIssue {
	_, issues := NormalizeProjectRepositoryInput(input)
	return issues
}

func NormalizeProjectInput(input ProjectInput) (NormalizedProjectInput, []ProjectValidationIssue) {
	normalized := NormalizedProjectInput{
		ProjectID:           strings.TrimSpace(input.ProjectID),
		Name:                strings.TrimSpace(input.Name),
		Description:         strings.TrimSpace(input.Description),
		Status:              strings.TrimSpace(input.Status),
		DefaultRepositoryID: strings.TrimSpace(input.DefaultRepositoryID),
	}
	if normalized.Status == "" {
		normalized.Status = ProjectStatusActive
	}

	var issues []ProjectValidationIssue
	if normalized.ProjectID == "" {
		issues = append(issues, validationIssue("project_id", "required", "project_id is required"))
	}
	if normalized.Name == "" {
		issues = append(issues, validationIssue("name", "required", "name is required"))
	}
	if _, ok := allowedProjectStatuses[normalized.Status]; !ok {
		issues = append(issues, validationIssue("status", "invalid_status", "status must be active or archived"))
	}

	return normalized, issues
}

func NormalizeProjectRepositoryInput(input ProjectRepositoryInput) (NormalizedProjectRepositoryInput, []ProjectValidationIssue) {
	normalized := NormalizedProjectRepositoryInput{
		ProjectID:        strings.TrimSpace(input.ProjectID),
		RepoID:           strings.TrimSpace(input.RepoID),
		Role:             strings.TrimSpace(input.Role),
		LocalPath:        filepath.Clean(strings.TrimSpace(input.LocalPath)),
		RemoteLabel:      strings.TrimSpace(input.RemoteLabel),
		RemoteURL:        strings.TrimSpace(input.RemoteURL),
		DefaultBranch:    strings.TrimSpace(input.DefaultBranch),
		AllowedRoots:     make([]string, 0, len(input.AllowedRoots)),
		IgnoredGlobs:     make([]string, 0, len(input.IgnoredGlobs)),
		MaxFileSizeBytes: input.MaxFileSizeBytes,
		IncludeUntracked: input.IncludeUntracked,
		Enabled:          input.Enabled,
	}

	if normalized.DefaultBranch == "" {
		normalized.DefaultBranch = DefaultBranch
	}
	if normalized.MaxFileSizeBytes == 0 {
		normalized.MaxFileSizeBytes = DefaultMaxFileSizeBytes
	}

	var issues []ProjectValidationIssue
	if normalized.ProjectID == "" {
		issues = append(issues, validationIssue("project_id", "required", "project_id is required"))
	}
	if normalized.RepoID == "" {
		issues = append(issues, validationIssue("repo_id", "required", "repo_id is required"))
	}
	if normalized.Role == "" {
		issues = append(issues, validationIssue("role", "required", "role is required"))
	} else if _, ok := allowedRepositoryRoles[normalized.Role]; !ok {
		issues = append(issues, validationIssue("role", "invalid_role", "role must be one of primary, reference, contracts, or docs"))
	}
	if strings.TrimSpace(input.LocalPath) == "" {
		issues = append(issues, validationIssue("local_path", "required", "local_path is required"))
	} else if pathIssue := validateLocalPath(strings.TrimSpace(input.LocalPath)); pathIssue != nil {
		issues = append(issues, *pathIssue)
	}
	if normalized.MaxFileSizeBytes < MinMaxFileSizeBytes || normalized.MaxFileSizeBytes > MaxAllowedFileSizeBytes {
		issues = append(issues, validationIssue("max_file_size_bytes", "invalid_range", "max_file_size_bytes must be between 1024 and 10485760"))
	}

	for _, root := range input.AllowedRoots {
		cleaned, issue := validateAllowedRoot(root)
		if issue != nil {
			issues = append(issues, *issue)
			continue
		}
		normalized.AllowedRoots = append(normalized.AllowedRoots, cleaned)
	}

	for _, glob := range input.IgnoredGlobs {
		cleaned, issue := validateIgnoredGlob(glob)
		if issue != nil {
			issues = append(issues, *issue)
			continue
		}
		normalized.IgnoredGlobs = append(normalized.IgnoredGlobs, cleaned)
	}

	if len(issues) > 0 {
		return normalized, issues
	}

	allowedRootsJSON, err := json.Marshal(normalized.AllowedRoots)
	if err != nil {
		issues = append(issues, validationIssue("allowed_roots", "invalid_json", "allowed_roots must marshal to a JSON string array"))
	} else {
		normalized.AllowedRootsJSON = string(allowedRootsJSON)
	}

	ignoredGlobsJSON, err := json.Marshal(normalized.IgnoredGlobs)
	if err != nil {
		issues = append(issues, validationIssue("ignored_globs", "invalid_json", "ignored_globs must marshal to a JSON string array"))
	} else {
		normalized.IgnoredGlobsJSON = string(ignoredGlobsJSON)
	}

	if normalized.IncludeUntracked {
		normalized.IncludeUntrackedInt = 1
	}
	if normalized.Enabled {
		normalized.EnabledInt = 1
	}

	return normalized, issues
}

func validationIssue(field, code, message string) ProjectValidationIssue {
	return ProjectValidationIssue{
		Field:   field,
		Code:    code,
		Message: message,
	}
}

func validateLocalPath(value string) *ProjectValidationIssue {
	if containsUnsafeLineBreaks(value) {
		issue := validationIssue("local_path", "invalid_path", "local_path must not contain NUL or newline characters")
		return &issue
	}
	if strings.Contains(value, "..") {
		issue := validationIssue("local_path", "invalid_path", "local_path must not contain parent traversal segments")
		return &issue
	}
	if strings.ContainsAny(value, "|&;<>`") {
		issue := validationIssue("local_path", "invalid_path", "local_path contains unsupported shell metacharacters")
		return &issue
	}
	return nil
}

func validateAllowedRoot(value string) (string, *ProjectValidationIssue) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		issue := validationIssue("allowed_roots", "invalid_root", "allowed_roots entries must not be empty")
		return "", &issue
	}
	if containsUnsafeLineBreaks(trimmed) || strings.Contains(trimmed, "\\") {
		issue := validationIssue("allowed_roots", "invalid_root", "allowed_roots entries must use forward slashes and must not contain NUL or newline characters")
		return "", &issue
	}
	if isAbsolutePolicyPath(trimmed) || hasParentTraversal(trimmed) {
		issue := validationIssue("allowed_roots", "invalid_root", "allowed_roots entries must be repo-relative and must not contain '..'")
		return "", &issue
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." && trimmed != "." {
		cleaned = ""
	}
	if cleaned == "" {
		issue := validationIssue("allowed_roots", "invalid_root", "allowed_roots entries must not be empty")
		return "", &issue
	}
	return cleaned, nil
}

func validateIgnoredGlob(value string) (string, *ProjectValidationIssue) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		issue := validationIssue("ignored_globs", "invalid_glob", "ignored_globs entries must not be empty")
		return "", &issue
	}
	if containsUnsafeLineBreaks(trimmed) || strings.Contains(trimmed, "\\") {
		issue := validationIssue("ignored_globs", "invalid_glob", "ignored_globs entries must use forward slashes and must not contain NUL or newline characters")
		return "", &issue
	}
	if isAbsolutePolicyPath(trimmed) || hasParentTraversal(trimmed) {
		issue := validationIssue("ignored_globs", "invalid_glob", "ignored_globs entries must be repo-relative and must not contain '..'")
		return "", &issue
	}
	return trimmed, nil
}

func containsUnsafeLineBreaks(value string) bool {
	return strings.Contains(value, "\x00") || strings.Contains(value, "\n") || strings.Contains(value, "\r")
}

func hasParentTraversal(value string) bool {
	for _, segment := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func isAbsolutePolicyPath(value string) bool {
	return path.IsAbs(value) || filepath.IsAbs(value)
}
