package artifacts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const BaseDir = "data/artifacts"

var allowedKinds = map[string]bool{
	"original_handoff":        true,
	"handoff_validation_json": true,
	"ready_prompt":            true,
	"audit_packet":            true,
	"opencode_handoff_packet": true,
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
