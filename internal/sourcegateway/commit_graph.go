package sourcegateway

import (
	"bytes"
	"container/heap"
	"context"
	"relay/internal/app/operations"
	"relay/internal/sourcevault"
)

type commitGraphEntry struct {
	node     sourcevault.RetainedCommitNode
	distance int64
}
type commitQueueItem struct {
	oid      string
	distance int64
}
type commitQueue []commitQueueItem

func (q commitQueue) Len() int { return len(q) }
func (q commitQueue) Less(i, j int) bool {
	if q[i].distance != q[j].distance {
		return q[i].distance < q[j].distance
	}
	return bytes.Compare([]byte(q[i].oid), []byte(q[j].oid)) < 0
}
func (q commitQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *commitQueue) Push(v any)   { *q = append(*q, v.(commitQueueItem)) }
func (q *commitQueue) Pop() any {
	v := *q
	last := len(v) - 1
	out := v[last]
	*q = v[:last]
	return out
}
func (s *Service) commitOrder(ctx context.Context, a operations.SourceReadAuthority) ([]commitGraphEntry, error) {
	v, e := s.fidelityVault()
	if e != nil {
		return nil, e
	}
	q := &commitQueue{{oid: a.Relationship.CommitOID}}
	heap.Init(q)
	seen := map[string]struct{}{}
	var out []commitGraphEntry
	for q.Len() > 0 {
		item := heap.Pop(q).(commitQueueItem)
		if _, ok := seen[item.oid]; ok {
			continue
		}
		seen[item.oid] = struct{}{}
		n, re := v.ReadRetainedCommitNode(ctx, sourcevault.ReadRetainedCommitNodeRequest{Relationship: a.Relationship, CommitOID: item.oid})
		if re != nil {
			return nil, mapVaultError(re)
		}
		if n.CommitOID != item.oid || !validLowerHex(n.TreeOID, 40) || n.RawSize < 0 || n.MessageOffset < 0 || n.MessageSize < 0 || n.MessageOffset+n.MessageSize != n.RawSize {
			return nil, &Error{Code: CodeObjectMismatch}
		}
		if item.distance == 0 && n.TreeOID != a.Relationship.TreeOID {
			return nil, &Error{Code: CodeObjectMismatch}
		}
		for _, p := range n.ParentOIDs {
			if !validLowerHex(p, 40) {
				return nil, &Error{Code: CodeObjectMismatch}
			}
			if _, ok := seen[p]; !ok {
				heap.Push(q, commitQueueItem{oid: p, distance: item.distance + 1})
			}
		}
		out = append(out, commitGraphEntry{node: n, distance: item.distance})
	}
	return out, nil
}
func (s *Service) resolveReachableCommit(ctx context.Context, a operations.SourceReadAuthority, oid string) (commitGraphEntry, error) {
	if oid == "" {
		oid = a.Relationship.CommitOID
	}
	if !validLowerHex(oid, 40) {
		return commitGraphEntry{}, &Error{Code: CodeInvalidRequest}
	}
	order, e := s.commitOrder(ctx, a)
	if e != nil {
		return commitGraphEntry{}, e
	}
	for _, v := range order {
		if v.node.CommitOID == oid {
			return v, nil
		}
	}
	return commitGraphEntry{}, &Error{Code: CodeCommitUnreachable}
}
func commitSummary(n sourcevault.RetainedCommitNode) CommitSummary {
	r := CommitSummary{CommitOID: n.CommitOID, TreeOID: n.TreeOID, ParentCount: int64(len(n.ParentOIDs)), RawSize: n.RawSize, MessageSize: n.MessageSize}
	for _, h := range n.Headers {
		switch string(h.Name) {
		case "author":
			if r.AuthorRaw == nil {
				r.AuthorRaw = append([]byte(nil), h.Value...)
			}
		case "committer":
			if r.CommitterRaw == nil {
				r.CommitterRaw = append([]byte(nil), h.Value...)
			}
		case "encoding":
			if r.EncodingRaw == nil {
				r.EncodingRaw = append([]byte(nil), h.Value...)
			}
		case "gpgsig", "gpgsig-sha256":
			r.SignatureRaw = append(r.SignatureRaw, append([]byte(nil), h.Raw...))
		}
	}
	return r
}
