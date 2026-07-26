package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/speccompiler"
)

var ErrDeterministicRepositoryBasis = errors.New("repository does not match deterministic execution basis")

type DeterministicPreflightStatus string

const (
	DeterministicPreflightNotPresent DeterministicPreflightStatus = "not_present"
	DeterministicPreflightReady      DeterministicPreflightStatus = "ready"
	DeterministicPreflightFailed     DeterministicPreflightStatus = "preflight_failed"
)

type DeterministicPreflightInput struct {
	RepositoryRoot string
	ExpectedBranch string
	ExpectedCommit string
	Document       *speccompiler.DeterministicOperationsDocument
}

type DeterministicPreflightResult struct {
	Status   DeterministicPreflightStatus
	Coverage string
	Plan     *DeterministicMutationPlan
	Failure  *DeterministicPreflightFailure
}

type DeterministicPreflightFailure struct {
	Code           string
	OperationIndex int
	DirectiveIndex int
	Path           string
	Destination    string
	Expected       string
	Observed       string
}

// FileState is an exact byte snapshot. Bytes are copied before leaving this package.
type FileState struct {
	Exists bool
	SHA256 string
	Size   int64
	Bytes  []byte
}

type PreparedDeterministicOperation struct {
	Index             int
	Operation         string
	SourcePath        string
	DestinationPath   string
	Before            FileState
	After             FileState
	DestinationBefore FileState
	DestinationAfter  FileState
	ParentDirectories []string
}

type DeterministicMutationPlan struct {
	Coverage   string
	Operations []PreparedDeterministicOperation
}

// PreflightDeterministicOperations verifies an entire compiled deterministic
// artifact without writing to the worktree or changing Git state.
func PreflightDeterministicOperations(input DeterministicPreflightInput) (DeterministicPreflightResult, error) {
	if input.Document == nil {
		return DeterministicPreflightResult{Status: DeterministicPreflightNotPresent}, nil
	}
	root, err := admitDeterministicRepository(input)
	if err != nil {
		return DeterministicPreflightResult{}, err
	}

	p := deterministicPreflighter{
		root:      root,
		states:    make(map[string]FileState),
		inspected: make(map[string]bool),
		safePaths: make(map[string][]string),
	}
	plan := &DeterministicMutationPlan{
		Coverage:   input.Document.Coverage,
		Operations: make([]PreparedDeterministicOperation, 0, len(input.Document.Operations)),
	}
	for operationIndex, operation := range input.Document.Operations {
		prepared, failure, preflightErr := p.operation(operationIndex+1, operation)
		if preflightErr != nil {
			return DeterministicPreflightResult{}, preflightErr
		}
		if failure != nil {
			return DeterministicPreflightResult{
				Status:   DeterministicPreflightFailed,
				Coverage: input.Document.Coverage,
				Failure:  failure,
			}, nil
		}
		plan.Operations = append(plan.Operations, prepared)
	}
	return DeterministicPreflightResult{
		Status:   DeterministicPreflightReady,
		Coverage: input.Document.Coverage,
		Plan:     cloneDeterministicPlan(plan),
	}, nil
}

func admitDeterministicRepository(input DeterministicPreflightInput) (string, error) {
	if strings.TrimSpace(input.RepositoryRoot) == "" || strings.TrimSpace(input.RepositoryRoot) != input.RepositoryRoot {
		return "", fmt.Errorf("%w: repository root is required", ErrDeterministicRepositoryBasis)
	}
	root, err := filepath.Abs(input.RepositoryRoot)
	if err != nil {
		return "", fmt.Errorf("%w: resolve repository root: %v", ErrDeterministicRepositoryBasis, err)
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || isIndirect(info) {
		return "", fmt.Errorf("%w: repository root is not a safe directory", ErrDeterministicRepositoryBasis)
	}
	gitRoot, err := gitWorktreeRoot(root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDeterministicRepositoryBasis, err)
	}
	if !samePath(root, gitRoot) {
		return "", fmt.Errorf("%w: repository root is not the Git worktree root", ErrDeterministicRepositoryBasis)
	}
	preflight := workflowrepos.VerifyExecutionPreflight(context.Background(), root, input.ExpectedBranch, input.ExpectedCommit)
	if !preflight.OK {
		return "", fmt.Errorf("%w: %s", ErrDeterministicRepositoryBasis, preflight.BlockerText)
	}
	return root, nil
}

func gitWorktreeRoot(root string) (string, error) {
	command := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git worktree root: %s", strings.TrimSpace(string(output)))
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", errors.New("Git worktree root is empty")
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve Git worktree root: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func samePath(left, right string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

type deterministicPreflighter struct {
	root      string
	states    map[string]FileState
	inspected map[string]bool
	safePaths map[string][]string
}

func (p *deterministicPreflighter) operation(index int, operation speccompiler.DeterministicOperation) (PreparedDeterministicOperation, *DeterministicPreflightFailure, error) {
	path, parentDirectories, err := p.safePath(operation.Path)
	if err != nil {
		return PreparedDeterministicOperation{}, nil, err
	}
	before, err := p.state(path)
	if err != nil {
		var nonRegular *nonRegularDeterministicPathError
		if errors.As(err, &nonRegular) {
			return PreparedDeterministicOperation{}, failure("source_not_regular", index, 0, path, "", "regular file", nonRegular.observed), nil
		}
		return PreparedDeterministicOperation{}, nil, err
	}
	prepared := PreparedDeterministicOperation{Index: index, Operation: operation.Operation, SourcePath: path, Before: cloneFileState(before)}

	switch operation.Operation {
	case "modify":
		if !before.Exists {
			return prepared, failure("source_missing", index, 0, path, "", "exists=true", stateSummary(before)), nil
		}
		current := append([]byte(nil), before.Bytes...)
		for directiveIndex, directive := range operation.Implementation.Changes {
			updated, directiveFailure := applyDeterministicDirective(index, directiveIndex+1, path, current, directive)
			if directiveFailure != nil {
				return prepared, directiveFailure, nil
			}
			current = updated
		}
		after := newFileState(current)
		p.states[path] = after
		prepared.After = cloneFileState(after)
	case "create":
		if before.Exists {
			return prepared, failure("destination_exists", index, 0, path, "", "exists=false", stateSummary(before)), nil
		}
		after := newFileState([]byte(operation.Implementation.Content))
		p.states[path] = after
		prepared.After = cloneFileState(after)
		prepared.ParentDirectories = parentDirectories
	case "delete":
		if !before.Exists {
			return prepared, failure("source_missing", index, 0, path, "", "exists=true", stateSummary(before)), nil
		}
		if string(before.Bytes) != operation.Implementation.ExpectedContent {
			return prepared, failure("expected_content_mismatch", index, 0, path, "", contentSummary([]byte(operation.Implementation.ExpectedContent)), stateSummary(before)), nil
		}
		p.states[path] = FileState{}
		prepared.After = FileState{}
	case "rename":
		destination, destinationParents, destinationErr := p.safePath(operation.DestinationPath)
		if destinationErr != nil {
			return PreparedDeterministicOperation{}, nil, destinationErr
		}
		destinationBefore, stateErr := p.state(destination)
		if stateErr != nil {
			return PreparedDeterministicOperation{}, nil, stateErr
		}
		prepared.DestinationPath = destination
		prepared.DestinationBefore = cloneFileState(destinationBefore)
		if !before.Exists {
			return prepared, failure("source_missing", index, 0, path, destination, "exists=true", stateSummary(before)), nil
		}
		if destinationBefore.Exists {
			return prepared, failure("destination_exists", index, 0, path, destination, "exists=false", stateSummary(destinationBefore)), nil
		}
		if string(before.Bytes) != operation.Implementation.ExpectedContent {
			return prepared, failure("expected_content_mismatch", index, 0, path, destination, contentSummary([]byte(operation.Implementation.ExpectedContent)), stateSummary(before)), nil
		}
		afterDestination := before
		if operation.Implementation.PreserveContent == nil || !*operation.Implementation.PreserveContent {
			afterDestination = newFileState([]byte(operation.Implementation.Content))
		}
		p.states[path] = FileState{}
		p.states[destination] = afterDestination
		prepared.After = FileState{}
		prepared.DestinationAfter = cloneFileState(afterDestination)
		prepared.ParentDirectories = destinationParents
	default:
		return PreparedDeterministicOperation{}, nil, fmt.Errorf("unsupported deterministic operation %q", operation.Operation)
	}
	return prepared, nil, nil
}

func (p *deterministicPreflighter) safePath(value string) (string, []string, error) {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || filepath.IsAbs(value) || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return "", nil, fmt.Errorf("unsafe repository path %q", value)
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", nil, fmt.Errorf("unsafe repository path %q", value)
		}
	}
	if parts[0] == ".git" {
		return "", nil, fmt.Errorf("unsafe repository path %q", value)
	}
	clean := strings.Join(parts, "/")
	if parents, known := p.safePaths[clean]; known {
		return clean, append([]string(nil), parents...), nil
	}
	absolute := filepath.Clean(filepath.Join(p.root, filepath.FromSlash(clean)))
	if !withinRoot(p.root, absolute) {
		return "", nil, fmt.Errorf("repository path escapes root %q", value)
	}
	missing := make([]string, 0)
	current := p.root
	for i, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			for j := i; j < len(parts)-1; j++ {
				missing = append(missing, strings.Join(parts[:j+1], "/"))
			}
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("inspect path parent %q: %w", clean, err)
		}
		if isIndirect(info) {
			return "", nil, fmt.Errorf("unsafe indirect path component %q", clean)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("path parent is not a directory %q", clean)
		}
	}
	p.safePaths[clean] = append([]string(nil), missing...)
	return clean, missing, nil
}

func withinRoot(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func (p *deterministicPreflighter) state(path string) (FileState, error) {
	if p.inspected[path] {
		return p.states[path], nil
	}
	p.inspected[path] = true
	absolute := filepath.Join(p.root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		p.states[path] = FileState{}
		return FileState{}, nil
	}
	if err != nil {
		return FileState{}, fmt.Errorf("inspect path %q: %w", path, err)
	}
	if isIndirect(info) {
		return FileState{}, fmt.Errorf("unsafe indirect path %q", path)
	}
	if !info.Mode().IsRegular() {
		return FileState{}, &nonRegularDeterministicPathError{path: path, observed: info.Mode().String()}
	}
	bytes, err := os.ReadFile(absolute)
	if err != nil {
		return FileState{}, fmt.Errorf("read path %q: %w", path, err)
	}
	state := newFileState(bytes)
	p.states[path] = state
	return state, nil
}

type nonRegularDeterministicPathError struct {
	path     string
	observed string
}

func (e *nonRegularDeterministicPathError) Error() string {
	return fmt.Sprintf("source path is not a regular file %q", e.path)
}

func isIndirect(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeIrregular != 0
}

func applyDeterministicDirective(operationIndex, directiveIndex int, path string, current []byte, directive speccompiler.DeterministicChange) ([]byte, *DeterministicPreflightFailure) {
	if !utf8.Valid(current) {
		return nil, failure("invalid_utf8", operationIndex, directiveIndex, path, "", "valid UTF-8", contentSummary(current))
	}
	text := string(current)
	switch directive.Kind {
	case "replace", "remove":
		count := strings.Count(text, directive.OldText)
		if count != directive.ExpectedOccurrences {
			return nil, failure("selector_occurrence_mismatch", operationIndex, directiveIndex, path, "", occurrenceSummary(directive.ExpectedOccurrences), occurrenceSummary(count))
		}
		if directive.Kind == "replace" {
			return []byte(strings.Replace(text, directive.OldText, directive.NewText, directive.ExpectedOccurrences)), nil
		}
		return []byte(strings.Replace(text, directive.OldText, "", directive.ExpectedOccurrences)), nil
	case "insert_before", "insert_after":
		count := strings.Count(text, directive.Anchor)
		if count != directive.ExpectedOccurrences {
			return nil, failure("selector_occurrence_mismatch", operationIndex, directiveIndex, path, "", occurrenceSummary(directive.ExpectedOccurrences), occurrenceSummary(count))
		}
		replacement := directive.Content + directive.Anchor
		if directive.Kind == "insert_after" {
			replacement = directive.Anchor + directive.Content
		}
		return []byte(strings.Replace(text, directive.Anchor, replacement, directive.ExpectedOccurrences)), nil
	case "replace_file":
		if text != directive.ExpectedContent {
			return nil, failure("expected_content_mismatch", operationIndex, directiveIndex, path, "", contentSummary([]byte(directive.ExpectedContent)), contentSummary(current))
		}
		return []byte(directive.Content), nil
	default:
		return nil, failure("unsupported_directive", operationIndex, directiveIndex, path, "", "supported directive", directive.Kind)
	}
}

func newFileState(bytes []byte) FileState {
	sum := sha256.Sum256(bytes)
	return FileState{Exists: true, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(bytes)), Bytes: append([]byte(nil), bytes...)}
}

func cloneFileState(value FileState) FileState {
	value.Bytes = append([]byte(nil), value.Bytes...)
	return value
}

func cloneDeterministicPlan(value *DeterministicMutationPlan) *DeterministicMutationPlan {
	if value == nil {
		return nil
	}
	copyPlan := &DeterministicMutationPlan{Coverage: value.Coverage, Operations: make([]PreparedDeterministicOperation, len(value.Operations))}
	for index, operation := range value.Operations {
		operation.Before = cloneFileState(operation.Before)
		operation.After = cloneFileState(operation.After)
		operation.DestinationBefore = cloneFileState(operation.DestinationBefore)
		operation.DestinationAfter = cloneFileState(operation.DestinationAfter)
		operation.ParentDirectories = append([]string(nil), operation.ParentDirectories...)
		copyPlan.Operations[index] = operation
	}
	return copyPlan
}

func failure(code string, operationIndex, directiveIndex int, path, destination, expected, observed string) *DeterministicPreflightFailure {
	return &DeterministicPreflightFailure{Code: code, OperationIndex: operationIndex, DirectiveIndex: directiveIndex, Path: path, Destination: destination, Expected: expected, Observed: observed}
}

func stateSummary(value FileState) string {
	if !value.Exists {
		return "exists=false"
	}
	return fmt.Sprintf("exists=true sha256=%s size=%d", value.SHA256, value.Size)
}

func contentSummary(value []byte) string { return stateSummary(newFileState(value)) }
func occurrenceSummary(value int) string { return fmt.Sprintf("occurrences=%d", value) }
