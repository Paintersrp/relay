package plans

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"relay/internal/validation"
)

const (
	// MaxRawPlanJSONSize is the maximum size for raw plan JSON storage (256 KiB)
	MaxRawPlanJSONSize = 256 * 1024

	// IDSlugLength is the length of the UUID-derived slug for IDs
	IDSlugLength = 8
)

// sha256Bytes computes the SHA256 hash of bytes and returns it in the format "sha256:hex"
func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// canonicalJSONHash computes the canonical JSON hash of a value
func canonicalJSONHash(v any) (string, []byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", nil, fmt.Errorf("marshal JSON: %w", err)
	}
	return sha256Bytes(b), b, nil
}

// validateSHA256 validates that a string is a valid sha256:hex format
func validateSHA256(s string) bool {
	if !strings.HasPrefix(s, "sha256:") {
		return false
	}
	hexPart := strings.TrimPrefix(s, "sha256:")
	if len(hexPart) != 64 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

// validateAttemptArtifactRef validates an artifact reference for safe path and correct format
func validateAttemptArtifactRef(ref PlanArtifactRef, expectedExt string) error {
	if ref.Path == "" {
		return fmt.Errorf("artifact path is required")
	}

	// Check for absolute paths
	if filepath.IsAbs(ref.Path) {
		return fmt.Errorf("absolute paths are not allowed: %s", ref.Path)
	}

	// Check for path traversal
	cleanPath := filepath.Clean(ref.Path)
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", ref.Path)
	}

	// Check for backslashes (Windows path separators)
	if strings.Contains(ref.Path, "\\") {
		return fmt.Errorf("backslashes not allowed in path: %s", ref.Path)
	}

	// Check for newline characters
	if strings.ContainsAny(ref.Path, "\n\r") {
		return fmt.Errorf("newlines not allowed in path")
	}

	// Check for shell metacharacters
	if strings.ContainsAny(ref.Path, ";&|$`()+{}<>\"") {
		return fmt.Errorf("shell metacharacters not allowed in path")
	}

	// Validate extension
	ext := strings.ToLower(filepath.Ext(ref.Path))
	if ext != expectedExt {
		return fmt.Errorf("invalid extension: expected %s, got %s", expectedExt, ext)
	}

	// Validate SHA256 format
	if !validateSHA256(ref.SHA256) {
		return fmt.Errorf("invalid SHA256 format: %s", ref.SHA256)
	}

	return nil
}

// containsSecretLikeContent scans a value recursively for secret-like content
func containsSecretLikeContent(v any) bool {
	switch val := v.(type) {
	case string:
		return validation.HasSecret(val)
	case []byte:
		return validation.HasSecret(string(val))
	case []string:
		for _, s := range val {
			if validation.HasSecret(s) {
				return true
			}
		}
	case map[string]any:
		for _, v := range val {
			if containsSecretLikeContent(v) {
				return true
			}
		}
	case json.RawMessage:
		if len(val) > 0 {
			return containsSecretLikeContent(string(val))
		}
	}
	return false
}

// checkIntentPacketInputForSecrets checks intent packet input for secret-like content
func checkIntentPacketInputForSecrets(input IntentPacketInput) error {
	if validation.HasSecret(input.Summary) {
		return fmt.Errorf("summary contains secret-like content")
	}
	if validation.HasSecret(input.LiteralUserRequest) {
		return fmt.Errorf("literal_user_request contains secret-like content")
	}
	for _, c := range input.Constraints {
		if validation.HasSecret(c) {
			return fmt.Errorf("constraint contains secret-like content: %s", c)
		}
	}
	if validation.HasSecret(input.Source.CapturedBy) {
		return fmt.Errorf("captured_by contains secret-like content")
	}
	if validation.HasSecret(input.Source.SourceArtifactPath) {
		return fmt.Errorf("source_artifact_path contains secret-like content")
	}
	return nil
}

// newPlanAttemptID generates a deterministic plan attempt ID
func newPlanAttemptID(prefixSlug string) string {
	return fmt.Sprintf("plan-attempt-%s-%s", time.Now().Format("2006-01-02"), prefixSlug)
}

// newIntentThreadID generates a deterministic intent thread ID
func newIntentThreadID(prefixSlug string) string {
	return fmt.Sprintf("intent-thread-%s-%s", time.Now().Format("2006-01-02"), prefixSlug)
}

// newIntentPacketID generates a deterministic intent packet ID
func newIntentPacketID(prefixSlug string) string {
	return fmt.Sprintf("intent-packet-%s-%s", time.Now().Format("2006-01-02"), prefixSlug)
}

// newIntentDriftReviewID generates a deterministic drift review ID
func newIntentDriftReviewID(prefixSlug string) string {
	return fmt.Sprintf("intent-drift-review-%s-%s", time.Now().Format("2006-01-02"), prefixSlug)
}

// newReviewPacketID generates a deterministic review packet ID
func newReviewPacketID(prefixSlug string) string {
	return fmt.Sprintf("plan-intent-review-packet-%s-%s", time.Now().Format("2006-01-02"), prefixSlug)
}

// generateSlug generates a URL-safe slug from a UUID-like string
func generateSlug() string {
	// Use current timestamp and random component for slug
	// In production, this could use uuid package
	return fmt.Sprintf("%s-%s",
		strings.ReplaceAll(time.Now().UTC().Format("150405"), ":", ""),
		strings.ToLower(hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano()))))[:IDSlugLength])
}

// sanitizeSlug makes a slug safe by keeping only lowercase letters, digits, and hyphens
func sanitizeSlug(s string) string {
	// Keep only alphanumeric and hyphen
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	return strings.Trim(reg.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// ValidatePlanArtifactPath checks if a plan artifact path exists and is readable
func ValidatePlanArtifactPath(path string) error {
	if path == "" {
		return fmt.Errorf("artifact path is required")
	}

	// Use filepath.Clean to normalize path
	cleanPath := filepath.Clean(path)

	// Check for absolute paths (should be relative for safety)
	if filepath.IsAbs(cleanPath) {
		return fmt.Errorf("absolute paths are not allowed")
	}

	// Check for path traversal
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	// Check file exists
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("artifact file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access artifact: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("artifact path is a directory, not a file")
	}

	return nil
}

// NullString converts a string to sql.NullString
func NullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// SafeNullString converts a possibly empty string to sql.NullString, treating empty as NULL
func SafeNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// blockAttempt returns a PlanAttemptResult with the given blocker code and message
func blockAttempt(code PlanAttemptBlockerCode, msg string) (*PlanAttemptResult, error) {
	return &PlanAttemptResult{
		OK:          false,
		BlockerCode: code,
		Message:     msg,
	}, nil
}

// validateDriftReviewMode validates the drift review mode, defaulting to "disabled"
func validateDriftReviewMode(mode string) string {
	switch mode {
	case DriftReviewModeManual, DriftReviewModeAutomatic, DriftReviewModeExternal:
		return mode
	default:
		return DriftReviewModeDisabled
	}
}

// validateModelTier validates the model tier, defaulting to "standard"
func validateModelTier(tier string) string {
	switch tier {
	case ModelTierEconomy, ModelTierHighAssurance, ModelTierAutoEscalate:
		return tier
	default:
		return ModelTierStandard
	}
}

// canonicalRawPlanJSON canonicalizes raw Plan JSON and computes its hash
func canonicalRawPlanJSON(raw json.RawMessage) ([]byte, string, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", fmt.Errorf("unmarshal raw plan JSON: %w", err)
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical plan JSON: %w", err)
	}
	hash := sha256Bytes(canonical)
	return canonical, hash, nil
}

// validatePlanJSONArtifact validates a JSON plan artifact reference
func validatePlanJSONArtifact(ref PlanArtifactRef, expectedHash string) error {
	if err := validateAttemptArtifactRef(ref, ".json"); err != nil {
		return err
	}
	if ref.ArtifactKind != "planner-pass-plan-json" {
		return fmt.Errorf("invalid artifact kind: expected planner-pass-plan-json, got %s", ref.ArtifactKind)
	}
	if ref.SHA256 != expectedHash {
		return fmt.Errorf("artifact SHA256 mismatch: expected %s, got %s", expectedHash, ref.SHA256)
	}
	return nil
}

// validateOptionalMarkdownArtifact validates an optional Markdown artifact reference
func validateOptionalMarkdownArtifact(ref *PlanArtifactRef) error {
	if ref == nil {
		return nil
	}
	if err := validateAttemptArtifactRef(*ref, ".md"); err != nil {
		return err
	}
	if ref.ArtifactKind != "planner-pass-plan-markdown" {
		return fmt.Errorf("invalid artifact kind: expected planner-pass-plan-markdown, got %s", ref.ArtifactKind)
	}
	return nil
}

// constraintsJSON serializes constraints to JSON
func constraintsJSON(constraints []string) (string, error) {
	if constraints == nil {
		return "[]", nil
	}
	b, err := json.Marshal(constraints)
	if err != nil {
		return "", fmt.Errorf("marshal constraints: %w", err)
	}
	return string(b), nil
}
