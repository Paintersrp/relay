package sourcevault

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workflowstore "relay/internal/store/workflow"
)

// ReadPath resolves a repository-relative path within a retained source-vault
// closure and returns the exact blob bytes and object OID. The closure must be
// ready, the vault must be valid, and at least one active retention must hold
// the closure under the existing source-vault rules.
func (m *Manager) ReadPath(ctx context.Context, request ReadPathRequest) (ReadPathResult, error) {
	if m == nil {
		return ReadPathResult{}, &Error{Code: CodeInvalidRequest}
	}
	if err := validateReadPathRequest(request); err != nil {
		return ReadPathResult{}, err
	}
	closure, err := m.store.GetSourceVaultClosureByClosureID(ctx, request.ClosureID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closure.State != workflowstore.SourceVaultClosureStateReady) {
		return ReadPathResult{}, &Error{Code: CodeVaultUnavailable}
	}
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	active, err := m.store.CountActiveSourceVaultRetentions(ctx, closure.ID)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	if active == 0 {
		return ReadPathResult{}, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.sourceVaultForClosure(ctx, closure)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeDatabaseFailure)
	}
	unlock := m.lockVault(vault.VaultID)
	defer unlock()
	vaultPath, err := m.git.VaultPath(vault.RelativePath)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeVaultUnavailable)
	}
	if err := m.git.ValidateVault(ctx, vaultPath); err != nil {
		return ReadPathResult{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailureVaultInvalid)
	}
	if err := m.git.VerifyVaultClosure(ctx, vaultPath, closure.CommitOID, closure.TreeOID, closure.RefName); err != nil {
		return ReadPathResult{}, m.failClosure(ctx, closure, err, workflowstore.SourceVaultFailurePostImportVerification)
	}
	oid, objectType, err := m.git.ResolvePath(ctx, vaultPath, closure.CommitOID, request.Path)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	if objectType != "blob" {
		return ReadPathResult{}, &Error{Code: CodeObjectUnavailable}
	}
	data, err := m.git.ReadObject(ctx, vaultPath, oid, "blob", request.MaxBytes)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	return ReadPathResult{ObjectOID: oid, Bytes: append([]byte(nil), data...)}, nil
}

func validateReadPathRequest(request ReadPathRequest) error {
	if strings.TrimSpace(request.ClosureID) != request.ClosureID || request.ClosureID == "" {
		return &Error{Code: CodeInvalidRequest}
	}
	if request.Path == "" || strings.TrimSpace(request.Path) != request.Path {
		return &Error{Code: CodeInvalidRequest}
	}
	if strings.HasPrefix(request.Path, "/") || strings.HasPrefix(request.Path, "\\") {
		return &Error{Code: CodeInvalidRequest}
	}
	if isWindowsDrivePath(request.Path) {
		return &Error{Code: CodeInvalidRequest}
	}
	if strings.Contains(request.Path, "\\") {
		return &Error{Code: CodeInvalidRequest}
	}
	for _, segment := range strings.Split(request.Path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return &Error{Code: CodeInvalidRequest}
		}
	}
	if !utf8.ValidString(request.Path) {
		return &Error{Code: CodeInvalidRequest}
	}
	for _, r := range request.Path {
		if r < 0x20 {
			return &Error{Code: CodeInvalidRequest}
		}
	}
	if filepath.Base(request.Path) == "" || strings.TrimSpace(filepath.Base(request.Path)) != filepath.Base(request.Path) {
		return &Error{Code: CodeInvalidRequest}
	}
	if request.MaxBytes <= 0 || request.MaxBytes > MaxObjectReadBytes {
		return &Error{Code: CodeInvalidRequest}
	}
	return nil
}

func isWindowsDrivePath(path string) bool {
	if len(path) < 2 {
		return false
	}
	c := path[0]
	return (c >= 'A' && c <= 'Z') && path[1] == ':'
}

// ResolvePath resolves a repository-relative path within the retained vault to
// its Git object type and OID. It uses git ls-tree against the supplied commit
// and rejects ambiguous, multiple, or malformed results.
func (g *commandGit) ResolvePath(ctx context.Context, vaultPath, commitOID, path string) (string, string, error) {
	cmd := gitCommand(ctx, vaultPath, true, "ls-tree", "-z", commitOID, "--", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", &Error{Code: CodeObjectUnavailable}
	}
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", "", &gitFailure{reason: workflowstore.SourceVaultFailureVaultGitStartFailed, code: CodeVaultUnavailable, err: err}
	}
	defer func() { _ = cmd.Wait() }()

	buffered := bufio.NewReader(stdout)
	line, err := buffered.ReadBytes(0)
	if err == io.EOF && len(line) == 0 {
		return "", "", &Error{Code: CodeObjectUnavailable}
	}
	if err != nil {
		return "", "", &Error{Code: CodeObjectUnavailable}
	}
	if len(line) > 0 && line[len(line)-1] == 0 {
		line = line[:len(line)-1]
	}
	if _, peekErr := buffered.ReadByte(); peekErr != io.EOF {
		return "", "", &Error{Code: CodeObjectUnavailable}
	}
	mode, objectType, oid, resolvedPath, ok := parseLsTreeEntry(string(line))
	if !ok || resolvedPath != path {
		return "", "", &Error{Code: CodeObjectUnavailable}
	}
	_ = mode
	return oid, objectType, nil
}

func parseLsTreeEntry(line string) (mode, objectType, oid, path string, ok bool) {
	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return "", "", "", "", false
	}
	meta := strings.Split(parts[0], " ")
	if len(meta) != 3 {
		return "", "", "", "", false
	}
	mode, objectType, oid = meta[0], meta[1], meta[2]
	if !validOID(oid) {
		return "", "", "", "", false
	}
	return mode, objectType, oid, parts[1], true
}
