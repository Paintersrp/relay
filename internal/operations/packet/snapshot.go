package packet

import (
	"crypto/sha256"
	"encoding/hex"
)

type Snapshot struct {
	document Document
	bytes    []byte
	sha256   string
}

func NewSnapshot(document Document) (Snapshot, error) {
	canonical, _, err := validateAndCanonicalize(document)
	if err != nil {
		return Snapshot{}, err
	}
	encoded, err := encodeCanonical(canonical)
	if err != nil {
		return Snapshot{}, err
	}
	digest := sha256.Sum256(encoded)
	return Snapshot{
		document: canonical,
		bytes:    append([]byte(nil), encoded...),
		sha256:   hex.EncodeToString(digest[:]),
	}, nil
}

func Validate(document Document) error {
	_, _, err := validateAndCanonicalize(document)
	return err
}

func CanonicalBytes(document Document) ([]byte, error) {
	snapshot, err := NewSnapshot(document)
	if err != nil {
		return nil, err
	}
	return snapshot.Bytes(), nil
}

func VerifyBytes(data []byte, expectedSHA256 string, expectedSize int64) error {
	if expectedSize < 0 || int64(len(data)) != expectedSize || !validSHA256(expectedSHA256) {
		return invalid("packet_artifact_identity")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expectedSHA256 {
		return invalid("packet_artifact_sha256")
	}
	return nil
}

func (s Snapshot) Document() Document {
	return cloneDocument(s.document)
}

func (s Snapshot) Bytes() []byte {
	return append([]byte(nil), s.bytes...)
}

func (s Snapshot) SHA256() string {
	return s.sha256
}

func (s Snapshot) SizeBytes() int64 {
	return int64(len(s.bytes))
}

func (s Snapshot) MediaType() string {
	return MediaType
}
