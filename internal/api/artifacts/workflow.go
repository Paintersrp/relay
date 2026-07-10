package artifacts

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"relay/internal/api/shared"
	workflowapp "relay/internal/app/workflow"

	"github.com/go-chi/chi/v5"
)

type WorkflowArtifactService interface {
	GetArtifact(context.Context, string) (workflowapp.ArtifactMetadata, error)
	GetArtifactContent(context.Context, workflowapp.ArtifactContentInput) (workflowapp.ArtifactContent, error)
}

type WorkflowHandler struct {
	service WorkflowArtifactService
}

func NewWorkflowHandler(service WorkflowArtifactService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

type workflowArtifactMetadataResponse struct {
	ArtifactID string `json:"artifactId"`
	OwnerType  string `json:"ownerType"`
	Kind       string `json:"kind"`
	MediaType  string `json:"mediaType"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"sizeBytes"`
	CreatedAt  string `json:"createdAt"`
	ContentURL string `json:"contentUrl"`
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	artifact, err := h.service.GetArtifact(r.Context(), strings.TrimSpace(chi.URLParam(r, "artifactID")))
	if err != nil {
		writeWorkflowArtifactError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, workflowArtifactMetadataDTO(artifact))
}

func (h *WorkflowHandler) Content(w http.ResponseWriter, r *http.Request) {
	offset, limit, ok := workflowArtifactRange(r)
	if !ok {
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid artifact content range")
		return
	}
	content, err := h.service.GetArtifactContent(r.Context(), workflowapp.ArtifactContentInput{
		ArtifactID: strings.TrimSpace(chi.URLParam(r, "artifactID")),
		Offset:     offset,
		Limit:      limit,
	})
	if err != nil {
		writeWorkflowArtifactError(w, err)
		return
	}
	value := string(content.Bytes)
	if content.Encoding == "base64" {
		value = base64.StdEncoding.EncodeToString(content.Bytes)
	}
	response := map[string]any{
		"artifact":  workflowArtifactMetadataDTO(content.Artifact),
		"offset":    content.Offset,
		"byteCount": len(content.Bytes),
		"encoding":  content.Encoding,
		"content":   value,
		"truncated": content.Truncated,
	}
	if content.HasNext {
		response["nextOffset"] = content.NextOffset
	}
	shared.JSON(w, http.StatusOK, response)
}

func workflowArtifactRange(r *http.Request) (int64, int64, bool) {
	offset := int64(0)
	limit := int64(0)
	if value := strings.TrimSpace(r.URL.Query().Get("offset")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, false
		}
		offset = parsed
	}
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return 0, 0, false
		}
		limit = parsed
	}
	return offset, limit, true
}

func workflowArtifactMetadataDTO(value workflowapp.ArtifactMetadata) workflowArtifactMetadataResponse {
	return workflowArtifactMetadataResponse{
		ArtifactID: value.ArtifactID,
		OwnerType:  value.OwnerType,
		Kind:       value.Kind,
		MediaType:  value.MediaType,
		SHA256:     value.SHA256,
		SizeBytes:  value.SizeBytes,
		CreatedAt:  value.CreatedAt,
		ContentURL: "/api/artifacts/" + value.ArtifactID + "/content",
	}
}

func writeWorkflowArtifactError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Artifact was not found")
	case errors.Is(err, workflowapp.ErrInvalidWorkflowRequest):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, workflowapp.ErrArtifactIntegrity):
		shared.Error(w, http.StatusConflict, "ARTIFACT_INTEGRITY_FAILED", "Artifact content no longer matches its durable metadata")
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Artifact operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/artifacts/{artifactID}", handler.Get)
	r.Get("/artifacts/{artifactID}/content", handler.Content)
}
