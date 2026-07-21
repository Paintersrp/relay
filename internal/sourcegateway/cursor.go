package sourcegateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

type cursorPayload struct {
	Version                         string        `json:"version"`
	Kind                            string        `json:"kind"`
	PacketID                        string        `json:"packet_id"`
	PacketSHA256                    string        `json:"packet_sha256"`
	SurfaceContract                 string        `json:"surface_contract"`
	OperationID                     string        `json:"operation_id"`
	ProjectID                       string        `json:"project_id"`
	RepositoryKey                   string        `json:"repository_key"`
	DependencyKey                   string        `json:"dependency_key,omitempty"`
	AnchorName                      string        `json:"anchor_name,omitempty"`
	PublicationID                   string        `json:"publication_id"`
	VaultRelationshipRowID          int64         `json:"vault_relationship_row_id"`
	CommitOID                       string        `json:"commit_oid"`
	TreeOID                         string        `json:"tree_oid"`
	SecondaryDependencyKey          string        `json:"secondary_dependency_key,omitempty"`
	SecondaryAnchorName             string        `json:"secondary_anchor_name,omitempty"`
	SecondaryPublicationID          string        `json:"secondary_publication_id,omitempty"`
	SecondaryVaultRelationshipRowID int64         `json:"secondary_vault_relationship_row_id,omitempty"`
	SecondaryCommitOID              string        `json:"secondary_commit_oid,omitempty"`
	SecondaryTreeOID                string        `json:"secondary_tree_oid,omitempty"`
	RequestFingerprint              string        `json:"request_fingerprint"`
	ObjectOID                       string        `json:"object_oid,omitempty"`
	PathID                          string        `json:"path_id,omitempty"`
	AfterPath                       PathReference `json:"after_path,omitempty"`
	LastCommitOID                   string        `json:"last_commit_oid,omitempty"`
	LastEntryID                     string        `json:"last_entry_id,omitempty"`
	NextIndex                       int64         `json:"next_index,omitempty"`
	NextOffset                      int64         `json:"next_offset,omitempty"`
	TextLineOpen                    bool          `json:"text_line_open,omitempty"`
	SearchPhase                     searchPhase   `json:"search_phase,omitempty"`
	SearchObjectSize                int64         `json:"search_object_size,omitempty"`
	SearchObjectSizeKnown           bool          `json:"search_object_size_known,omitempty"`
}

type HMACCursorCodec struct{ key []byte }

func NewHMACCursorCodec(key []byte) (*HMACCursorCodec, error) {
	if len(key) < 32 {
		return nil, errors.New("source cursor key must contain at least 32 bytes")
	}
	return &HMACCursorCodec{key: append([]byte(nil), key...)}, nil
}

func (c *HMACCursorCodec) Encode(value cursorPayload) (string, error) {
	if c == nil || len(c.key) < 32 || value.Version != CursorVersion || value.Kind == "" {
		return "", &Error{Code: CodeInvalidCursor}
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", &Error{Code: CodeInternalFailure}
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > MaxCursorTokenBytes {
		return "", &Error{Code: CodeInvalidCursor}
	}
	return token, nil
}

func (c *HMACCursorCodec) Decode(token string) (cursorPayload, error) {
	if c == nil || len(c.key) < 32 || strings.TrimSpace(token) != token || token == "" || len(token) > MaxCursorTokenBytes {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != parts[0] {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(signature) != parts[1] {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	var value cursorPayload
	if err := json.Unmarshal(payload, &value); err != nil || !validCursorPayload(value) {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	canonical, err := json.Marshal(value)
	if err != nil || string(canonical) != string(payload) {
		return cursorPayload{}, &Error{Code: CodeInvalidCursor}
	}
	return value, nil
}

func validCursorPayload(value cursorPayload) bool {
	if value.Version != CursorVersion || value.Kind == "" || value.NextOffset < 0 || value.NextIndex < 0 || !validLowerHex(value.PacketSHA256, 64) || !validLowerHex(value.CommitOID, 40) || !validLowerHex(value.TreeOID, 40) || !validLowerHex(value.RequestFingerprint, 64) {
		return false
	}
	for _, oid := range []string{value.ObjectOID, value.LastCommitOID, value.SecondaryCommitOID, value.SecondaryTreeOID} {
		if oid != "" && !validLowerHex(oid, 40) {
			return false
		}
	}
	for _, digest := range []string{value.PathID, value.LastEntryID} {
		if digest != "" && !validLowerHex(digest, 64) {
			return false
		}
	}
	secondaryPresent := value.SecondaryDependencyKey != "" || value.SecondaryAnchorName != "" || value.SecondaryPublicationID != "" || value.SecondaryVaultRelationshipRowID != 0 || value.SecondaryCommitOID != "" || value.SecondaryTreeOID != ""
	if secondaryPresent && (value.SecondaryDependencyKey == "" || value.SecondaryPublicationID == "" || value.SecondaryVaultRelationshipRowID <= 0 || value.SecondaryCommitOID == "" || value.SecondaryTreeOID == "") {
		return false
	}
	if value.Kind == "search" {
		if !validSearchPhase(value.SearchPhase) || value.SearchObjectSize < 0 || (!value.SearchObjectSizeKnown && value.SearchObjectSize != 0) {
			return false
		}
	} else if value.SearchPhase != "" || value.SearchObjectSize != 0 || value.SearchObjectSizeKnown {
		return false
	}
	return true
}

func requestFingerprint(values ...string) string {
	digest := sha256.New()
	for _, value := range values {
		writeDigestPart(digest, []byte(value))
	}
	return hex.EncodeToString(digest.Sum(nil))
}
