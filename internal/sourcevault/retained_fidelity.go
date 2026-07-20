package sourcevault

import (
	"bytes"
	"context"
	workflowstore "relay/internal/store/workflow"
	"sort"
)

type RetainedCommitHeader struct{ Name, Value, Raw []byte }
type RetainedCommitNode struct {
	CommitOID, TreeOID                  string
	ParentOIDs                          []string
	Headers                             []RetainedCommitHeader
	RawSize, MessageOffset, MessageSize int64
}
type ReadRetainedCommitRangeRequest struct {
	Relationship  workflowstore.OperationPacketVaultRelationship
	CommitOID     string
	Offset, Limit int64
}
type ReadRetainedCommitRangeResult struct {
	CommitOID         string
	Offset, TotalSize int64
	Bytes             []byte
}
type ReadRetainedCommitNodeRequest struct {
	Relationship workflowstore.OperationPacketVaultRelationship
	CommitOID    string
}
type RetainedPathEntry struct {
	Path                        []byte
	Mode, ObjectType, ObjectOID string
}
type ReadRetainedComparisonRequest struct {
	Before, After workflowstore.OperationPacketVaultRelationship
}
type ReadRetainedComparisonResult struct {
	BeforeCommitOID, BeforeTreeOID, AfterCommitOID, AfterTreeOID string
	BeforeEntries, AfterEntries                                  []RetainedPathEntry
}
type ReadRetainedDiffRangeRequest struct {
	Before, After workflowstore.OperationPacketVaultRelationship
	Offset, Limit int64
}
type ReadRetainedDiffRangeResult struct {
	BeforeCommitOID, AfterCommitOID string
	Offset, TotalSize               int64
	Bytes                           []byte
}

func (m *Manager) ReadRetainedCommitRange(ctx context.Context, r ReadRetainedCommitRangeRequest) (ReadRetainedCommitRangeResult, error) {
	if m == nil || !validOID(r.CommitOID) || r.Offset < 0 || r.Limit <= 0 {
		return ReadRetainedCommitRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	var out ReadRetainedCommitRangeResult
	err := m.withActiveRetentionEdge(ctx, r.Relationship, func(path string, _ workflowstore.SourceVaultClosure) error {
		p, e := readGitObjectRange(ctx, path, r.CommitOID, "commit", r.Offset, r.Limit)
		if e != nil {
			return e
		}
		out = ReadRetainedCommitRangeResult{CommitOID: r.CommitOID, Offset: p.Offset, TotalSize: p.TotalSize, Bytes: append([]byte(nil), p.Bytes...)}
		return nil
	})
	if err != nil {
		return ReadRetainedCommitRangeResult{}, err
	}
	return out, nil
}
func (m *Manager) ReadRetainedCommitNode(ctx context.Context, r ReadRetainedCommitNodeRequest) (RetainedCommitNode, error) {
	if m == nil || !validOID(r.CommitOID) {
		return RetainedCommitNode{}, &Error{Code: CodeInvalidRequest}
	}
	var out RetainedCommitNode
	err := m.withActiveRetentionEdge(ctx, r.Relationship, func(path string, _ workflowstore.SourceVaultClosure) error {
		n, e := readGitCommitNode(ctx, path, r.CommitOID)
		if e != nil {
			return e
		}
		out = cloneCommitNode(n)
		return nil
	})
	if err != nil {
		return RetainedCommitNode{}, err
	}
	return out, nil
}
func (m *Manager) ReadRetainedComparison(ctx context.Context, r ReadRetainedComparisonRequest) (ReadRetainedComparisonResult, error) {
	if m == nil {
		return ReadRetainedComparisonResult{}, &Error{Code: CodeInvalidRequest}
	}
	var out ReadRetainedComparisonResult
	err := m.withActiveRetentionEdges(ctx, r.Before, r.After, func(path string, bc, ac workflowstore.SourceVaultClosure) error {
		b, e := readTreeSnapshot(ctx, m.git, path, bc.TreeOID)
		if e != nil {
			return e
		}
		a, e := readTreeSnapshot(ctx, m.git, path, ac.TreeOID)
		if e != nil {
			return e
		}
		out = ReadRetainedComparisonResult{BeforeCommitOID: bc.CommitOID, BeforeTreeOID: bc.TreeOID, AfterCommitOID: ac.CommitOID, AfterTreeOID: ac.TreeOID, BeforeEntries: clonePathEntries(b), AfterEntries: clonePathEntries(a)}
		return nil
	})
	if err != nil {
		return ReadRetainedComparisonResult{}, err
	}
	return out, nil
}
func (m *Manager) ReadRetainedDiffRange(ctx context.Context, r ReadRetainedDiffRangeRequest) (ReadRetainedDiffRangeResult, error) {
	if m == nil || r.Offset < 0 || r.Limit <= 0 {
		return ReadRetainedDiffRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	var out ReadRetainedDiffRangeResult
	err := m.withActiveRetentionEdges(ctx, r.Before, r.After, func(path string, bc, ac workflowstore.SourceVaultClosure) error {
		p, e := readGitDiffRange(ctx, path, bc.CommitOID, ac.CommitOID, r.Offset, r.Limit)
		if e != nil {
			return e
		}
		out = ReadRetainedDiffRangeResult{BeforeCommitOID: bc.CommitOID, AfterCommitOID: ac.CommitOID, Offset: p.Offset, TotalSize: p.TotalSize, Bytes: append([]byte(nil), p.Bytes...)}
		return nil
	})
	if err != nil {
		return ReadRetainedDiffRangeResult{}, err
	}
	return out, nil
}

func readTreeSnapshot(ctx context.Context, git gitClient, path, root string) ([]RetainedPathEntry, error) {
	if !validOID(root) {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	var out []RetainedPathEntry
	var walk func(string, []byte) error
	walk = func(tree string, parent []byte) error {
		entries, e := git.ReadTree(ctx, path, tree)
		if e != nil {
			return managerError(ctx, e, CodeObjectUnavailable)
		}
		for _, entry := range entries {
			p := append([]byte(nil), entry.Name...)
			if len(parent) > 0 {
				p = append(append(append([]byte(nil), parent...), '/'), entry.Name...)
			}
			out = append(out, RetainedPathEntry{Path: p, Mode: entry.Mode, ObjectType: entry.ObjectType, ObjectOID: entry.ObjectOID})
			if entry.ObjectType == "tree" {
				if e := walk(entry.ObjectOID, p); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := walk(root, nil); e != nil {
		return nil, e
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Path, out[j].Path) < 0 })
	for i := 1; i < len(out); i++ {
		if bytes.Equal(out[i-1].Path, out[i].Path) {
			return nil, &Error{Code: CodeObjectMismatch}
		}
	}
	return out, nil
}
func cloneCommitNode(v RetainedCommitNode) RetainedCommitNode {
	r := v
	r.ParentOIDs = append([]string(nil), v.ParentOIDs...)
	r.Headers = make([]RetainedCommitHeader, len(v.Headers))
	for i, h := range v.Headers {
		r.Headers[i] = RetainedCommitHeader{Name: append([]byte(nil), h.Name...), Value: append([]byte(nil), h.Value...), Raw: append([]byte(nil), h.Raw...)}
	}
	return r
}
func clonePathEntries(v []RetainedPathEntry) []RetainedPathEntry {
	r := make([]RetainedPathEntry, len(v))
	for i, e := range v {
		r[i] = e
		r[i].Path = append([]byte(nil), e.Path...)
	}
	return r
}
