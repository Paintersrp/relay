package sourcevault

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

const gitDiagnosticLimit = 64 << 10

var zeroOID = strings.Repeat("0", 40)

type gitClient interface {
	ValidateRepositorySeparation(context.Context, string) (bool, error)
	VaultPath(relativePath string) (string, error)
	EnsureVault(context.Context, string) error
	ValidateVault(context.Context, string) error
	VerifySource(context.Context, string, string, string) error
	ImportClosure(context.Context, string, string, string) error
	VerifyVaultClosure(context.Context, string, string, string, string) error
	ReadRef(context.Context, string, string) (string, bool, error)
	CreateRef(context.Context, string, string, string) error
	DeleteRef(context.Context, string, string, string) error
	ReadObject(context.Context, string, string, string, int64) ([]byte, error)
	ReadTree(context.Context, string, string) ([]RetainedTreeEntry, error)
	ReadBlobRange(context.Context, string, string, int64, int64) (ReadRetainedBlobRangeResult, error)
	GarbageCollect(context.Context, string) error
}

type commandGit struct {
	root string
}

func newCommandGit(
	ctx context.Context,
	root string,
	repositories []workflowstore.RepositoryTarget,
) (*commandGit, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	canonicalRoot, err := canonicalPathForCreation(root)
	if err != nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	git := &commandGit{root: canonicalRoot}
	for _, repository := range repositories {
		if _, err := git.ValidateRepositorySeparation(ctx, repository.LocalPath); err != nil {
			return nil, err
		}
	}
	if err := git.ensureRoot(); err != nil {
		return nil, err
	}
	return git, nil
}

func (g *commandGit) ensureRoot() error {
	if err := os.MkdirAll(g.root, 0o755); err != nil {
		return &Error{Code: CodeVaultUnavailable}
	}
	info, err := os.Lstat(g.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: CodeInvalidRequest, FailureReason: workflowstore.SourceVaultFailureVaultInvalid}
	}
	return nil
}

func (g *commandGit) ValidateRepositorySeparation(ctx context.Context, localPath string) (bool, error) {
	storedPath, err := canonicalPathForCreation(localPath)
	if err != nil {
		return false, &Error{Code: CodeRepositoryMismatch}
	}
	managedRoot := filepath.Join(g.root, "repositories")
	overlapsVaultStorage := func(candidate string) bool {
		return pathsOverlap(g.root, candidate) || pathsOverlap(managedRoot, candidate)
	}
	if overlapsVaultStorage(storedPath) {
		return false, &Error{Code: CodeUnsafeVaultRoot}
	}
	info, err := os.Stat(storedPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() {
		return false, &Error{Code: CodeRepositoryMismatch}
	}
	sourcePath, err := canonicalExistingPath(localPath)
	if err != nil {
		return false, &Error{Code: CodeRepositoryMismatch}
	}
	gitDirectory, commonDirectory, err := resolveGitDirectories(ctx, sourcePath)
	if err != nil {
		return false, err
	}
	if overlapsVaultStorage(sourcePath) ||
		overlapsVaultStorage(gitDirectory) ||
		overlapsVaultStorage(commonDirectory) {
		return false, &Error{Code: CodeUnsafeVaultRoot}
	}
	return true, nil
}

func canonicalPathForCreation(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source vault root must not be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}

	current := absolute
	missing := make([]string, 0)
	for {
		if _, statErr := os.Lstat(current); statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("source vault root has no existing ancestor")
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func canonicalExistingPath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func resolveGitDirectories(ctx context.Context, sourcePath string) (string, string, error) {
	cmd := exec.CommandContext(
		ctx,
		"git",
		"--no-replace-objects",
		"-C",
		sourcePath,
		"rev-parse",
		"--path-format=absolute",
		"--git-dir",
		"--git-common-dir",
	)
	cmd.Env = controlledGitEnvironment()
	stdout := newLimitedBuffer(gitDiagnosticLimit)
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", "", &Error{Code: CodeRepositoryMismatch}
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || strings.TrimSpace(lines[0]) == "" || strings.TrimSpace(lines[1]) == "" {
		return "", "", &Error{Code: CodeRepositoryMismatch}
	}
	gitDirectory, err := canonicalExistingPath(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", "", &Error{Code: CodeRepositoryMismatch}
	}
	commonDirectory, err := canonicalExistingPath(strings.TrimSpace(lines[1]))
	if err != nil {
		return "", "", &Error{Code: CodeRepositoryMismatch}
	}
	return gitDirectory, commonDirectory, nil
}

func pathWithin(candidate, protected string) bool {
	relative, err := filepath.Rel(protected, candidate)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func (g *commandGit) VaultPath(relativePath string) (string, error) {
	if relativePath == "" || strings.TrimSpace(relativePath) != relativePath || filepath.IsAbs(relativePath) || strings.Contains(relativePath, "\\") {
		return "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
	}
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
		}
	}
	path := filepath.Join(append([]string{g.root}, parts...)...)
	rel, err := filepath.Rel(g.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
	}
	current := g.root
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: statErr}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
		}
	}
	return path, nil
}

func (g *commandGit) EnsureVault(ctx context.Context, vaultPath string) error {
	info, err := os.Lstat(vaultPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
			return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
		}
		if _, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, "", false, "init", "--bare", vaultPath); err != nil {
			return err
		}
	} else if err != nil {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
	}
	return g.ValidateVault(ctx, vaultPath)
}

func (g *commandGit) ValidateVault(ctx context.Context, vaultPath string) error {
	info, err := os.Lstat(vaultPath)
	if errors.Is(err, os.ErrNotExist) {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultMissing, code: CodeVaultUnavailable}
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
	}
	if err := validateVaultStorage(vaultPath); err != nil {
		return err
	}
	result, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "rev-parse", "--is-bare-repository")
	if err != nil {
		if start := matchingGitFailure(err, workflowstore.SourceVaultFailureVaultGitStartFailed); start != nil {
			return start
		}
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
	}
	if strings.TrimSpace(result) != "true" {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
	}
	return nil
}

func (g *commandGit) VerifySource(ctx context.Context, sourcePath, commitOID, treeOID string) error {
	if err := requireObjectType(ctx, sourcePath, false, commitOID, "commit", workflowstore.SourceVaultFailureSourceCommitMissing, workflowstore.SourceVaultFailureSourceCommitTypeMismatch); err != nil {
		return err
	}
	if err := requireObjectType(ctx, sourcePath, false, treeOID, "tree", workflowstore.SourceVaultFailureSourceTreeMissing, workflowstore.SourceVaultFailureSourceTreeTypeMismatch); err != nil {
		return err
	}
	derived, err := runGit(ctx, workflowstore.SourceVaultFailureSourceGitStartFailed, sourcePath, false, "rev-parse", "--verify", "--end-of-options", commitOID+"^{tree}")
	if err != nil {
		if start := matchingGitFailure(err, workflowstore.SourceVaultFailureSourceGitStartFailed); start != nil {
			return start
		}
		return &gitFailure{reason: workflowstore.SourceVaultFailureSourceTreeMismatch, code: CodeObjectMismatch, err: err}
	}
	if strings.TrimSpace(derived) != treeOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureSourceTreeMismatch, code: CodeObjectMismatch}
	}
	return nil
}

func (g *commandGit) ImportClosure(ctx context.Context, sourcePath, vaultPath, commitOID string) error {
	producer := gitCommand(ctx, sourcePath, false, "pack-objects", "--stdout", "--revs")
	producer.Stdin = strings.NewReader(commitOID + "\n")
	producerErr := newLimitedBuffer(gitDiagnosticLimit)
	producer.Stderr = producerErr
	pipe, err := producer.StdoutPipe()
	if err != nil {
		return &gitFailure{reason: workflowstore.SourceVaultFailureSourceGitStartFailed, code: CodeSourceObjectUnavailable, err: err}
	}

	consumer := gitCommand(ctx, vaultPath, true, "index-pack", "--stdin", "--fix-thin")
	consumer.Stdin = pipe
	consumerOut := newLimitedBuffer(gitDiagnosticLimit)
	consumerErr := newLimitedBuffer(gitDiagnosticLimit)
	consumer.Stdout = consumerOut
	consumer.Stderr = consumerErr
	if err := consumer.Start(); err != nil {
		_ = pipe.Close()
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultGitStartFailed, code: CodeVaultUnavailable, err: err}
	}
	if err := producer.Start(); err != nil {
		_ = pipe.Close()
		killProcess(consumer)
		_ = consumer.Wait()
		return &gitFailure{reason: workflowstore.SourceVaultFailureSourceGitStartFailed, code: CodeSourceObjectUnavailable, err: err}
	}
	_ = pipe.Close()

	type processResult struct {
		stage string
		err   error
	}
	results := make(chan processResult, 2)
	go func() {
		results <- processResult{stage: "producer", err: producer.Wait()}
	}()
	go func() {
		results <- processResult{stage: "consumer", err: consumer.Wait()}
	}()

	first := <-results
	if first.err != nil || ctx.Err() != nil {
		killProcess(producer)
		killProcess(consumer)
	}
	second := <-results
	if ctx.Err() != nil {
		return &gitFailure{
			reason: workflowstore.SourceVaultFailureOperationCancelled,
			code:   CodeOperationCancelled,
			err:    ctx.Err(),
		}
	}
	failed := first
	if failed.err == nil {
		failed = second
	}
	if failed.err == nil {
		return nil
	}
	if failed.stage == "producer" {
		return &gitFailure{
			reason: workflowstore.SourceVaultFailurePackGenerationFailed,
			code:   CodeSourceObjectUnavailable,
			err:    commandFailure(failed.err, producerErr.String()),
		}
	}
	return &gitFailure{
		reason: workflowstore.SourceVaultFailurePackIndexFailed,
		code:   CodeVaultUnavailable,
		err:    commandFailure(failed.err, consumerErr.String()),
	}
}

func killProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (g *commandGit) VerifyVaultClosure(ctx context.Context, vaultPath, commitOID, treeOID, refName string) error {
	if err := requireObjectType(ctx, vaultPath, true, commitOID, "commit", workflowstore.SourceVaultFailureVaultCommitMissing, workflowstore.SourceVaultFailureVaultCommitTypeMismatch); err != nil {
		return err
	}
	if err := requireObjectType(ctx, vaultPath, true, treeOID, "tree", workflowstore.SourceVaultFailureVaultTreeMissing, workflowstore.SourceVaultFailureVaultTreeTypeMismatch); err != nil {
		return err
	}
	derived, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "rev-parse", "--verify", "--end-of-options", commitOID+"^{tree}")
	if err != nil {
		if start := matchingGitFailure(err, workflowstore.SourceVaultFailureVaultGitStartFailed); start != nil {
			return start
		}
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultTreeMismatch, code: CodeObjectMismatch, err: err}
	}
	if strings.TrimSpace(derived) != treeOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultTreeMismatch, code: CodeObjectMismatch}
	}
	refOID, exists, err := g.ReadRef(ctx, vaultPath, refName)
	if err != nil {
		return err
	}
	if !exists {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMissing, code: CodeVaultUnavailable}
	}
	if refOID != commitOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
	}
	return nil
}

func (g *commandGit) ReadRef(ctx context.Context, vaultPath, refName string) (string, bool, error) {
	cmd := gitCommand(ctx, vaultPath, true, "show-ref", "--verify", "--hash", refName)
	stdout := newLimitedBuffer(gitDiagnosticLimit)
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", false, &gitFailure{reason: workflowstore.SourceVaultFailureVaultGitStartFailed, code: CodeVaultUnavailable, err: err}
	}
	err := cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == 128) {
			return "", false, nil
		}
		return "", false, &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: commandFailure(err, stderr.String())}
	}
	oid := strings.TrimSpace(stdout.String())
	if !validOID(oid) {
		return "", false, &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
	}
	return oid, true, nil
}

func (g *commandGit) CreateRef(ctx context.Context, vaultPath, refName, commitOID string) error {
	if _, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "update-ref", refName, commitOID, zeroOID); err != nil {
		if start := matchingGitFailure(err, workflowstore.SourceVaultFailureVaultGitStartFailed); start != nil {
			return start
		}
		oid, exists, readErr := g.ReadRef(ctx, vaultPath, refName)
		if readErr == nil && exists && oid == commitOID {
			return nil
		}
		if readErr == nil && exists {
			return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
		}
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefCreateFailed, code: CodeVaultUnavailable, err: err}
	}
	return nil
}

func (g *commandGit) DeleteRef(ctx context.Context, vaultPath, refName, commitOID string) error {
	if _, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "update-ref", "-d", refName, commitOID); err != nil {
		if start := matchingGitFailure(err, workflowstore.SourceVaultFailureVaultGitStartFailed); start != nil {
			return start
		}
		oid, exists, readErr := g.ReadRef(ctx, vaultPath, refName)
		if readErr == nil && !exists {
			return nil
		}
		if readErr == nil && exists && oid != commitOID {
			return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
		}
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefDeleteFailed, code: CodeVaultUnavailable, err: err}
	}
	return nil
}

func (g *commandGit) ReadObject(ctx context.Context, vaultPath, oid, expectedType string, maxBytes int64) ([]byte, error) {
	if err := requireObjectType(ctx, vaultPath, true, oid, expectedType, "", ""); err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	cmd := gitCommand(ctx, vaultPath, true, "cat-file", expectedType, oid)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes+1))
	if int64(len(data)) > maxBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, &Error{Code: CodeObjectLimitExceeded}
	}
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	return data, nil
}

func (g *commandGit) ReadTree(ctx context.Context, vaultPath, treeOID string) ([]RetainedTreeEntry, error) {
	if err := requireObjectType(ctx, vaultPath, true, treeOID, "tree", "", ""); err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	cmd := gitCommand(ctx, vaultPath, true, "cat-file", "tree", treeOID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	entries, readErr := parseRawTree(stdout)
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	return entries, nil
}

func (g *commandGit) ReadBlobRange(ctx context.Context, vaultPath, blobOID string, offset, limit int64) (ReadRetainedBlobRangeResult, error) {
	if offset < 0 || limit <= 0 {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	if err := requireObjectType(ctx, vaultPath, true, blobOID, "blob", "", ""); err != nil {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	sizeText, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "cat-file", "-s", blobOID)
	if err != nil {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	totalSize, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
	if err != nil || totalSize < 0 {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if offset > totalSize {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	if offset == totalSize {
		return ReadRetainedBlobRangeResult{BlobOID: blobOID, Offset: offset, TotalSize: totalSize, Bytes: []byte{}}, nil
	}
	cmd := gitCommand(ctx, vaultPath, true, "cat-file", "blob", blobOID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, stdout, offset); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
		}
	}
	length := totalSize - offset
	if length > limit {
		length = limit
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(stdout, data); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if _, err := io.Copy(io.Discard, stdout); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if err := cmd.Wait(); err != nil {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	return ReadRetainedBlobRangeResult{BlobOID: blobOID, Offset: offset, TotalSize: totalSize, Bytes: data}, nil
}

func parseRawTree(reader io.Reader) ([]RetainedTreeEntry, error) {
	buffered := bufio.NewReader(reader)
	entries := make([]RetainedTreeEntry, 0)
	for {
		modeBytes, err := buffered.ReadBytes(' ')
		if err == io.EOF && len(modeBytes) == 0 {
			break
		}
		if err != nil || len(modeBytes) < 2 || modeBytes[len(modeBytes)-1] != ' ' {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
		name, err := buffered.ReadBytes(0)
		if err != nil || len(name) < 2 || name[len(name)-1] != 0 {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
		name = append([]byte(nil), name[:len(name)-1]...)
		if len(name) == 0 || bytes.IndexByte(name, '/') >= 0 || bytes.Equal(name, []byte(".")) || bytes.Equal(name, []byte("..")) {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
		oidBytes := make([]byte, 20)
		if _, err := io.ReadFull(buffered, oidBytes); err != nil {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
		mode, objectType, ok := normalizeTreeMode(string(modeBytes[:len(modeBytes)-1]))
		if !ok {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
		entries = append(entries, RetainedTreeEntry{Name: name, Mode: mode, ObjectType: objectType, ObjectOID: hex.EncodeToString(oidBytes)})
	}
	sort.Slice(entries, func(left, right int) bool { return bytes.Compare(entries[left].Name, entries[right].Name) < 0 })
	for index := 1; index < len(entries); index++ {
		if bytes.Equal(entries[index-1].Name, entries[index].Name) {
			return nil, &Error{Code: CodeObjectUnavailable}
		}
	}
	return entries, nil
}

func normalizeTreeMode(value string) (string, string, bool) {
	switch value {
	case "40000", "040000":
		return "040000", "tree", true
	case "100644":
		return "100644", "blob", true
	case "100755":
		return "100755", "blob", true
	case "120000":
		return "120000", "blob", true
	case "160000":
		return "160000", "commit", true
	default:
		return "", "", false
	}
}
func (g *commandGit) GarbageCollect(ctx context.Context, vaultPath string) error {
	_, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, vaultPath, true, "gc", "--prune=now")
	return err
}

func requireObjectType(
	ctx context.Context,
	path string,
	bare bool,
	oid string,
	want string,
	missingReason string,
	wrongTypeReason string,
) error {
	startReason := chooseStartReason(bare)
	result, err := runGit(ctx, startReason, path, bare, "cat-file", "-t", oid)
	if err != nil {
		if start := matchingGitFailure(err, startReason); start != nil {
			return start
		}
		return &gitFailure{reason: missingReason, code: chooseUnavailableCode(bare), err: err}
	}
	if strings.TrimSpace(result) != want {
		return &gitFailure{reason: wrongTypeReason, code: CodeObjectMismatch}
	}
	return nil
}

func matchingGitFailure(err error, reason string) *gitFailure {
	var failure *gitFailure
	if errors.As(err, &failure) && failure.reason == reason {
		return failure
	}
	return nil
}

func chooseStartReason(bare bool) string {
	if bare {
		return workflowstore.SourceVaultFailureVaultGitStartFailed
	}
	return workflowstore.SourceVaultFailureSourceGitStartFailed
}

func chooseUnavailableCode(bare bool) string {
	if bare {
		return CodeVaultUnavailable
	}
	return CodeSourceObjectUnavailable
}

func runGit(ctx context.Context, startReason, path string, bare bool, args ...string) (string, error) {
	cmd := gitCommand(ctx, path, bare, args...)
	stdout := newLimitedBuffer(gitDiagnosticLimit)
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		code := CodeInternal
		switch startReason {
		case workflowstore.SourceVaultFailureSourceGitStartFailed:
			code = CodeSourceObjectUnavailable
		case workflowstore.SourceVaultFailureVaultGitStartFailed:
			code = CodeVaultUnavailable
		}
		return "", &gitFailure{reason: startReason, code: code, err: err}
	}
	if err := cmd.Wait(); err != nil {
		code := CodeInternal
		switch startReason {
		case workflowstore.SourceVaultFailureSourceGitStartFailed:
			code = CodeSourceObjectUnavailable
		case workflowstore.SourceVaultFailureVaultGitStartFailed:
			code = CodeVaultUnavailable
		}
		return "", &gitFailure{code: code, err: commandFailure(err, stderr.String())}
	}
	return stdout.String(), nil
}

func gitCommand(ctx context.Context, path string, bare bool, args ...string) *exec.Cmd {
	base := []string{"--no-replace-objects"}
	if path != "" {
		if bare {
			base = append(base, "--git-dir", path)
		} else {
			base = append(base, "-C", path)
		}
	}
	base = append(base, args...)
	cmd := exec.CommandContext(ctx, "git", base...)
	cmd.Env = controlledGitEnvironment()
	return cmd
}

func controlledGitEnvironment() []string {
	values := make([]string, 0, len(os.Environ())+5)
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if !ok || strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		values = append(values, value)
	}
	return append(
		values,
		"GIT_NO_LAZY_FETCH=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_ATTR_NOSYSTEM=1",
	)
}

func validateVaultStorage(vaultPath string) error {
	for _, relative := range []string{
		"objects",
		filepath.Join("objects", "info"),
		filepath.Join("objects", "pack"),
		"refs",
	} {
		path := filepath.Join(vaultPath, relative)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
		}
	}
	for _, relative := range []string{
		filepath.Join("objects", "info", "alternates"),
		filepath.Join("objects", "info", "http-alternates"),
	} {
		_, err := os.Lstat(filepath.Join(vaultPath, relative))
		if err == nil {
			return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return &gitFailure{reason: workflowstore.SourceVaultFailureVaultInvalid, code: CodeVaultUnavailable, err: err}
		}
	}
	return nil
}

func commandFailure(err error, diagnostic string) error {
	if diagnostic == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, diagnostic)
}

func validOID(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

type limitedBuffer struct {
	limit int
	buf   bytes.Buffer
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buf.Write(value)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	return strings.TrimSpace(b.buf.String())
}
