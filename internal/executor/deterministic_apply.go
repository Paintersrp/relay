package executor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrDeterministicPlanInvalid            = errors.New("deterministic mutation plan is invalid")
	ErrDeterministicPlanStale              = errors.New("repository state changed after deterministic preflight")
	ErrDeterministicApplicationFailed      = errors.New("deterministic application failed and was rolled back")
	ErrDeterministicMutationReconciliation = errors.New("deterministic application failed and rollback was incomplete")
)

// DeterministicApplyInput deliberately accepts only a prepared plan. It never
// accepts the authored operations document or reruns directive matching.
type DeterministicApplyInput struct {
	RepositoryRoot string
	ExpectedBranch string
	ExpectedCommit string
	Plan           *DeterministicMutationPlan
}

type AppliedFileState struct {
	Exists bool
	SHA256 string
	Size   int64
}

type AppliedDeterministicOperation struct {
	Index             int
	Operation         string
	SourcePath        string
	DestinationPath   string
	SourceBefore      AppliedFileState
	SourceAfter       AppliedFileState
	DestinationBefore AppliedFileState
	DestinationAfter  AppliedFileState
}

type DeterministicApplicationResult struct {
	Coverage     string
	Operations   []AppliedDeterministicOperation
	ChangedPaths []string
}

// deterministicApplyFailureHook is a deliberately small package-private test
// seam. Production leaves it nil and uses ordinary filesystem operations.
var deterministicApplyFailureHook func(phase string, operationIndex int) error

// ApplyDeterministicMutationPlan applies a successful preflight plan as one
// all-or-nothing filesystem mutation.
func ApplyDeterministicMutationPlan(input DeterministicApplyInput) (DeterministicApplicationResult, error) {
	model, err := validateDeterministicPlan(input.Plan)
	if err != nil {
		return DeterministicApplicationResult{}, err
	}
	root, err := admitDeterministicRepository(DeterministicPreflightInput{
		RepositoryRoot: input.RepositoryRoot, ExpectedBranch: input.ExpectedBranch, ExpectedCommit: input.ExpectedCommit,
	})
	if err != nil {
		return DeterministicApplicationResult{}, err
	}
	if err := validateInitialStates(root, model); err != nil {
		return DeterministicApplicationResult{}, err
	}
	rollback, err := captureDeterministicRollback(root, model)
	if err != nil {
		return DeterministicApplicationResult{}, fmt.Errorf("%w: prepare rollback: %v", ErrDeterministicPlanStale, err)
	}
	indexBefore, err := deterministicIndexHash(root)
	if err != nil {
		return DeterministicApplicationResult{}, err
	}

	mutated := false
	for _, operation := range model.operations {
		if err := deterministicApplyHook("before_operation", operation.Index); err != nil {
			return deterministicApplyFailure(root, rollback, mutated, err)
		}
		mutated = true // applyOperation can fail after a partial filesystem change.
		if err := applyDeterministicOperation(root, operation); err != nil {
			return deterministicApplyFailure(root, rollback, true, err)
		}
		if err := deterministicApplyHook("after_mutation", operation.Index); err != nil {
			return deterministicApplyFailure(root, rollback, true, err)
		}
		if err := verifyOperationState(root, operation); err != nil {
			return deterministicApplyFailure(root, rollback, true, err)
		}
	}
	if err := verifyFinalDeterministicState(root, model, indexBefore, input.ExpectedBranch, input.ExpectedCommit); err != nil {
		return deterministicApplyFailure(root, rollback, true, err)
	}
	return applicationResult(model), nil
}

type deterministicPlanModel struct {
	coverage   string
	operations []PreparedDeterministicOperation
	initial    map[string]FileState
	final      map[string]FileState
	paths      []string
}

func validateDeterministicPlan(plan *DeterministicMutationPlan) (deterministicPlanModel, error) {
	if plan == nil || (plan.Coverage != "partial" && plan.Coverage != "complete") || len(plan.Operations) == 0 {
		return deterministicPlanModel{}, fmt.Errorf("%w: plan, coverage, and operations are required", ErrDeterministicPlanInvalid)
	}
	model := deterministicPlanModel{coverage: plan.Coverage, operations: make([]PreparedDeterministicOperation, len(plan.Operations)), initial: map[string]FileState{}, final: map[string]FileState{}}
	virtual := map[string]FileState{}
	seen := map[string]bool{}
	addPath := func(path string) {
		if !seen[path] {
			seen[path] = true
			model.paths = append(model.paths, path)
		}
	}
	for i, raw := range plan.Operations {
		op := clonePreparedOperation(raw)
		if op.Index != i+1 || !validDeterministicPath(op.SourcePath) || !validPreparedStates(op) {
			return deterministicPlanModel{}, fmt.Errorf("%w: malformed operation %d", ErrDeterministicPlanInvalid, i+1)
		}
		if err := validateOperationShape(op); err != nil {
			return deterministicPlanModel{}, err
		}
		if err := validateVirtualParents(op.SourcePath, virtual); err != nil {
			return deterministicPlanModel{}, err
		}
		if current, exists := virtual[op.SourcePath]; exists {
			if !equalFileState(current, op.Before) {
				return deterministicPlanModel{}, fmt.Errorf("%w: operation %d source continuity", ErrDeterministicPlanInvalid, op.Index)
			}
		} else {
			model.initial[op.SourcePath] = cloneFileState(op.Before)
		}
		addPath(op.SourcePath)
		if op.Operation == "rename" {
			if err := validateVirtualParents(op.DestinationPath, virtual); err != nil {
				return deterministicPlanModel{}, err
			}
			if current, exists := virtual[op.DestinationPath]; exists {
				if !equalFileState(current, op.DestinationBefore) {
					return deterministicPlanModel{}, fmt.Errorf("%w: operation %d destination continuity", ErrDeterministicPlanInvalid, op.Index)
				}
			} else {
				model.initial[op.DestinationPath] = cloneFileState(op.DestinationBefore)
			}
			addPath(op.DestinationPath)
		}
		virtual[op.SourcePath] = cloneFileState(op.After)
		if op.Operation == "rename" {
			virtual[op.DestinationPath] = cloneFileState(op.DestinationAfter)
		}
		model.operations[i] = op
	}
	for _, path := range model.paths {
		model.final[path] = cloneFileState(virtual[path])
	}
	return model, nil
}

func clonePreparedOperation(value PreparedDeterministicOperation) PreparedDeterministicOperation {
	value.Before, value.After = cloneFileState(value.Before), cloneFileState(value.After)
	value.DestinationBefore, value.DestinationAfter = cloneFileState(value.DestinationBefore), cloneFileState(value.DestinationAfter)
	value.ParentDirectories = append([]string(nil), value.ParentDirectories...)
	return value
}

func validPreparedStates(op PreparedDeterministicOperation) bool {
	for _, state := range []FileState{op.Before, op.After, op.DestinationBefore, op.DestinationAfter} {
		if state.Exists {
			if state.Bytes == nil || state.Size != int64(len(state.Bytes)) || !equalFileState(state, newFileState(state.Bytes)) {
				return false
			}
		} else if state.SHA256 != "" || state.Size != 0 || len(state.Bytes) != 0 {
			return false
		}
	}
	return true
}

func validateOperationShape(op PreparedDeterministicOperation) error {
	emptyDestination := op.DestinationPath == "" && !op.DestinationBefore.Exists && !op.DestinationAfter.Exists
	switch op.Operation {
	case "modify":
		if !op.Before.Exists || !op.After.Exists || !emptyDestination {
			return fmt.Errorf("%w: malformed modify", ErrDeterministicPlanInvalid)
		}
	case "create":
		if op.Before.Exists || !op.After.Exists || !emptyDestination {
			return fmt.Errorf("%w: malformed create", ErrDeterministicPlanInvalid)
		}
	case "delete":
		if !op.Before.Exists || op.After.Exists || !emptyDestination {
			return fmt.Errorf("%w: malformed delete", ErrDeterministicPlanInvalid)
		}
	case "rename":
		if !op.Before.Exists || op.After.Exists || !validDeterministicPath(op.DestinationPath) || op.SourcePath == op.DestinationPath || op.DestinationBefore.Exists || !op.DestinationAfter.Exists {
			return fmt.Errorf("%w: malformed rename", ErrDeterministicPlanInvalid)
		}
	default:
		return fmt.Errorf("%w: unsupported operation %q", ErrDeterministicPlanInvalid, op.Operation)
	}
	return nil
}

func validDeterministicPath(path string) bool {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) != path || filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.Contains(path, ":") {
		return false
	}
	parts := strings.Split(path, "/")
	if parts[0] == ".git" {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validateVirtualParents(path string, virtual map[string]FileState) error {
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		if state, exists := virtual[strings.Join(parts[:i], "/")]; exists && state.Exists {
			return fmt.Errorf("%w: regular-file ancestor for %q", ErrDeterministicPlanInvalid, path)
		}
	}
	for other, state := range virtual {
		if state.Exists && strings.HasPrefix(other, path+"/") {
			return fmt.Errorf("%w: regular file %q has virtual descendant", ErrDeterministicPlanInvalid, path)
		}
	}
	return nil
}

func validateInitialStates(root string, model deterministicPlanModel) error {
	for _, path := range model.paths {
		if err := validatePhysicalParents(root, path, model); err != nil {
			return stalePathError(path, err.Error())
		}
		observed, err := readPhysicalFileState(root, path)
		if err != nil {
			return stalePathError(path, err.Error())
		}
		if !equalFileState(observed, model.initial[path]) {
			return stalePathError(path, fmt.Sprintf("expected %s observed %s", stateSummary(model.initial[path]), stateSummary(observed)))
		}
	}
	return nil
}

func validatePhysicalParents(root, path string, model deterministicPlanModel) error {
	current := root
	parts := strings.Split(path, "/")
	for i, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if isIndirect(info) {
			return errors.New("indirect parent")
		}
		if info.IsDir() {
			continue
		}
		ancestor := strings.Join(parts[:i+1], "/")
		if final, changed := model.final[ancestor]; changed && !final.Exists {
			continue
		}
		return errors.New("parent is not a directory")
	}
	return nil
}

func readPhysicalFileState(root, path string) (FileState, error) {
	abs := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return FileState{}, nil
	}
	if err != nil {
		return FileState{}, err
	}
	if isIndirect(info) || !info.Mode().IsRegular() {
		return FileState{}, errors.New("path is not a regular file")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileState{}, err
	}
	return newFileState(data), nil
}

func stalePathError(path, detail string) error {
	return fmt.Errorf("%w: path %q: %s", ErrDeterministicPlanStale, path, detail)
}

type deterministicRollback struct {
	files       map[string]rollbackFile
	absent      map[string]bool
	directories map[string]bool
}
type rollbackFile struct {
	bytes []byte
	mode  os.FileMode
}

func captureDeterministicRollback(root string, model deterministicPlanModel) (deterministicRollback, error) {
	r := deterministicRollback{files: map[string]rollbackFile{}, absent: map[string]bool{}, directories: map[string]bool{}}
	for _, path := range model.paths {
		if err := r.captureParentDirectories(root, path); err != nil {
			return r, err
		}
		state, err := readPhysicalFileState(root, path)
		if err != nil {
			return r, err
		}
		if !state.Exists {
			r.absent[path] = true
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return r, err
		}
		r.files[path] = rollbackFile{bytes: append([]byte(nil), state.Bytes...), mode: info.Mode().Perm()}
	}
	return r, nil
}

func (r deterministicRollback) captureParentDirectories(root, path string) error {
	current := root
	parts := strings.Split(path, "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if isIndirect(info) {
			return errors.New("indirect rollback parent")
		}
		if info.IsDir() {
			relative, relErr := filepath.Rel(root, current)
			if relErr != nil {
				return relErr
			}
			r.directories[filepath.ToSlash(relative)] = true
			continue
		}
		return nil
	}
	return nil
}

func applyDeterministicOperation(root string, op PreparedDeterministicOperation) error {
	source := filepath.Join(root, filepath.FromSlash(op.SourcePath))
	before, err := readPhysicalFileState(root, op.SourcePath)
	if err != nil || !equalFileState(before, op.Before) {
		return stalePathError(op.SourcePath, "prepared source before state no longer matches")
	}
	switch op.Operation {
	case "modify":
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		return writeAtomic(source, op.After.Bytes, info.Mode().Perm())
	case "create":
		if before.Exists {
			return stalePathError(op.SourcePath, "create target exists")
		}
		if err := ensureParents(root, op.SourcePath); err != nil {
			return err
		}
		return writeAtomic(source, op.After.Bytes, 0o644)
	case "delete":
		return os.Remove(source)
	case "rename":
		destinationBefore, err := readPhysicalFileState(root, op.DestinationPath)
		if err != nil || !equalFileState(destinationBefore, op.DestinationBefore) {
			return stalePathError(op.DestinationPath, "rename destination state no longer matches")
		}
		if err := ensureParents(root, op.DestinationPath); err != nil {
			return err
		}
		destination := filepath.Join(root, filepath.FromSlash(op.DestinationPath))
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if bytes.Equal(op.Before.Bytes, op.DestinationAfter.Bytes) {
			return os.Rename(source, destination)
		}
		if err := writeAtomic(destination, op.DestinationAfter.Bytes, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Remove(source)
	}
	return fmt.Errorf("unsupported operation %q", op.Operation)
}

func ensureParents(root, path string) error {
	current := root
	parts := strings.Split(path, "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if isIndirect(info) || !info.IsDir() {
			return errors.New("parent is not a safe directory")
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".relay-deterministic-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func verifyOperationState(root string, op PreparedDeterministicOperation) error {
	state, err := readPhysicalFileState(root, op.SourcePath)
	if err != nil || !equalFileState(state, op.After) {
		return stalePathError(op.SourcePath, "operation result does not match prepared state")
	}
	if op.Operation == "rename" {
		state, err = readPhysicalFileState(root, op.DestinationPath)
		if err != nil || !equalFileState(state, op.DestinationAfter) {
			return stalePathError(op.DestinationPath, "operation result does not match prepared state")
		}
	}
	return nil
}

func verifyFinalDeterministicState(root string, model deterministicPlanModel, indexBefore, branch, commit string) error {
	for _, path := range model.paths {
		state, err := readPhysicalFileState(root, path)
		if err == nil && equalFileState(state, model.final[path]) {
			continue
		}
		if !finalImplicitDirectory(root, path, model) {
			return stalePathError(path, "final state does not match prepared state")
		}
	}
	if _, err := admitDeterministicRepositoryAfterApply(root, branch, commit); err != nil {
		return err
	}
	indexAfter, err := deterministicIndexHash(root)
	if err != nil || indexAfter != indexBefore {
		return fmt.Errorf("deterministic application changed Git index")
	}
	if err := verifyExpectedWorktreeChanges(root, model); err != nil {
		return err
	}
	return nil
}

func verifyExpectedWorktreeChanges(root string, model deterministicPlanModel) error {
	expected := map[string]bool{}
	for _, path := range model.paths {
		if !equalFileState(model.initial[path], model.final[path]) {
			expected[path] = true
		}
	}
	output, err := osCommand([]string{"git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all"})
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if len(record) < 4 {
			return errors.New("unexpected Git status record")
		}
		path := filepath.ToSlash(string(record[3:]))
		if !expected[path] {
			return fmt.Errorf("unexpected worktree change at %q", path)
		}
	}
	return nil
}

func finalImplicitDirectory(root, path string, model deterministicPlanModel) bool {
	if model.final[path].Exists {
		return false
	}
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || isIndirect(info) || !info.IsDir() {
		return false
	}
	for other, state := range model.final {
		if state.Exists && strings.HasPrefix(other, path+"/") {
			return true
		}
	}
	return false
}

func admitDeterministicRepositoryAfterApply(root, branch, commit string) (string, error) {
	gitRoot, err := gitWorktreeRoot(root)
	if err != nil || !samePath(root, gitRoot) {
		return "", fmt.Errorf("repository basis changed after application")
	}
	currentBranch, err := osCommand([]string{"git", "-C", root, "symbolic-ref", "--quiet", "--short", "HEAD"})
	if err != nil || strings.TrimSpace(string(currentBranch)) != branch {
		return "", fmt.Errorf("branch changed after application")
	}
	head, err := osCommand([]string{"git", "-C", root, "rev-parse", "--verify", "HEAD"})
	if err != nil || !strings.EqualFold(strings.TrimSpace(string(head)), commit) {
		return "", fmt.Errorf("HEAD changed after application")
	}
	return root, nil
}

func deterministicIndexHash(root string) (string, error) {
	command := []string{"git", "-C", root, "rev-parse", "--git-path", "index"}
	output, err := osCommand(command)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return newFileState(data).SHA256, nil
}

var osCommand = func(command []string) ([]byte, error) {
	output, err := exec.Command(command[0], command[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(command, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func deterministicApplyFailure(root string, rollback deterministicRollback, mutated bool, applicationErr error) (DeterministicApplicationResult, error) {
	if !mutated {
		return DeterministicApplicationResult{}, applicationErr
	}
	if err := rollback.restore(root); err != nil {
		return DeterministicApplicationResult{}, fmt.Errorf("%w: application error: %v; rollback error: %v", ErrDeterministicMutationReconciliation, applicationErr, err)
	}
	return DeterministicApplicationResult{}, fmt.Errorf("%w: %v", ErrDeterministicApplicationFailed, applicationErr)
}

func (r deterministicRollback) restore(root string) error {
	paths := make([]string, 0, len(r.absent))
	for path := range r.absent {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		if err := deterministicApplyHook("rollback", 0); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return err
		}
	}
	createdDirectories := make([]string, 0)
	for path := range r.absent {
		parts := strings.Split(path, "/")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			if !r.directories[parent] {
				createdDirectories = append(createdDirectories, parent)
			}
		}
	}
	sort.Slice(createdDirectories, func(i, j int) bool { return len(createdDirectories[i]) > len(createdDirectories[j]) })
	seenDirectories := map[string]bool{}
	for _, path := range createdDirectories {
		if seenDirectories[path] {
			continue
		}
		seenDirectories[path] = true
		err := os.Remove(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil && !os.IsNotExist(err) && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	existing := make([]string, 0, len(r.files))
	for path := range r.files {
		existing = append(existing, path)
	}
	sort.Strings(existing)
	for _, path := range existing {
		if err := deterministicApplyHook("rollback", 0); err != nil {
			return err
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.RemoveAll(full); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		file := r.files[path]
		if err := writeAtomic(full, file.bytes, file.mode); err != nil {
			return err
		}
	}
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			return fmt.Errorf("rollback path remains %q", path)
		}
	}
	for _, path := range existing {
		state, err := readPhysicalFileState(root, path)
		if err != nil || !state.Exists || !bytes.Equal(state.Bytes, r.files[path].bytes) {
			return fmt.Errorf("rollback path not restored %q", path)
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || info.Mode().Perm() != r.files[path].mode {
			return fmt.Errorf("rollback mode not restored %q", path)
		}
	}
	return nil
}

func deterministicApplyHook(phase string, index int) error {
	if deterministicApplyFailureHook != nil {
		return deterministicApplyFailureHook(phase, index)
	}
	return nil
}

func applicationResult(model deterministicPlanModel) DeterministicApplicationResult {
	result := DeterministicApplicationResult{Coverage: model.coverage, Operations: make([]AppliedDeterministicOperation, len(model.operations))}
	for i, op := range model.operations {
		result.Operations[i] = AppliedDeterministicOperation{Index: op.Index, Operation: op.Operation, SourcePath: op.SourcePath, DestinationPath: op.DestinationPath, SourceBefore: appliedState(op.Before), SourceAfter: appliedState(op.After), DestinationBefore: appliedState(op.DestinationBefore), DestinationAfter: appliedState(op.DestinationAfter)}
	}
	for _, path := range model.paths {
		if !equalFileState(model.initial[path], model.final[path]) {
			result.ChangedPaths = append(result.ChangedPaths, path)
		}
	}
	return result
}
func appliedState(state FileState) AppliedFileState {
	return AppliedFileState{Exists: state.Exists, SHA256: state.SHA256, Size: state.Size}
}
func equalFileState(left, right FileState) bool {
	return left.Exists == right.Exists && left.SHA256 == right.SHA256 && left.Size == right.Size && bytes.Equal(left.Bytes, right.Bytes)
}
