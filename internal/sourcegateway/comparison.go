package sourcegateway

import (
	"bytes"
	"context"
	"relay/internal/app/operations"
	"relay/internal/sourcevault"
	"sort"
	"strconv"
)

type rawComparisonEntry struct {
	kind          ChangeKind
	before, after *sourcevault.RetainedPathEntry
	id            string
}

func (s *Service) Compare(ctx context.Context, r CompareRequest) (CompareResult, error) {
	if r.Limit <= 0 || r.Limit > MaxComparisonPageEntries {
		return CompareResult{}, &Error{Code: CodeInvalidRequest}
	}
	before, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.Before)
	if e != nil {
		return CompareResult{}, e
	}
	after, e := s.resolveRevisionAuthority(ctx, r.PacketID, r.SurfaceContract, r.OperationID, r.RepositoryKey, r.After)
	if e != nil {
		return CompareResult{}, e
	}
	pair := revisionPairID(before, after)
	fp := pairFingerprint(before, after, "comparison", pair, strconv.Itoa(r.Limit))
	last := ""
	if r.Cursor != "" {
		c, de := s.cursors.Decode(r.Cursor)
		if de != nil || !fidelityPairCursorMatches(c, before, after, "comparison", fp) || c.LastEntryID == "" || c.ObjectOID != "" || c.PathID != "" || c.LastCommitOID != "" || c.NextIndex != 0 || c.NextOffset != 0 {
			return CompareResult{}, &Error{Code: CodeInvalidCursor}
		}
		last = c.LastEntryID
	}
	v, e := s.fidelityVault()
	if e != nil {
		return CompareResult{}, e
	}
	snap, e := v.ReadRetainedComparison(ctx, sourcevault.ReadRetainedComparisonRequest{Before: before.Relationship, After: after.Relationship})
	if e != nil {
		return CompareResult{}, mapVaultError(e)
	}
	if snap.BeforeCommitOID != before.Relationship.CommitOID || snap.BeforeTreeOID != before.Relationship.TreeOID || snap.AfterCommitOID != after.Relationship.CommitOID || snap.AfterTreeOID != after.Relationship.TreeOID {
		return CompareResult{}, &Error{Code: CodeObjectMismatch}
	}
	raw, e := s.buildComparisonEntries(ctx, before, after, snap.BeforeEntries, snap.AfterEntries)
	if e != nil {
		return CompareResult{}, e
	}
	start, e := resumeComparisonIndex(raw, last)
	if e != nil {
		return CompareResult{}, e
	}
	end := start + r.Limit
	if end > len(raw) {
		end = len(raw)
	}
	entries := make([]ComparisonEntry, 0, end-start)
	for _, x := range raw[start:end] {
		z, e := s.materializeComparisonEntry(ctx, before, after, x)
		if e != nil {
			return CompareResult{}, e
		}
		entries = append(entries, z)
	}
	out := CompareResult{Before: fidelitySourceIdentity(before), After: fidelitySourceIdentity(after), Entries: entries, Complete: end == len(raw)}
	if !out.Complete {
		if len(entries) == 0 {
			return CompareResult{}, &Error{Code: CodeInternalFailure}
		}
		c := fidelityPairCursorBase(before, after, "comparison", fp)
		c.LastEntryID = entries[len(entries)-1].EntryID
		out.Cursor, e = s.cursors.Encode(c)
		if e != nil {
			return CompareResult{}, e
		}
	}
	return out, nil
}
func (s *Service) buildComparisonEntries(ctx context.Context, ba, aa operations.SourceReadAuthority, bv, av []sourcevault.RetainedPathEntry) ([]rawComparisonEntry, error) {
	bm, e := pathEntryMap(bv)
	if e != nil {
		return nil, e
	}
	am, e := pathEntryMap(av)
	if e != nil {
		return nil, e
	}
	beforeRemaining := cloneEntryMap(bm)
	afterRemaining := cloneEntryMap(am)
	var out []rawComparisonEntry
	for path, b := range bm {
		a, ok := am[path]
		if !ok {
			continue
		}
		delete(beforeRemaining, path)
		delete(afterRemaining, path)
		if pathEntryEqual(b, a) {
			continue
		}
		kind, e := s.samePathChangeKind(ctx, ba, aa, b, a)
		if e != nil {
			return nil, e
		}
		bc, ac := b, a
		out = append(out, newRawComparison(kind, &bc, &ac))
	}
	added := groupEntriesByObject(afterRemaining)
	deleted := groupEntriesByObject(beforeRemaining)
	keys := map[string]struct{}{}
	for k := range added {
		keys[k] = struct{}{}
	}
	for k := range deleted {
		keys[k] = struct{}{}
	}
	objectKeys := make([]string, 0, len(keys))
	for k := range keys {
		objectKeys = append(objectKeys, k)
	}
	sort.Strings(objectKeys)
	usedAdded := map[string]struct{}{}
	usedDeleted := map[string]struct{}{}
	for _, key := range objectKeys {
		as, ds := added[key], deleted[key]
		sortPathEntries(as)
		sortPathEntries(ds)
		source := exactSourceRemaining(bm, am, key)
		if source != nil {
			for _, a := range as {
				bc, ac := *source, a
				out = append(out, newRawComparison(ChangeCopy, &bc, &ac))
				usedAdded[string(a.Path)] = struct{}{}
			}
			continue
		}
		count := len(as)
		if len(ds) < count {
			count = len(ds)
		}
		for i := 0; i < count; i++ {
			bc, ac := ds[i], as[i]
			out = append(out, newRawComparison(ChangeRename, &bc, &ac))
			usedDeleted[string(bc.Path)] = struct{}{}
			usedAdded[string(ac.Path)] = struct{}{}
		}
	}
	for path, b := range beforeRemaining {
		if _, ok := usedDeleted[path]; ok {
			continue
		}
		bc := b
		out = append(out, newRawComparison(ChangeDeletion, &bc, nil))
	}
	for path, a := range afterRemaining {
		if _, ok := usedAdded[path]; ok {
			continue
		}
		ac := a
		out = append(out, newRawComparison(ChangeAddition, nil, &ac))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := comparisonSortPath(out[i]), comparisonSortPath(out[j])
		if c := bytes.Compare(a, b); c != 0 {
			return c < 0
		}
		if out[i].kind != out[j].kind {
			return out[i].kind < out[j].kind
		}
		return out[i].id < out[j].id
	})
	for i := 1; i < len(out); i++ {
		if out[i-1].id == out[i].id {
			return nil, &Error{Code: CodeIntegrityFailure}
		}
	}
	return out, nil
}
func (s *Service) samePathChangeKind(ctx context.Context, ba, aa operations.SourceReadAuthority, b, a sourcevault.RetainedPathEntry) (ChangeKind, error) {
	if b.ObjectType != a.ObjectType {
		return ChangeType, nil
	}
	if b.ObjectOID == a.ObjectOID && b.Mode != a.Mode {
		return ChangeMode, nil
	}
	if b.ObjectType == "blob" && b.ObjectOID != a.ObjectOID {
		bb, e := s.binaryBlob(ctx, ba, b.ObjectOID)
		if e != nil {
			return "", e
		}
		ab, e := s.binaryBlob(ctx, aa, a.ObjectOID)
		if e != nil {
			return "", e
		}
		if bb || ab {
			return ChangeBinary, nil
		}
	}
	return ChangeModification, nil
}
func (s *Service) binaryBlob(ctx context.Context, a operations.SourceReadAuthority, oid string) (bool, error) {
	p, e := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: a.Relationship, BlobOID: oid, Offset: 0, Limit: binaryProbeBytes})
	if e != nil {
		return false, mapVaultError(e)
	}
	if p.BlobOID != oid || p.Offset != 0 || p.TotalSize < 0 || int64(len(p.Bytes)) > binaryProbeBytes {
		return false, &Error{Code: CodeObjectMismatch}
	}
	return bytes.IndexByte(p.Bytes, 0) >= 0, nil
}
func (s *Service) materializeComparisonEntry(ctx context.Context, ba, aa operations.SourceReadAuthority, x rawComparisonEntry) (ComparisonEntry, error) {
	out := ComparisonEntry{EntryID: x.id, Kind: x.kind}
	if x.before != nil {
		id, e := s.makePathIdentity(ctx, ba, x.before.Path)
		if e != nil {
			return ComparisonEntry{}, e
		}
		out.BeforePath = &id
		out.BeforeMode = x.before.Mode
		out.BeforeObjectType = x.before.ObjectType
		out.BeforeObjectOID = x.before.ObjectOID
	}
	if x.after != nil {
		id, e := s.makePathIdentity(ctx, aa, x.after.Path)
		if e != nil {
			return ComparisonEntry{}, e
		}
		out.AfterPath = &id
		out.AfterMode = x.after.Mode
		out.AfterObjectType = x.after.ObjectType
		out.AfterObjectOID = x.after.ObjectOID
	}
	return out, nil
}
func newRawComparison(k ChangeKind, b, a *sourcevault.RetainedPathEntry) rawComparisonEntry {
	var bp, ap []byte
	var bm, bt, bo, am, at, ao string
	if b != nil {
		bp = b.Path
		bm = b.Mode
		bt = b.ObjectType
		bo = b.ObjectOID
	}
	if a != nil {
		ap = a.Path
		am = a.Mode
		at = a.ObjectType
		ao = a.ObjectOID
	}
	return rawComparisonEntry{kind: k, before: b, after: a, id: comparisonEntryID(k, bp, bm, bt, bo, ap, am, at, ao)}
}
func pathEntryMap(v []sourcevault.RetainedPathEntry) (map[string]sourcevault.RetainedPathEntry, error) {
	out := map[string]sourcevault.RetainedPathEntry{}
	for _, e := range v {
		if !validatePath(e.Path, false) || e.Mode == "" || e.ObjectType == "" || !validLowerHex(e.ObjectOID, 40) {
			return nil, &Error{Code: CodeObjectMismatch}
		}
		key := string(e.Path)
		if _, ok := out[key]; ok {
			return nil, &Error{Code: CodeIntegrityFailure}
		}
		e.Path = append([]byte(nil), e.Path...)
		out[key] = e
	}
	return out, nil
}
func cloneEntryMap(v map[string]sourcevault.RetainedPathEntry) map[string]sourcevault.RetainedPathEntry {
	out := map[string]sourcevault.RetainedPathEntry{}
	for k, e := range v {
		out[k] = e
	}
	return out
}
func pathEntryEqual(a, b sourcevault.RetainedPathEntry) bool {
	return a.Mode == b.Mode && a.ObjectType == b.ObjectType && a.ObjectOID == b.ObjectOID
}
func objectKey(e sourcevault.RetainedPathEntry) string { return e.ObjectType + "\x00" + e.ObjectOID }
func groupEntriesByObject(v map[string]sourcevault.RetainedPathEntry) map[string][]sourcevault.RetainedPathEntry {
	out := map[string][]sourcevault.RetainedPathEntry{}
	for _, e := range v {
		out[objectKey(e)] = append(out[objectKey(e)], e)
	}
	return out
}
func exactSourceRemaining(b, a map[string]sourcevault.RetainedPathEntry, key string) *sourcevault.RetainedPathEntry {
	var vals []sourcevault.RetainedPathEntry
	for path, be := range b {
		if objectKey(be) != key {
			continue
		}
		ae, ok := a[path]
		if ok && ae.ObjectType == be.ObjectType && ae.ObjectOID == be.ObjectOID {
			vals = append(vals, be)
		}
	}
	if len(vals) == 0 {
		return nil
	}
	sortPathEntries(vals)
	v := vals[0]
	return &v
}
func sortPathEntries(v []sourcevault.RetainedPathEntry) {
	sort.Slice(v, func(i, j int) bool { return bytes.Compare(v[i].Path, v[j].Path) < 0 })
}
func comparisonSortPath(v rawComparisonEntry) []byte {
	if v.after != nil {
		return v.after.Path
	}
	return v.before.Path
}
func resumeComparisonIndex(v []rawComparisonEntry, last string) (int, error) {
	if last == "" {
		return 0, nil
	}
	for i, e := range v {
		if e.id == last {
			return i + 1, nil
		}
	}
	return 0, &Error{Code: CodeInvalidCursor}
}
