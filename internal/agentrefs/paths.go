package agentrefs

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	OutputDir                = "docs/generated/agent-references"
	IndexJSONPath            = OutputDir + "/index.json"
	IndexMarkdownPath        = OutputDir + "/index.md"
	BackendSurfaceJSONPath   = OutputDir + "/backend-surface.json"
	BackendSurfaceMarkdownPath = OutputDir + "/backend-surface.md"
	WorkflowSurfaceJSONPath    = OutputDir + "/workflow-surfaces.json"
	WorkflowSurfaceMarkdownPath = OutputDir + "/workflow-surfaces.md"
	StorageSurfaceJSONPath     = OutputDir + "/storage-surface.json"
	StorageSurfaceMarkdownPath = OutputDir + "/storage-surface.md"
	SchemaPath                 = "schema/project_agent_reference.schema.json"
)

func ValidateRepoRelativePath(path string) error {
	if path == "" {
		return fmt.Errorf("path must not be empty")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must not be absolute (leading slash): %q", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain '..': %q", path)
	}
	if strings.ContainsAny(path, "\\") {
		return fmt.Errorf("path must not contain backslashes: %q", path)
	}
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("path must not contain newline characters: %q", path)
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("path must not escape repo root: %q", path)
	}
	return nil
}
