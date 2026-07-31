package submissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"relay/internal/planningartifacts"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const MaxDiagnostics = 50

type ValidationInput struct {
	DisplayName    string
	CanonicalBytes []byte
}

type ValidationResult struct {
	OK          bool
	Status      string
	Kind        string
	SHA256      string
	Diagnostics []speccompiler.Diagnostic
	Notices     []speccompiler.Diagnostic
}

// Service validates canonical artifact bytes. Legacy Plan submission is
// retired; this service performs no workflow writes.
type Service struct {
	store *workflowstore.Store
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) ValidateArtifact(_ context.Context, input ValidationInput) (ValidationResult, error) {
	return validateArtifact(input), nil
}

func validateArtifact(input ValidationInput) ValidationResult {
	kind := "unknown"
	diagnostics := []speccompiler.Diagnostic{}
	notices := []speccompiler.Diagnostic{}
	ok := false
	if identity, filenameDiagnostics := speccompiler.ParseFilename(input.DisplayName); len(filenameDiagnostics) == 0 {
		kind = string(identity.Kind)
		switch identity.Kind {
		case speccompiler.ArtifactRequirements, speccompiler.ArtifactSharedDesign, speccompiler.ArtifactTicketDesignBrief:
			diagnostics = planningartifacts.Validate(identity.Kind, input.CanonicalBytes)
			ok = len(diagnostics) == 0
		default:
			compiled := speccompiler.Compile(input.DisplayName, input.CanonicalBytes)
			diagnostics = compiled.Errors
			notices = compiled.Notices
			ok = len(diagnostics) == 0
		}
	} else {
		diagnostics = filenameDiagnostics
	}
	result := ValidationResult{
		OK:          ok,
		Status:      "valid",
		Kind:        kind,
		SHA256:      SHA256(input.CanonicalBytes),
		Diagnostics: boundedDiagnostics(diagnostics),
		Notices:     boundedDiagnostics(notices),
	}
	if !result.OK {
		result.Status = "blocked"
	}
	return result
}

func ArtifactKind(displayName string) string {
	identity, diagnostics := speccompiler.ParseFilename(displayName)
	if len(diagnostics) != 0 {
		return "unknown"
	}
	return string(identity.Kind)
}

func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func boundedDiagnostics(values []speccompiler.Diagnostic) []speccompiler.Diagnostic {
	if len(values) > MaxDiagnostics {
		values = values[:MaxDiagnostics]
	}
	result := make([]speccompiler.Diagnostic, len(values))
	copy(result, values)
	return result
}
