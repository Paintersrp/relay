package sourcegateway

import (
	"bytes"
	"container/heap"
	"context"

	"relay/internal/app/operations"
	"relay/internal/sourcevault"
)

type treeCandidate struct {
	path  []byte
	entry sourcevault.RetainedTreeEntry
}
type candidateHeap []treeCandidate

func (h candidateHeap) Len() int { return len(h) }
func (h candidateHeap) Less(left, right int) bool {
	return bytes.Compare(h[left].path, h[right].path) < 0
}
func (h candidateHeap) Swap(left, right int) { h[left], h[right] = h[right], h[left] }
func (h *candidateHeap) Push(value any)      { *h = append(*h, value.(treeCandidate)) }
func (h *candidateHeap) Pop() any {
	values := *h
	last := len(values) - 1
	value := values[last]
	*h = values[:last]
	return value
}

func (s *Service) ListTree(ctx context.Context, request ListTreeRequest) (ListTreeResult, error) {
	if request.Limit <= 0 || request.Limit > MaxTreePageEntries {
		return ListTreeResult{}, &Error{Code: CodeInvalidRequest}
	}
	authority, err := s.resolveAuthority(ctx, request.PacketID, request.SurfaceContract, request.OperationID, request.RepositoryKey)
	if err != nil {
		return ListTreeResult{}, err
	}
	directory, err := s.resolvePathReference(ctx, authority, request.Directory, true)
	if err != nil {
		return ListTreeResult{}, err
	}
	directoryIdentity, err := s.makePathIdentity(ctx, authority, directory)
	if err != nil {
		return ListTreeResult{}, err
	}
	treeOID, err := s.resolveDirectoryTree(ctx, authority, directory)
	if err != nil {
		return ListTreeResult{}, err
	}
	fingerprint := treeFingerprint(authority, directoryIdentity.PathID, request.Recursive, request.Limit)
	var after []byte
	if request.Cursor != "" {
		cursor, decodeErr := s.cursors.Decode(request.Cursor)
		if decodeErr != nil || !cursorMatchesAuthority(cursor, authority, fingerprint, "tree") || cursor.NextOffset != 0 {
			return ListTreeResult{}, &Error{Code: CodeInvalidCursor}
		}
		after, err = s.resolvePathReference(ctx, authority, cursor.AfterPath, false)
		if err != nil || !withinDirectory(directory, after) {
			return ListTreeResult{}, &Error{Code: CodeInvalidCursor}
		}
	}
	candidates, err := s.initialCandidates(ctx, authority, directory, treeOID)
	if err != nil {
		return ListTreeResult{}, err
	}
	values := make([]treeCandidate, 0, request.Limit+1)
	for candidates.Len() > 0 && len(values) < request.Limit+1 {
		current := heap.Pop(candidates).(treeCandidate)
		if request.Recursive && current.entry.ObjectType == "tree" {
			children, childErr := s.readTree(ctx, authority, current.entry.ObjectOID)
			if childErr != nil {
				return ListTreeResult{}, childErr
			}
			for _, child := range children {
				heap.Push(candidates, treeCandidate{path: joinPath(current.path, child.Name), entry: child})
			}
		}
		if len(after) > 0 && bytes.Compare(current.path, after) <= 0 {
			continue
		}
		values = append(values, current)
	}
	complete := len(values) <= request.Limit
	if !complete {
		values = values[:request.Limit]
	}
	entries := make([]TreeEntry, 0, len(values))
	for _, value := range values {
		pathIdentity, identityErr := s.makePathIdentity(ctx, authority, value.path)
		if identityErr != nil {
			return ListTreeResult{}, identityErr
		}
		basenameIdentity, identityErr := s.makePathIdentity(ctx, authority, basename(value.path))
		if identityErr != nil {
			return ListTreeResult{}, identityErr
		}
		entries = append(entries, TreeEntry{Path: pathIdentity, Basename: basenameIdentity, Mode: value.entry.Mode, ObjectType: value.entry.ObjectType, ObjectOID: value.entry.ObjectOID, Directory: value.entry.ObjectType == "tree"})
	}
	result := ListTreeResult{Source: sourceIdentity(authority), Directory: directoryIdentity, Entries: entries, Complete: complete}
	if !complete {
		if len(entries) == 0 {
			return ListTreeResult{}, &Error{Code: CodeInternalFailure}
		}
		result.Cursor, err = s.cursors.Encode(cursorPayload{Version: CursorVersion, Kind: "tree", PacketID: authority.Summary.PacketID, PacketSHA256: authority.Summary.PacketSHA256, SurfaceContract: string(authority.Summary.SurfaceContract), OperationID: string(authority.Summary.OperationID), ProjectID: authority.Summary.ProjectID, RepositoryKey: authority.RepositoryKey, PublicationID: authority.PublicationID, VaultRelationshipRowID: authority.Relationship.ID, CommitOID: authority.Relationship.CommitOID, TreeOID: authority.Relationship.TreeOID, RequestFingerprint: fingerprint, AfterPath: referenceFromIdentity(entries[len(entries)-1].Path)})
		if err != nil {
			return ListTreeResult{}, err
		}
	}
	return result, nil
}
func (s *Service) resolveDirectoryTree(ctx context.Context, authority operations.SourceReadAuthority, path []byte) (string, error) {
	if len(path) == 0 {
		return authority.Relationship.TreeOID, nil
	}
	current := authority.Relationship.TreeOID
	for _, component := range bytes.Split(path, []byte{'/'}) {
		entries, err := s.readTree(ctx, authority, current)
		if err != nil {
			return "", err
		}
		entry, ok := exactEntry(entries, component)
		if !ok || entry.ObjectType != "tree" {
			return "", &Error{Code: CodePathAbsent}
		}
		current = entry.ObjectOID
	}
	return current, nil
}
func (s *Service) resolvePathEntry(ctx context.Context, authority operations.SourceReadAuthority, path []byte) (sourcevault.RetainedTreeEntry, error) {
	if !validatePath(path, false) {
		return sourcevault.RetainedTreeEntry{}, &Error{Code: CodeInvalidRequest}
	}
	components := bytes.Split(path, []byte{'/'})
	current := authority.Relationship.TreeOID
	for index, component := range components {
		entries, err := s.readTree(ctx, authority, current)
		if err != nil {
			return sourcevault.RetainedTreeEntry{}, err
		}
		entry, ok := exactEntry(entries, component)
		if !ok {
			return sourcevault.RetainedTreeEntry{}, &Error{Code: CodePathAbsent}
		}
		if index == len(components)-1 {
			return entry, nil
		}
		if entry.ObjectType != "tree" {
			return sourcevault.RetainedTreeEntry{}, &Error{Code: CodePathAbsent}
		}
		current = entry.ObjectOID
	}
	return sourcevault.RetainedTreeEntry{}, &Error{Code: CodePathAbsent}
}
func (s *Service) initialCandidates(ctx context.Context, authority operations.SourceReadAuthority, directory []byte, treeOID string) (*candidateHeap, error) {
	entries, err := s.readTree(ctx, authority, treeOID)
	if err != nil {
		return nil, err
	}
	values := candidateHeap{}
	heap.Init(&values)
	for _, entry := range entries {
		heap.Push(&values, treeCandidate{path: joinPath(directory, entry.Name), entry: entry})
	}
	return &values, nil
}
func (s *Service) readTree(ctx context.Context, authority operations.SourceReadAuthority, treeOID string) ([]sourcevault.RetainedTreeEntry, error) {
	result, err := s.vault.ReadRetainedTree(ctx, sourcevault.ReadRetainedTreeRequest{Relationship: authority.Relationship, TreeOID: treeOID})
	if err != nil {
		return nil, mapVaultError(err)
	}
	if result.TreeOID != treeOID {
		return nil, &Error{Code: CodeObjectMismatch}
	}
	return result.Entries, nil
}
func exactEntry(entries []sourcevault.RetainedTreeEntry, name []byte) (sourcevault.RetainedTreeEntry, bool) {
	for _, entry := range entries {
		comparison := bytes.Compare(entry.Name, name)
		if comparison == 0 {
			return entry, true
		}
		if comparison > 0 {
			break
		}
	}
	return sourcevault.RetainedTreeEntry{}, false
}
func withinDirectory(directory, path []byte) bool {
	if len(directory) == 0 {
		return len(path) > 0
	}
	return len(path) > len(directory) && bytes.Equal(path[:len(directory)], directory) && path[len(directory)] == '/'
}
