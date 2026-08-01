// Package indexerprotocol defines the canonical stdin/stdout indexer protocol.
package indexerprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"relay/internal/sourceindex"
)

const ProtocolVersion = "relay.source-indexer-protocol.v1"

type BuildRequest struct {
	Version        string                         `json:"version"`
	GenerationID   string                         `json:"generation_id"`
	Identity       sourceindex.GenerationIdentity `json:"identity"`
	BuildOptions   sourceindex.BuildOptions       `json:"build_options"`
	RepositoryPath string                         `json:"repository_path"`
	IndexRoot      string                         `json:"index_root"`
	StagingNonce   string                         `json:"staging_nonce"`
}
type BuildStatus string

const (
	BuildStatusSuccess BuildStatus = "success"
	BuildStatusFailed  BuildStatus = "failed"
)

type BuildResult struct {
	StagingRelativeDirectory string                     `json:"staging_relative_directory"`
	GenerationManifestSHA256 string                     `json:"generation_manifest_sha256"`
	CoverageManifestSHA256   string                     `json:"coverage_manifest_sha256"`
	ArtifactManifestSHA256   string                     `json:"artifact_manifest_sha256"`
	CoverageCounts           sourceindex.CoverageCounts `json:"coverage_counts"`
	ShardCount               int64                      `json:"shard_count"`
}
type BuildFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type BuildResponse struct {
	Version      string        `json:"version"`
	Status       BuildStatus   `json:"status"`
	GenerationID string        `json:"generation_id,omitempty"`
	Result       *BuildResult  `json:"result,omitempty"`
	Failure      *BuildFailure `json:"failure,omitempty"`
}

func cleanAbsolute(p string) bool { return p != "" && filepath.IsAbs(p) && filepath.Clean(p) == p }
func hex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validateRequest(v BuildRequest) error {
	if v.Version != ProtocolVersion || !hex(v.GenerationID, 64) || !cleanAbsolute(v.RepositoryPath) || !cleanAbsolute(v.IndexRoot) {
		return errors.New("invalid request")
	}
	if _, err := sourceindex.MarshalGenerationIdentity(v.Identity); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if _, err := sourceindex.MarshalBuildOptions(v.BuildOptions); err != nil {
		return fmt.Errorf("build options: %w", err)
	}
	id, err := sourceindex.GenerationID(v.Identity)
	if err != nil || id != v.GenerationID {
		return errors.New("generation id")
	}
	d, err := sourceindex.BuildOptionsSHA256(v.BuildOptions)
	if err != nil || d != v.Identity.BuildOptionsSHA256 {
		return errors.New("build options digest")
	}
	if _, err := sourceindex.StagingRelativeDirectory(v.GenerationID, v.StagingNonce); err != nil {
		return errors.New("staging nonce")
	}
	return nil
}
func validateResponse(v BuildResponse) error {
	if v.Version != ProtocolVersion || (v.Status != BuildStatusSuccess && v.Status != BuildStatusFailed) {
		return errors.New("status")
	}
	if v.GenerationID != "" && !hex(v.GenerationID, 64) {
		return errors.New("generation id")
	}
	if v.Status == BuildStatusSuccess {
		if !hex(v.GenerationID, 64) || v.Result == nil || v.Failure != nil || v.Result.ShardCount <= 0 || !hex(v.Result.GenerationManifestSHA256, 64) || !hex(v.Result.CoverageManifestSHA256, 64) || !hex(v.Result.ArtifactManifestSHA256, 64) || v.Result.CoverageCounts.Total < 0 {
			return errors.New("success response")
		}
		if !strings.HasPrefix(v.Result.StagingRelativeDirectory, sourceindex.StagingDirectoryName+"/") || strings.Contains(v.Result.StagingRelativeDirectory, "\\") {
			return errors.New("staging directory")
		}
		return nil
	}
	if v.Result != nil || v.Failure == nil || len(v.Failure.Code) == 0 || len(v.Failure.Code) > 128 || len(v.Failure.Message) == 0 || len(v.Failure.Message) > 4096 {
		return errors.New("failure response")
	}
	return nil
}
func marshal(v any, validate func() error) ([]byte, error) {
	if err := validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func parse(raw []byte, out any, validate func() error) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var x any
	if err := d.Decode(&x); !errors.Is(err, io.EOF) {
		return errors.New("trailing json")
	}
	if err := validate(); err != nil {
		return err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, b) {
		return errors.New("noncanonical json")
	}
	return nil
}
func MarshalBuildRequest(v BuildRequest) ([]byte, error) {
	return marshal(v, func() error { return validateRequest(v) })
}
func ParseBuildRequest(raw []byte) (BuildRequest, error) {
	var v BuildRequest
	return v, parse(raw, &v, func() error { return validateRequest(v) })
}
func MarshalBuildResponse(v BuildResponse) ([]byte, error) {
	return marshal(v, func() error { return validateResponse(v) })
}
func ParseBuildResponse(raw []byte) (BuildResponse, error) {
	var v BuildResponse
	return v, parse(raw, &v, func() error { return validateResponse(v) })
}
