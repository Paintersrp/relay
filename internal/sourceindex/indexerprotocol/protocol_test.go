package indexerprotocol

import (
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/sourceindex"
)

func request(t *testing.T) BuildRequest {
	t.Helper()
	o := sourceindex.DefaultBuildOptions()
	d, err := sourceindex.BuildOptionsSHA256(o)
	if err != nil {
		t.Fatal(err)
	}
	i, err := sourceindex.NewGenerationIdentity("vault", strings.Repeat("1", 40), strings.Repeat("2", 40), d)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(i)
	if err != nil {
		t.Fatal(err)
	}
	return BuildRequest{ProtocolVersion, id, i, o, filepath.Join(t.TempDir(), "repo"), filepath.Join(t.TempDir(), "index"), strings.Repeat("3", 32)}
}
func TestRequestCanonical(t *testing.T) {
	r := request(t)
	raw, err := MarshalBuildRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ParseBuildRequest(raw); err != nil || got != r {
		t.Fatalf("parse: %#v %v", got, err)
	}
	for _, bad := range [][]byte{append(raw, ' '), append(raw, []byte(`{}`)...), []byte(`{"unknown":1}`)} {
		if _, err := ParseBuildRequest(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestResponseCanonical(t *testing.T) {
	r := request(t)
	v := BuildResponse{Version: ProtocolVersion, Status: BuildStatusSuccess, GenerationID: r.GenerationID, Result: &BuildResult{StagingRelativeDirectory: "staging/" + r.GenerationID + "-" + r.StagingNonce, GenerationManifestSHA256: strings.Repeat("1", 64), CoverageManifestSHA256: strings.Repeat("2", 64), ArtifactManifestSHA256: strings.Repeat("3", 64), CoverageCounts: sourceindex.CoverageCounts{Total: 1}, ShardCount: 1}}
	b, err := MarshalBuildResponse(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseBuildResponse(b); err != nil {
		t.Fatal(err)
	}
	f := BuildResponse{Version: ProtocolVersion, Status: BuildStatusFailed, Failure: &BuildFailure{Code: "invalid_request", Message: "invalid request"}}
	b, err = MarshalBuildResponse(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseBuildResponse(b); err != nil {
		t.Fatal(err)
	}
	f.Failure.Message = strings.Repeat("x", 4097)
	if _, err = MarshalBuildResponse(f); err == nil {
		t.Fatal("accepted unbounded failure")
	}
}
