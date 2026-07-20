package sourcevault

import (
	"bufio"
	"bytes"
	"context"
	"io"
	workflowstore "relay/internal/store/workflow"
	"strconv"
	"strings"
)

type gitRangeResult struct {
	Offset, TotalSize int64
	Bytes             []byte
}

func readGitObjectRange(ctx context.Context, path, oid, typ string, offset, limit int64) (gitRangeResult, error) {
	if offset < 0 || limit <= 0 || limit > MaxObjectReadBytes {
		return gitRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	if err := requireObjectType(ctx, path, true, oid, typ, "", ""); err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	sizeText, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, path, true, "cat-file", "-s", oid)
	if err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	total, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
	if err != nil || total < 0 || offset > total {
		return gitRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	if offset == total {
		return gitRangeResult{Offset: offset, TotalSize: total}, nil
	}
	cmd := gitCommand(ctx, path, true, "cat-file", typ, oid)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	cmd.Stderr = newLimitedBuffer(gitDiagnosticLimit)
	if err := cmd.Start(); err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, out, offset); err != nil {
			killProcess(cmd)
			_ = cmd.Wait()
			return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
		}
	}
	length := total - offset
	if length > limit {
		length = limit
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(out, data); err != nil {
		killProcess(cmd)
		_ = cmd.Wait()
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if _, err := io.Copy(io.Discard, out); err != nil {
		killProcess(cmd)
		_ = cmd.Wait()
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if err := cmd.Wait(); err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	return gitRangeResult{Offset: offset, TotalSize: total, Bytes: data}, nil
}

func readGitCommitNode(ctx context.Context, path, oid string) (RetainedCommitNode, error) {
	if err := requireObjectType(ctx, path, true, oid, "commit", "", ""); err != nil {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	sizeText, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, path, true, "cat-file", "-s", oid)
	if err != nil {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	total, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
	if err != nil || total < 0 {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	cmd := gitCommand(ctx, path, true, "cat-file", "commit", oid)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	cmd.Stderr = newLimitedBuffer(gitDiagnosticLimit)
	if err := cmd.Start(); err != nil {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	br := bufio.NewReader(out)
	var headers []RetainedCommitHeader
	var current []byte
	var consumed int64
	for {
		line, readErr := br.ReadBytes('\n')
		if readErr != nil || len(line) == 0 || line[len(line)-1] != '\n' {
			killProcess(cmd)
			_ = cmd.Wait()
			return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
		}
		consumed += int64(len(line))
		if bytes.Equal(line, []byte{'\n'}) {
			if len(current) == 0 {
				killProcess(cmd)
				_ = cmd.Wait()
				return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
			}
			h, e := parseCommitHeaderRecord(current)
			if e != nil {
				killProcess(cmd)
				_ = cmd.Wait()
				return RetainedCommitNode{}, e
			}
			headers = append(headers, h)
			break
		}
		if line[0] == ' ' {
			if len(current) == 0 {
				killProcess(cmd)
				_ = cmd.Wait()
				return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
			}
			current = append(current, line...)
			continue
		}
		if len(current) > 0 {
			h, e := parseCommitHeaderRecord(current)
			if e != nil {
				killProcess(cmd)
				_ = cmd.Wait()
				return RetainedCommitNode{}, e
			}
			headers = append(headers, h)
		}
		current = append(current[:0], line...)
	}
	remaining, e := io.Copy(io.Discard, br)
	waitErr := cmd.Wait()
	if e != nil || waitErr != nil || consumed+remaining != total {
		return RetainedCommitNode{}, &Error{Code: CodeObjectUnavailable}
	}
	return commitNodeFromHeaders(oid, headers, total, consumed)
}
func parseCommitNode(oid string, raw []byte) (RetainedCommitNode, error) {
	sep := bytes.Index(raw, []byte("\n\n"))
	if sep < 0 {
		return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
	}
	headers, err := parseCommitHeaders(raw[:sep+1])
	if err != nil {
		return RetainedCommitNode{}, err
	}
	return commitNodeFromHeaders(oid, headers, int64(len(raw)), int64(sep+2))
}
func commitNodeFromHeaders(oid string, headers []RetainedCommitHeader, rawSize, messageOffset int64) (RetainedCommitNode, error) {
	if !validOID(oid) || rawSize < 0 || messageOffset <= 0 || messageOffset > rawSize {
		return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
	}
	n := RetainedCommitNode{CommitOID: oid, Headers: headers, RawSize: rawSize, MessageOffset: messageOffset, MessageSize: rawSize - messageOffset}
	for _, h := range headers {
		switch string(h.Name) {
		case "tree":
			if n.TreeOID != "" || !validOID(string(h.Value)) {
				return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
			}
			n.TreeOID = string(h.Value)
		case "parent":
			if !validOID(string(h.Value)) {
				return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
			}
			n.ParentOIDs = append(n.ParentOIDs, string(h.Value))
		}
	}
	if n.TreeOID == "" {
		return RetainedCommitNode{}, &Error{Code: CodeObjectMismatch}
	}
	return n, nil
}
func parseCommitHeaders(block []byte) ([]RetainedCommitHeader, error) {
	if len(block) == 0 || block[len(block)-1] != '\n' {
		return nil, &Error{Code: CodeObjectMismatch}
	}
	var result []RetainedCommitHeader
	start := 0
	for pos := 0; pos < len(block); {
		end := bytes.IndexByte(block[pos:], '\n')
		if end < 0 {
			return nil, &Error{Code: CodeObjectMismatch}
		}
		end += pos
		if pos > start && block[pos] != ' ' {
			h, e := parseCommitHeaderRecord(block[start:pos])
			if e != nil {
				return nil, e
			}
			result = append(result, h)
			start = pos
		}
		if pos == start && block[pos] == ' ' {
			return nil, &Error{Code: CodeObjectMismatch}
		}
		pos = end + 1
	}
	h, e := parseCommitHeaderRecord(block[start:])
	if e != nil {
		return nil, e
	}
	return append(result, h), nil
}
func parseCommitHeaderRecord(raw []byte) (RetainedCommitHeader, error) {
	firstEnd := bytes.IndexByte(raw, '\n')
	if firstEnd < 0 {
		return RetainedCommitHeader{}, &Error{Code: CodeObjectMismatch}
	}
	first := raw[:firstEnd]
	space := bytes.IndexByte(first, ' ')
	if space <= 0 {
		return RetainedCommitHeader{}, &Error{Code: CodeObjectMismatch}
	}
	name := append([]byte(nil), first[:space]...)
	value := append([]byte(nil), first[space+1:]...)
	for pos := firstEnd + 1; pos < len(raw); {
		end := bytes.IndexByte(raw[pos:], '\n')
		if end < 0 {
			return RetainedCommitHeader{}, &Error{Code: CodeObjectMismatch}
		}
		end += pos
		if len(raw[pos:end]) == 0 || raw[pos] != ' ' {
			return RetainedCommitHeader{}, &Error{Code: CodeObjectMismatch}
		}
		value = append(value, '\n')
		value = append(value, raw[pos:end]...)
		pos = end + 1
	}
	return RetainedCommitHeader{Name: name, Value: value, Raw: append([]byte(nil), raw...)}, nil
}

func readGitDiffRange(ctx context.Context, path, before, after string, offset, limit int64) (gitRangeResult, error) {
	if offset < 0 || limit <= 0 || limit > MaxObjectReadBytes || !validOID(before) || !validOID(after) {
		return gitRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	for _, oid := range []string{before, after} {
		if err := requireObjectType(ctx, path, true, oid, "commit", "", ""); err != nil {
			return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
		}
	}
	cmd := gitCommand(ctx, path, true, "-c", "color.ui=false", "-c", "core.quotePath=true", "-c", "diff.algorithm=myers", "-c", "diff.indentHeuristic=false", "-c", "diff.mnemonicPrefix=false", "-c", "diff.noprefix=false", "diff-tree", "--no-commit-id", "-r", "-p", "--binary", "--full-index", "--find-renames=100%", "--find-copies=100%", "--find-copies-harder", "--no-ext-diff", "--no-textconv", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", before, after, "--")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	cmd.Stderr = newLimitedBuffer(gitDiagnosticLimit)
	if err := cmd.Start(); err != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, out, offset); err != nil {
			killProcess(cmd)
			_ = cmd.Wait()
			return gitRangeResult{}, &Error{Code: CodeInvalidRequest}
		}
	}
	data, e := io.ReadAll(io.LimitReader(out, limit))
	remaining, de := io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if e != nil || de != nil || waitErr != nil {
		return gitRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	return gitRangeResult{Offset: offset, TotalSize: offset + int64(len(data)) + remaining, Bytes: data}, nil
}
