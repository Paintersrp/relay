package audits

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/executor"
)

const WorkflowPackageAuditPacketSchemaVersion = "3.0"

var ErrWorkflowPackageAuditPacketV3Invalid = errors.New(
	"workflow package audit packet v3 is invalid",
)

// WorkflowPackageAuditPacketV3 is the canonical package audit packet v3 model.
// Field order defines JSON property order.
type WorkflowPackageAuditPacketV3 struct {
	SchemaVersion            string                                         `json:"schema_version"`
	Run                      WorkflowPackageAuditRunV3                      `json:"run"`
	Repository               WorkflowPackageAuditRepositoryV3               `json:"repository"`
	Authority                WorkflowPackageAuditAuthorityV3                `json:"authority"`
	DeterministicApplication WorkflowPackageAuditDeterministicApplicationV3 `json:"deterministic_application"`
	Execution                WorkflowPackageAuditExecutionV3                `json:"execution"`
	ChangedFiles             []WorkflowPackageAuditChangedFileV3            `json:"changed_files"`
	RelevantSourcePaths      []string                                       `json:"relevant_source_paths"`
	Validation               []WorkflowPackageAuditValidationResultV3       `json:"validation"`
	Artifacts                []WorkflowPackageAuditEmbeddedArtifactV3       `json:"artifacts"`
}

type WorkflowPackageAuditRunV3 struct {
	RunID      int64  `json:"run_id"`
	UserIntent string `json:"user_intent"`
}

type WorkflowPackageAuditRepositoryV3 struct {
	RepoTarget    string `json:"repo_target"`
	Branch        string `json:"branch"`
	BaseCommit    string `json:"base_commit"`
	AuditedCommit string `json:"audited_commit"`
}

type WorkflowPackageAuditAuthorityV3 struct {
	DeliveryTicket          WorkflowPackageAuditEmbeddedArtifactV3   `json:"delivery_ticket"`
	Requirements            []WorkflowPackageAuditEmbeddedArtifactV3 `json:"requirements"`
	SharedDesign            []WorkflowPackageAuditEmbeddedArtifactV3 `json:"shared_design"`
	TicketDesignBrief       WorkflowPackageAuditEmbeddedArtifactV3   `json:"ticket_design_brief"`
	DeterministicOperations *WorkflowPackageAuditEmbeddedArtifactV3  `json:"deterministic_operations,omitempty"`
	ExecutionAssignment     WorkflowPackageAuditArtifactReferenceV3  `json:"execution_assignment"`
	EffectiveExecutorBrief  WorkflowPackageAuditArtifactReferenceV3  `json:"effective_executor_brief"`
}

type WorkflowPackageAuditArtifactReferenceV3 struct {
	ArtifactReference string `json:"artifact_reference"`
	SHA256            string `json:"sha256"`
}

type WorkflowPackageAuditDeterministicApplicationV3 struct {
	Outcome  string                                  `json:"outcome"`
	Coverage *string                                 `json:"coverage,omitempty"`
	Evidence WorkflowPackageAuditArtifactReferenceV3 `json:"evidence"`
}

type WorkflowPackageAuditExecutionV3 struct {
	AdaptiveAttemptDispatched bool   `json:"adaptive_attempt_dispatched"`
	Status                    string `json:"status"`
	CommittedSHA              string `json:"committed_sha"`
	CompletionSummary         string `json:"completion_summary"`
}

type WorkflowPackageAuditChangedFileV3 struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previous_path,omitempty"`
	ChangeType   string `json:"change_type"`
	Additions    int64  `json:"additions"`
	Deletions    int64  `json:"deletions"`
}

type WorkflowPackageAuditValidationResultV3 struct {
	Command       string `json:"command"`
	Expected      string `json:"expected"`
	Status        string `json:"status"`
	ConciseResult string `json:"concise_result"`
}

// WorkflowPackageAuditEmbeddedArtifactV3 embeds an exact artifact. Content is
// json.RawMessage so JSON-authored artifacts render as JSON values and
// Markdown/text artifacts render as JSON strings. SHA-256 always covers the
// exact original artifact bytes, not the JSON encoding of content.
type WorkflowPackageAuditEmbeddedArtifactV3 struct {
	Filename string          `json:"filename"`
	SHA256   string          `json:"sha256"`
	Content  json.RawMessage `json:"content"`
}

type WorkflowPackageAuditPacketV3Input struct {
	Evidence WorkflowPackageExecutionEvidence

	UserIntent string

	DeliveryTicket WorkflowPackageAuditEmbeddedArtifactV3Input

	Commit WorkflowPackageAuditCommitInputV3

	Execution WorkflowPackageAuditExecutionInputV3

	RelevantSourcePaths []string
	Validation          []WorkflowPackageAuditValidationResultV3
	Artifacts           []WorkflowPackageAuditEmbeddedArtifactV3Input
}

type WorkflowPackageAuditEmbeddedArtifactV3Input struct {
	Filename  string
	MediaType string
	SHA256    string
	Bytes     []byte
}

type WorkflowPackageAuditCommitInputV3 struct {
	RepoTarget    string
	Branch        string
	BaseCommit    string
	AuditedCommit string
	ChangedFiles  []WorkflowPackageAuditChangedFileV3
}

type WorkflowPackageAuditExecutionInputV3 struct {
	AdaptiveAttemptDispatched bool
	Status                    string
	CommittedSHA              string
	CompletionSummary         string
}

func buildWorkflowPackageAuditPacketV3(
	input WorkflowPackageAuditPacketV3Input,
) (WorkflowPackageAuditPacketV3, []byte, error) {
	if err := validateWorkflowPackageAuditPacketV3Input(input); err != nil {
		return WorkflowPackageAuditPacketV3{}, nil, fmt.Errorf("%w: %v", ErrWorkflowPackageAuditPacketV3Invalid, err)
	}

	packet, err := constructWorkflowPackageAuditPacketV3(input)
	if err != nil {
		return WorkflowPackageAuditPacketV3{}, nil, fmt.Errorf("%w: %v", ErrWorkflowPackageAuditPacketV3Invalid, err)
	}

	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return WorkflowPackageAuditPacketV3{}, nil, fmt.Errorf("%w: marshal packet: %v", ErrWorkflowPackageAuditPacketV3Invalid, err)
	}
	data = append(data, '\n')

	if err := validateWorkflowPackageAuditPacketV3Bytes(data); err != nil {
		return WorkflowPackageAuditPacketV3{}, nil, fmt.Errorf("%w: %v", ErrWorkflowPackageAuditPacketV3Invalid, err)
	}

	return packet, append([]byte(nil), data...), nil
}

func constructWorkflowPackageAuditPacketV3(input WorkflowPackageAuditPacketV3Input) (WorkflowPackageAuditPacketV3, error) {
	run := input.Evidence.Run

	requirements := make([]WorkflowPackageAuditEmbeddedArtifactV3, 0, len(input.Evidence.Authority.AuthorityLayers))
	sharedDesign := make([]WorkflowPackageAuditEmbeddedArtifactV3, 0, len(input.Evidence.Authority.AuthorityLayers))
	for _, layer := range input.Evidence.Authority.AuthorityLayers {
		artifact, err := embeddedArtifactFromLayer(layer)
		if err != nil {
			return WorkflowPackageAuditPacketV3{}, fmt.Errorf("authority layer %d: %v", layer.Sequence, err)
		}
		switch layer.Kind {
		case workflowPackageAuthorityLayerRequirements:
			requirements = append(requirements, artifact)
		case workflowPackageAuthorityLayerSharedDesign:
			sharedDesign = append(sharedDesign, artifact)
		default:
			return WorkflowPackageAuditPacketV3{}, fmt.Errorf("authority layer %d: unknown kind %q", layer.Sequence, layer.Kind)
		}
	}

	brief, err := embeddedArtifactFromApprovedDocument(input.Evidence.Authority.TicketDesignBrief)
	if err != nil {
		return WorkflowPackageAuditPacketV3{}, fmt.Errorf("ticket design brief: %v", err)
	}

	var deterministicOps *WorkflowPackageAuditEmbeddedArtifactV3
	if input.Evidence.Authority.DeterministicOperations != nil {
		ops, err := embeddedArtifactFromApprovedDocument(input.Evidence.Authority.DeterministicOperations.ApprovedDocument)
		if err != nil {
			return WorkflowPackageAuditPacketV3{}, fmt.Errorf("deterministic operations: %v", err)
		}
		if err := validateWorkflowPackageDeterministicOperationsArtifactV3(ops, input.Evidence.Authority.DeterministicOperations.Coverage); err != nil {
			return WorkflowPackageAuditPacketV3{}, fmt.Errorf("deterministic operations: %v", err)
		}
		deterministicOps = &ops
	}

	delivery, err := embeddedArtifactFromInput(input.DeliveryTicket)
	if err != nil {
		return WorkflowPackageAuditPacketV3{}, fmt.Errorf("delivery ticket: %v", err)
	}
	if !strings.EqualFold(input.DeliveryTicket.MediaType, "application/json") {
		return WorkflowPackageAuditPacketV3{}, fmt.Errorf("delivery ticket media type must be application/json")
	}
	if !json.Valid(input.DeliveryTicket.Bytes) {
		return WorkflowPackageAuditPacketV3{}, fmt.Errorf("delivery ticket content must be valid JSON")
	}

	outcome, coverage, err := deterministicApplicationV3(input.Evidence.Deterministic)
	if err != nil {
		return WorkflowPackageAuditPacketV3{}, fmt.Errorf("deterministic application: %v", err)
	}

	artifacts := make([]WorkflowPackageAuditEmbeddedArtifactV3, 0, len(input.Artifacts))
	for _, artifactInput := range input.Artifacts {
		artifact, err := embeddedArtifactFromInput(artifactInput)
		if err != nil {
			return WorkflowPackageAuditPacketV3{}, fmt.Errorf("artifact %q: %v", artifactInput.Filename, err)
		}
		artifacts = append(artifacts, artifact)
	}

	changedFiles := make([]WorkflowPackageAuditChangedFileV3, len(input.Commit.ChangedFiles))
	copy(changedFiles, input.Commit.ChangedFiles)

	validation := make([]WorkflowPackageAuditValidationResultV3, len(input.Validation))
	copy(validation, input.Validation)

	relevant := make([]string, len(input.RelevantSourcePaths))
	copy(relevant, input.RelevantSourcePaths)

	packet := WorkflowPackageAuditPacketV3{
		SchemaVersion: WorkflowPackageAuditPacketSchemaVersion,
		Run: WorkflowPackageAuditRunV3{
			RunID:      run.ID,
			UserIntent: input.UserIntent,
		},
		Repository: WorkflowPackageAuditRepositoryV3{
			RepoTarget:    input.Commit.RepoTarget,
			Branch:        input.Commit.Branch,
			BaseCommit:    input.Commit.BaseCommit,
			AuditedCommit: input.Commit.AuditedCommit,
		},
		Authority: WorkflowPackageAuditAuthorityV3{
			DeliveryTicket:          delivery,
			Requirements:            requirements,
			SharedDesign:            sharedDesign,
			TicketDesignBrief:       brief,
			DeterministicOperations: deterministicOps,
			ExecutionAssignment: WorkflowPackageAuditArtifactReferenceV3{
				ArtifactReference: input.Evidence.Assignment.Artifact.ArtifactID,
				SHA256:            input.Evidence.Assignment.Artifact.SHA256,
			},
			EffectiveExecutorBrief: WorkflowPackageAuditArtifactReferenceV3{
				ArtifactReference: input.Evidence.EffectiveBrief.Artifact.ArtifactID,
				SHA256:            input.Evidence.EffectiveBrief.Artifact.SHA256,
			},
		},
		DeterministicApplication: WorkflowPackageAuditDeterministicApplicationV3{
			Outcome:  outcome,
			Coverage: coverage,
			Evidence: WorkflowPackageAuditArtifactReferenceV3{
				ArtifactReference: input.Evidence.Deterministic.Artifact.ArtifactID,
				SHA256:            input.Evidence.Deterministic.Artifact.SHA256,
			},
		},
		Execution: WorkflowPackageAuditExecutionV3{
			AdaptiveAttemptDispatched: input.Execution.AdaptiveAttemptDispatched,
			Status:                    input.Execution.Status,
			CommittedSHA:              input.Execution.CommittedSHA,
			CompletionSummary:         input.Execution.CompletionSummary,
		},
		ChangedFiles:        changedFiles,
		RelevantSourcePaths: relevant,
		Validation:          validation,
		Artifacts:           artifacts,
	}
	return packet, nil
}

const (
	workflowPackageAuthorityLayerRequirements = "requirements"
	workflowPackageAuthorityLayerSharedDesign = "shared_design"
)

func embeddedArtifactFromLayer(layer workflowpackages.ApprovedAuthorityLayer) (WorkflowPackageAuditEmbeddedArtifactV3, error) {
	filename := filepath.Base(layer.RelativePath)
	if filename == "" || !workflowPackageSafeFilename(filename) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("filename %q is unsafe", filename)
	}
	if layer.SHA256 == "" || !workflowPackageValidSHA256(layer.SHA256) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("SHA-256 is malformed")
	}
	if layer.Bytes == nil {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("content bytes are required")
	}
	if layer.SHA256 != workflowPackageSHA256(layer.Bytes) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("SHA-256 does not match content bytes")
	}
	var content json.RawMessage
	if strings.EqualFold(layer.MediaType, "application/json") && json.Valid(layer.Bytes) {
		content = json.RawMessage(layer.Bytes)
	} else {
		if !utf8.Valid(layer.Bytes) {
			return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("content is not valid UTF-8")
		}
		encoded, err := json.Marshal(string(layer.Bytes))
		if err != nil {
			return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("encode text content: %v", err)
		}
		content = json.RawMessage(encoded)
	}
	return WorkflowPackageAuditEmbeddedArtifactV3{
		Filename: filename,
		SHA256:   layer.SHA256,
		Content:  content,
	}, nil
}

func embeddedArtifactFromApprovedDocument(doc workflowpackages.ApprovedDocument) (WorkflowPackageAuditEmbeddedArtifactV3, error) {
	artifactInput := WorkflowPackageAuditEmbeddedArtifactV3Input{
		Filename:  filepath.Base(doc.RelativePath),
		MediaType: doc.MediaType,
		SHA256:    doc.SHA256,
		Bytes:     doc.Bytes,
	}
	return embeddedArtifactFromInput(artifactInput)
}

func embeddedArtifactFromInput(input WorkflowPackageAuditEmbeddedArtifactV3Input) (WorkflowPackageAuditEmbeddedArtifactV3, error) {
	if input.Filename == "" {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("filename is required")
	}
	if !workflowPackageSafeFilename(input.Filename) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("filename %q is unsafe", input.Filename)
	}
	if input.SHA256 == "" {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("SHA-256 is required")
	}
	if !workflowPackageValidSHA256(input.SHA256) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("SHA-256 %q is malformed", input.SHA256)
	}
	if input.Bytes == nil {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("content bytes are required")
	}
	if input.SHA256 != workflowPackageSHA256(input.Bytes) {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("SHA-256 does not match content bytes")
	}
	if input.MediaType == "" {
		return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("media type is required")
	}
	if strings.EqualFold(input.MediaType, "application/json") {
		if !json.Valid(input.Bytes) {
			return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("content is not valid JSON")
		}
		return WorkflowPackageAuditEmbeddedArtifactV3{
			Filename: input.Filename,
			SHA256:   input.SHA256,
			Content:  json.RawMessage(input.Bytes),
		}, nil
	}
	if strings.HasPrefix(input.MediaType, "text/") {
		if !utf8.Valid(input.Bytes) {
			return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("content is not valid UTF-8")
		}
		encoded, err := json.Marshal(string(input.Bytes))
		if err != nil {
			return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("encode text content: %v", err)
		}
		return WorkflowPackageAuditEmbeddedArtifactV3{
			Filename: input.Filename,
			SHA256:   input.SHA256,
			Content:  json.RawMessage(encoded),
		}, nil
	}
	return WorkflowPackageAuditEmbeddedArtifactV3{}, fmt.Errorf("unsupported media type %q", input.MediaType)
}

func deterministicApplicationV3(outcome executor.DeterministicOutcomeResult) (string, *string, error) {
	switch outcome.Outcome.Outcome.Status {
	case string(executor.DeterministicPreflightNotPresent):
		return "not_present", nil, nil
	case string(executor.DeterministicPreflightFailed):
		coverage := outcome.Outcome.Outcome.Coverage
		if coverage != "partial" && coverage != "complete" {
			return "", nil, fmt.Errorf("preflight_failed coverage must be partial or complete")
		}
		return "preflight_failed", &coverage, nil
	case "applied":
		coverage := outcome.Outcome.Outcome.Coverage
		if coverage != "partial" && coverage != "complete" {
			return "", nil, fmt.Errorf("applied coverage must be partial or complete")
		}
		return "applied", &coverage, nil
	default:
		return "", nil, fmt.Errorf("unsupported outcome status %q", outcome.Outcome.Outcome.Status)
	}
}

func validateWorkflowPackageAuditPacketV3Input(input WorkflowPackageAuditPacketV3Input) error {
	run := input.Evidence.Run
	authority := input.Evidence.Authority

	if strings.TrimSpace(input.UserIntent) == "" {
		return fmt.Errorf("user_intent is required")
	}
	if input.UserIntent != strings.TrimSpace(input.UserIntent) {
		return fmt.Errorf("user_intent must not have leading or trailing whitespace")
	}
	if run.ID <= 0 {
		return fmt.Errorf("run id must be a positive integer")
	}

	if input.Commit.RepoTarget != run.RepoTarget || input.Commit.RepoTarget != authority.Run.RepoTarget {
		return fmt.Errorf("commit repo_target must match evidence")
	}
	if input.Commit.Branch != run.Branch || input.Commit.Branch != authority.Run.Branch {
		return fmt.Errorf("commit branch must match evidence")
	}
	if input.Commit.BaseCommit != run.BaseCommit || input.Commit.BaseCommit != authority.Run.BaseCommit {
		return fmt.Errorf("commit base_commit must match evidence")
	}
	if !workflowPackageValidSHA40(input.Commit.AuditedCommit) {
		return fmt.Errorf("audited_commit must be a lowercase 40-character SHA")
	}
	if input.Commit.AuditedCommit == input.Commit.BaseCommit {
		return fmt.Errorf("audited_commit must differ from base_commit")
	}

	if input.DeliveryTicket.Filename == "" {
		return fmt.Errorf("delivery ticket filename is required")
	}
	if !workflowPackageSafeFilename(input.DeliveryTicket.Filename) {
		return fmt.Errorf("delivery ticket filename %q is unsafe", input.DeliveryTicket.Filename)
	}
	if input.DeliveryTicket.SHA256 == "" {
		return fmt.Errorf("delivery ticket SHA-256 is required")
	}
	if input.DeliveryTicket.Bytes == nil {
		return fmt.Errorf("delivery ticket bytes are required")
	}
	if !strings.EqualFold(input.DeliveryTicket.MediaType, "application/json") {
		return fmt.Errorf("delivery ticket media type must be application/json")
	}
	if !json.Valid(input.DeliveryTicket.Bytes) {
		return fmt.Errorf("delivery ticket content must be valid JSON")
	}
	if input.DeliveryTicket.SHA256 != workflowPackageSHA256(input.DeliveryTicket.Bytes) {
		return fmt.Errorf("delivery ticket SHA-256 does not match bytes")
	}

	if len(authority.AuthorityLayers) == 0 {
		return fmt.Errorf("at least one authority layer is required")
	}
	var previousSequence int64
	seenSequence := make(map[int64]struct{}, len(authority.AuthorityLayers))
	hasRequirements := false
	for index, layer := range authority.AuthorityLayers {
		if layer.Sequence < 1 {
			return fmt.Errorf("authority layer %d sequence must be positive", index)
		}
		if layer.Sequence <= previousSequence {
			return fmt.Errorf("authority layer %d sequence must be increasing", index)
		}
		previousSequence = layer.Sequence
		if _, duplicate := seenSequence[layer.Sequence]; duplicate {
			return fmt.Errorf("authority layer %d duplicate sequence %d", index, layer.Sequence)
		}
		seenSequence[layer.Sequence] = struct{}{}
		if layer.Kind == "" {
			return fmt.Errorf("authority layer %d kind is required", index)
		}
		if layer.Kind != workflowPackageAuthorityLayerRequirements && layer.Kind != workflowPackageAuthorityLayerSharedDesign {
			return fmt.Errorf("authority layer %d unknown kind %q", index, layer.Kind)
		}
		if layer.Kind == workflowPackageAuthorityLayerRequirements {
			hasRequirements = true
		}
		if layer.RelativePath == "" {
			return fmt.Errorf("authority layer %d relative path is required", index)
		}
		if layer.SHA256 == "" {
			return fmt.Errorf("authority layer %d SHA-256 is required", index)
		}
		if !workflowPackageValidSHA256(layer.SHA256) {
			return fmt.Errorf("authority layer %d SHA-256 is malformed", index)
		}
		if layer.Bytes == nil {
			return fmt.Errorf("authority layer %d bytes are required", index)
		}
		if layer.SHA256 != workflowPackageSHA256(layer.Bytes) {
			return fmt.Errorf("authority layer %d SHA-256 does not match bytes", index)
		}
		if layer.MediaType == "" {
			return fmt.Errorf("authority layer %d media type is required", index)
		}
		if filepath.Base(layer.RelativePath) == "" {
			return fmt.Errorf("authority layer %d filename is empty", index)
		}
		if !workflowPackageSafeFilename(filepath.Base(layer.RelativePath)) {
			return fmt.Errorf("authority layer %d filename is unsafe", index)
		}
	}
	if !hasRequirements {
		return fmt.Errorf("at least one requirements authority layer is required")
	}

	if authority.TicketDesignBrief.SHA256 == "" {
		return fmt.Errorf("ticket design brief SHA-256 is required")
	}
	if !workflowPackageValidSHA256(authority.TicketDesignBrief.SHA256) {
		return fmt.Errorf("ticket design brief SHA-256 is malformed")
	}
	if authority.TicketDesignBrief.Bytes == nil {
		return fmt.Errorf("ticket design brief bytes are required")
	}
	if authority.TicketDesignBrief.SHA256 != workflowPackageSHA256(authority.TicketDesignBrief.Bytes) {
		return fmt.Errorf("ticket design brief SHA-256 does not match bytes")
	}
	if authority.TicketDesignBrief.MediaType == "" {
		return fmt.Errorf("ticket design brief media type is required")
	}
	if !strings.HasPrefix(authority.TicketDesignBrief.MediaType, "text/") {
		return fmt.Errorf("ticket design brief media type must be text/*")
	}
	if !utf8.Valid(authority.TicketDesignBrief.Bytes) {
		return fmt.Errorf("ticket design brief content must be valid UTF-8")
	}
	wantBriefName := fmt.Sprintf("%s.ticket-%s.r%d.design-brief.md", authority.Workspace.FeatureSlug, authority.Ticket.TicketID, authority.TicketRevision.RevisionNumber)
	if filepath.Base(authority.TicketDesignBrief.RelativePath) != wantBriefName {
		return fmt.Errorf("ticket design brief filename must be %q", wantBriefName)
	}

	if authority.DeterministicOperations != nil {
		ops := authority.DeterministicOperations
		if ops.SHA256 == "" {
			return fmt.Errorf("deterministic operations SHA-256 is required")
		}
		if !workflowPackageValidSHA256(ops.SHA256) {
			return fmt.Errorf("deterministic operations SHA-256 is malformed")
		}
		if ops.Bytes == nil {
			return fmt.Errorf("deterministic operations bytes are required")
		}
		if ops.SHA256 != workflowPackageSHA256(ops.Bytes) {
			return fmt.Errorf("deterministic operations SHA-256 does not match bytes")
		}
		if ops.Coverage != "partial" && ops.Coverage != "complete" {
			return fmt.Errorf("deterministic operations coverage must be partial or complete")
		}
		if ops.Coverage != input.Evidence.Assignment.Assignment.DeterministicOperations.Coverage {
			return fmt.Errorf("deterministic operations coverage must match assignment")
		}
		if ops.Coverage != input.Evidence.Deterministic.Outcome.Outcome.Coverage {
			return fmt.Errorf("deterministic operations coverage must match outcome")
		}
		wantOpsName := fmt.Sprintf("%s.ticket-%s.r%d.deterministic-operations.json", authority.Workspace.FeatureSlug, authority.Ticket.TicketID, authority.TicketRevision.RevisionNumber)
		if filepath.Base(ops.RelativePath) != wantOpsName {
			return fmt.Errorf("deterministic operations filename must be %q", wantOpsName)
		}
		if ops.MediaType != "application/json" {
			return fmt.Errorf("deterministic operations media type must be application/json")
		}
		if !json.Valid(ops.Bytes) {
			return fmt.Errorf("deterministic operations content must be valid JSON")
		}
	} else {
		if input.Evidence.Assignment.Assignment.DeterministicOperations.Presence != "absent" {
			return fmt.Errorf("deterministic operations absent but assignment reports present")
		}
		if input.Evidence.Deterministic.Outcome.Outcome.Coverage != "" {
			return fmt.Errorf("deterministic operations absent but outcome has coverage")
		}
	}

	if input.Evidence.Assignment.Artifact.ArtifactID == "" {
		return fmt.Errorf("execution assignment artifact reference is required")
	}
	if !workflowPackageValidSHA256(input.Evidence.Assignment.Artifact.SHA256) {
		return fmt.Errorf("execution assignment SHA-256 is malformed")
	}
	if input.Evidence.EffectiveBrief.Artifact == nil {
		return fmt.Errorf("effective executor brief artifact is required")
	}
	if input.Evidence.EffectiveBrief.Artifact.ArtifactID == "" {
		return fmt.Errorf("effective executor brief artifact reference is required")
	}
	if !workflowPackageValidSHA256(input.Evidence.EffectiveBrief.Artifact.SHA256) {
		return fmt.Errorf("effective executor brief SHA-256 is malformed")
	}
	if input.Evidence.Deterministic.Artifact.ArtifactID == "" {
		return fmt.Errorf("deterministic outcome artifact reference is required")
	}
	if !workflowPackageValidSHA256(input.Evidence.Deterministic.Artifact.SHA256) {
		return fmt.Errorf("deterministic outcome SHA-256 is malformed")
	}

	if input.Execution.Status == "" {
		return fmt.Errorf("execution status is required")
	}
	if strings.TrimSpace(input.Execution.Status) != input.Execution.Status || input.Execution.Status == "" {
		return fmt.Errorf("execution status is invalid")
	}
	if !workflowPackageValidSHA40(input.Execution.CommittedSHA) {
		return fmt.Errorf("execution committed_sha must be a lowercase 40-character SHA")
	}
	if input.Execution.CommittedSHA != input.Commit.AuditedCommit {
		return fmt.Errorf("execution committed_sha must equal repository audited_commit")
	}
	if input.Execution.CompletionSummary == "" {
		return fmt.Errorf("execution completion summary is required")
	}
	if strings.TrimSpace(input.Execution.CompletionSummary) == "" {
		return fmt.Errorf("execution completion summary must contain non-whitespace text")
	}

	if len(input.Commit.ChangedFiles) == 0 {
		return fmt.Errorf("at least one changed file is required")
	}
	seenPath := make(map[string]struct{}, len(input.Commit.ChangedFiles))
	for _, file := range input.Commit.ChangedFiles {
		if file.Path == "" {
			return fmt.Errorf("changed file path is required")
		}
		if !workflowPackageSafePath(file.Path) {
			return fmt.Errorf("changed file path %q is unsafe", file.Path)
		}
		if _, duplicate := seenPath[file.Path]; duplicate {
			return fmt.Errorf("duplicate changed file path %q", file.Path)
		}
		seenPath[file.Path] = struct{}{}
		if file.PreviousPath != "" && file.ChangeType != "renamed" && file.ChangeType != "copied" {
			return fmt.Errorf("previous_path is only allowed for renamed or copied files")
		}
		if (file.ChangeType == "renamed" || file.ChangeType == "copied") && file.PreviousPath == "" {
			return fmt.Errorf("previous_path is required for %s files", file.ChangeType)
		}
		switch file.ChangeType {
		case "added", "modified", "deleted", "renamed", "copied", "type_changed":
		default:
			return fmt.Errorf("invalid change type %q", file.ChangeType)
		}
		if file.Additions < 0 || file.Deletions < 0 {
			return fmt.Errorf("changed file additions and deletions must be nonnegative")
		}
	}

	if len(input.RelevantSourcePaths) == 0 {
		return fmt.Errorf("at least one relevant source path is required")
	}
	seenRelevant := make(map[string]struct{}, len(input.RelevantSourcePaths))
	for _, path := range input.RelevantSourcePaths {
		if path == "" {
			return fmt.Errorf("relevant source path is required")
		}
		if !workflowPackageSafePath(path) {
			return fmt.Errorf("relevant source path %q is unsafe", path)
		}
		if _, duplicate := seenRelevant[path]; duplicate {
			return fmt.Errorf("duplicate relevant source path %q", path)
		}
		seenRelevant[path] = struct{}{}
	}

	if len(input.Validation) == 0 {
		return fmt.Errorf("at least one validation entry is required")
	}
	for index, validation := range input.Validation {
		if validation.Command == "" {
			return fmt.Errorf("validation entry %d command is required", index)
		}
		if strings.TrimSpace(validation.Command) != validation.Command || validation.Command == "" {
			return fmt.Errorf("validation entry %d command is invalid", index)
		}
		if validation.Expected == "" {
			return fmt.Errorf("validation entry %d expected is required", index)
		}
		if strings.TrimSpace(validation.Expected) != validation.Expected || validation.Expected == "" {
			return fmt.Errorf("validation entry %d expected is invalid", index)
		}
		if validation.Status == "" {
			return fmt.Errorf("validation entry %d status is required", index)
		}
		switch validation.Status {
		case "passed", "failed", "not_run":
		default:
			return fmt.Errorf("validation entry %d invalid status %q", index, validation.Status)
		}
		if validation.ConciseResult == "" {
			return fmt.Errorf("validation entry %d concise_result is required", index)
		}
		if strings.TrimSpace(validation.ConciseResult) == "" {
			return fmt.Errorf("validation entry %d concise_result must contain non-whitespace text", index)
		}
	}

	if len(input.Artifacts) == 0 {
		return fmt.Errorf("at least one artifact is required")
	}
	seenArtifact := make(map[string]struct{}, len(input.Artifacts))
	for index, artifact := range input.Artifacts {
		if artifact.Filename == "" {
			return fmt.Errorf("artifact %d filename is required", index)
		}
		if !workflowPackageSafeFilename(artifact.Filename) {
			return fmt.Errorf("artifact %d filename %q is unsafe", index, artifact.Filename)
		}
		if _, duplicate := seenArtifact[artifact.Filename]; duplicate {
			return fmt.Errorf("duplicate artifact filename %q", artifact.Filename)
		}
		seenArtifact[artifact.Filename] = struct{}{}
		if artifact.SHA256 == "" {
			return fmt.Errorf("artifact %d SHA-256 is required", index)
		}
		if !workflowPackageValidSHA256(artifact.SHA256) {
			return fmt.Errorf("artifact %d SHA-256 is malformed", index)
		}
		if artifact.Bytes == nil {
			return fmt.Errorf("artifact %d bytes are required", index)
		}
		if artifact.SHA256 != workflowPackageSHA256(artifact.Bytes) {
			return fmt.Errorf("artifact %d SHA-256 does not match bytes", index)
		}
		if artifact.MediaType == "" {
			return fmt.Errorf("artifact %d media type is required", index)
		}
		if strings.EqualFold(artifact.MediaType, "application/json") {
			if !json.Valid(artifact.Bytes) {
				return fmt.Errorf("artifact %d JSON content is invalid", index)
			}
		} else if strings.HasPrefix(artifact.MediaType, "text/") {
			if !utf8.Valid(artifact.Bytes) {
				return fmt.Errorf("artifact %d text content is invalid UTF-8", index)
			}
		} else {
			return fmt.Errorf("artifact %d unsupported media type %q", index, artifact.MediaType)
		}
	}

	mode, adaptive, err := deriveWorkflowPackageEffectiveMode(input.Evidence.Deterministic.Outcome.Outcome)
	if err != nil {
		return fmt.Errorf("effective mode: %v", err)
	}
	if input.Evidence.EffectiveBrief.Mode != mode {
		return fmt.Errorf("effective brief mode %q does not match deterministic outcome", input.Evidence.EffectiveBrief.Mode)
	}
	if input.Evidence.EffectiveBrief.AdaptiveDispatchRequired != adaptive {
		return fmt.Errorf("effective brief adaptive dispatch does not match mode")
	}
	if input.Execution.AdaptiveAttemptDispatched != adaptive {
		return fmt.Errorf("execution adaptive_attempt_dispatched %v does not match effective mode %q", input.Execution.AdaptiveAttemptDispatched, mode)
	}
	if mode == executor.EffectiveExecutorBriefDeterministicComplete && input.Execution.AdaptiveAttemptDispatched {
		return fmt.Errorf("deterministic-complete requires adaptive_attempt_dispatched false")
	}

	if input.Evidence.Assignment.Assignment.DeterministicOperations.Presence == "absent" && input.Evidence.Deterministic.Outcome.Outcome.Coverage != "" {
		return fmt.Errorf("deterministic operations absent but outcome coverage is nonblank")
	}
	if input.Evidence.Assignment.Assignment.DeterministicOperations.Presence == "present" && input.Evidence.Deterministic.Outcome.Outcome.Coverage == "" {
		return fmt.Errorf("deterministic operations present but outcome coverage is blank")
	}

	return nil
}

func validateWorkflowPackageDeterministicOperationsArtifactV3(artifact WorkflowPackageAuditEmbeddedArtifactV3, coverage string) error {
	if len(artifact.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	if !json.Valid(artifact.Content) {
		return fmt.Errorf("content must be valid JSON")
	}
	var document struct {
		Coverage string `json:"coverage"`
	}
	if err := json.Unmarshal(artifact.Content, &document); err != nil {
		return fmt.Errorf("parse deterministic operations: %v", err)
	}
	if document.Coverage != coverage {
		return fmt.Errorf("deterministic operations coverage %q does not match expected %q", document.Coverage, coverage)
	}
	if document.Coverage != "partial" && document.Coverage != "complete" {
		return fmt.Errorf("deterministic operations coverage must be partial or complete")
	}
	return nil
}

func validateWorkflowPackageAuditPacketV3(packet WorkflowPackageAuditPacketV3) error {
	if packet.SchemaVersion != WorkflowPackageAuditPacketSchemaVersion {
		return fmt.Errorf("schema version must be %q", WorkflowPackageAuditPacketSchemaVersion)
	}
	if packet.Run.RunID <= 0 {
		return fmt.Errorf("run_id must be a positive integer")
	}
	if strings.TrimSpace(packet.Run.UserIntent) == "" {
		return fmt.Errorf("run user_intent is required")
	}
	if strings.TrimSpace(packet.Run.UserIntent) != packet.Run.UserIntent {
		return fmt.Errorf("run user_intent must not have leading or trailing whitespace")
	}

	if strings.TrimSpace(packet.Repository.RepoTarget) == "" {
		return fmt.Errorf("repository repo_target is required")
	}
	if strings.TrimSpace(packet.Repository.RepoTarget) != packet.Repository.RepoTarget {
		return fmt.Errorf("repository repo_target must not have leading or trailing whitespace")
	}
	if strings.TrimSpace(packet.Repository.Branch) == "" {
		return fmt.Errorf("repository branch is required")
	}
	if strings.TrimSpace(packet.Repository.Branch) != packet.Repository.Branch {
		return fmt.Errorf("repository branch must not have leading or trailing whitespace")
	}
	if !workflowPackageValidSHA40(packet.Repository.BaseCommit) {
		return fmt.Errorf("repository base_commit must be a lowercase 40-character SHA")
	}
	if !workflowPackageValidSHA40(packet.Repository.AuditedCommit) {
		return fmt.Errorf("repository audited_commit must be a lowercase 40-character SHA")
	}
	if packet.Repository.AuditedCommit == packet.Repository.BaseCommit {
		return fmt.Errorf("repository audited_commit must differ from base_commit")
	}

	if err := jsonAuthoredContentArtifact(packet.Authority.DeliveryTicket, "delivery_ticket"); err != nil {
		return fmt.Errorf("authority delivery_ticket: %v", err)
	}
	if len(packet.Authority.Requirements) == 0 {
		return fmt.Errorf("authority requirements must have at least one layer")
	}
	for index, artifact := range packet.Authority.Requirements {
		if err := jsonAuthoredContentArtifact(artifact, "requirements"); err != nil {
			return fmt.Errorf("authority requirements %d: %v", index, err)
		}
	}
	for index, artifact := range packet.Authority.SharedDesign {
		if err := jsonAuthoredContentArtifact(artifact, "shared_design"); err != nil {
			return fmt.Errorf("authority shared_design %d: %v", index, err)
		}
	}
	if err := textAuthoredContentArtifact(packet.Authority.TicketDesignBrief, "ticket_design_brief"); err != nil {
		return fmt.Errorf("authority ticket_design_brief: %v", err)
	}
	var deterministicOpsCoverage string
	if packet.Authority.DeterministicOperations != nil {
		if err := jsonAuthoredContentArtifact(*packet.Authority.DeterministicOperations, "deterministic_operations"); err != nil {
			return fmt.Errorf("authority deterministic_operations: %v", err)
		}
		var document struct {
			Coverage string `json:"coverage"`
		}
		if err := json.Unmarshal(packet.Authority.DeterministicOperations.Content, &document); err != nil {
			return fmt.Errorf("authority deterministic_operations: parse coverage: %v", err)
		}
		if document.Coverage != "partial" && document.Coverage != "complete" {
			return fmt.Errorf("authority deterministic_operations coverage must be partial or complete")
		}
		deterministicOpsCoverage = document.Coverage
	}
	if packet.Authority.ExecutionAssignment.ArtifactReference == "" {
		return fmt.Errorf("authority execution_assignment artifact_reference is required")
	}
	if !workflowPackageValidSHA256(packet.Authority.ExecutionAssignment.SHA256) {
		return fmt.Errorf("authority execution_assignment SHA-256 is malformed")
	}
	if packet.Authority.EffectiveExecutorBrief.ArtifactReference == "" {
		return fmt.Errorf("authority effective_executor_brief artifact_reference is required")
	}
	if !workflowPackageValidSHA256(packet.Authority.EffectiveExecutorBrief.SHA256) {
		return fmt.Errorf("authority effective_executor_brief SHA-256 is malformed")
	}

	if packet.DeterministicApplication.Evidence.ArtifactReference == "" {
		return fmt.Errorf("deterministic_application evidence artifact_reference is required")
	}
	if !workflowPackageValidSHA256(packet.DeterministicApplication.Evidence.SHA256) {
		return fmt.Errorf("deterministic_application evidence SHA-256 is malformed")
	}
	switch packet.DeterministicApplication.Outcome {
	case "not_present":
		if packet.DeterministicApplication.Coverage != nil {
			return fmt.Errorf("not_present outcome must not have coverage")
		}
		if packet.Authority.DeterministicOperations != nil {
			return fmt.Errorf("not_present outcome must not have deterministic_operations")
		}
		if packet.Execution.AdaptiveAttemptDispatched {
			// allowed
		}
	case "preflight_failed":
		if packet.DeterministicApplication.Coverage == nil {
			return fmt.Errorf("preflight_failed outcome requires coverage")
		}
		if *packet.DeterministicApplication.Coverage != "partial" && *packet.DeterministicApplication.Coverage != "complete" {
			return fmt.Errorf("preflight_failed coverage must be partial or complete")
		}
		if packet.Authority.DeterministicOperations == nil {
			return fmt.Errorf("preflight_failed outcome requires deterministic_operations")
		}
		if deterministicOpsCoverage != *packet.DeterministicApplication.Coverage {
			return fmt.Errorf("deterministic_operations coverage %q does not match deterministic_application coverage %q", deterministicOpsCoverage, *packet.DeterministicApplication.Coverage)
		}
		if !packet.Execution.AdaptiveAttemptDispatched {
			return fmt.Errorf("preflight_failed outcome requires adaptive_attempt_dispatched true")
		}
	case "applied":
		if packet.DeterministicApplication.Coverage == nil {
			return fmt.Errorf("applied outcome requires coverage")
		}
		if *packet.DeterministicApplication.Coverage != "partial" && *packet.DeterministicApplication.Coverage != "complete" {
			return fmt.Errorf("applied coverage must be partial or complete")
		}
		if packet.Authority.DeterministicOperations == nil {
			return fmt.Errorf("applied outcome requires deterministic_operations")
		}
		if deterministicOpsCoverage != *packet.DeterministicApplication.Coverage {
			return fmt.Errorf("deterministic_operations coverage %q does not match deterministic_application coverage %q", deterministicOpsCoverage, *packet.DeterministicApplication.Coverage)
		}
		if *packet.DeterministicApplication.Coverage == "complete" {
			if packet.Execution.AdaptiveAttemptDispatched {
				return fmt.Errorf("applied complete outcome requires adaptive_attempt_dispatched false")
			}
		} else {
			if !packet.Execution.AdaptiveAttemptDispatched {
				return fmt.Errorf("applied partial outcome requires adaptive_attempt_dispatched true")
			}
		}
	default:
		return fmt.Errorf("deterministic_application outcome %q is unsupported", packet.DeterministicApplication.Outcome)
	}

	if packet.Execution.CommittedSHA != packet.Repository.AuditedCommit {
		return fmt.Errorf("execution committed_sha must equal repository audited_commit")
	}
	if strings.TrimSpace(packet.Execution.Status) == "" {
		return fmt.Errorf("execution status is required")
	}
	if strings.TrimSpace(packet.Execution.Status) != packet.Execution.Status {
		return fmt.Errorf("execution status must not have leading or trailing whitespace")
	}
	if strings.TrimSpace(packet.Execution.CompletionSummary) == "" {
		return fmt.Errorf("execution completion_summary must contain non-whitespace text")
	}

	if len(packet.ChangedFiles) == 0 {
		return fmt.Errorf("at least one changed file is required")
	}
	seenPath := make(map[string]struct{}, len(packet.ChangedFiles))
	for _, file := range packet.ChangedFiles {
		if file.Path == "" {
			return fmt.Errorf("changed file path is required")
		}
		if !workflowPackageSafePath(file.Path) {
			return fmt.Errorf("changed file path %q is unsafe", file.Path)
		}
		if _, duplicate := seenPath[file.Path]; duplicate {
			return fmt.Errorf("duplicate changed file path %q", file.Path)
		}
		seenPath[file.Path] = struct{}{}
		if file.PreviousPath != "" && file.ChangeType != "renamed" && file.ChangeType != "copied" {
			return fmt.Errorf("previous_path is only allowed for renamed or copied files")
		}
		if (file.ChangeType == "renamed" || file.ChangeType == "copied") && file.PreviousPath == "" {
			return fmt.Errorf("previous_path is required for %s files", file.ChangeType)
		}
		if file.PreviousPath != "" && !workflowPackageSafePath(file.PreviousPath) {
			return fmt.Errorf("changed file previous_path %q is unsafe", file.PreviousPath)
		}
		switch file.ChangeType {
		case "added", "modified", "deleted", "renamed", "copied", "type_changed":
		default:
			return fmt.Errorf("invalid change type %q", file.ChangeType)
		}
		if file.Additions < 0 || file.Deletions < 0 {
			return fmt.Errorf("changed file additions and deletions must be nonnegative")
		}
	}

	if len(packet.RelevantSourcePaths) == 0 {
		return fmt.Errorf("at least one relevant source path is required")
	}
	seenRelevant := make(map[string]struct{}, len(packet.RelevantSourcePaths))
	for _, path := range packet.RelevantSourcePaths {
		if path == "" {
			return fmt.Errorf("relevant source path is required")
		}
		if !workflowPackageSafePath(path) {
			return fmt.Errorf("relevant source path %q is unsafe", path)
		}
		if _, duplicate := seenRelevant[path]; duplicate {
			return fmt.Errorf("duplicate relevant source path %q", path)
		}
		seenRelevant[path] = struct{}{}
	}

	if len(packet.Validation) == 0 {
		return fmt.Errorf("at least one validation entry is required")
	}
	for _, validation := range packet.Validation {
		if strings.TrimSpace(validation.Command) == "" {
			return fmt.Errorf("validation entry command is required")
		}
		if strings.TrimSpace(validation.Command) != validation.Command {
			return fmt.Errorf("validation entry command must not have leading or trailing whitespace")
		}
		if strings.TrimSpace(validation.Expected) == "" {
			return fmt.Errorf("validation entry expected is required")
		}
		if strings.TrimSpace(validation.Expected) != validation.Expected {
			return fmt.Errorf("validation entry expected must not have leading or trailing whitespace")
		}
		if strings.TrimSpace(validation.Status) == "" {
			return fmt.Errorf("validation entry status is required")
		}
		switch validation.Status {
		case "passed", "failed", "not_run":
		default:
			return fmt.Errorf("invalid validation status %q", validation.Status)
		}
		if strings.TrimSpace(validation.ConciseResult) == "" {
			return fmt.Errorf("validation entry concise_result must contain non-whitespace text")
		}
	}

	if len(packet.Artifacts) == 0 {
		return fmt.Errorf("at least one artifact is required")
	}
	seenArtifact := make(map[string]struct{}, len(packet.Artifacts))
	for _, artifact := range packet.Artifacts {
		if artifact.Filename == "" {
			return fmt.Errorf("artifact filename is required")
		}
		if !workflowPackageSafeFilename(artifact.Filename) {
			return fmt.Errorf("artifact filename %q is unsafe", artifact.Filename)
		}
		if _, duplicate := seenArtifact[artifact.Filename]; duplicate {
			return fmt.Errorf("duplicate artifact filename %q", artifact.Filename)
		}
		seenArtifact[artifact.Filename] = struct{}{}
		if err := jsonOrTextAuthoredContentArtifact(artifact, "artifact"); err != nil {
			return fmt.Errorf("artifact %q: %v", artifact.Filename, err)
		}
	}

	return nil
}

// firstNonSpaceByte returns the first byte in raw that is not a JSON whitespace
// character, or 0 if the slice contains only whitespace.
func firstNonSpaceByte(raw []byte) byte {
	for _, b := range raw {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b
		}
	}
	return 0
}

// isJSONStringContent reports whether raw is a JSON string literal. Callers
// must already have confirmed that raw is valid JSON.
func isJSONStringContent(raw json.RawMessage) bool {
	return firstNonSpaceByte(raw) == '"'
}

// isJSONNullContent reports whether raw is the JSON null literal. Callers
// must already have confirmed that raw is valid JSON.
func isJSONNullContent(raw json.RawMessage) bool {
	return firstNonSpaceByte(raw) == 'n'
}

// jsonAuthoredContentArtifact validates an embedded artifact whose content is
// authored as JSON.
//
// Requirements:
//   - filename and SHA-256 are well-formed;
//   - content is nonempty, valid JSON;
//   - content is not a JSON string representation of text.
//
// Digest distinction:
//   - builder validation binds the declared digest to exact source JSON bytes;
//   - decoded packet validation verifies canonical structure but cannot
//     reconstruct discarded source JSON whitespace from a JSON value, so it does
//     not recompute or compare the SHA-256 for JSON content.
func jsonAuthoredContentArtifact(artifact WorkflowPackageAuditEmbeddedArtifactV3, context string) error {
	if err := validateWorkflowPackageAuditArtifactBasics(artifact, context); err != nil {
		return err
	}
	if len(artifact.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	if !json.Valid(artifact.Content) {
		return fmt.Errorf("content must be valid JSON")
	}
	if isJSONStringContent(artifact.Content) {
		return fmt.Errorf("JSON-authored content must not be a JSON string representation of text")
	}
	return nil
}

// textAuthoredContentArtifact validates an embedded artifact whose content is
// authored as text and carried as a JSON string.
//
// Requirements:
//   - filename and SHA-256 are well-formed;
//   - content is a valid JSON string;
//   - decoded bytes are valid UTF-8;
//   - the declared digest matches the exact decoded UTF-8 string bytes.
func textAuthoredContentArtifact(artifact WorkflowPackageAuditEmbeddedArtifactV3, context string) error {
	if err := validateWorkflowPackageAuditArtifactBasics(artifact, context); err != nil {
		return err
	}
	if len(artifact.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	if !json.Valid(artifact.Content) {
		return fmt.Errorf("content must be valid JSON")
	}
	if !isJSONStringContent(artifact.Content) {
		return fmt.Errorf("text-authored content must be a JSON string")
	}
	var text string
	if err := json.Unmarshal(artifact.Content, &text); err != nil {
		return fmt.Errorf("content must decode as a JSON string: %v", err)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("decoded text content must be valid UTF-8")
	}
	if artifact.SHA256 != workflowPackageSHA256([]byte(text)) {
		return fmt.Errorf("SHA-256 does not match decoded text content")
	}
	return nil
}

// jsonOrTextAuthoredContentArtifact validates a top-level embedded artifact
// that may carry either JSON-authored content or text-authored content. The
// packet does not carry a media type, so the content shape is used to decide:
// a JSON string literal is treated as text; any other valid JSON value is
// treated as JSON-authored content.
//
// For text content, the digest is recomputed from the decoded UTF-8 string
// bytes. For JSON content, decoded packet validation verifies canonical
// structure but cannot reconstruct discarded source JSON whitespace from a JSON
// value, so the SHA-256 is not recomputed or compared here.
func jsonOrTextAuthoredContentArtifact(artifact WorkflowPackageAuditEmbeddedArtifactV3, context string) error {
	if err := validateWorkflowPackageAuditArtifactBasics(artifact, context); err != nil {
		return err
	}
	if len(artifact.Content) == 0 {
		return fmt.Errorf("content is required")
	}
	if !json.Valid(artifact.Content) {
		return fmt.Errorf("content must be valid JSON")
	}
	if isJSONNullContent(artifact.Content) {
		return fmt.Errorf("content must not be null")
	}
	if isJSONStringContent(artifact.Content) {
		var text string
		if err := json.Unmarshal(artifact.Content, &text); err != nil {
			return fmt.Errorf("content must decode as a JSON string: %v", err)
		}
		if !utf8.ValidString(text) {
			return fmt.Errorf("decoded text content must be valid UTF-8")
		}
		if artifact.SHA256 != workflowPackageSHA256([]byte(text)) {
			return fmt.Errorf("SHA-256 does not match decoded text content")
		}
		return nil
	}
	// JSON-authored content: decoded packet validation verifies canonical
	// structure but cannot reconstruct discarded source JSON whitespace, so the
	// digest is not recomputed here.
	return nil
}

// validateWorkflowPackageAuditArtifactBasics enforces common filename and
// SHA-256 rules for all embedded artifacts.
func validateWorkflowPackageAuditArtifactBasics(artifact WorkflowPackageAuditEmbeddedArtifactV3, context string) error {
	if artifact.Filename == "" {
		return fmt.Errorf("filename is required")
	}
	if !workflowPackageSafeFilename(artifact.Filename) {
		return fmt.Errorf("filename %q is unsafe", artifact.Filename)
	}
	if artifact.SHA256 == "" {
		return fmt.Errorf("SHA-256 is required")
	}
	if !workflowPackageValidSHA256(artifact.SHA256) {
		return fmt.Errorf("SHA-256 is malformed")
	}
	return nil
}

func validateWorkflowPackageAuditPacketV3Bytes(data []byte) error {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return fmt.Errorf("packet must end with exactly one trailing newline")
	}
	if len(data) > 1 && data[len(data)-2] == '\n' {
		return fmt.Errorf("packet must not have multiple trailing newlines")
	}
	var packet WorkflowPackageAuditPacketV3
	if err := json.Unmarshal(data, &packet); err != nil {
		return fmt.Errorf("decode packet: %v", err)
	}
	if err := validateWorkflowPackageAuditPacketV3(packet); err != nil {
		return fmt.Errorf("validate decoded packet: %v", err)
	}
	canonical, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return fmt.Errorf("re-marshal packet: %v", err)
	}
	canonical = append(canonical, '\n')
	if !workflowPackageByteEqual(data, canonical) {
		return fmt.Errorf("re-marshaled bytes differ from canonical bytes")
	}
	return nil
}

func workflowPackageByteEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func workflowPackageSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func workflowPackageValidSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func workflowPackageValidSHA40(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func workflowPackageSafeFilename(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return false
	}
	if name != strings.TrimSpace(name) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func workflowPackageSafePath(path string) bool {
	if path == "" {
		return false
	}
	if path != strings.TrimSpace(path) {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		return false
	}
	if strings.HasPrefix(path, "//") {
		return false
	}
	if strings.ContainsRune(path, '\\') {
		return false
	}
	if strings.ContainsRune(path, '\r') || strings.ContainsRune(path, '\n') {
		return false
	}
	if len(path) >= 2 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' {
		return false
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
