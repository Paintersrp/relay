package audits

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/planningartifacts"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

func assemblePackageAuditInput(
	evidence WorkflowPackageExecutionEvidence,
	commit workflowrepos.AuditCommitEvidence,
) (WorkflowPackageAuditPacketInput, error) {
	intent := evidence.Authority.TicketRevision.Goal
	if intent == "" {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("user intent is required")
	}
	if !utf8.ValidString(intent) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("user intent is not valid UTF-8")
	}
	if intent != strings.TrimSpace(intent) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("user intent must not have leading or trailing whitespace")
	}

	dt := evidence.Authority.DeliveryTicket
	if dt.DisplayName == "" {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket display name is required")
	}
	if dt.RelativePath == "" || filepath.Base(dt.RelativePath) != dt.DisplayName {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket display name %q must equal base name of relative path %q", dt.DisplayName, dt.RelativePath)
	}
	if !strings.EqualFold(dt.MediaType, "application/json") {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket media type must be application/json")
	}
	if len(dt.Bytes) == 0 || !json.Valid(dt.Bytes) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket content must be valid JSON")
	}
	if int64(len(dt.Bytes)) != dt.SizeBytes {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket byte size mismatch")
	}
	if dt.SHA256 == "" || dt.SHA256 != workflowPackageSHA256(dt.Bytes) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket SHA-256 does not match bytes")
	}
	if !workflowPackageValidSHA40(dt.ObjectOID) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("delivery ticket object OID must be a lowercase 40-character Git object ID")
	}

	deliveryTicketInput := WorkflowPackageAuditEmbeddedArtifactInput{
		Filename:  dt.DisplayName,
		MediaType: dt.MediaType,
		SHA256:    dt.SHA256,
		Bytes:     append([]byte(nil), dt.Bytes...),
	}

	if commit.Branch == "" || commit.Branch != evidence.Run.Branch {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit branch %q must match evidence run branch %q", commit.Branch, evidence.Run.Branch)
	}
	if commit.BaseCommit == "" || commit.BaseCommit != evidence.Run.BaseCommit {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit base_commit %q must match evidence run base_commit %q", commit.BaseCommit, evidence.Run.BaseCommit)
	}
	if !workflowPackageValidSHA40(commit.AuditedCommit) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit audited_commit must be a lowercase 40-character SHA")
	}
	if commit.AuditedCommit == evidence.Run.BaseCommit {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit audited_commit must differ from base_commit")
	}
	if evidence.Run.RepoTarget == "" {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("run repo_target is required")
	}

	if len(commit.FileChanges) == 0 {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("at least one typed file change is required")
	}

	changedFilesSet := make(map[string]bool, len(commit.ChangedFiles))
	for _, p := range commit.ChangedFiles {
		changedFilesSet[p] = true
	}

	fileChangesPathsSet := make(map[string]bool)
	seenResultingPaths := make(map[string]bool)
	changedFiles := make([]WorkflowPackageAuditChangedFile, len(commit.FileChanges))

	for i, fc := range commit.FileChanges {
		if fc.Path == "" {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d path is empty", i)
		}
		if !workflowPackageSafePath(fc.Path) {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d path %q is unsafe", i, fc.Path)
		}
		if seenResultingPaths[fc.Path] {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("duplicate resulting path %q in file changes", fc.Path)
		}
		seenResultingPaths[fc.Path] = true

		if !changedFilesSet[fc.Path] {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("resulting path %q does not appear in commit.ChangedFiles", fc.Path)
		}
		fileChangesPathsSet[fc.Path] = true

		switch fc.ChangeType {
		case "added", "modified", "deleted", "type_changed":
			if fc.PreviousPath != "" {
				return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d previous_path not allowed for change_type %q", i, fc.ChangeType)
			}
		case "renamed", "copied":
			if fc.PreviousPath == "" {
				return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d previous_path required for change_type %q", i, fc.ChangeType)
			}
			if !workflowPackageSafePath(fc.PreviousPath) {
				return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d previous_path %q is unsafe", i, fc.PreviousPath)
			}
			if fc.PreviousPath == fc.Path {
				return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d previous_path and path are identical: %q", i, fc.Path)
			}
			fileChangesPathsSet[fc.PreviousPath] = true
		default:
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d unsupported change_type %q", i, fc.ChangeType)
		}

		if fc.Additions < 0 || fc.Deletions < 0 {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("file change %d additions and deletions must be non-negative", i)
		}

		changedFiles[i] = WorkflowPackageAuditChangedFile{
			Path:         fc.Path,
			PreviousPath: fc.PreviousPath,
			ChangeType:   fc.ChangeType,
			Additions:    fc.Additions,
			Deletions:    fc.Deletions,
		}
	}

	for _, p := range commit.ChangedFiles {
		if !fileChangesPathsSet[p] {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("changed_files entry %q does not correspond to any path or previous_path in file_changes", p)
		}
	}

	relevantSet := make(map[string]bool)
	for _, fc := range commit.FileChanges {
		if fc.Path != "" {
			relevantSet[fc.Path] = true
		}
		if fc.PreviousPath != "" {
			relevantSet[fc.PreviousPath] = true
		}
	}
	relevantPaths := make([]string, 0, len(relevantSet))
	for p := range relevantSet {
		relevantPaths = append(relevantPaths, p)
	}
	sort.Strings(relevantPaths)

	mode, adaptive, err := deriveWorkflowPackageEffectiveMode(evidence.Deterministic.Outcome.Outcome)
	if err != nil {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("derive effective mode: %w", err)
	}
	if evidence.EffectiveBrief.Mode != mode {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("effective brief mode %q does not match deterministic outcome mode %q", evidence.EffectiveBrief.Mode, mode)
	}
	if evidence.EffectiveBrief.AdaptiveDispatchRequired != adaptive {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("effective brief adaptive dispatch requirement does not match mode")
	}

	var completionSummary string
	if adaptive {
		if evidence.Attempt == nil {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("adaptive mode %q requires execution attempt evidence", mode)
		}
		completionSummary = "The authorized adaptive Executor attempt completed successfully."
	} else {
		if mode != executor.EffectiveExecutorBriefDeterministicComplete {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("unsupported non-adaptive mode %q", mode)
		}
		if evidence.Attempt != nil {
			return WorkflowPackageAuditPacketInput{}, fmt.Errorf("deterministic-complete mode requires no execution attempt evidence")
		}
		completionSummary = "Deterministic Operations completely fulfilled the approved Brief; no adaptive Executor attempt was dispatched."
	}

	executionInput := WorkflowPackageAuditExecutionInput{
		Status:            "completed",
		CommittedSHA:      commit.AuditedCommit,
		CompletionSummary: completionSummary,
	}

	if len(commit.Diff) == 0 {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit diff is empty")
	}
	if len(commit.Diff) > workflowrepos.MaxAuditDiffBytes {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit diff exceeds max size")
	}
	if !utf8.ValidString(commit.Diff) {
		return WorkflowPackageAuditPacketInput{}, fmt.Errorf("commit diff is not valid UTF-8")
	}

	diffBytes := []byte(commit.Diff)
	diffSHA := workflowPackageSHA256(diffBytes)

	artifacts := []WorkflowPackageAuditEmbeddedArtifactInput{
		{
			Filename:  "committed.diff",
			MediaType: "text/x-diff; charset=utf-8",
			SHA256:    diffSHA,
			Bytes:     append([]byte(nil), diffBytes...),
		},
	}

	commitInput := WorkflowPackageAuditCommitInput{
		RepoTarget:    evidence.Run.RepoTarget,
		Branch:        commit.Branch,
		BaseCommit:    commit.BaseCommit,
		AuditedCommit: commit.AuditedCommit,
		ChangedFiles:  changedFiles,
	}

	return WorkflowPackageAuditPacketInput{
		Evidence:            copyWorkflowPackageExecutionEvidence(evidence),
		UserIntent:          intent,
		DeliveryTicket:      deliveryTicketInput,
		Commit:              commitInput,
		Execution:           executionInput,
		RelevantSourcePaths: relevantPaths,
		Artifacts:           artifacts,
	}, nil
}

func copyWorkflowPackageExecutionEvidence(src WorkflowPackageExecutionEvidence) WorkflowPackageExecutionEvidence {
	dst := src

	if src.Validation != nil {
		dst.Validation = make([]WorkflowPackageAuditValidationResult, len(src.Validation))
		copy(dst.Validation, src.Validation)
	}

	if src.Authority.AuthorityLayers != nil {
		dst.Authority.AuthorityLayers = make([]workflowpackages.ApprovedAuthorityLayer, len(src.Authority.AuthorityLayers))
		for i, layer := range src.Authority.AuthorityLayers {
			layerCopy := layer
			if layer.Bytes != nil {
				layerCopy.Bytes = append([]byte(nil), layer.Bytes...)
			}
			dst.Authority.AuthorityLayers[i] = layerCopy
		}
	}

	if src.Authority.TicketMembers != nil {
		dst.Authority.TicketMembers = make([]workflowstore.DeliveryTicketRevisionMember, len(src.Authority.TicketMembers))
		copy(dst.Authority.TicketMembers, src.Authority.TicketMembers)
	}

	if src.Authority.TicketDependencies != nil {
		dst.Authority.TicketDependencies = make([]workflowstore.DeliveryTicketRevisionDependency, len(src.Authority.TicketDependencies))
		copy(dst.Authority.TicketDependencies, src.Authority.TicketDependencies)
	}

	if src.Authority.BriefProjection.ValidationCommands != nil {
		cmds := make([]planningartifacts.ValidationCommand, len(src.Authority.BriefProjection.ValidationCommands))
		copy(cmds, src.Authority.BriefProjection.ValidationCommands)
		dst.Authority.BriefProjection.ValidationCommands = cmds
	}

	if src.Authority.DeliveryTicket.Bytes != nil {
		dst.Authority.DeliveryTicket.Bytes = append([]byte(nil), src.Authority.DeliveryTicket.Bytes...)
	}

	if src.Authority.TicketDesignBrief.Bytes != nil {
		dst.Authority.TicketDesignBrief.Bytes = append([]byte(nil), src.Authority.TicketDesignBrief.Bytes...)
	}

	if src.Authority.DeterministicOperations != nil {
		ops := *src.Authority.DeterministicOperations
		if ops.Bytes != nil {
			ops.Bytes = append([]byte(nil), ops.Bytes...)
		}
		if ops.Document != nil {
			ops.Document = cloneDeterministicOperationsDocument(ops.Document)
		}
		dst.Authority.DeterministicOperations = &ops
	}

	if src.Assignment.Assignment.AuthorityLayers != nil {
		layers := make([]executor.ExecutionAssignmentLayer, len(src.Assignment.Assignment.AuthorityLayers))
		copy(layers, src.Assignment.Assignment.AuthorityLayers)
		dst.Assignment.Assignment.AuthorityLayers = layers
	}

	if src.Assignment.Assignment.ValidationCommands != nil {
		cmds := make([]executor.ExecutionAssignmentValidationCommand, len(src.Assignment.Assignment.ValidationCommands))
		copy(cmds, src.Assignment.Assignment.ValidationCommands)
		dst.Assignment.Assignment.ValidationCommands = cmds
	}

	if src.Assignment.Bytes != nil {
		dst.Assignment.Bytes = append([]byte(nil), src.Assignment.Bytes...)
	}

	if src.Deterministic.Bytes != nil {
		dst.Deterministic.Bytes = append([]byte(nil), src.Deterministic.Bytes...)
	}

	if src.Deterministic.Outcome.PreflightFailure != nil {
		pf := *src.Deterministic.Outcome.PreflightFailure
		dst.Deterministic.Outcome.PreflightFailure = &pf
	}

	if src.Deterministic.Outcome.Application != nil {
		app := *src.Deterministic.Outcome.Application
		if app.Operations != nil {
			ops := make([]executor.AppliedDeterministicOperationEvidence, len(src.Deterministic.Outcome.Application.Operations))
			copy(ops, src.Deterministic.Outcome.Application.Operations)
			app.Operations = ops
		}
		if app.ChangedPaths != nil {
			paths := make([]string, len(src.Deterministic.Outcome.Application.ChangedPaths))
			copy(paths, src.Deterministic.Outcome.Application.ChangedPaths)
			app.ChangedPaths = paths
		}
		dst.Deterministic.Outcome.Application = &app
	}

	if src.EffectiveBrief.Artifact != nil {
		art := *src.EffectiveBrief.Artifact
		dst.EffectiveBrief.Artifact = &art
	}

	if src.EffectiveBrief.Bytes != nil {
		dst.EffectiveBrief.Bytes = append([]byte(nil), src.EffectiveBrief.Bytes...)
	}

	if src.Attempt != nil {
		att := *src.Attempt
		if att.Bytes != nil {
			att.Bytes = append([]byte(nil), att.Bytes...)
		}
		dst.Attempt = &att
	}

	return dst
}

func cloneDeterministicOperationsDocument(doc *speccompiler.DeterministicOperationsDocument) *speccompiler.DeterministicOperationsDocument {
	if doc == nil {
		return nil
	}
	dst := *doc
	dst.SchemaVersion = cloneAny(doc.SchemaVersion)
	if doc.Operations != nil {
		dst.Operations = make([]speccompiler.DeterministicOperation, len(doc.Operations))
		for i, op := range doc.Operations {
			opCopy := op
			if op.Implementation.Changes != nil {
				changes := make([]speccompiler.DeterministicChange, len(op.Implementation.Changes))
				copy(changes, op.Implementation.Changes)
				opCopy.Implementation.Changes = changes
			}
			if op.Implementation.PreserveContent != nil {
				val := *op.Implementation.PreserveContent
				opCopy.Implementation.PreserveContent = &val
			}
			dst.Operations[i] = opCopy
		}
	}
	return &dst
}

func cloneAny(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return append([]byte(nil), val...)
	case []any:
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = cloneAny(elem)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, elem := range val {
			out[k] = cloneAny(elem)
		}
		return out
	default:
		return val
	}
}
