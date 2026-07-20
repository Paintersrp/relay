package sourcegateway

import (
	"context"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
	"strconv"
	"unicode/utf8"
)

func (s *Service) ReadText(ctx context.Context, r ReadTextRequest) (ReadTextResult, error) {
	if r.Offset < 0 || r.Limit < MinTextPageBytes || r.Limit > MaxTextPageBytes {
		return ReadTextResult{}, &Error{Code: CodeInvalidRange}
	}
	a, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Revision)
	if e != nil {
		return ReadTextResult{}, e
	}
	path, e := s.resolvePathReference(ctx, a, r.Path, false)
	if e != nil {
		return ReadTextResult{}, e
	}
	identity, e := s.makePathIdentity(ctx, a, path)
	if e != nil {
		return ReadTextResult{}, e
	}
	entry, e := s.resolvePathEntry(ctx, a, path)
	if e != nil {
		return ReadTextResult{}, e
	}
	if entry.ObjectType != "blob" {
		return ReadTextResult{}, &Error{Code: CodeObjectMismatch}
	}
	fingerprint := requestFingerprint(append(revisionFingerprint(a), "text", identity.PathID, strconv.FormatInt(r.Offset, 10), strconv.FormatInt(r.Limit, 10))...)
	actual := r.Offset
	open := false
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityCursorMatches(c, a, "text", fingerprint) || c.ObjectOID != entry.ObjectOID || c.PathID != identity.PathID || c.NextOffset < r.Offset || c.NextIndex != 0 || c.LastCommitOID != "" || c.LastEntryID != "" {
			return ReadTextResult{}, &Error{Code: CodeInvalidCursor}
		}
		actual = c.NextOffset
		open = c.TextLineOpen
	}
	total, e := s.validateUTF8Blob(ctx, a.Relationship, entry.ObjectOID)
	if e != nil {
		return ReadTextResult{}, e
	}
	if actual > total {
		return ReadTextResult{}, &Error{Code: CodeInvalidRange}
	}
	if r.Cursor == "" {
		open, e = s.textStartState(ctx, a.Relationship, entry.ObjectOID, actual, total)
		if e != nil {
			return ReadTextResult{}, e
		}
	}
	segments, next, nextOpen, e := s.segmentTextPage(ctx, a.Relationship, entry.ObjectOID, actual, r.Limit, total, open)
	if e != nil {
		return ReadTextResult{}, e
	}
	complete := next == total
	out := ReadTextResult{Source: fidelitySourceIdentity(a), Path: identity, Mode: entry.Mode, ObjectOID: entry.ObjectOID, Segments: segments, Offset: actual, NextOffset: next, TotalSize: total, Complete: complete}
	if !complete {
		c := fidelityCursorBase(a, "text", fingerprint)
		c.ObjectOID = entry.ObjectOID
		c.PathID = identity.PathID
		c.NextOffset = next
		c.TextLineOpen = nextOpen
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return ReadTextResult{}, e
		}
	}
	return out, nil
}

func (s *Service) validateUTF8Blob(ctx context.Context, r workflowstore.OperationPacketVaultRelationship, oid string) (int64, error) {
	var offset int64
	var total int64 = -1
	var carry []byte
	for total < 0 || offset < total {
		p, e := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: r, BlobOID: oid, Offset: offset, Limit: textValidationChunkBytes})
		if e != nil {
			return 0, mapVaultError(e)
		}
		if p.BlobOID != oid || p.Offset != offset || p.TotalSize < 0 || p.Offset+int64(len(p.Bytes)) > p.TotalSize {
			return 0, &Error{Code: CodeObjectMismatch}
		}
		if total < 0 {
			total = p.TotalSize
		} else if total != p.TotalSize {
			return 0, &Error{Code: CodeObjectMismatch}
		}
		data := append(append([]byte(nil), carry...), p.Bytes...)
		carry = nil
		for len(data) > 0 {
			if !utf8.FullRune(data) {
				carry = append(carry, data...)
				break
			}
			runeValue, size := utf8.DecodeRune(data)
			if runeValue == utf8.RuneError && size == 1 {
				return 0, &Error{Code: CodeInvalidTextProjection}
			}
			data = data[size:]
		}
		offset += int64(len(p.Bytes))
		if len(p.Bytes) == 0 && offset < total {
			return 0, &Error{Code: CodeObjectMismatch}
		}
	}
	if len(carry) > 0 {
		return 0, &Error{Code: CodeInvalidTextProjection}
	}
	return total, nil
}
func (s *Service) textStartState(ctx context.Context, r workflowstore.OperationPacketVaultRelationship, oid string, offset, total int64) (bool, error) {
	if offset == 0 {
		return false, nil
	}
	start := offset - 1
	limit := int64(2)
	if start+limit > total {
		limit = total - start
	}
	p, e := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: r, BlobOID: oid, Offset: start, Limit: limit})
	if e != nil {
		return false, mapVaultError(e)
	}
	if len(p.Bytes) == 0 || p.Offset != start || p.TotalSize != total {
		return false, &Error{Code: CodeObjectMismatch}
	}
	previous := p.Bytes[0]
	if offset < total && (len(p.Bytes) < 2 || p.Bytes[1]&0xc0 == 0x80 || (previous == '\r' && p.Bytes[1] == '\n')) {
		return false, &Error{Code: CodeInvalidRange}
	}
	return previous != '\n' && previous != '\r', nil
}
func (s *Service) segmentTextPage(ctx context.Context, r workflowstore.OperationPacketVaultRelationship, oid string, offset, limit, total int64, lineOpen bool) ([]TextSegment, int64, bool, error) {
	if offset == total {
		if total == 0 {
			return []TextSegment{{StartOffset: 0, EndOffset: 0, LineComplete: true, FinalLine: true}}, total, false, nil
		}
		last, e := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: r, BlobOID: oid, Offset: total - 1, Limit: 1})
		if e != nil {
			return nil, 0, false, mapVaultError(e)
		}
		if len(last.Bytes) == 1 && (last.Bytes[0] == '\n' || last.Bytes[0] == '\r') {
			return []TextSegment{{StartOffset: total, EndOffset: total, LineComplete: true, FinalLine: true}}, total, false, nil
		}
		return []TextSegment{}, total, false, nil
	}
	readLimit := limit + utf8.UTFMax
	if rem := total - offset; readLimit > rem {
		readLimit = rem
	}
	p, e := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: r, BlobOID: oid, Offset: offset, Limit: readLimit})
	if e != nil {
		return nil, 0, false, mapVaultError(e)
	}
	if p.BlobOID != oid || p.Offset != offset || p.TotalSize != total || len(p.Bytes) == 0 {
		return nil, 0, false, &Error{Code: CodeObjectMismatch}
	}
	end := int64(len(p.Bytes))
	if end > limit {
		end = limit
	}
	for end > 0 && end < int64(len(p.Bytes)) && p.Bytes[end]&0xc0 == 0x80 {
		end--
	}
	if end > 0 && offset+end < total && p.Bytes[end-1] == '\r' && end < int64(len(p.Bytes)) && p.Bytes[end] == '\n' {
		end--
	}
	if end <= 0 {
		return nil, 0, false, &Error{Code: CodeInvalidRange}
	}
	data := p.Bytes[:end]
	segments := make([]TextSegment, 0)
	start := 0
	continuation := lineOpen
	for i := 0; i < len(data); {
		tl := 0
		if data[i] == '\n' || data[i] == '\r' {
			tl = 1
			if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
				tl = 2
			}
		}
		if tl == 0 {
			i++
			continue
		}
		segments = append(segments, TextSegment{StartOffset: offset + int64(start), EndOffset: offset + int64(i+tl), Bytes: append([]byte(nil), data[start:i]...), Terminator: append([]byte(nil), data[i:i+tl]...), ContinuesLine: continuation, LineComplete: true})
		start = i + tl
		i = start
		continuation = false
	}
	next := offset + end
	if start < len(data) {
		final := next == total
		segments = append(segments, TextSegment{StartOffset: offset + int64(start), EndOffset: next, Bytes: append([]byte(nil), data[start:]...), ContinuesLine: continuation, LineComplete: final, FinalLine: final})
		continuation = !final
	} else if next == total {
		segments = append(segments, TextSegment{StartOffset: total, EndOffset: total, LineComplete: true, FinalLine: true})
		continuation = false
	}
	return segments, next, continuation, nil
}
