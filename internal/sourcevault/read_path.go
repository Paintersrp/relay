package sourcevault

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode"
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
	resolved, err := m.git.ResolvePath(ctx, vaultPath, closure.CommitOID, request.Path)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	if resolved.ObjectType != "blob" || (resolved.Mode != "100644" && resolved.Mode != "100755") {
		return ReadPathResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if !validOID(resolved.ObjectOID) {
		return ReadPathResult{}, &Error{Code: CodeObjectUnavailable}
	}
	data, err := m.git.ReadObject(ctx, vaultPath, resolved.ObjectOID, "blob", request.MaxBytes)
	if err != nil {
		return ReadPathResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	return ReadPathResult{ObjectOID: resolved.ObjectOID, Bytes: append([]byte(nil), data...)}, nil
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
		if unicode.IsControl(r) {
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
	return ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) && path[1] == ':'
}

// ResolvePath resolves a repository-relative path within the retained vault to
// its Git object type and OID. It uses git ls-tree against the supplied commit
// and rejects ambiguous, multiple, or malformed results.
func (g *commandGit) ResolvePath(ctx context.Context, vaultPath, commitOID, path string) (resolvedPath, error) {
	cmd := gitCommand(ctx, vaultPath, true, "ls-tree", "-z", commitOID, "--", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	stderr := newLimitedBuffer(gitDiagnosticLimit)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return resolvedPath{}, &gitFailure{reason: workflowstore.SourceVaultFailureVaultGitStartFailed, code: CodeVaultUnavailable, err: err}
	}

	stdoutBytes, readErr := io.ReadAll(io.LimitReader(stdout, gitDiagnosticLimit+1))
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		return resolvedPath{}, &gitFailure{reason: workflowstore.SourceVaultFailureOperationCancelled, code: CodeOperationCancelled, err: ctx.Err()}
	}
	if readErr != nil || waitErr != nil {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	if len(stdoutBytes) == 0 || stdoutBytes[len(stdoutBytes)-1] != 0 {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	entries := bytes.Split(stdoutBytes, []byte{0})
	if len(entries) != 2 || len(entries[1]) != 0 {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}

	mode, objectType, oid, entryPath, ok := parseLsTreeEntry(string(entries[0]))
	if !ok || entryPath != path {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	if objectType != "blob" {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	if mode != "100644" && mode != "100755" {
		return resolvedPath{}, &Error{Code: CodeObjectUnavailable}
	}
	return resolvedPath{Mode: mode, ObjectType: objectType, ObjectOID: oid}, nil
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
