package sourcegateway

import (
	"context"
	"relay/internal/sourcevault"
	"strconv"
)

func (s *Service) ReadDiff(ctx context.Context, r ReadDiffRequest) (ReadDiffResult, error) {
	if r.Offset < 0 || r.Limit <= 0 || r.Limit > MaxDiffPageBytes {
		return ReadDiffResult{}, &Error{Code: CodeInvalidRange}
	}
	before, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Before)
	if e != nil {
		return ReadDiffResult{}, e
	}
	after, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.After)
	if e != nil {
		return ReadDiffResult{}, e
	}
	pair := revisionPairID(before, after)
	fp := pairFingerprint(before, after, "diff", pair, strconv.FormatInt(r.Offset, 10), strconv.FormatInt(r.Limit, 10))
	actual := r.Offset
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityPairCursorMatches(c, before, after, "diff", fp) || c.NextOffset < r.Offset || c.ObjectOID != "" || c.PathID != "" || c.LastCommitOID != "" || c.LastEntryID != "" || c.NextIndex != 0 {
			return ReadDiffResult{}, &Error{Code: CodeInvalidCursor}
		}
		actual = c.NextOffset
	}
	v, e := s.fidelityVault()
	if e != nil {
		return ReadDiffResult{}, e
	}
	p, e := v.ReadRetainedDiffRange(ctx, sourcevault.ReadRetainedDiffRangeRequest{Before: before.Relationship, After: after.Relationship, Offset: actual, Limit: r.Limit})
	if e != nil {
		mapped := mapVaultError(e)
		if ErrorCode(mapped) == CodeInvalidRequest {
			mapped = &Error{Code: CodeInvalidRange}
		}
		return ReadDiffResult{}, mapped
	}
	if p.BeforeCommitOID != before.Relationship.CommitOID || p.AfterCommitOID != after.Relationship.CommitOID || p.Offset != actual || p.TotalSize < 0 || int64(len(p.Bytes)) > r.Limit || p.Offset+int64(len(p.Bytes)) > p.TotalSize {
		return ReadDiffResult{}, &Error{Code: CodeObjectMismatch}
	}
	out := ReadDiffResult{Before: fidelitySourceIdentity(before), After: fidelitySourceIdentity(after), Offset: p.Offset, ReturnedLength: int64(len(p.Bytes)), TotalSize: p.TotalSize, Bytes: append([]byte(nil), p.Bytes...), Complete: p.Offset+int64(len(p.Bytes)) == p.TotalSize}
	if !out.Complete {
		c := fidelityPairCursorBase(before, after, "diff", fp)
		c.NextOffset = p.Offset + int64(len(p.Bytes))
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadDiffResult{}, e
		}
	}
	return out, nil
}
