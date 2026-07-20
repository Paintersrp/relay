package sourcegateway

import (
	"context"
	"relay/internal/sourcevault"
	"strconv"
)

func (s *Service) ReadCommitBytes(ctx context.Context, r ReadCommitBytesRequest) (ReadCommitBytesResult, error) {
	if r.Offset < 0 || r.Limit <= 0 || r.Limit > MaxCommitPageBytes {
		return ReadCommitBytesResult{}, &Error{Code: CodeInvalidRange}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return ReadCommitBytesResult{}, e
	}
	g, e := s.resolveReachableCommit(ctx, a, r.CommitOID)
	if e != nil {
		return ReadCommitBytesResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "commit_bytes", g.node.CommitOID, strconv.FormatInt(r.Offset, 10), strconv.FormatInt(r.Limit, 10))...)
	actual := r.Offset
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "commit_bytes", fp) || c.ObjectOID != g.node.CommitOID || c.NextOffset < r.Offset || c.NextIndex != 0 || c.PathID != "" || c.LastCommitOID != "" || c.LastEntryID != "" {
			return ReadCommitBytesResult{}, &Error{Code: CodeInvalidCursor}
		}
		actual = c.NextOffset
	}
	if actual > g.node.RawSize {
		return ReadCommitBytesResult{}, &Error{Code: CodeInvalidRange}
	}
	v, e := s.fidelityVault()
	if e != nil {
		return ReadCommitBytesResult{}, e
	}
	p, e := v.ReadRetainedCommitRange(ctx, sourcevault.ReadRetainedCommitRangeRequest{Relationship: a.Relationship, CommitOID: g.node.CommitOID, Offset: actual, Limit: r.Limit})
	if e != nil {
		return ReadCommitBytesResult{}, mapVaultError(e)
	}
	if p.CommitOID != g.node.CommitOID || p.Offset != actual || p.TotalSize != g.node.RawSize || p.Offset+int64(len(p.Bytes)) > p.TotalSize {
		return ReadCommitBytesResult{}, &Error{Code: CodeObjectMismatch}
	}
	complete := p.Offset+int64(len(p.Bytes)) == p.TotalSize
	out := ReadCommitBytesResult{Source: fidelitySourceIdentity(a), Summary: commitSummary(g.node), Offset: p.Offset, ReturnedLength: int64(len(p.Bytes)), TotalSize: p.TotalSize, Bytes: append([]byte(nil), p.Bytes...), Complete: complete}
	if !complete {
		c := fidelityCursorBase(a, "commit_bytes", fp)
		c.ObjectOID = g.node.CommitOID
		c.NextOffset = p.Offset + int64(len(p.Bytes))
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadCommitBytesResult{}, e
		}
	}
	return out, nil
}
func (s *Service) ReadCommitHeaders(ctx context.Context, r ReadCommitHeadersRequest) (ReadCommitHeadersResult, error) {
	if r.Limit <= 0 || r.Limit > MaxCommitHeaderEntries {
		return ReadCommitHeadersResult{}, &Error{Code: CodeInvalidRequest}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return ReadCommitHeadersResult{}, e
	}
	g, e := s.resolveReachableCommit(ctx, a, r.CommitOID)
	if e != nil {
		return ReadCommitHeadersResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "commit_headers", g.node.CommitOID, strconv.Itoa(r.Limit))...)
	start := 0
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "commit_headers", fp) || c.ObjectOID != g.node.CommitOID || c.NextOffset != 0 || c.PathID != "" || c.LastCommitOID != "" || c.LastEntryID != "" {
			return ReadCommitHeadersResult{}, &Error{Code: CodeInvalidCursor}
		}
		start = int(c.NextIndex)
	}
	if start < 0 || start > len(g.node.Headers) {
		return ReadCommitHeadersResult{}, &Error{Code: CodeInvalidCursor}
	}
	end := start + r.Limit
	if end > len(g.node.Headers) {
		end = len(g.node.Headers)
	}
	headers := make([]CommitHeader, 0, end-start)
	for _, h := range g.node.Headers[start:end] {
		headers = append(headers, CommitHeader{Name: append([]byte(nil), h.Name...), Value: append([]byte(nil), h.Value...), Raw: append([]byte(nil), h.Raw...)})
	}
	complete := end == len(g.node.Headers)
	out := ReadCommitHeadersResult{Source: fidelitySourceIdentity(a), Summary: commitSummary(g.node), Headers: headers, Complete: complete}
	if !complete {
		c := fidelityCursorBase(a, "commit_headers", fp)
		c.ObjectOID = g.node.CommitOID
		c.NextIndex = int64(end)
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadCommitHeadersResult{}, e
		}
	}
	return out, nil
}
func (s *Service) ReadCommitParents(ctx context.Context, r ReadCommitParentsRequest) (ReadCommitParentsResult, error) {
	if r.Limit <= 0 || r.Limit > MaxCommitParentEntries {
		return ReadCommitParentsResult{}, &Error{Code: CodeInvalidRequest}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return ReadCommitParentsResult{}, e
	}
	g, e := s.resolveReachableCommit(ctx, a, r.CommitOID)
	if e != nil {
		return ReadCommitParentsResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "commit_parents", g.node.CommitOID, strconv.Itoa(r.Limit))...)
	start := 0
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "commit_parents", fp) || c.ObjectOID != g.node.CommitOID || c.NextOffset != 0 || c.PathID != "" || c.LastCommitOID != "" || c.LastEntryID != "" {
			return ReadCommitParentsResult{}, &Error{Code: CodeInvalidCursor}
		}
		start = int(c.NextIndex)
	}
	if start < 0 || start > len(g.node.ParentOIDs) {
		return ReadCommitParentsResult{}, &Error{Code: CodeInvalidCursor}
	}
	end := start + r.Limit
	if end > len(g.node.ParentOIDs) {
		end = len(g.node.ParentOIDs)
	}
	out := ReadCommitParentsResult{Source: fidelitySourceIdentity(a), Summary: commitSummary(g.node), ParentOIDs: append([]string(nil), g.node.ParentOIDs[start:end]...), Complete: end == len(g.node.ParentOIDs)}
	if !out.Complete {
		c := fidelityCursorBase(a, "commit_parents", fp)
		c.ObjectOID = g.node.CommitOID
		c.NextIndex = int64(end)
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadCommitParentsResult{}, e
		}
	}
	return out, nil
}
func (s *Service) ReadCommitMessage(ctx context.Context, r ReadCommitMessageRequest) (ReadCommitMessageResult, error) {
	if r.Offset < 0 || r.Limit <= 0 || r.Limit > MaxCommitPageBytes {
		return ReadCommitMessageResult{}, &Error{Code: CodeInvalidRange}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return ReadCommitMessageResult{}, e
	}
	g, e := s.resolveReachableCommit(ctx, a, r.CommitOID)
	if e != nil {
		return ReadCommitMessageResult{}, e
	}
	fp := requestFingerprint(append(revisionFingerprint(a), "commit_message", g.node.CommitOID, strconv.FormatInt(r.Offset, 10), strconv.FormatInt(r.Limit, 10))...)
	actual := r.Offset
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "commit_message", fp) || c.ObjectOID != g.node.CommitOID || c.NextOffset < r.Offset || c.NextIndex != 0 || c.PathID != "" || c.LastCommitOID != "" || c.LastEntryID != "" {
			return ReadCommitMessageResult{}, &Error{Code: CodeInvalidCursor}
		}
		actual = c.NextOffset
	}
	if actual > g.node.MessageSize {
		return ReadCommitMessageResult{}, &Error{Code: CodeInvalidRange}
	}
	v, e := s.fidelityVault()
	if e != nil {
		return ReadCommitMessageResult{}, e
	}
	p, e := v.ReadRetainedCommitRange(ctx, sourcevault.ReadRetainedCommitRangeRequest{Relationship: a.Relationship, CommitOID: g.node.CommitOID, Offset: g.node.MessageOffset + actual, Limit: r.Limit})
	if e != nil {
		return ReadCommitMessageResult{}, mapVaultError(e)
	}
	if p.CommitOID != g.node.CommitOID || p.TotalSize != g.node.RawSize || p.Offset != g.node.MessageOffset+actual {
		return ReadCommitMessageResult{}, &Error{Code: CodeObjectMismatch}
	}
	if int64(len(p.Bytes)) > g.node.MessageSize-actual {
		return ReadCommitMessageResult{}, &Error{Code: CodeObjectMismatch}
	}
	complete := actual+int64(len(p.Bytes)) == g.node.MessageSize
	out := ReadCommitMessageResult{Source: fidelitySourceIdentity(a), Summary: commitSummary(g.node), Offset: actual, ReturnedLength: int64(len(p.Bytes)), TotalSize: g.node.MessageSize, Bytes: append([]byte(nil), p.Bytes...), Complete: complete}
	if !complete {
		c := fidelityCursorBase(a, "commit_message", fp)
		c.ObjectOID = g.node.CommitOID
		c.NextOffset = actual + int64(len(p.Bytes))
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadCommitMessageResult{}, e
		}
	}
	return out, nil
}
