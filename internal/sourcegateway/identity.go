package sourcegateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"relay/internal/app/operations"
)

func validatePath(value []byte, allowRoot bool) bool {
	if len(value) == 0 {
		return allowRoot
	}
	if value[0] == '/' || bytes.IndexByte(value, 0) >= 0 {
		return false
	}
	for _, part := range bytes.Split(value, []byte{'/'}) {
		if len(part) == 0 || bytes.Equal(part, []byte(".")) || bytes.Equal(part, []byte("..")) {
			return false
		}
	}
	return true
}
func pathID(value []byte) string {
	digest := sha256.New()
	digest.Write([]byte(PathIdentityVersion))
	digest.Write([]byte{0})
	digest.Write(value)
	return hex.EncodeToString(digest.Sum(nil))
}
func canonicalInline(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
func decodeCanonicalInline(value string) ([]byte, bool) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || canonicalInline(decoded) != value {
		return nil, false
	}
	return decoded, true
}
func displayPath(value []byte) (string, bool) {
	if !utf8.Valid(value) {
		return "", false
	}
	return string(value), true
}
func basename(value []byte) []byte {
	if index := bytes.LastIndexByte(value, '/'); index >= 0 {
		return append([]byte(nil), value[index+1:]...)
	}
	return append([]byte(nil), value...)
}
func joinPath(parent, child []byte) []byte {
	if len(parent) == 0 {
		return append([]byte(nil), child...)
	}
	result := make([]byte, 0, len(parent)+1+len(child))
	result = append(result, parent...)
	result = append(result, '/')
	result = append(result, child...)
	return result
}

func selectorID(authority operations.SourceReadAuthority, pathDigest string, path []byte) string {
	digest := sha256.New()
	for _, value := range []string{"relay.source-path-selector.v1", authority.Summary.PacketID, string(authority.Summary.SurfaceContract), string(authority.Summary.OperationID), authority.Summary.ProjectID, authority.RepositoryKey, authority.PublicationID, authority.Relationship.CommitOID, authority.Relationship.TreeOID, pathDigest} {
		writeDigestPart(digest, []byte(value))
	}
	var relation [8]byte
	binary.BigEndian.PutUint64(relation[:], uint64(authority.Relationship.ID))
	writeDigestPart(digest, relation[:])
	writeDigestPart(digest, path)
	return "spath-" + hex.EncodeToString(digest.Sum(nil))
}
func writeDigestPart(digest interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(value)
}
func validLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func referenceFromIdentity(value PathIdentity) PathReference {
	return PathReference{PathID: value.PathID, InlineBase64: value.InlineBase64, SelectorID: value.SelectorID}
}
