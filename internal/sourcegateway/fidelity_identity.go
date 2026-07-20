package sourcegateway

import (
	"crypto/sha256"
	"encoding/hex"
	"relay/internal/app/operations"
)

func revisionPairID(before, after operations.SourceReadAuthority) string {
	d := sha256.New()
	writeDigestPart(d, []byte("relay.source-revision-pair.v1"))
	for _, v := range append(revisionFingerprint(before), revisionFingerprint(after)...) {
		writeDigestPart(d, []byte(v))
	}
	return hex.EncodeToString(d.Sum(nil))
}
func comparisonEntryID(kind ChangeKind, beforePath []byte, beforeMode, beforeType, beforeOID string, afterPath []byte, afterMode, afterType, afterOID string) string {
	d := sha256.New()
	writeDigestPart(d, []byte("relay.source-comparison-entry.v1"))
	writeDigestPart(d, []byte(kind))
	writeDigestPart(d, beforePath)
	writeDigestPart(d, []byte(beforeMode))
	writeDigestPart(d, []byte(beforeType))
	writeDigestPart(d, []byte(beforeOID))
	writeDigestPart(d, afterPath)
	writeDigestPart(d, []byte(afterMode))
	writeDigestPart(d, []byte(afterType))
	writeDigestPart(d, []byte(afterOID))
	return hex.EncodeToString(d.Sum(nil))
}
