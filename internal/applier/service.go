package applier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

type OperationOutcome string

const (
	OperationApplied       OperationOutcome = "applied"
	OperationSkipped       OperationOutcome = "skipped"
	OperationUnsupported   OperationOutcome = "unsupported"
	OperationModelRequired OperationOutcome = "model_required"
	OperationResidual      OperationOutcome = "residual"
	OperationBlocked       OperationOutcome = "blocked"
	OperationNotAttempted  OperationOutcome = "not_attempted"
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
	Projection     speccompiler.ExecutionPayloadProjection
	EvidenceWriter EvidenceWriter
}

type Result struct {
	Outcome              Outcome              `json:"outcome"`
	ActorKind            ActorKind            `json:"actor_kind"`
	Ledger               Ledger               `json:"ledger"`
	ImplementationResult ImplementationResult `json:"implementation_result"`
	FailurePacket        *FailurePacket       `json:"failure_packet,omitempty"`
	ChangedFiles         []string             `json:"changed_files"`
	Evidence             []EvidenceArtifact   `json:"evidence,omitempty"`
}

type Ledger struct {
	Entries []LedgerEntry `json:"entries"`
}

type LedgerEntry struct {
	OperationID  string           `json:"operation_id"`
	Kind         string           `json:"kind,omitempty"`
	Mode         string           `json:"mode,omitempty"`
	Paths        []string         `json:"paths,omitempty"`
	Group        string           `json:"group,omitempty"`
	DependsOn    []string         `json:"depends_on,omitempty"`
	OnFailure    string           `json:"on_failure,omitempty"`
	Outcome      OperationOutcome `json:"outcome"`
	Reason       string           `json:"reason,omitempty"`
	Failure      FailureClass     `json:"failure_class,omitempty"`
	ChangedFiles []string         `json:"changed_files,omitempty"`
}

type ImplementationResult struct {
	Outcome               Outcome      `json:"outcome"`
	ActorKind             ActorKind    `json:"actor_kind"`
	CompletedOperations   []string     `json:"completed_operations"`
	ResidualOperations    []string     `json:"residual_operations"`
	SkippedOperations     []string     `json:"skipped_operations"`
	BlockedOperations     []string     `json:"blocked_operations"`
	ChangedFiles          []string     `json:"changed_files"`
	ModelExecutorRequired bool         `json:"model_executor_required"`
	FailureClass          FailureClass `json:"failure_class,omitempty"`
	FailureReason         string       `json:"failure_reason,omitempty"`
}

type FailurePacket struct {
	FailureClass       FailureClass `json:"failure_class"`
	Summary            string       `json:"summary"`
	BlockedOperations  []string     `json:"blocked_operations"`
	ResidualOperations []string     `json:"residual_operations,omitempty"`
	ChangedFiles       []string     `json:"changed_files,omitempty"`
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

type Service struct{}

func NewService() Service { return Service{} }

type operationPlan struct {
	index        int
	id           string
	op           speccompiler.ProjectedDeterministicOperation
	outcome      OperationOutcome
	failure      FailureClass
	reason       string
	changedFiles []string
	apply        func() error
}

type operationPayload struct {
	Content             string `json:"content"`
	OldText             string `json:"old_text"`
	NewText             string `json:"new_text"`
	Anchor              string `json:"anchor"`
	ExpectedOccurrences int    `json:"expected_occurrences"`
	DestinationPath     string `json:"destination_path"`
	DeleteFile          bool   `json:"delete_file"`
}

type sourceGuards struct {
	FileExists  []string    `json:"file_exists"`
	FileAbsent  []string    `json:"file_absent"`
	Contains    []textGuard `json:"contains"`
	NotContains []textGuard `json:"not_contains"`
}

type textGuard struct {
	Path                string `json:"path"`
	Text                string `json:"text"`
	ExpectedOccurrences int    `json:"expected_occurrences"`
}

func (s Service) Apply(ctx context.Context, input Input) (Result, error) {
	result := Result{Outcome: OutcomeNotAttempted, ActorKind: ActorKindApplier, Ledger: Ledger{Entries: []LedgerEntry{}}, ChangedFiles: []string{}}
	if len(input.Projection.DeterministicOperations) == 0 {
		result.ImplementationResult = implementationResult(result)
		return result, nil
	}
	root, blocker := workspaceRoot(input.WorkspaceRoot)
	if blocker != "" {
		entry := LedgerEntry{OperationID: "workspace", Outcome: OperationBlocked, Failure: FailureClassEnvironment, Reason: blocker}
		result.Ledger.Entries = append(result.Ledger.Entries, entry)
		result = blockedResult(result, FailureClassEnvironment, blocker)
		return result, nil
	}
	plans := make([]operationPlan, 0, len(input.Projection.DeterministicOperations))
	byID := map[string]int{}
	for i, op := range input.Projection.DeterministicOperations {
		plan := s.planOperation(root, i, op)
		plans = append(plans, plan)
		byID[plan.id] = len(plans) - 1
	}
	applyDependencies(plans, byID)
	applyAtomicGroups(plans, input.Projection.OperationGroups)
	for i := range plans {
		if plans[i].outcome != OperationNotAttempted || plans[i].apply == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			plans[i].outcome = OperationBlocked
			plans[i].failure = FailureClassEnvironment
			plans[i].reason = err.Error()
			continue
		}
		if err := plans[i].apply(); err != nil {
			plans[i].outcome = OperationBlocked
			plans[i].failure = FailureClassEnvironment
			plans[i].reason = err.Error()
			continue
		}
		plans[i].outcome = OperationApplied
	}
	result.Ledger.Entries = ledgerEntries(plans)
	result.ChangedFiles = uniqueChangedFiles(result.Ledger.Entries)
	result = summarizeResult(result)
	if input.EvidenceWriter != nil && len(result.Ledger.Entries) > 0 {
		result = writeEvidence(ctx, input.EvidenceWriter, result)
	}
	return result, nil
}

func workspaceRoot(value string) (string, string) {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", "workspace root is required without outer whitespace"
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Sprintf("resolve workspace root: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", "workspace root is unavailable or not a directory"
	}
	return filepath.Clean(root), ""
}

func (s Service) planOperation(root string, index int, op speccompiler.ProjectedDeterministicOperation) operationPlan {
	id := strings.TrimSpace(op.ID)
	if id == "" {
		id = fmt.Sprintf("operation-%d", index+1)
	}
	plan := operationPlan{index: index, id: id, op: op, outcome: OperationNotAttempted}
	mode := strings.TrimSpace(op.Mode)
	if mode == "model_required" || mode == "executor" || mode == "model" {
		plan.outcome = OperationModelRequired
		plan.reason = "operation is marked for model-backed execution"
		return plan
	}
	if mode != "" && mode != "exact" {
		return unsupportedPlan(plan, "unsupported deterministic operation mode")
	}
	if err := checkGuards(root, op.Guards); err != nil {
		plan.outcome = OperationBlocked
		plan.failure = FailureClassUnsafeSource
		plan.reason = err.Error()
		return plan
	}
	var payload operationPayload
	if len(op.Payload) != 0 {
		if err := json.Unmarshal(op.Payload, &payload); err != nil {
			plan.outcome = OperationBlocked
			plan.failure = FailureClassMaterialSpecGap
			plan.reason = fmt.Sprintf("decode operation payload: %v", err)
			return plan
		}
	}
	switch strings.TrimSpace(op.Kind) {
	case "create":
		return planCreate(root, plan, payload)
	case "replace":
		return planReplace(root, plan, payload)
	case "insert_before":
		return planInsert(root, plan, payload, false)
	case "insert_after":
		return planInsert(root, plan, payload, true)
	case "remove":
		return planRemove(root, plan, payload)
	case "replace_file":
		return planReplaceFile(root, plan, payload)
	case "delete":
		return planDelete(root, plan, payload)
	case "rename":
		return planRename(root, plan, payload)
	default:
		return unsupportedPlan(plan, "unsupported deterministic operation kind")
	}
}

func unsupportedPlan(plan operationPlan, reason string) operationPlan {
	switch strings.TrimSpace(plan.op.OnFailure) {
	case "residual", "executor", "partial":
		plan.outcome = OperationResidual
		plan.reason = reason
	case "model_required", "model":
		plan.outcome = OperationModelRequired
		plan.reason = reason
	case "skip":
		plan.outcome = OperationSkipped
		plan.reason = reason
	default:
		plan.outcome = OperationBlocked
		plan.failure = FailureClassUnsupported
		plan.reason = reason
	}
	return plan
}

func operationPath(root string, op speccompiler.ProjectedDeterministicOperation, index int) (string, string, error) {
	if len(op.Paths) <= index {
		return "", "", fmt.Errorf("operation path %d is required", index+1)
	}
	return safePath(root, op.Paths[index])
}

func planCreate(root string, plan operationPlan, payload operationPayload) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if payload.Content == "" {
		return block(plan, FailureClassMaterialSpecGap, "create operation requires payload.content")
	}
	if _, err := os.Lstat(abs); err == nil {
		return block(plan, FailureClassUnsafeSource, "create destination already exists")
	} else if !os.IsNotExist(err) {
		return block(plan, FailureClassEnvironment, err.Error())
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, []byte(payload.Content), 0o644)
	}
	return plan
}

func planReplace(root string, plan operationPlan, payload operationPayload) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if payload.OldText == "" {
		return block(plan, FailureClassMaterialSpecGap, "replace operation requires payload.old_text")
	}
	expected := expectedOccurrences(plan.op, payload)
	content, err := os.ReadFile(abs)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	count := strings.Count(string(content), payload.OldText)
	if count != expected {
		return block(plan, FailureClassUnsafeSource, fmt.Sprintf("replace expected %d occurrence(s), found %d", expected, count))
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error {
		updated := strings.Replace(string(content), payload.OldText, payload.NewText, expected)
		return os.WriteFile(abs, []byte(updated), 0o644)
	}
	return plan
}

func planInsert(root string, plan operationPlan, payload operationPayload, after bool) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if payload.Anchor == "" {
		return block(plan, FailureClassMaterialSpecGap, "insert operation requires payload.anchor")
	}
	expected := expectedOccurrences(plan.op, payload)
	content, err := os.ReadFile(abs)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	text := string(content)
	count := strings.Count(text, payload.Anchor)
	if count != expected {
		return block(plan, FailureClassUnsafeSource, fmt.Sprintf("insert expected %d anchor occurrence(s), found %d", expected, count))
	}
	replacement := payload.Content + payload.Anchor
	if after {
		replacement = payload.Anchor + payload.Content
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error {
		updated := strings.Replace(text, payload.Anchor, replacement, expected)
		return os.WriteFile(abs, []byte(updated), 0o644)
	}
	return plan
}

func planRemove(root string, plan operationPlan, payload operationPayload) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if payload.OldText == "" {
		return block(plan, FailureClassMaterialSpecGap, "remove operation requires payload.old_text")
	}
	expected := expectedOccurrences(plan.op, payload)
	content, err := os.ReadFile(abs)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	count := strings.Count(string(content), payload.OldText)
	if count != expected {
		return block(plan, FailureClassUnsafeSource, fmt.Sprintf("remove expected %d occurrence(s), found %d", expected, count))
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error {
		updated := strings.Replace(string(content), payload.OldText, "", expected)
		return os.WriteFile(abs, []byte(updated), 0o644)
	}
	return plan
}

func planReplaceFile(root string, plan operationPlan, payload operationPayload) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if payload.Content == "" {
		return block(plan, FailureClassMaterialSpecGap, "replace_file operation requires payload.content")
	}
	if _, err := os.Stat(abs); err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error { return os.WriteFile(abs, []byte(payload.Content), 0o644) }
	return plan
}

func planDelete(root string, plan operationPlan, payload operationPayload) operationPlan {
	_, abs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if !payload.DeleteFile {
		return block(plan, FailureClassMaterialSpecGap, "delete operation requires payload.delete_file true")
	}
	if _, err := os.Stat(abs); err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	plan.changedFiles = []string{plan.op.Paths[0]}
	plan.apply = func() error { return os.Remove(abs) }
	return plan
}

func planRename(root string, plan operationPlan, payload operationPayload) operationPlan {
	sourceRel, sourceAbs, err := operationPath(root, plan.op, 0)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	destRel := payload.DestinationPath
	if destRel == "" && len(plan.op.Paths) > 1 {
		destRel = plan.op.Paths[1]
	}
	if strings.TrimSpace(destRel) == "" {
		return block(plan, FailureClassMaterialSpecGap, "rename operation requires destination path")
	}
	destRel, destAbs, err := safePath(root, destRel)
	if err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if _, err := os.Stat(sourceAbs); err != nil {
		return block(plan, FailureClassUnsafeSource, err.Error())
	}
	if _, err := os.Lstat(destAbs); err == nil {
		return block(plan, FailureClassUnsafeSource, "rename destination already exists")
	} else if !os.IsNotExist(err) {
		return block(plan, FailureClassEnvironment, err.Error())
	}
	plan.changedFiles = []string{sourceRel, destRel}
	plan.apply = func() error {
		if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
			return err
		}
		return os.Rename(sourceAbs, destAbs)
	}
	return plan
}

func block(plan operationPlan, class FailureClass, reason string) operationPlan {
	plan.outcome = OperationBlocked
	plan.failure = class
	plan.reason = reason
	return plan
}

func expectedOccurrences(op speccompiler.ProjectedDeterministicOperation, payload operationPayload) int {
	if op.ExpectedOccurrences > 0 {
		return op.ExpectedOccurrences
	}
	if payload.ExpectedOccurrences > 0 {
		return payload.ExpectedOccurrences
	}
	return 1
}

func applyDependencies(plans []operationPlan, byID map[string]int) {
	for i := range plans {
		if plans[i].outcome != OperationNotAttempted {
			continue
		}
		for _, dependency := range plans[i].op.DependsOn {
			depIndex, ok := byID[strings.TrimSpace(dependency)]
			if !ok {
				plans[i] = block(plans[i], FailureClassDependency, "dependency is not declared: "+dependency)
				break
			}
			dep := plans[depIndex]
			if dep.outcome == OperationBlocked {
				plans[i] = block(plans[i], FailureClassDependency, "dependency is blocked: "+dependency)
				break
			}
			if dep.outcome != OperationNotAttempted {
				plans[i].outcome = OperationResidual
				plans[i].reason = "dependency is not deterministically applied: " + dependency
				break
			}
		}
	}
}

func applyAtomicGroups(plans []operationPlan, groups []speccompiler.ProjectedOperationGroup) {
	atomic := map[string]bool{}
	for _, group := range groups {
		if group.Atomic && strings.TrimSpace(group.ID) != "" {
			atomic[strings.TrimSpace(group.ID)] = true
		}
	}
	for groupID := range atomic {
		var members []int
		hasBlocked := false
		hasResidual := false
		for i := range plans {
			if strings.TrimSpace(plans[i].op.Group) == groupID {
				members = append(members, i)
				if plans[i].outcome == OperationBlocked {
					hasBlocked = true
				}
				if plans[i].outcome != OperationNotAttempted {
					hasResidual = true
				}
			}
		}
		if !hasBlocked && !hasResidual {
			continue
		}
		for _, index := range members {
			if plans[index].outcome == OperationNotAttempted {
				if hasBlocked {
					plans[index] = block(plans[index], FailureClassGroup, "atomic group contains a blocked operation")
				} else {
					plans[index].outcome = OperationResidual
					plans[index].reason = "atomic group contains non-deterministic residual work"
				}
			}
		}
	}
}

func checkGuards(root string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var guards sourceGuards
	if err := json.Unmarshal(raw, &guards); err != nil {
		return fmt.Errorf("decode source guards: %w", err)
	}
	for _, path := range guards.FileExists {
		_, abs, err := safePath(root, path)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("source guard file_exists failed for %s: %w", path, err)
		}
	}
	for _, path := range guards.FileAbsent {
		_, abs, err := safePath(root, path)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(abs); err == nil {
			return fmt.Errorf("source guard file_absent failed for %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, guard := range guards.Contains {
		if err := checkTextGuard(root, guard, true); err != nil {
			return err
		}
	}
	for _, guard := range guards.NotContains {
		if err := checkTextGuard(root, guard, false); err != nil {
			return err
		}
	}
	return nil
}

func checkTextGuard(root string, guard textGuard, contains bool) error {
	_, abs, err := safePath(root, guard.Path)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	count := strings.Count(string(content), guard.Text)
	if contains {
		expected := guard.ExpectedOccurrences
		if expected == 0 {
			if count == 0 {
				return fmt.Errorf("source guard contains failed for %s", guard.Path)
			}
			return nil
		}
		if count != expected {
			return fmt.Errorf("source guard contains expected %d occurrence(s) in %s, found %d", expected, guard.Path, count)
		}
		return nil
	}
	if count != 0 {
		return fmt.Errorf("source guard not_contains failed for %s", guard.Path)
	}
	return nil
}

func safePath(root, rel string) (string, string, error) {
	if strings.TrimSpace(rel) == "" || strings.TrimSpace(rel) != rel {
		return "", "", fmt.Errorf("path must be nonblank without outer whitespace")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "//") || strings.Contains(rel, "\\") || strings.Contains(rel, ":") {
		return "", "", fmt.Errorf("unsafe repository path: %s", rel)
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", fmt.Errorf("unsafe repository path: %s", rel)
		}
	}
	if parts[0] == ".git" {
		return "", "", fmt.Errorf("unsafe repository path targets git metadata: %s", rel)
	}
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	prefix := root + string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, prefix) {
		return "", "", fmt.Errorf("unsafe repository path escapes workspace: %s", rel)
	}
	return filepath.ToSlash(rel), abs, nil
}

func ledgerEntries(plans []operationPlan) []LedgerEntry {
	entries := make([]LedgerEntry, 0, len(plans))
	for _, plan := range plans {
		entries = append(entries, LedgerEntry{OperationID: plan.id, Kind: plan.op.Kind, Mode: plan.op.Mode, Paths: append([]string(nil), plan.op.Paths...), Group: plan.op.Group, DependsOn: append([]string(nil), plan.op.DependsOn...), OnFailure: plan.op.OnFailure, Outcome: plan.outcome, Reason: plan.reason, Failure: plan.failure, ChangedFiles: append([]string(nil), plan.changedFiles...)})
	}
	return entries
}

func uniqueChangedFiles(entries []LedgerEntry) []string {
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Outcome != OperationApplied {
			continue
		}
		for _, path := range entry.ChangedFiles {
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

func summarizeResult(result Result) Result {
	blocked := []string{}
	residual := []string{}
	skipped := []string{}
	completed := []string{}
	var failure FailureClass
	reason := ""
	for _, entry := range result.Ledger.Entries {
		switch entry.Outcome {
		case OperationApplied:
			completed = append(completed, entry.OperationID)
		case OperationBlocked:
			blocked = append(blocked, entry.OperationID)
			if failure == FailureClassNone || failure == "" {
				failure = entry.Failure
				reason = entry.Reason
			}
		case OperationResidual, OperationModelRequired, OperationUnsupported:
			residual = append(residual, entry.OperationID)
		case OperationSkipped:
			skipped = append(skipped, entry.OperationID)
		}
	}
	switch {
	case len(blocked) > 0:
		result.Outcome = OutcomeBlocked
	case len(completed) > 0 && (len(residual) > 0 || len(skipped) > 0):
		result.Outcome = OutcomePartial
	case len(completed) > 0:
		result.Outcome = OutcomeCompleted
	default:
		result.Outcome = OutcomeNotAttempted
	}
	result.ChangedFiles = uniqueChangedFiles(result.Ledger.Entries)
	result.ImplementationResult = ImplementationResult{Outcome: result.Outcome, ActorKind: ActorKindApplier, CompletedOperations: completed, ResidualOperations: residual, SkippedOperations: skipped, BlockedOperations: blocked, ChangedFiles: result.ChangedFiles, ModelExecutorRequired: len(residual) > 0 || result.Outcome == OutcomeNotAttempted, FailureClass: failure, FailureReason: reason}
	if len(blocked) > 0 {
		result.FailurePacket = &FailurePacket{FailureClass: failure, Summary: reason, BlockedOperations: blocked, ResidualOperations: residual, ChangedFiles: result.ChangedFiles}
	}
	return result
}

func implementationResult(result Result) ImplementationResult {
	return ImplementationResult{Outcome: result.Outcome, ActorKind: result.ActorKind, CompletedOperations: []string{}, ResidualOperations: []string{}, SkippedOperations: []string{}, BlockedOperations: []string{}, ChangedFiles: []string{}, ModelExecutorRequired: true}
}

func blockedResult(result Result, class FailureClass, reason string) Result {
	result.Outcome = OutcomeBlocked
	result.FailurePacket = &FailurePacket{FailureClass: class, Summary: reason, BlockedOperations: []string{"workspace"}}
	result.ImplementationResult = ImplementationResult{Outcome: OutcomeBlocked, ActorKind: ActorKindApplier, BlockedOperations: []string{"workspace"}, ChangedFiles: []string{}, FailureClass: class, FailureReason: reason}
	return result
}

func writeEvidence(ctx context.Context, writer EvidenceWriter, result Result) Result {
	type evidenceDescriptor struct {
		kind     string
		filename string
		value    any
	}
	files := make([]evidenceDescriptor, 0, 4)
	files = append(files, evidenceDescriptor{kind: "applier_ledger_json", filename: "applier-ledger.json", value: result.Ledger})
	files = append(files, evidenceDescriptor{kind: "applier_result_json", filename: "applier-result.json", value: result.ImplementationResult})
	files = append(files, evidenceDescriptor{kind: "applier_changed_files_json", filename: "applier-changed-files.json", value: map[string][]string{"changed_files": result.ChangedFiles}})
	if result.FailurePacket != nil {
		files = append(files, evidenceDescriptor{kind: "applier_failure_packet_json", filename: "applier-failure-packet.json", value: result.FailurePacket})
	}
	for _, file := range files {
		data, err := json.MarshalIndent(file.value, "", "  ")
		if err != nil {
			return blockedResult(result, FailureClassEnvironment, fmt.Sprintf("marshal evidence %s: %v", file.kind, err))
		}
		data = append(data, '\n')
		artifact, err := writer.WriteEvidence(ctx, EvidenceFile{Kind: file.kind, Filename: file.filename, MediaType: "application/json", Data: data})
		if err != nil {
			return blockedResult(result, FailureClassEnvironment, fmt.Sprintf("write evidence %s: %v", file.kind, err))
		}
		result.Evidence = append(result.Evidence, artifact)
	}
	return result
}
