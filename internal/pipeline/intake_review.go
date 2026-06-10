package pipeline

import (
	"os"
	"path/filepath"
	"strings"
)

type IntakeReview struct {
	Metadata         HandoffMetadata   `json:"metadata"`
	RepoPath         string            `json:"repo_path"`
	ScopedFileChecks []ScopedFileCheck `json:"scoped_file_checks"`
	Warnings         []string          `json:"warnings"`
	Blockers         []string          `json:"blockers"`
}

type ScopedFileCheck struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

func BuildIntakeReview(metadata HandoffMetadata, repoPath string) IntakeReview {
	review := IntakeReview{
		Metadata:         metadata,
		RepoPath:         repoPath,
		ScopedFileChecks: make([]ScopedFileCheck, 0),
		Warnings:         make([]string, 0),
		Blockers:         make([]string, 0),
	}

	if repoPath == "" {
		review.Blockers = append(review.Blockers, "Selected repo path is missing.")
		return review
	}

	if len(metadata.ScopedFiles) == 0 {
		return review
	}

	repoPathClean := filepath.Clean(repoPath)
	filesFound := 0

	for _, sf := range metadata.ScopedFiles {
		cleanPath := filepath.Clean(sf.Path)
		// prevent path traversal outside repo
		if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, ".."+string(filepath.Separator)) {
			review.ScopedFileChecks = append(review.ScopedFileChecks, ScopedFileCheck{
				Path:   sf.Path,
				Exists: false,
			})
			continue
		}

		fullPath := filepath.Join(repoPathClean, cleanPath)
		// ensure we haven't escaped the repo path
		absRepo, _ := filepath.Abs(repoPathClean)
		absFull, _ := filepath.Abs(fullPath)
		if !strings.HasPrefix(absFull, absRepo+string(filepath.Separator)) && absFull != absRepo {
			review.ScopedFileChecks = append(review.ScopedFileChecks, ScopedFileCheck{
				Path:   sf.Path,
				Exists: false,
			})
			continue
		}

		_, err := os.Stat(fullPath)
		exists := err == nil
		review.ScopedFileChecks = append(review.ScopedFileChecks, ScopedFileCheck{
			Path:   sf.Path,
			Exists: exists,
		})
		if exists {
			filesFound++
		}
	}

	if filesFound == 0 {
		review.Blockers = append(review.Blockers, "Selected repo does not appear to match handoff scope.")
	} else if filesFound < len(metadata.ScopedFiles) {
		review.Warnings = append(review.Warnings, "Some scoped files were not found in the selected repo.")
	}

	if len(metadata.ValidationCommands) == 0 {
		review.Warnings = append(review.Warnings, "No validation commands found. Agent execution can continue, but Relay Validation will be unavailable until validation commands are added to the handoff or repo defaults.")
	}

	return review
}
