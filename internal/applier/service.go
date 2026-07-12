package applier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"relay/internal/speccompiler"
)

type Outcome string

const (
	OutcomeNotAttempted Outcome = "not_attempted"
	OutcomeCompleted    Outcome = "completed"
	OutcomePartial      Outcome = "partial"
	OutcomeBlocked      Outcome = "blocked"
)

type ActorKind string

const ActorKindApplier ActorKind = "applier"

type Disposition string

const (
	DispositionDeterministic Disposition = "deterministic"
	DispositionResidual      Disposition = "residual"
	DispositionBlocked       Disposition = "blocked"
)

type OperationOutcome string

const (
	OperationApplied      OperationOutcome = "applied"
	OperationResidual     OperationOutcome = "residual"
	OperationBlocked      OperationOutcome = "blocked"
	OperationUncertain    OperationOutcome = "uncertain"
	OperationNotAttempted OperationOutcome = "not_attempted"
)

type FailureClass string

const (
	FailureClassNone            FailureClass = ""
	FailureClassMaterialSpecGap FailureClass = "material_spec_gap"
	FailureClassUnsafeSource    FailureClass = "unsafe_source"
	FailureClassUnsupported     FailureClass = "unsupported"
	FailureClassDependency      FailureClass = "dependency_failure"
	FailureClassGroup           FailureClass = "group_failure"
	FailureClassEnvironment     FailureClass = "environment_failure"
)

type Input struct {
	WorkspaceRoot  string
	Projection     speccompiler.ExecutionProjection
	EvidenceWriter EvidenceWriter
}

type Result struct {
	Outcome              Outcome              `json:"outcome"`
	ActorKind            ActorKind            `json:"actor_kind"`
	Ledger               Ledger               `json:"ledger"`
	Partition            Partition            `json:"partition"`
	ImplementationResult ImplementationResult `json:"implementation_result"`
	FailurePacket        *FailurePacket       `json:"failure_packet,omitempty"`
	ChangedFiles         []string             `json:"changed_files"`
	Evidence             []EvidenceArtifact   `json:"evidence,omitempty"`
}

type Partition struct {
	DeterministicPathChains []string `json:"deterministic_path_chains"`
	ResidualPathChains      []string `json:"residual_path_chains"`
	DeterministicFileWork   []string `json:"deterministic_file_work"`
	ResidualFileWork        []string `json:"residual_file_work"`
	ProtectedPaths          []string `json:"protected_paths"`
	CoveredFileWork         []string `json:"covered_file_work"`
}

type Ledger struct {
	Entries []LedgerEntry `json:"entries"`
}

type LedgerEntry struct {
	PathChainRef   string                       `json:"path_chain_ref"`
	FileWorkRefs   []string                     `json:"file_work_refs"`
	SubstepRefs    []string                     `json:"substep_refs"`
	Operations     []string                     `json:"operations"`
	Paths          []string                     `json:"paths"`
	Dependencies   []string                     `json:"dependencies,omitempty"`
	Replay         speccompiler.ReplaySemantics `json:"replay"`
	Disposition    Disposition                  `json:"disposition"`
	Outcome        OperationOutcome             `json:"outcome"`
	Reason         string                       `json:"reason,omitempty"`
	Failure        FailureClass                 `json:"failure_class,omitempty"`
	ChangedPaths   []string                     `json:"changed_paths,omitempty"`
	ProtectedPaths []string                     `json:"protected_paths,omitempty"`
	UncertainPaths []string                     `json:"uncertain_paths,omitempty"`
}

type ImplementationResult struct {
	Outcome               Outcome      `json:"outcome"`
	ActorKind             ActorKind    `json:"actor_kind"`
	CompletedPathChains   []string     `json:"completed_path_chains"`
	ResidualPathChains    []string     `json:"residual_path_chains"`
	BlockedPathChains     []string     `json:"blocked_path_chains"`
	UncertainPathChains   []string     `json:"uncertain_path_chains"`
	UnattemptedPathChains []string     `json:"unattempted_path_chains"`
	CompletedFileWork     []string     `json:"completed_file_work"`
	ResidualFileWork      []string     `json:"residual_file_work"`
	BlockedFileWork       []string     `json:"blocked_file_work"`
	UncertainFileWork     []string     `json:"uncertain_file_work"`
	UnattemptedFileWork   []string     `json:"unattempted_file_work"`
	ChangedFiles          []string     `json:"changed_files"`
	ProtectedPaths        []string     `json:"protected_paths"`
	UncertainPaths        []string     `json:"uncertain_paths"`
	ModelExecutorRequired bool         `json:"model_executor_required"`
	FailureClass          FailureClass `json:"failure_class,omitempty"`
	FailureReason         string       `json:"failure_reason,omitempty"`
	EvidenceFailureReason string       `json:"evidence_failure_reason,omitempty"`
}

type FailurePacket struct {
	FailureClass          FailureClass `json:"failure_class"`
	Summary               string       `json:"summary"`
	BlockedPathChains     []string     `json:"blocked_path_chains"`
	BlockedFileWork       []string     `json:"blocked_file_work"`
	UncertainPathChains   []string     `json:"uncertain_path_chains,omitempty"`
	UncertainFileWork     []string     `json:"uncertain_file_work,omitempty"`
	UnattemptedPathChains []string     `json:"unattempted_path_chains,omitempty"`
	UnattemptedFileWork   []string     `json:"unattempted_file_work,omitempty"`
	ResidualPathChains    []string     `json:"residual_path_chains,omitempty"`
	ResidualFileWork      []string     `json:"residual_file_work,omitempty"`
	ChangedFiles          []string     `json:"changed_files,omitempty"`
	ProtectedPaths        []string     `json:"protected_paths,omitempty"`
	UncertainPaths        []string     `json:"uncertain_paths,omitempty"`
	EvidenceFailureReason string       `json:"evidence_failure_reason,omitempty"`
}

type EvidenceFile struct {
	Kind      string
	Filename  string
	MediaType string
	Data      []byte
}

type EvidenceArtifact struct {
	Kind      string `json:"kind"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type EvidenceWriter interface {
	WriteEvidence(context.Context, EvidenceFile) (EvidenceArtifact, error)
}

type MemoryEvidenceWriter struct {
	Files []EvidenceFile
}

func (w *MemoryEvidenceWriter) WriteEvidence(_ context.Context, file EvidenceFile) (EvidenceArtifact, error) {
	if w == nil {
		return EvidenceArtifact{}, errors.New("memory evidence writer is nil")
	}
	w.Files = append(w.Files, EvidenceFile{Kind: file.Kind, Filename: file.Filename, MediaType: file.MediaType, Data: append([]byte(nil), file.Data...)})
	digest := sha256.Sum256(file.Data)
	return EvidenceArtifact{Kind: file.Kind, Filename: file.Filename, MediaType: file.MediaType, SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(file.Data))}, nil
}

type mutationApplier func([]mutationAction) ([]string, error)

type Service struct {
	applyMutations mutationApplier
}

func NewService() Service {
	return Service{applyMutations: applyMutationActions}
}

func (s Service) Apply(ctx context.Context, input Input) (Result, error) {
	result := Result{
		Outcome:      OutcomeNotAttempted,
		ActorKind:    ActorKindApplier,
		Ledger:       Ledger{Entries: []LedgerEntry{}},
		ChangedFiles: []string{},
	}
	if len(input.Projection.FileWork) == 0 {
		result.ImplementationResult = summarizeImplementation(result, nil)
		return result, nil
	}
	root, blocker := workspaceRoot(input.WorkspaceRoot)
	if blocker != "" {
		return environmentBlockedResult(result, blocker), nil
	}
	if err := ctx.Err(); err != nil {
		return environmentBlockedResult(result, err.Error()), nil
	}

	plans := buildPathChainPlans(ctx, root, input.Projection)
	propagateDispositions(plans, input.Projection)
	partition, partitionFailure := buildPartition(plans, input.Projection)
	if partitionFailure != "" {
		covered := append([]string(nil), partition.CoveredFileWork...)
		partition = Partition{
			DeterministicPathChains: []string{},
			ResidualPathChains:      []string{},
			DeterministicFileWork:   []string{},
			ResidualFileWork:        []string{},
			ProtectedPaths:          []string{},
			CoveredFileWork:         covered,
		}
		markProjectionBlocked(plans, partitionFailure)
	}
	result.Partition = partition

	if hasBlockedPlan(plans) {
		result.Outcome = OutcomeBlocked
		return finalizeResult(ctx, input.EvidenceWriter, result, plans), nil
	}
	if len(partition.DeterministicPathChains) == 0 {
		result.Outcome = OutcomeNotAttempted
		return finalizeResult(ctx, input.EvidenceWriter, result, plans), nil
	}

	apply := s.applyMutations
	if apply == nil {
		apply = applyMutationActions
	}
	for index := range plans {
		plan := &plans[index]
		if plan.disposition != DispositionDeterministic {
			continue
		}
		if err := ctx.Err(); err != nil {
			plan.outcome = OperationBlocked
			plan.failure = FailureClassEnvironment
			plan.reason = err.Error()
			break
		}
		potentiallyAffected, err := apply(plan.actions)
		if err != nil {
			plan.outcome = OperationUncertain
			plan.failure = FailureClassEnvironment
			plan.reason = err.Error()
			plan.uncertainPaths = append([]string(nil), potentiallyAffected...)
			break
		}
		plan.outcome = OperationApplied
		plan.changedPaths = append([]string(nil), potentiallyAffected...)
		plan.protectedPaths = append([]string(nil), potentiallyAffected...)
	}

	result.ChangedFiles = changedFiles(plans)
	result.Partition.ProtectedPaths = append([]string(nil), result.ChangedFiles...)
	switch {
	case hasEnvironmentalStop(plans):
		result.Outcome = OutcomeBlocked
	case len(partition.ResidualPathChains) > 0:
		result.Outcome = OutcomePartial
	default:
		result.Outcome = OutcomeCompleted
	}
	return finalizeResult(ctx, input.EvidenceWriter, result, plans), nil
}

func finalizeResult(ctx context.Context, writer EvidenceWriter, result Result, plans []pathChainPlan) Result {
	result.Ledger.Entries = ledgerEntries(plans)
	result.ChangedFiles = changedFiles(plans)
	result.Partition.ProtectedPaths = append([]string(nil), result.ChangedFiles...)
	result.ImplementationResult = summarizeImplementation(result, plans)
	if result.Outcome == OutcomeBlocked {
		result.FailurePacket = failurePacket(result, plans)
	}
	return persistEvidence(ctx, writer, result)
}

func environmentBlockedResult(result Result, reason string) Result {
	result.Outcome = OutcomeBlocked
	result.Partition = Partition{
		DeterministicPathChains: []string{},
		ResidualPathChains:      []string{},
		DeterministicFileWork:   []string{},
		ResidualFileWork:        []string{},
		ProtectedPaths:          []string{},
		CoveredFileWork:         []string{},
	}
	result.FailurePacket = &FailurePacket{FailureClass: FailureClassEnvironment, Summary: reason, BlockedPathChains: []string{"workspace"}, BlockedFileWork: []string{}}
	result.ImplementationResult = ImplementationResult{
		Outcome:               OutcomeBlocked,
		ActorKind:             ActorKindApplier,
		CompletedPathChains:   []string{},
		ResidualPathChains:    []string{},
		BlockedPathChains:     []string{"workspace"},
		UncertainPathChains:   []string{},
		UnattemptedPathChains: []string{},
		CompletedFileWork:     []string{},
		ResidualFileWork:      []string{},
		BlockedFileWork:       []string{},
		UncertainFileWork:     []string{},
		UnattemptedFileWork:   []string{},
		ChangedFiles:          []string{},
		ProtectedPaths:        []string{},
		UncertainPaths:        []string{},
		ModelExecutorRequired: false,
		FailureClass:          FailureClassEnvironment,
		FailureReason:         reason,
	}
	return result
}

func summarizeImplementation(result Result, plans []pathChainPlan) ImplementationResult {
	implementation := ImplementationResult{
		Outcome:               result.Outcome,
		ActorKind:             ActorKindApplier,
		CompletedPathChains:   []string{},
		ResidualPathChains:    []string{},
		BlockedPathChains:     []string{},
		UncertainPathChains:   []string{},
		UnattemptedPathChains: []string{},
		CompletedFileWork:     []string{},
		ResidualFileWork:      []string{},
		BlockedFileWork:       []string{},
		UncertainFileWork:     []string{},
		UnattemptedFileWork:   []string{},
		ChangedFiles:          append([]string(nil), result.ChangedFiles...),
		ProtectedPaths:        append([]string(nil), result.Partition.ProtectedPaths...),
		UncertainPaths:        []string{},
	}
	for _, plan := range plans {
		switch plan.outcome {
		case OperationApplied:
			implementation.CompletedPathChains = append(implementation.CompletedPathChains, plan.ref)
			implementation.CompletedFileWork = append(implementation.CompletedFileWork, plan.fileWorkRefs...)
		case OperationResidual:
			implementation.ResidualPathChains = append(implementation.ResidualPathChains, plan.ref)
			implementation.ResidualFileWork = append(implementation.ResidualFileWork, plan.fileWorkRefs...)
		case OperationBlocked:
			implementation.BlockedPathChains = append(implementation.BlockedPathChains, plan.ref)
			implementation.BlockedFileWork = append(implementation.BlockedFileWork, plan.fileWorkRefs...)
		case OperationUncertain:
			implementation.UncertainPathChains = append(implementation.UncertainPathChains, plan.ref)
			implementation.UncertainFileWork = append(implementation.UncertainFileWork, plan.fileWorkRefs...)
			implementation.UncertainPaths = appendUnique(implementation.UncertainPaths, plan.uncertainPaths...)
		case OperationNotAttempted:
			if plan.disposition == DispositionResidual {
				implementation.ResidualPathChains = append(implementation.ResidualPathChains, plan.ref)
				implementation.ResidualFileWork = append(implementation.ResidualFileWork, plan.fileWorkRefs...)
			} else if plan.disposition == DispositionDeterministic {
				implementation.UnattemptedPathChains = append(implementation.UnattemptedPathChains, plan.ref)
				implementation.UnattemptedFileWork = append(implementation.UnattemptedFileWork, plan.fileWorkRefs...)
			}
		}
		if implementation.FailureClass == FailureClassNone && plan.failure != FailureClassNone {
			implementation.FailureClass = plan.failure
			implementation.FailureReason = plan.reason
		}
	}
	sort.Strings(implementation.UncertainPaths)
	implementation.ModelExecutorRequired = result.Outcome == OutcomeNotAttempted || result.Outcome == OutcomePartial
	return implementation
}

func failurePacket(result Result, plans []pathChainPlan) *FailurePacket {
	packet := &FailurePacket{
		BlockedPathChains:     []string{},
		BlockedFileWork:       []string{},
		UncertainPathChains:   []string{},
		UncertainFileWork:     []string{},
		UnattemptedPathChains: []string{},
		UnattemptedFileWork:   []string{},
		ResidualPathChains:    []string{},
		ResidualFileWork:      []string{},
		ChangedFiles:          append([]string(nil), result.ChangedFiles...),
		ProtectedPaths:        append([]string(nil), result.Partition.ProtectedPaths...),
		UncertainPaths:        []string{},
	}
	for _, plan := range plans {
		switch plan.outcome {
		case OperationBlocked:
			packet.BlockedPathChains = append(packet.BlockedPathChains, plan.ref)
			packet.BlockedFileWork = append(packet.BlockedFileWork, plan.fileWorkRefs...)
		case OperationUncertain:
			packet.UncertainPathChains = append(packet.UncertainPathChains, plan.ref)
			packet.UncertainFileWork = append(packet.UncertainFileWork, plan.fileWorkRefs...)
			packet.UncertainPaths = appendUnique(packet.UncertainPaths, plan.uncertainPaths...)
		case OperationNotAttempted:
			if plan.disposition == DispositionDeterministic {
				packet.UnattemptedPathChains = append(packet.UnattemptedPathChains, plan.ref)
				packet.UnattemptedFileWork = append(packet.UnattemptedFileWork, plan.fileWorkRefs...)
			} else if plan.disposition == DispositionResidual {
				packet.ResidualPathChains = append(packet.ResidualPathChains, plan.ref)
				packet.ResidualFileWork = append(packet.ResidualFileWork, plan.fileWorkRefs...)
			}
		case OperationResidual:
			packet.ResidualPathChains = append(packet.ResidualPathChains, plan.ref)
			packet.ResidualFileWork = append(packet.ResidualFileWork, plan.fileWorkRefs...)
		}
		if packet.FailureClass == FailureClassNone && plan.failure != FailureClassNone {
			packet.FailureClass = plan.failure
			packet.Summary = plan.reason
		}
	}
	sort.Strings(packet.UncertainPaths)
	if packet.Summary == "" {
		packet.FailureClass = FailureClassMaterialSpecGap
		packet.Summary = "deterministic projection is blocked"
	}
	return packet
}

func persistEvidence(ctx context.Context, writer EvidenceWriter, result Result) Result {
	if writer == nil || len(result.Ledger.Entries) == 0 {
		return result
	}
	type descriptor struct {
		kind     string
		filename string
		value    any
	}
	files := []descriptor{
		{kind: "applier_ledger_json", filename: "applier-ledger.json", value: result.Ledger},
		{kind: "applier_partition_json", filename: "applier-partition.json", value: result.Partition},
		{kind: "applier_result_json", filename: "applier-result.json", value: result.ImplementationResult},
		{kind: "applier_changed_files_json", filename: "applier-changed-files.json", value: map[string][]string{"changed_files": result.ChangedFiles}},
	}
	if result.FailurePacket != nil {
		files = append(files, descriptor{kind: "applier_failure_packet_json", filename: "applier-failure-packet.json", value: result.FailurePacket})
	}
	for _, file := range files {
		data, err := json.MarshalIndent(file.value, "", "  ")
		if err == nil {
			data = append(data, '\n')
			var artifact EvidenceArtifact
			artifact, err = writer.WriteEvidence(ctx, EvidenceFile{Kind: file.kind, Filename: file.filename, MediaType: "application/json", Data: data})
			if err == nil {
				result.Evidence = append(result.Evidence, artifact)
				continue
			}
		}
		reason := fmt.Sprintf("persist applier evidence %s: %v", file.kind, err)
		result.Outcome = OutcomeBlocked
		result.ImplementationResult.Outcome = OutcomeBlocked
		result.ImplementationResult.ModelExecutorRequired = false
		result.ImplementationResult.EvidenceFailureReason = reason
		if result.ImplementationResult.FailureClass == FailureClassNone {
			result.ImplementationResult.FailureClass = FailureClassEnvironment
			result.ImplementationResult.FailureReason = reason
		}
		if result.FailurePacket == nil {
			result.FailurePacket = &FailurePacket{
				FailureClass:          FailureClassEnvironment,
				Summary:               reason,
				BlockedPathChains:     []string{},
				BlockedFileWork:       []string{},
				UncertainPathChains:   append([]string(nil), result.ImplementationResult.UncertainPathChains...),
				UncertainFileWork:     append([]string(nil), result.ImplementationResult.UncertainFileWork...),
				UnattemptedPathChains: append([]string(nil), result.ImplementationResult.UnattemptedPathChains...),
				UnattemptedFileWork:   append([]string(nil), result.ImplementationResult.UnattemptedFileWork...),
				ResidualPathChains:    append([]string(nil), result.ImplementationResult.ResidualPathChains...),
				ResidualFileWork:      append([]string(nil), result.ImplementationResult.ResidualFileWork...),
				ChangedFiles:          append([]string(nil), result.ChangedFiles...),
				ProtectedPaths:        append([]string(nil), result.Partition.ProtectedPaths...),
				UncertainPaths:        append([]string(nil), result.ImplementationResult.UncertainPaths...),
			}
		}
		result.FailurePacket.EvidenceFailureReason = reason
		return result
	}
	return result
}

func ledgerEntries(plans []pathChainPlan) []LedgerEntry {
	entries := make([]LedgerEntry, 0, len(plans))
	for _, plan := range plans {
		entries = append(entries, LedgerEntry{
			PathChainRef:   plan.ref,
			FileWorkRefs:   append([]string(nil), plan.fileWorkRefs...),
			SubstepRefs:    append([]string(nil), plan.substepRefs...),
			Operations:     append([]string(nil), plan.operations...),
			Paths:          append([]string(nil), plan.paths...),
			Dependencies:   append([]string(nil), plan.dependencies...),
			Replay:         plan.replay,
			Disposition:    plan.disposition,
			Outcome:        plan.outcome,
			Reason:         plan.reason,
			Failure:        plan.failure,
			ChangedPaths:   append([]string(nil), plan.changedPaths...),
			ProtectedPaths: append([]string(nil), plan.protectedPaths...),
			UncertainPaths: append([]string(nil), plan.uncertainPaths...),
		})
	}
	return entries
}

func changedFiles(plans []pathChainPlan) []string {
	seen := map[string]struct{}{}
	for _, plan := range plans {
		if plan.outcome != OperationApplied {
			continue
		}
		for _, path := range plan.changedPaths {
			seen[path] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
