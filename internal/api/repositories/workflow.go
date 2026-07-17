package repositories

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	workflowapp "relay/internal/app/workflow"
	workflowrepos "relay/internal/repos/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowRepositoryService interface {
	ListRepositories(context.Context) ([]workflowapp.RepositoryTarget, error)
	GetRepository(context.Context, string) (workflowapp.RepositoryTarget, error)
	InspectRepository(context.Context, workflowapp.RepositoryInspectionInput) (workflowapp.RepositoryInspection, error)
	ConfirmRepository(context.Context, workflowapp.RepositoryConfirmationInput) (workflowapp.RepositoryRegistrationResult, error)
}

type WorkflowHandler struct {
	service WorkflowRepositoryService
	logger  *slog.Logger
}

func NewWorkflowHandler(service WorkflowRepositoryService, logger *slog.Logger) *WorkflowHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkflowHandler{service: service, logger: logger}
}

type repositoryResponse struct {
	RepoTarget           string  `json:"repoTarget"`
	LocalPath            string  `json:"localPath"`
	ConfiguredBranchRef  *string `json:"configuredBranchRef"`
	ConfigurationVersion int64   `json:"configurationVersion"`
	CreatedAt            string  `json:"createdAt"`
	UpdatedAt            string  `json:"updatedAt"`
}

type remoteCandidateResponse struct {
	Name                string `json:"name"`
	URL                 string `json:"url"`
	SuggestedRepoTarget string `json:"suggestedRepoTarget,omitempty"`
}

type inspectionResponse struct {
	State                        string                    `json:"state"`
	SelectedPath                 string                    `json:"selectedPath"`
	ResolvedLocalPath            string                    `json:"resolvedLocalPath"`
	Remotes                      []remoteCandidateResponse `json:"remotes"`
	SelectedRemote               *remoteCandidateResponse  `json:"selectedRemote,omitempty"`
	SuggestedRepoTarget          string                    `json:"suggestedRepoTarget,omitempty"`
	TargetOverrideReason         string                    `json:"targetOverrideReason,omitempty"`
	RepoTarget                   string                    `json:"repoTarget,omitempty"`
	RepoTargetSource             string                    `json:"repoTargetSource,omitempty"`
	RegistrationDisposition      string                    `json:"registrationDisposition,omitempty"`
	ExistingRepository           *repositoryResponse       `json:"existingRepository,omitempty"`
	CurrentConfiguredBranchRef   *string                   `json:"currentConfiguredBranchRef"`
	ExpectedConfigurationVersion int64                     `json:"expectedConfigurationVersion"`
	ProposedConfiguredBranchRef  *string                   `json:"proposedConfiguredBranchRef"`
	ProposedConfigurationVersion int64                     `json:"proposedConfigurationVersion"`
	ProposedBranchCommitOID      string                    `json:"proposedBranchCommitOid,omitempty"`
	ProposedBranchTreeOID        string                    `json:"proposedBranchTreeOid,omitempty"`
	ConfigurationDisposition     string                    `json:"configurationDisposition,omitempty"`
	ConflictKind                 string                    `json:"conflictKind,omitempty"`
	ConfirmationHash             string                    `json:"confirmationHash,omitempty"`
	Notices                      []string                  `json:"notices"`
}

type optionalBranchRef struct {
	Present bool
	Value   string
}

func (value *optionalBranchRef) UnmarshalJSON(data []byte) error {
	value.Present = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("proposedConfiguredBranchRef must not be null")
	}
	if err := json.Unmarshal(data, &value.Value); err != nil {
		return errors.New("proposedConfiguredBranchRef must be a string")
	}
	return nil
}

type inspectRepositoryRequest struct {
	LocalPath                   string            `json:"localPath"`
	RemoteName                  string            `json:"remoteName"`
	RepoTargetOverride          string            `json:"repoTargetOverride"`
	ProposedConfiguredBranchRef optionalBranchRef `json:"proposedConfiguredBranchRef"`
}

type confirmRepositoryRequest struct {
	LocalPath                   string            `json:"localPath"`
	RemoteName                  string            `json:"remoteName"`
	RepoTargetOverride          string            `json:"repoTargetOverride"`
	ProposedConfiguredBranchRef optionalBranchRef `json:"proposedConfiguredBranchRef"`
	ExpectedConfirmationHash    string            `json:"expectedConfirmationHash"`
}

func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.ListRepositories(r.Context())
	if err != nil {
		h.writeWorkflowRepositoryError(w, r, err)
		return
	}
	items := make([]repositoryResponse, 0, len(values))
	for _, value := range values {
		items = append(items, repositoryDTO(value))
	}
	shared.JSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	value, err := h.service.GetRepository(r.Context(), strings.TrimSpace(chi.URLParam(r, "repoTarget")))
	if err != nil {
		h.writeWorkflowRepositoryError(w, r, err)
		return
	}
	shared.JSON(w, http.StatusOK, repositoryDTO(value))
}

func (h *WorkflowHandler) Inspect(w http.ResponseWriter, r *http.Request) {
	var request inspectRepositoryRequest
	if err := decodeRepositoryRequest(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid repository inspection request")
		return
	}
	value, err := h.service.InspectRepository(r.Context(), workflowapp.RepositoryInspectionInput{
		LocalPath:                   request.LocalPath,
		RemoteName:                  request.RemoteName,
		RepoTargetOverride:          request.RepoTargetOverride,
		ProposedConfiguredBranchRef: request.ProposedConfiguredBranchRef.Value,
	})
	if err != nil {
		h.writeWorkflowRepositoryError(w, r, err)
		return
	}
	shared.JSON(w, http.StatusOK, inspectionDTO(value))
}

func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request confirmRepositoryRequest
	if err := decodeRepositoryRequest(r, &request); err != nil {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid repository confirmation request")
		return
	}
	value, err := h.service.ConfirmRepository(r.Context(), workflowapp.RepositoryConfirmationInput{
		LocalPath:                   request.LocalPath,
		RemoteName:                  request.RemoteName,
		RepoTargetOverride:          request.RepoTargetOverride,
		ProposedConfiguredBranchRef: request.ProposedConfiguredBranchRef.Value,
		ExpectedConfirmationHash:    request.ExpectedConfirmationHash,
	})
	if err != nil {
		var confirmationError *workflowrepos.ConfirmationError
		if errors.As(err, &confirmationError) {
			shared.JSON(w, http.StatusConflict, map[string]any{
				"error":   "CONFIRMATION_REQUIRED",
				"message": confirmationError.Error(),
				"details": map[string]any{
					"inspection": inspectionDTO(confirmationError.Inspection),
				},
			})
			return
		}
		h.writeWorkflowRepositoryError(w, r, err)
		return
	}
	status := http.StatusCreated
	if value.Outcome == workflowrepos.RegistrationOutcomeReused {
		status = http.StatusOK
	}
	shared.JSON(w, status, map[string]any{
		"outcome":                  value.Outcome,
		"configurationDisposition": value.ConfigurationDisposition,
		"repository":               repositoryDTO(value.Repository),
	})
}

func decodeRepositoryRequest(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("repository request must contain one JSON object")
	}
	return nil
}

func repositoryDTO(value workflowapp.RepositoryTarget) repositoryResponse {
	return repositoryResponse{
		RepoTarget:           value.RepoTarget,
		LocalPath:            value.LocalPath,
		ConfiguredBranchRef:  nullableStringDTO(value.ConfiguredBranchRef),
		ConfigurationVersion: value.ConfigurationVersion,
		CreatedAt:            value.CreatedAt,
		UpdatedAt:            value.UpdatedAt,
	}
}

func nullableStringDTO(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

func inspectionDTO(value workflowapp.RepositoryInspection) inspectionResponse {
	remotes := make([]remoteCandidateResponse, 0, len(value.Remotes))
	for _, remote := range value.Remotes {
		remotes = append(remotes, remoteDTO(remote))
	}
	var selectedRemote *remoteCandidateResponse
	if value.SelectedRemote != nil {
		dto := remoteDTO(*value.SelectedRemote)
		selectedRemote = &dto
	}
	var existingRepository *repositoryResponse
	if value.ExistingRepository != nil {
		dto := repositoryDTO(*value.ExistingRepository)
		existingRepository = &dto
	}
	return inspectionResponse{
		State:                        value.State,
		SelectedPath:                 value.SelectedPath,
		ResolvedLocalPath:            value.ResolvedLocalPath,
		Remotes:                      remotes,
		SelectedRemote:               selectedRemote,
		SuggestedRepoTarget:          value.SuggestedRepoTarget,
		TargetOverrideReason:         value.TargetOverrideReason,
		RepoTarget:                   value.RepoTarget,
		RepoTargetSource:             value.RepoTargetSource,
		RegistrationDisposition:      value.RegistrationDisposition,
		ExistingRepository:           existingRepository,
		CurrentConfiguredBranchRef:   nullableStringDTO(value.CurrentConfiguredBranchRef),
		ExpectedConfigurationVersion: value.ExpectedConfigurationVersion,
		ProposedConfiguredBranchRef:  nullableStringDTO(value.ProposedConfiguredBranchRef),
		ProposedConfigurationVersion: value.ProposedConfigurationVersion,
		ProposedBranchCommitOID:      value.ProposedBranchCommitOID,
		ProposedBranchTreeOID:        value.ProposedBranchTreeOID,
		ConfigurationDisposition:     value.ConfigurationDisposition,
		ConflictKind:                 value.ConflictKind,
		ConfirmationHash:             value.ConfirmationHash,
		Notices:                      append([]string{}, value.Notices...),
	}
}

func remoteDTO(value workflowapp.RepositoryRemoteCandidate) remoteCandidateResponse {
	return remoteCandidateResponse{
		Name:                value.Name,
		URL:                 value.URL,
		SuggestedRepoTarget: value.SuggestedRepoTarget,
	}
}

func (h *WorkflowHandler) writeWorkflowRepositoryError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Repository target was not found")
	case errors.Is(err, workflowrepos.ErrInvalidRepositoryPath):
		shared.Error(w, http.StatusUnprocessableEntity, "INVALID_REPOSITORY_PATH", err.Error())
	case errors.Is(err, workflowrepos.ErrInvalidConfiguredBranch),
		errors.Is(err, workflowrepos.ErrConfiguredBranchUnavailable):
		shared.Error(w, http.StatusUnprocessableEntity, "INVALID_REPOSITORY_AUTHORITY", err.Error())
	case errors.Is(err, workflowrepos.ErrGitUnavailable),
		errors.Is(err, workflowrepos.ErrGitTimeout),
		errors.Is(err, workflowrepos.ErrGitOutputLimit):
		shared.Error(w, http.StatusServiceUnavailable, "GIT_UNAVAILABLE", err.Error())
	case errors.Is(err, workflowapp.ErrInvalidWorkflowRequest),
		strings.Contains(err.Error(), "repository target"),
		strings.Contains(err.Error(), "repository path"),
		strings.Contains(err.Error(), "configured Git remote"),
		strings.Contains(err.Error(), "confirmation hash"):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		h.logger.ErrorContext(
			r.Context(),
			"repository operation failed",
			"method",
			r.Method,
			"path",
			r.URL.Path,
			"error",
			err,
		)
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Repository operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/repositories", handler.List)
	r.Post("/repositories/inspect", handler.Inspect)
	r.Post("/repositories", handler.Create)
	r.Get("/repositories/{repoTarget}", handler.Get)
}
