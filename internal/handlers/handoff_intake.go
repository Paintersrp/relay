package handlers

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"relay/internal/pipeline"
)

const maxHandoffUploadBytes = 2 << 20 // 2 MiB

func resolveHandoffText(r *http.Request) (text string, source string, err error) {
	if err := r.ParseMultipartForm(maxHandoffUploadBytes); err != nil {
		if err := r.ParseForm(); err != nil {
			return "", "", fmt.Errorf("failed to parse form: %w", err)
		}
	}

	pasted := r.FormValue("handoff_text")

	file, header, err := r.FormFile("handoff_file")
	if err == nil {
		defer file.Close()

		if header.Filename != "" {
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext != ".txt" && ext != ".md" {
				return "", "", fmt.Errorf("handoff upload must be a .txt or .md file")
			}

			limited := io.LimitReader(file, int64(maxHandoffUploadBytes))
			data, readErr := io.ReadAll(limited)
			if readErr != nil {
				return "", "", fmt.Errorf("failed to read uploaded file: %w", readErr)
			}

			uploadedText := string(data)
			if strings.TrimSpace(uploadedText) == "" {
				return "", "", fmt.Errorf("uploaded handoff file is empty")
			}

			return uploadedText, "upload", nil
		}
	}

	trimmed := strings.TrimSpace(pasted)
	if trimmed == "" {
		return "", "", fmt.Errorf("handoff text is required")
	}

	return pasted, "paste", nil
}

func deriveRunTitle(providedTitle string, handoffText string) string {
	trimmed := strings.TrimSpace(providedTitle)
	if trimmed != "" {
		return trimmed
	}

	meta := pipeline.ParseHandoffMetadata(handoffText, "")
	if meta.Title != "" {
		return meta.Title
	}

	return "Untitled handoff"
}
