package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/indexerprotocol"
)

func commandGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, err := c.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(b))
}

func commandRequest(t *testing.T) indexerprotocol.BuildRequest {
	t.Helper()
	root := t.TempDir()
	work, repo := filepath.Join(root, "work"), filepath.Join(root, "repo.git")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	commandGit(t, work, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(work, "readme.txt"), []byte("alpha beta gamma"), 0600); err != nil {
		t.Fatal(err)
	}
	commandGit(t, work, "add", "--all")
	commandGit(t, work, "commit", "--quiet", "-m", "fixture")
	commit, tree := commandGit(t, work, "rev-parse", "HEAD"), commandGit(t, work, "rev-parse", "HEAD^{tree}")
	commandGit(t, work, "clone", "--quiet", "--bare", ".", repo)
	options := sourceindex.DefaultBuildOptions()
	digest, err := sourceindex.BuildOptionsSHA256(options)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", commit, tree, digest)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return indexerprotocol.BuildRequest{Version: indexerprotocol.ProtocolVersion, GenerationID: id, Identity: identity, BuildOptions: options, RepositoryPath: repo, IndexRoot: filepath.Join(root, "index"), StagingNonce: strings.Repeat("d", 32)}
}

func TestCommandCanonicalProcessContract(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "relay-source-indexer")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = "."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command: %v\n%s", err, output)
	}
	r := commandRequest(t)
	raw, err := indexerprotocol.MarshalBuildRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	run := func(input []byte) ([]byte, error) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cmd := exec.CommandContext(ctx, exe)
		cmd.Stdin = bytes.NewReader(input)
		return cmd.Output()
	}
	out, err := run(raw)
	if runtime.GOOS != "windows" && err != nil {
		t.Fatalf("successful command exited nonzero: %v", err)
	}
	if runtime.GOOS == "windows" && err == nil {
		t.Fatal("unsupported platform command succeeded")
	}
	response, parseErr := indexerprotocol.ParseBuildResponse(out)
	if parseErr != nil {
		t.Fatalf("response is not canonical: %v; %q", parseErr, out)
	}
	if bytes.HasSuffix(out, []byte{'\n'}) || response.Version != indexerprotocol.ProtocolVersion {
		t.Fatal("response framing or version is invalid")
	}
	if runtime.GOOS != "windows" {
		if response.Result == nil || response.Status != indexerprotocol.BuildStatusSuccess {
			t.Fatalf("successful request returned failure: %#v", response)
		}
		root := filepath.Join(r.IndexRoot, filepath.FromSlash(response.Result.StagingRelativeDirectory))
		if err := indexer.Verify(root, r, response.Result.ShardCount); err != nil {
			t.Fatalf("command generation failed verification: %v", err)
		}
	}
	failed, err := run([]byte(`{"version":"relay.source-indexer-protocol.v1","unknown":1}`))
	if err == nil {
		t.Fatal("invalid request exited zero")
	}
	failure, parseErr := indexerprotocol.ParseBuildResponse(failed)
	if parseErr != nil || failure.Failure == nil || failure.Failure.Code != indexerprotocol.FailureInvalidRequest {
		t.Fatalf("invalid request response: %v %#v", parseErr, failure)
	}
	oversized, err := run(bytes.Repeat([]byte{'x'}, indexerprotocol.MaxRequestBytes+1))
	if err == nil {
		t.Fatal("oversized request exited zero")
	}
	if response, parseErr := indexerprotocol.ParseBuildResponse(oversized); parseErr != nil || response.Failure == nil || response.Failure.Code != indexerprotocol.FailureInvalidRequest {
		t.Fatalf("oversized request response: %v %#v", parseErr, response)
	}
	if runtime.GOOS != "windows" {
		failedRequest := r
		failedRequest.RepositoryPath = filepath.Join(t.TempDir(), "missing-repository")
		failedRequest.IndexRoot = filepath.Join(t.TempDir(), "failed-index")
		failedRaw, err := indexerprotocol.MarshalBuildRequest(failedRequest)
		if err != nil {
			t.Fatal(err)
		}
		failed, err := run(failedRaw)
		if err == nil {
			t.Fatal("handled build failure exited zero")
		}
		failure, parseErr := indexerprotocol.ParseBuildResponse(failed)
		if parseErr != nil || failure.Failure == nil {
			t.Fatalf("handled failure response: %v %#v", parseErr, failure)
		}
		if _, err := os.Stat(filepath.Join(failedRequest.IndexRoot, sourceindex.StagingDirectoryName)); err == nil {
			t.Fatal("failed command left a usable staging target")
		}
	}
}
