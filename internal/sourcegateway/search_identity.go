package sourcegateway

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"relay/internal/app/operations"
)

func searchQueryID(mode SearchMode, literal []byte) string {
	digest := sha256.New()
	writeDigestPart(digest, []byte("relay.source-search-query.v1"))
	writeDigestPart(digest, []byte(mode))
	writeDigestPart(digest, literal)
	return hex.EncodeToString(digest.Sum(nil))
}

func searchFilterID(prefixes []canonicalSearchPrefix) string {
	digest := sha256.New()
	writeDigestPart(digest, []byte("relay.source-search-filter.v1"))
	for _, prefix := range prefixes {
		writeDigestPart(digest, prefix.bytes)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func searchMatchID(authority operations.SourceReadAuthority, mode SearchMode, queryID, filterID, pathID, blobOID string, offset, length, ordinal int64) string {
	digest := sha256.New()
	writeDigestPart(digest, []byte("relay.source-search-match.v1"))
	for _, value := range revisionFingerprint(authority) {
		writeDigestPart(digest, []byte(value))
	}
	writeDigestPart(digest, []byte(mode))
	writeDigestPart(digest, []byte(queryID))
	writeDigestPart(digest, []byte(filterID))
	writeDigestPart(digest, []byte(pathID))
	writeDigestPart(digest, []byte(blobOID))
	writeDigestPart(digest, []byte(strconv.FormatInt(offset, 10)))
	writeDigestPart(digest, []byte(strconv.FormatInt(length, 10)))
	writeDigestPart(digest, []byte(strconv.FormatInt(ordinal, 10)))
	return hex.EncodeToString(digest.Sum(nil))
}
