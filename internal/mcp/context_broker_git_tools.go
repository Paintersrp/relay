package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"relay/internal/sources"
	"relay/internal/store"
)

const (
	defaultChangedFilesMaxResults = 200
	maxChangedFilesMaxResults     = 1000
	maxDiffBytes                  = 262144
	maxDiffContextLines           = 10
)

var getRepositoryGitStatusSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "repo_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_id": {
      "type": "string",
      "minLength": 1,
      "description": "Registered repository identifier under the Relay project."
    }
  }
}`)

var getRepositoryRecentCommitSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "repo_id"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_id": {
      "type": "string",
      "minLength": 1,
      "description": "Registered repository identifier under the Relay project."
    }
  }
}`)

var listRepositoryChangedFilesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "repo_id", "mode"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_id": {
      "type": "string",
      "minLength": 1,
      "description": "Registered repository identifier under the Relay project."
    },
    "mode": {
      "type": "string",
      "enum": ["worktree", "staged"],
      "description": "Read-only diff scope to inspect."
    },
    "max_results": {
      "type": "integer",
      "minimum": 1,
      "maximum": 1000,
      "description": "Maximum changed file records to return."
    }
  }
}`)

var getRepositoryDiffSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["project_id", "repo_id", "mode"],
  "properties": {
    "project_id": {
      "type": "string",
      "minLength": 1,
      "description": "Relay project identifier."
    },
    "repo_id": {
      "type": "string",
      "minLength": 1,
      "description": "Registered repository identifier under the Relay project."
    },
    "mode": {
      "type": "string",
      "enum": ["worktree", "staged"],
      "description": "Read-only diff scope to inspect."
    },
    "max_bytes": {
      "type": "integer",
      "minimum": 1,
      "maximum": 262144,
      "description": "Maximum diff bytes to return."
    },
    "context_lines": {
      "type": "integer",
      "minimum": 0,
      "maximum": 10,
      "description": "Unified diff context lines."
    }
  }
}`)

var (
	ToolGetRepositoryGitStatus = ToolDefinition{
		Name:        "get_repository_git_status",
		Description: "Return bounded read-only git status evidence for a registered Relay repository without exposing local paths.",
		InputSchema: getRepositoryGitStatusSchema,
	}
	ToolGetRepositoryRecentCommit = ToolDefinition{
		Name:        "get_repository_recent_commit",
		Description: "Return recent commit metadata for a registered Relay repository without author email or local paths.",
		InputSchema: getRepositoryRecentCommitSchema,
	}
	ToolListRepositoryChangedFiles = ToolDefinition{
		Name:        "list_repository_changed_files",
		Description: "List bounded changed-file evidence for a registered Relay repository in worktree or staged mode.",
		InputSchema: listRepositoryChangedFilesSchema,
	}
	ToolGetRepositoryDiff = ToolDefinition{
		Name:        "get_repository_diff",
		Description: "Return bounded and redacted diff evidence for a registered Relay repository in worktree or staged mode.",
		InputSchema: getRepositoryDiffSchema,
	}
)

type repositoryGitArgs struct {
	ProjectID string `json:"project_id"`
	RepoID    string `json:"repo_id"`
}

type listRepositoryChangedFilesArgs struct {
	ProjectID  string `json:"project_id"`
	RepoID     string `json:"repo_id"`
	Mode       string `json:"mode"`
	MaxResults int    `json:"max_results"`
}

type getRepositoryDiffArgs struct {
	ProjectID    string `json:"project_id"`
	RepoID       string `json:"repo_id"`
	Mode         string `json:"mode"`
	MaxBytes     int    `json:"max_bytes"`
	ContextLines int    `json:"context_lines"`
}

type brokerRepositoryGitStatusResult struct {
	ProjectID          string `json:"project_id"`
	RepoID             string `json:"repo_id"`
	GeneratedAt        string `json:"generated_at"`
	RedactionStatus    string `json:"redaction_status"`
	Truncated          bool   `json:"truncated"`
	CurrentBranch      string `json:"current_branch"`
	HeadSHA            string `json:"head_sha"`
	Dirty              bool   `json:"dirty"`
	StagedCount        int    `json:"staged_count"`
	UnstagedCount      int    `json:"unstaged_count"`
	UntrackedCount     int    `json:"untracked_count"`
	ChangedFileCount   int    `json:"changed_file_count"`
	PorcelainHash      string `json:"porcelain_hash"`
	GitStatusAvailable bool   `json:"git_status_available"`
}

type brokerRepositoryRecentCommitResult struct {
	ProjectID       string `json:"project_id"`
	RepoID          string `json:"repo_id"`
	GeneratedAt     string `json:"generated_at"`
	RedactionStatus string `json:"redaction_status"`
	Truncated       bool   `json:"truncated"`
	CommitSHA       string `json:"commit_sha"`
	AuthorName      string `json:"author_name"`
	AuthorDate      string `json:"author_date"`
	Subject         string `json:"subject"`
}

type brokerRepositoryChangedFileResult struct {
	RepoID string `json:"repo_id"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged"`
}

type brokerRepositoryChangedFilesResult struct {
	ProjectID       string                              `json:"project_id"`
	RepoID          string                              `json:"repo_id"`
	GeneratedAt     string                              `json:"generated_at"`
	RedactionStatus string                              `json:"redaction_status"`
	Truncated       bool                                `json:"truncated"`
	Mode            string                              `json:"mode"`
	MaxResults      int                                 `json:"max_results"`
	Files           []brokerRepositoryChangedFileResult `json:"files"`
}

type brokerRepositoryDiffResult struct {
	ProjectID       string `json:"project_id"`
	RepoID          string `json:"repo_id"`
	GeneratedAt     string `json:"generated_at"`
	RedactionStatus string `json:"redaction_status"`
	Truncated       bool   `json:"truncated"`
	Mode            string `json:"mode"`
	Content         string `json:"content"`
	ContentHash     string `json:"content_hash"`
	MaxBytes        int    `json:"max_bytes"`
}

func (s *Server) HandleGetRepositoryGitStatus(rawArgs json.RawMessage) ToolCallResult {
	var args repositoryGitArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID, repoID, err := validateRepositoryGitArgs(args.ProjectID, args.RepoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	repo, err := s.loadRegisteredProjectRepository(projectID, repoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	status, err := sources.NewService(s.deps.Store).GetRepositoryGitStatus(context.Background(), repo)
	if err != nil {
		return brokerToolErr("DEPENDENCY_ERROR", "git status unavailable: "+err.Error())
	}
	result := brokerRepositoryGitStatusResult{
		ProjectID:          projectID,
		RepoID:             repoID,
		GeneratedAt:        brokerGeneratedAt(),
		RedactionStatus:    sources.RedactionStatusNotNeeded,
		Truncated:          false,
		CurrentBranch:      status.CurrentBranch,
		HeadSHA:            status.HeadSHA,
		Dirty:              status.Dirty,
		StagedCount:        status.StagedCount,
		UnstagedCount:      status.UnstagedCount,
		UntrackedCount:     status.UntrackedCount,
		ChangedFileCount:   status.ChangedFileCount,
		PorcelainHash:      status.PorcelainHash,
		GitStatusAvailable: status.GitStatusAvailable,
	}
	return brokerToolOK(ToolGetRepositoryGitStatus.Name, result)
}

func (s *Server) HandleGetRepositoryRecentCommit(rawArgs json.RawMessage) ToolCallResult {
	var args repositoryGitArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID, repoID, err := validateRepositoryGitArgs(args.ProjectID, args.RepoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	repo, err := s.loadRegisteredProjectRepository(projectID, repoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	commit, err := sources.NewService(s.deps.Store).GetRecentCommit(context.Background(), repo)
	if err != nil {
		return brokerToolErr("DEPENDENCY_ERROR", "recent commit unavailable: "+err.Error())
	}
	result := brokerRepositoryRecentCommitResult{
		ProjectID:       projectID,
		RepoID:          repoID,
		GeneratedAt:     brokerGeneratedAt(),
		RedactionStatus: sources.RedactionStatusNotNeeded,
		Truncated:       false,
		CommitSHA:       commit.CommitSHA,
		AuthorName:      commit.AuthorName,
		AuthorDate:      commit.AuthorDate,
		Subject:         commit.Subject,
	}
	return brokerToolOK(ToolGetRepositoryRecentCommit.Name, result)
}

func (s *Server) HandleListRepositoryChangedFiles(rawArgs json.RawMessage) ToolCallResult {
	var args listRepositoryChangedFilesArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID, repoID, err := validateRepositoryGitArgs(args.ProjectID, args.RepoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	mode, err := validateRepositoryGitMode(args.Mode)
	if err != nil {
		return brokerWrappedErr(err)
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = defaultChangedFilesMaxResults
	}
	if maxResults > maxChangedFilesMaxResults {
		maxResults = maxChangedFilesMaxResults
	}
	repo, err := s.loadRegisteredProjectRepository(projectID, repoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	files, err := sources.NewService(s.deps.Store).GetChangedFiles(context.Background(), repo, mode)
	if err != nil {
		return brokerToolErr("DEPENDENCY_ERROR", "changed files unavailable: "+err.Error())
	}
	truncated := len(files) > maxResults
	if truncated {
		files = files[:maxResults]
	}
	resultFiles := make([]brokerRepositoryChangedFileResult, 0, len(files))
	for _, file := range files {
		resultFiles = append(resultFiles, brokerRepositoryChangedFileResult{
			RepoID: file.RepoID,
			Path:   file.Path,
			Status: file.Status,
			Staged: file.Staged,
		})
	}
	result := brokerRepositoryChangedFilesResult{
		ProjectID:       projectID,
		RepoID:          repoID,
		GeneratedAt:     brokerGeneratedAt(),
		RedactionStatus: sources.RedactionStatusNotNeeded,
		Truncated:       truncated,
		Mode:            mode,
		MaxResults:      maxResults,
		Files:           resultFiles,
	}
	return brokerToolOK(ToolListRepositoryChangedFiles.Name, result)
}

func (s *Server) HandleGetRepositoryDiff(rawArgs json.RawMessage) ToolCallResult {
	var args getRepositoryDiffArgs
	if err := brokerDecodeStrict(rawArgs, &args); err != nil {
		return brokerToolErr("VALIDATION_ERROR", "invalid params: "+err.Error())
	}
	projectID, repoID, err := validateRepositoryGitArgs(args.ProjectID, args.RepoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	mode, err := validateRepositoryGitMode(args.Mode)
	if err != nil {
		return brokerWrappedErr(err)
	}
	maxBytes := args.MaxBytes
	if maxBytes > maxDiffBytes {
		maxBytes = maxDiffBytes
	}
	contextLines := args.ContextLines
	if contextLines > maxDiffContextLines {
		contextLines = maxDiffContextLines
	}
	repo, err := s.loadRegisteredProjectRepository(projectID, repoID)
	if err != nil {
		return brokerWrappedErr(err)
	}
	diff, err := sources.NewService(s.deps.Store).GetBoundedDiff(context.Background(), repo, mode, maxBytes, contextLines)
	if err != nil {
		return brokerToolErr("DEPENDENCY_ERROR", "diff unavailable: "+err.Error())
	}
	content := diff.Content
	if diff.RedactionStatus == sources.RedactionStatusBlocked {
		content = ""
	}
	result := brokerRepositoryDiffResult{
		ProjectID:       projectID,
		RepoID:          repoID,
		GeneratedAt:     brokerGeneratedAt(),
		RedactionStatus: diff.RedactionStatus,
		Truncated:       diff.Truncated,
		Mode:            diff.Mode,
		Content:         content,
		ContentHash:     diff.ContentHash,
		MaxBytes:        diff.MaxBytes,
	}
	return brokerToolOK(ToolGetRepositoryDiff.Name, result)
}

func (s *Server) loadRegisteredProjectRepository(projectID, repoID string) (store.ProjectRepository, error) {
	project, repos, err := s.loadProjectWithRepos(projectID)
	if err != nil {
		return store.ProjectRepository{}, err
	}
	for _, repo := range repos {
		if repo.RepoID != repoID {
			continue
		}
		if repo.Enabled != 1 {
			return store.ProjectRepository{}, brokerOpError{Code: "NOT_FOUND", Message: fmt.Sprintf("repository %q not found under project %q", repoID, project.ProjectID)}
		}
		return repo, nil
	}
	return store.ProjectRepository{}, brokerOpError{Code: "NOT_FOUND", Message: fmt.Sprintf("repository %q not found under project %q", repoID, project.ProjectID)}
}

func validateRepositoryGitArgs(projectID, repoID string) (string, string, error) {
	projectID = strings.TrimSpace(projectID)
	repoID = strings.TrimSpace(repoID)
	if projectID == "" {
		return "", "", brokerOpError{Code: "VALIDATION_ERROR", Message: "project_id is required"}
	}
	if repoID == "" {
		return "", "", brokerOpError{Code: "VALIDATION_ERROR", Message: "repo_id is required"}
	}
	return projectID, repoID, nil
}

func validateRepositoryGitMode(mode string) (string, error) {
	mode = strings.TrimSpace(mode)
	switch mode {
	case sources.DiffModeWorktree, sources.DiffModeStaged:
		return mode, nil
	default:
		return "", brokerOpError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("unsupported mode %q", mode)}
	}
}

func brokerGeneratedAt() string {
	return time.Now().UTC().Format(time.RFC3339)
}
