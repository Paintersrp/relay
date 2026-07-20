package sourcegateway

import (
	"bytes"
	"context"
	"relay/internal/app/operations"
	"strconv"
)

func (s *Service) CommitHistory(ctx context.Context, r CommitHistoryRequest) (CommitHistoryResult, error) {
	if r.Limit <= 0 || r.Limit > MaxHistoryPageEntries {
		return CommitHistoryResult{}, &Error{Code: CodeInvalidRequest}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return CommitHistoryResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "commit_history", strconv.Itoa(r.Limit))...)
	last := ""
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "commit_history", fp) || c.LastCommitOID == "" || c.ObjectOID != "" || c.PathID != "" || c.LastEntryID != "" || c.NextIndex != 0 || c.NextOffset != 0 {
			return CommitHistoryResult{}, &Error{Code: CodeInvalidCursor}
		}
		last = c.LastCommitOID
	}
	order, e := s.commitOrder(ctx, a)
	if e != nil {
		return CommitHistoryResult{}, e
	}
	start, e := resumeCommitIndex(order, last)
	if e != nil {
		return CommitHistoryResult{}, e
	}
	end := start + r.Limit
	if end > len(order) {
		end = len(order)
	}
	entries := make([]CommitHistoryEntry, 0, end-start)
	for _, v := range order[start:end] {
		entries = append(entries, CommitHistoryEntry{CommitOID: v.node.CommitOID, TreeOID: v.node.TreeOID, ParentOIDs: append([]string(nil), v.node.ParentOIDs...), RawSize: v.node.RawSize, Distance: v.distance})
	}
	out := CommitHistoryResult{Source: fidelitySourceIdentity(a), Entries: entries, Complete: end == len(order)}
	if !out.Complete {
		if len(entries) == 0 {
			return CommitHistoryResult{}, &Error{Code: CodeInternalFailure}
		}
		c := fidelityCursorBase(a, "commit_history", fp)
		c.LastCommitOID = entries[len(entries)-1].CommitOID
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return CommitHistoryResult{}, e
		}
	}
	return out, nil
}
func (s *Service) PathHistory(ctx context.Context, r PathHistoryRequest) (PathHistoryResult, error) {
	if r.Limit <= 0 || r.Limit > MaxHistoryPageEntries {
		return PathHistoryResult{}, &Error{Code: CodeInvalidRequest}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return PathHistoryResult{}, e
	}
	path, id, input, e := s.authorizeHistoryPath(ctx, a, r.Path, r.PathSeed)
	if e != nil {
		return PathHistoryResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "path_history", id.PathID, input, strconv.Itoa(r.Limit))...)
	last := ""
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "path_history", fp) || c.PathID != id.PathID || c.LastCommitOID == "" || c.ObjectOID != "" || c.LastEntryID != "" || c.NextIndex != 0 || c.NextOffset != 0 {
			return PathHistoryResult{}, &Error{Code: CodeInvalidCursor}
		}
		last = c.LastCommitOID
	}
	order, e := s.commitOrder(ctx, a)
	if e != nil {
		return PathHistoryResult{}, e
	}
	emitted, e := s.pathHistoryEntries(ctx, a, order, path)
	if e != nil {
		return PathHistoryResult{}, e
	}
	start, e := resumePathHistoryIndex(emitted, last)
	if e != nil {
		return PathHistoryResult{}, e
	}
	end := start + r.Limit
	if end > len(emitted) {
		end = len(emitted)
	}
	out := PathHistoryResult{Source: fidelitySourceIdentity(a), Path: id, Entries: append([]PathHistoryEntry(nil), emitted[start:end]...), Complete: end == len(emitted)}
	if !out.Complete {
		if len(out.Entries) == 0 {
			return PathHistoryResult{}, &Error{Code: CodeInternalFailure}
		}
		c := fidelityCursorBase(a, "path_history", fp)
		c.PathID = id.PathID
		c.LastCommitOID = out.Entries[len(out.Entries)-1].CommitOID
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return PathHistoryResult{}, e
		}
	}
	return out, nil
}
func (s *Service) authorizeHistoryPath(ctx context.Context, a operations.SourceReadAuthority, ref PathReference, seed []byte) ([]byte, PathIdentity, string, error) {
	present := ref.PathID != "" || ref.InlineBase64 != "" || ref.SelectorID != ""
	seedPresent := seed != nil
	if present == seedPresent {
		return nil, PathIdentity{}, "", &Error{Code: CodeInvalidRequest}
	}
	if present {
		path, e := s.resolvePathReference(ctx, a, ref, false)
		if e != nil {
			return nil, PathIdentity{}, "", e
		}
		id, e := s.makePathIdentity(ctx, a, path)
		if e != nil {
			return nil, PathIdentity{}, "", e
		}
		return path, id, requestFingerprint("reference", ref.PathID, ref.InlineBase64, ref.SelectorID), nil
	}
	if !validatePath(seed, false) {
		return nil, PathIdentity{}, "", &Error{Code: CodeInvalidRequest}
	}
	path := append([]byte(nil), seed...)
	id, e := s.makePathIdentity(ctx, a, path)
	if e != nil {
		return nil, PathIdentity{}, "", e
	}
	return path, id, requestFingerprint("seed", id.PathID, canonicalInline(path)), nil
}
func (s *Service) pathHistoryEntries(ctx context.Context, a operations.SourceReadAuthority, order []commitGraphEntry, path []byte) ([]PathHistoryEntry, error) {
	if len(order) == 0 {
		return nil, &Error{Code: CodeObjectMismatch}
	}
	nodes := map[string]commitGraphEntry{}
	states := map[string]PathState{}
	for _, v := range order {
		nodes[v.node.CommitOID] = v
	}
	stateFor := func(oid string) (PathState, error) {
		if v, ok := states[oid]; ok {
			return v, nil
		}
		entry, ok := nodes[oid]
		if !ok {
			return PathState{}, &Error{Code: CodeObjectMismatch}
		}
		v, e := s.resolvePathStateAtTree(ctx, a, entry.node.TreeOID, path)
		if e != nil {
			return PathState{}, e
		}
		states[oid] = v
		return v, nil
	}
	var out []PathHistoryEntry
	for i, entry := range order {
		state, e := stateFor(entry.node.CommitOID)
		if e != nil {
			return nil, e
		}
		emit := i == 0
		if !emit && len(entry.node.ParentOIDs) == 0 && state.Present {
			emit = true
		}
		if !emit && len(entry.node.ParentOIDs) > 0 {
			for _, p := range entry.node.ParentOIDs {
				ps, e := stateFor(p)
				if e != nil {
					return nil, e
				}
				if !pathStatesEqual(state, ps) {
					emit = true
					break
				}
			}
		}
		if emit {
			out = append(out, PathHistoryEntry{CommitOID: entry.node.CommitOID, TreeOID: entry.node.TreeOID, ParentOIDs: append([]string(nil), entry.node.ParentOIDs...), Distance: entry.distance, State: state})
		}
	}
	return out, nil
}
func (s *Service) resolvePathStateAtTree(ctx context.Context, a operations.SourceReadAuthority, root string, path []byte) (PathState, error) {
	if !validLowerHex(root, 40) || !validatePath(path, false) {
		return PathState{}, &Error{Code: CodeInvalidRequest}
	}
	tree := root
	for i, component := range bytes.Split(path, []byte{'/'}) {
		entries, e := s.readTree(ctx, a, tree)
		if e != nil {
			return PathState{}, e
		}
		entry, ok := exactEntry(entries, component)
		if !ok {
			return PathState{Present: false}, nil
		}
		if i == len(bytes.Split(path, []byte{'/'}))-1 {
			return PathState{Present: true, Mode: entry.Mode, ObjectType: entry.ObjectType, ObjectOID: entry.ObjectOID}, nil
		}
		if entry.ObjectType != "tree" {
			return PathState{Present: false}, nil
		}
		tree = entry.ObjectOID
	}
	return PathState{Present: false}, nil
}
func pathStatesEqual(a, b PathState) bool {
	return a.Present == b.Present && a.Mode == b.Mode && a.ObjectType == b.ObjectType && a.ObjectOID == b.ObjectOID
}
func resumeCommitIndex(v []commitGraphEntry, last string) (int, error) {
	if last == "" {
		return 0, nil
	}
	for i, e := range v {
		if e.node.CommitOID == last {
			return i + 1, nil
		}
	}
	return 0, &Error{Code: CodeInvalidCursor}
}
func resumePathHistoryIndex(v []PathHistoryEntry, last string) (int, error) {
	if last == "" {
		return 0, nil
	}
	for i, e := range v {
		if e.CommitOID == last {
			return i + 1, nil
		}
	}
	return 0, &Error{Code: CodeInvalidCursor}
}
