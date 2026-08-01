// Package supervisor owns synchronous source-index generation builds.
package supervisor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/fsatomic"
	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/indexerprotocol"
	workflow "relay/internal/store/workflow"
)

const (
	MaxIndexerResponseBytes int64 = 1 << 20
	MaxIndexerStderrBytes   int64 = 4 << 10
)

var (
	ErrInvalidConfiguration        = errors.New("invalid supervisor configuration")
	ErrGenerationLifecycleConflict = errors.New("source-index generation lifecycle conflict")
	ErrAuthorityUnavailable        = errors.New("source-index authority unavailable")
	ErrChildProtocol               = errors.New("source-index child protocol failure")
	ErrStagedVerification          = errors.New("source-index staged verification failure")
	ErrPublication                 = errors.New("source-index publication failure")
	ErrPublicationAfterExposure    = errors.New("source-index publication reconciliation required")
	ErrFailureFinalization         = errors.New("source-index failure finalization required")
	ErrPersistenceAfterPublication = errors.New("source-index persistence failure after publication")
)

type GenerationStore interface {
	GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error)
	BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error)
	MarkSourceIndexGenerationReady(context.Context, workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error)
	MarkSourceIndexGenerationFailed(context.Context, workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error)
}
type SourceAuthority interface {
	AcquireSourceIndexLease(context.Context, sourceindex.GenerationIdentity) (SourceLease, error)
}
type SourceLease interface {
	RepositoryPath() string
	Close() error
}
type Config struct {
	IndexRoot        string
	IndexerPath      string
	ProtectedStorage sourceindex.ProtectedStorage
}
type Supervisor struct {
	store     GenerationStore
	authority SourceAuthority
	config    Config
}

func New(store GenerationStore, authority SourceAuthority, config Config) (*Supervisor, error) {
	if store == nil || authority == nil || !cleanAbsolute(config.IndexRoot) || !cleanAbsolute(config.IndexerPath) {
		return nil, ErrInvalidConfiguration
	}
	if err := sourceindex.ValidateIndexRoot(config.IndexRoot, config.ProtectedStorage); err != nil {
		return nil, fmt.Errorf("%w: index root", ErrInvalidConfiguration)
	}
	info, err := os.Lstat(config.IndexerPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !executable(info) {
		return nil, fmt.Errorf("%w: indexer path", ErrInvalidConfiguration)
	}
	return &Supervisor{store: store, authority: authority, config: config}, nil
}

func cleanAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func (s *Supervisor) BuildGeneration(ctx context.Context, generationID string) (workflow.SourceIndexGeneration, error) {
	if s == nil || !validGenerationID(generationID) {
		return workflow.SourceIndexGeneration{}, fmt.Errorf("%w: invalid generation", ErrGenerationLifecycleConflict)
	}
	if _, err := s.store.GetSourceIndexGeneration(ctx, generationID); err != nil {
		return workflow.SourceIndexGeneration{}, err
	}
	generation, err := s.store.BeginSourceIndexGenerationBuild(ctx, generationID)
	if err != nil {
		if errors.Is(err, workflow.ErrSourceIndexGenerationLifecycleConflict) {
			return workflow.SourceIndexGeneration{}, fmt.Errorf("%w: %v", ErrGenerationLifecycleConflict, err)
		}
		return workflow.SourceIndexGeneration{}, err
	}

	lease, _, _, err := s.acquireLease(ctx, generation)
	if err != nil {
		return s.fail(generationID, "source_unavailable", "source authority is unavailable", nil, "", ErrAuthorityUnavailable)
	}

	options := sourceindex.DefaultBuildOptions()
	if _, err := sourceindex.MarshalBuildOptions(options); err != nil {
		return s.fail(generationID, "build_options_mismatch", "default build options are invalid", lease, "", err)
	}
	digest, err := sourceindex.BuildOptionsSHA256(options)
	if err != nil || digest != generation.Identity.BuildOptionsSHA256 {
		return s.fail(generationID, "build_options_mismatch", "build options do not match generation", lease, "", err)
	}
	nonce, err := stagingNonce()
	if err != nil {
		return s.fail(generationID, "indexer_start_failed", "cannot create staging nonce", lease, "", err)
	}
	request := indexerprotocol.BuildRequest{Version: indexerprotocol.ProtocolVersion, GenerationID: generationID, Identity: generation.Identity, BuildOptions: options, RepositoryPath: lease.RepositoryPath(), IndexRoot: s.config.IndexRoot, StagingNonce: nonce}
	requestBytes, err := indexerprotocol.MarshalBuildRequest(request)
	if err != nil {
		return s.fail(generationID, "indexer_start_failed", "cannot construct indexer request", lease, nonce, err)
	}
	response, code, message, err := s.runChild(ctx, requestBytes, generationID)
	if err != nil {
		if ctx.Err() != nil {
			code, message = "cancelled", "source-index build cancelled"
		}
		return s.fail(generationID, code, message, lease, nonce, err)
	}
	if response.Status == indexerprotocol.BuildStatusFailed {
		return s.fail(generationID, response.Failure.Code, safeFailureMessage(response.Failure.Message), lease, nonce, errors.New("indexer reported failure"))
	}
	verified, err := s.verify(request, nonce, *response.Result)
	if err != nil {
		return s.fail(generationID, "verification_failed", "staged source-index verification failed", lease, nonce, ErrStagedVerification)
	}
	publication, err := s.publish(generationID, nonce)
	if err != nil {
		if publication.Exposed {
			closeErr := lease.Close()
			return workflow.SourceIndexGeneration{}, errors.Join(ErrPublicationAfterExposure, sanitizedError(err), sanitizedError(closeErr))
		}
		return s.fail(generationID, "publication_failed", "source-index publication failed", lease, nonce, ErrPublication)
	}
	ready, err := s.store.MarkSourceIndexGenerationReady(ctx, workflow.MarkSourceIndexGenerationReadyParams{GenerationID: generationID, GenerationManifestSHA256: verified.generation, CoverageManifestSHA256: verified.coverage, ArtifactManifestSHA256: verified.artifact})
	if err != nil {
		closeErr := lease.Close()
		return workflow.SourceIndexGeneration{}, errors.Join(ErrPublicationAfterExposure, ErrPersistenceAfterPublication, sanitizedError(closeErr))
	}
	if err := lease.Close(); err != nil {
		return workflow.SourceIndexGeneration{}, fmt.Errorf("%w: lease release", ErrPublicationAfterExposure)
	}
	return ready, nil
}

func (s *Supervisor) acquireLease(ctx context.Context, generation workflow.SourceIndexGeneration) (SourceLease, string, string, error) {
	lease, err := s.authority.AcquireSourceIndexLease(ctx, generation.Identity)
	if err != nil || lease == nil || !cleanAbsolute(lease.RepositoryPath()) {
		if lease != nil {
			_ = lease.Close()
		}
		return nil, "source_unavailable", "source authority is unavailable", errors.New("lease unavailable")
	}
	return lease, "", "", nil
}

type verifiedResult struct{ generation, coverage, artifact string }

func (s *Supervisor) verify(request indexerprotocol.BuildRequest, nonce string, result indexerprotocol.BuildResult) (verifiedResult, error) {
	expected, err := sourceindex.StagingRelativeDirectory(request.GenerationID, nonce)
	if err != nil || result.StagingRelativeDirectory != expected {
		return verifiedResult{}, errors.New("staging directory mismatch")
	}
	staging, err := sourceindex.StagingDirectory(s.config.IndexRoot, request.GenerationID, nonce)
	if err != nil {
		return verifiedResult{}, err
	}
	if err := indexer.Verify(staging, request, result.ShardCount); err != nil {
		return verifiedResult{}, err
	}
	gb, err := os.ReadFile(filepath.Join(staging, sourceindex.GenerationManifestFileName))
	if err != nil {
		return verifiedResult{}, err
	}
	cb, err := os.ReadFile(filepath.Join(staging, sourceindex.CoverageManifestFileName))
	if err != nil {
		return verifiedResult{}, err
	}
	ab, err := os.ReadFile(filepath.Join(staging, sourceindex.ArtifactManifestFileName))
	if err != nil {
		return verifiedResult{}, err
	}
	g, err := sourceindex.ParseGenerationManifest(gb)
	if err != nil {
		return verifiedResult{}, err
	}
	c, err := sourceindex.ParseCoverageManifest(cb)
	if err != nil {
		return verifiedResult{}, err
	}
	a, err := sourceindex.ParseArtifactManifest(ab)
	if err != nil {
		return verifiedResult{}, err
	}
	gd, err := sourceindex.GenerationManifestSHA256(g)
	if err != nil {
		return verifiedResult{}, err
	}
	cd, err := sourceindex.CoverageManifestSHA256(c)
	if err != nil {
		return verifiedResult{}, err
	}
	ad, err := sourceindex.ArtifactManifestSHA256(a)
	if err != nil {
		return verifiedResult{}, err
	}
	if gd != result.GenerationManifestSHA256 || cd != result.CoverageManifestSHA256 || ad != result.ArtifactManifestSHA256 || c.Counts != result.CoverageCounts {
		return verifiedResult{}, errors.New("reported artifact values mismatch")
	}
	return verifiedResult{gd, cd, ad}, nil
}

type PublicationResult struct{ Exposed bool }

func (s *Supervisor) publish(generationID, nonce string) (PublicationResult, error) {
	if err := sourceindex.ValidateIndexRoot(s.config.IndexRoot, s.config.ProtectedStorage); err != nil {
		return PublicationResult{}, err
	}
	if err := os.MkdirAll(s.config.IndexRoot, 0700); err != nil {
		return PublicationResult{}, err
	}
	rootInfo, err := os.Lstat(s.config.IndexRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return PublicationResult{}, errors.New("unsafe index root")
	}
	staging, err := sourceindex.StagingDirectory(s.config.IndexRoot, generationID, nonce)
	if err != nil {
		return PublicationResult{}, err
	}
	target, err := sourceindex.GenerationDirectory(s.config.IndexRoot, generationID)
	if err != nil {
		return PublicationResult{}, err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return PublicationResult{}, err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return PublicationResult{}, errors.New("unsafe generation parent")
	}
	if info, err := os.Lstat(staging); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return PublicationResult{}, errors.New("unsafe staging directory")
	}
	if _, err := os.Lstat(target); err == nil {
		return PublicationResult{}, errors.New("generation target exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return PublicationResult{}, err
	}
	if err := fsatomic.SyncDirectory(staging); err != nil {
		return PublicationResult{}, err
	}
	if err := fsatomic.SyncDirectory(parent); err != nil {
		return PublicationResult{}, err
	}
	if err := fsatomic.RenameNoReplace(staging, target); err != nil {
		return PublicationResult{}, err
	}
	if err := fsatomic.SyncDirectory(parent); err != nil {
		return PublicationResult{Exposed: true}, err
	}
	if err := fsatomic.SyncDirectory(s.config.IndexRoot); err != nil {
		return PublicationResult{Exposed: true}, err
	}
	return PublicationResult{Exposed: true}, nil
}

func (s *Supervisor) fail(generationID, code, message string, lease SourceLease, nonce string, cause error) (workflow.SourceIndexGeneration, error) {
	cause = sanitizedError(cause)
	var cleanupErr error
	if nonce != "" {
		cleanupErr = s.cleanup(generationID, nonce)
	}
	if lease != nil {
		cleanupErr = errors.Join(cleanupErr, lease.Close())
	}
	if cleanupErr != nil {
		return workflow.SourceIndexGeneration{}, errors.Join(cause, ErrFailureFinalization)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, markErr := s.store.MarkSourceIndexGenerationFailed(ctx, workflow.MarkSourceIndexGenerationFailedParams{GenerationID: generationID, FailureCode: safeFailureCode(code), FailureMessage: message})
	if markErr != nil {
		return workflow.SourceIndexGeneration{}, errors.Join(cause, errors.New("source-index failure finalization failed"))
	}
	return workflow.SourceIndexGeneration{}, cause
}

func (s *Supervisor) cleanup(generationID, nonce string) error {
	if err := sourceindex.ValidateIndexRoot(s.config.IndexRoot, s.config.ProtectedStorage); err != nil {
		return errors.New("unsafe index root")
	}
	rootInfo, err := os.Lstat(s.config.IndexRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errors.New("unsafe index root")
	}
	stagingParent := filepath.Join(s.config.IndexRoot, sourceindex.StagingDirectoryName)
	parentInfo, err := os.Lstat(stagingParent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return errors.New("unsafe staging parent")
	}
	path, err := sourceindex.StagingDirectory(s.config.IndexRoot, generationID, nonce)
	if err != nil {
		return errors.New("invalid owned staging directory")
	}
	if filepath.Dir(path) != stagingParent || filepath.Base(path) != generationID+"-"+nonce {
		return errors.New("owned staging path disagreement")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot inspect owned staging directory")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.IsDir() {
		return errors.New("owned staging path is not a directory")
	}
	return os.RemoveAll(path)
}

func stagingNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func validGenerationID(id string) bool {
	_, err := sourceindex.GenerationRelativeDirectory(id)
	return err == nil
}
func safeFailureCode(code string) string {
	if strings.TrimSpace(code) == code && code != "" && len(code) <= 128 {
		return code
	}
	return "indexer_process_failed"
}
func safeFailureMessage(message string) string {
	if strings.TrimSpace(message) == message && message != "" && len(message) <= 4096 {
		return message
	}
	return "indexer reported build failure"
}

func sanitizedError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrAuthorityUnavailable):
		return ErrAuthorityUnavailable
	case errors.Is(err, ErrChildProtocol):
		return ErrChildProtocol
	case errors.Is(err, ErrStagedVerification):
		return ErrStagedVerification
	case errors.Is(err, ErrPublication):
		return ErrPublication
	case errors.Is(err, ErrPublicationAfterExposure):
		return ErrPublicationAfterExposure
	case errors.Is(err, ErrFailureFinalization):
		return ErrFailureFinalization
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return context.Canceled
	default:
		return errors.New("source-index build failed")
	}
}

func (s *Supervisor) runChild(ctx context.Context, request []byte, generationID string) (indexerprotocol.BuildResponse, string, string, error) {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.Command(s.config.IndexerPath)
	configureProcess(cmd)
	cmd.Env = childEnvironment()
	cmd.Stdin = bytes.NewReader(request)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot prepare indexer output", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot prepare indexer diagnostics", err
	}
	if err := cmd.Start(); err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot start indexer", err
	}
	childReaped := make(chan struct{})
	var processMu sync.Mutex
	reaped := false
	go func() {
		select {
		case <-childCtx.Done():
			processMu.Lock()
			if !reaped {
				terminateProcess(cmd)
			}
			processMu.Unlock()
		case <-childReaped:
		}
	}()
	out := &boundedBuffer{limit: MaxIndexerResponseBytes, cancel: cancel}
	errout := &boundedBuffer{limit: MaxIndexerStderrBytes, cancel: cancel}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(out, stdout); done <- struct{}{} }()
	go func() { _, _ = io.Copy(errout, stderr); done <- struct{}{} }()
	waitResult := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		processMu.Lock()
		reaped = true
		close(childReaped)
		processMu.Unlock()
		waitResult <- waitErr
	}()
	waitErr := <-waitResult
	// Descendants may retain inherited pipes after the direct child exits.
	terminateResidualProcessGroup(cmd)
	_ = stdout.Close()
	_ = stderr.Close()
	copyDone := make(chan struct{})
	go func() { <-done; <-done; close(copyDone) }()
	select {
	case <-copyDone:
	case <-time.After(5 * time.Second):
		return indexerprotocol.BuildResponse{}, "indexer_process_failed", "indexer output did not terminate", errors.New("indexer pipes did not close")
	}
	if out.exceeded {
		return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer response exceeded supervisor limit", errors.New("indexer response exceeded limit")
	}
	if errout.exceeded {
		return indexerprotocol.BuildResponse{}, "indexer_process_failed", "indexer diagnostics exceeded supervisor limit", errors.New("indexer diagnostics exceeded limit")
	}
	response, parseErr := indexerprotocol.ParseBuildResponse(out.bytes())
	if parseErr != nil {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response is invalid", fmt.Errorf("%w: invalid response", ErrChildProtocol)
	}
	if response.GenerationID != "" && response.GenerationID != generationID {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response generation does not match", fmt.Errorf("%w: generation mismatch", ErrChildProtocol)
	}
	if waitErr == nil {
		if response.Status != indexerprotocol.BuildStatusSuccess || response.Result == nil || response.Failure != nil {
			return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer process and response disagree", fmt.Errorf("%w: exit mismatch", ErrChildProtocol)
		}
		return response, "", "", nil
	}
	if ctx.Err() != nil {
		return indexerprotocol.BuildResponse{}, "cancelled", "source-index build cancelled", ctx.Err()
	}
	if response.Status != indexerprotocol.BuildStatusFailed || response.Failure == nil || response.Result != nil {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer process and response disagree", fmt.Errorf("%w: exit mismatch", ErrChildProtocol)
	}
	return response, "", "", nil
}

type boundedBuffer struct {
	limit    int64
	data     []byte
	exceeded bool
	cancel   func()
	mu       sync.Mutex
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(len(b.data))+int64(len(p)) > b.limit {
		remaining := b.limit - int64(len(b.data))
		if remaining > 0 {
			b.data = append(b.data, p[:remaining]...)
		}
		b.exceeded = true
		if b.cancel != nil {
			b.cancel()
		}
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *boundedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

func childEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "SystemRoot": true, "WINDIR": true, "ComSpec": true, "TMP": true, "TEMP": true}
	result := make([]string, 0, len(allowed))
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if allowed[name] {
			result = append(result, value)
		}
	}
	return result
}
