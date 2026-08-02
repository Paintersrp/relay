// Package supervisor owns synchronous source-index generation builds.
package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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
	// processShutdownGrace bounds cleanup only after a process has been stopped
	// or a direct child has exited while descendants retain its pipes.
	processShutdownGrace = 5 * time.Second
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
	child     func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error)
	verifier  func(indexerprotocol.BuildRequest, string, indexerprotocol.BuildResult) (verifiedResult, error)
	publisher func(string, string) (PublicationResult, error)
	cleaner   func(string, string) error
	nonce     func() (string, error)
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

	lease, err := s.acquireLease(ctx, generation)
	if err != nil {
		if errors.Is(err, ErrFailureFinalization) {
			return workflow.SourceIndexGeneration{}, err
		}
		return s.fail(ctx, generationID, "source_unavailable", "source authority is unavailable", nil, "", ErrAuthorityUnavailable)
	}

	options := sourceindex.DefaultBuildOptions()
	if _, err := sourceindex.MarshalBuildOptions(options); err != nil {
		return s.fail(ctx, generationID, "build_options_mismatch", "default build options are invalid", lease, "", err)
	}
	digest, err := sourceindex.BuildOptionsSHA256(options)
	if err != nil || digest != generation.Identity.BuildOptionsSHA256 {
		return s.fail(ctx, generationID, "build_options_mismatch", "build options do not match generation", lease, "", err)
	}
	nonceFn := s.nonce
	if nonceFn == nil {
		nonceFn = stagingNonce
	}
	nonce, err := nonceFn()
	if err != nil {
		return s.fail(ctx, generationID, "indexer_start_failed", "cannot create staging nonce", lease, "", err)
	}
	request := indexerprotocol.BuildRequest{Version: indexerprotocol.ProtocolVersion, GenerationID: generationID, Identity: generation.Identity, BuildOptions: options, RepositoryPath: lease.RepositoryPath(), IndexRoot: s.config.IndexRoot, StagingNonce: nonce}
	requestBytes, err := indexerprotocol.MarshalBuildRequest(request)
	if err != nil {
		return s.fail(ctx, generationID, "indexer_start_failed", "cannot construct indexer request", lease, nonce, err)
	}
	child := s.child
	if child == nil {
		child = s.runChild
	}
	response, code, message, err := child(ctx, requestBytes, generationID)
	if err != nil {
		if ctx.Err() != nil {
			code, message = "cancelled", "source-index build cancelled"
		}
		return s.fail(ctx, generationID, code, message, lease, nonce, err)
	}
	if response.Status == indexerprotocol.BuildStatusFailed {
		return s.fail(ctx, generationID, response.Failure.Code, safeFailureMessage(response.Failure.Code), lease, nonce, errors.New("indexer reported failure"))
	}
	verifier := s.verifier
	if verifier == nil {
		verifier = s.verify
	}
	verified, err := verifier(request, nonce, *response.Result)
	if err != nil {
		return s.fail(ctx, generationID, "verification_failed", "staged source-index verification failed", lease, nonce, ErrStagedVerification)
	}
	publisher := s.publisher
	if publisher == nil {
		publisher = s.publish
	}
	publication, err := publisher(generationID, nonce)
	if err != nil {
		if publication.Exposed {
			closeErr := lease.Close()
			return workflow.SourceIndexGeneration{}, errors.Join(ErrPublicationAfterExposure, sanitizedError(err), sanitizedError(closeErr))
		}
		return s.fail(ctx, generationID, "publication_failed", "source-index publication failed", lease, nonce, ErrPublication)
	}
	if err := lease.Close(); err != nil {
		return workflow.SourceIndexGeneration{}, fmt.Errorf("%w: lease release", ErrPublicationAfterExposure)
	}
	ready, err := s.store.MarkSourceIndexGenerationReady(ctx, workflow.MarkSourceIndexGenerationReadyParams{GenerationID: generationID, GenerationManifestSHA256: verified.generation, CoverageManifestSHA256: verified.coverage, ArtifactManifestSHA256: verified.artifact})
	if err != nil {
		return workflow.SourceIndexGeneration{}, ErrPersistenceAfterPublication
	}
	return ready, nil
}

type ownedLease struct {
	lease SourceLease
	once  sync.Once
	err   error
}

func (l *ownedLease) RepositoryPath() string { return l.lease.RepositoryPath() }
func (l *ownedLease) Close() error {
	l.once.Do(func() { l.err = l.lease.Close() })
	return l.err
}

func (s *Supervisor) acquireLease(ctx context.Context, generation workflow.SourceIndexGeneration) (SourceLease, error) {
	lease, err := s.authority.AcquireSourceIndexLease(ctx, generation.Identity)
	if lease == nil {
		return nil, errors.New("lease unavailable")
	}
	owned := &ownedLease{lease: lease}
	if err != nil || !cleanAbsolute(lease.RepositoryPath()) {
		if closeErr := owned.Close(); closeErr != nil {
			return nil, ErrFailureFinalization
		}
		return nil, errors.New("lease unavailable")
	}
	return owned, nil
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

func (s *Supervisor) fail(lifecycle context.Context, generationID, code, message string, lease SourceLease, nonce string, cause error) (workflow.SourceIndexGeneration, error) {
	cause = sanitizedError(cause)
	var cleanupErr error
	if nonce != "" {
		cleaner := s.cleaner
		if cleaner == nil {
			cleaner = s.cleanup
		}
		cleanupErr = cleaner(generationID, nonce)
	}
	if lease != nil {
		cleanupErr = errors.Join(cleanupErr, lease.Close())
	}
	if cleanupErr != nil {
		return workflow.SourceIndexGeneration{}, errors.Join(cause, ErrFailureFinalization)
	}
	// Preserve lifecycle values while allowing finalization after cancellation.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(lifecycle), 5*time.Second)
	defer cancel()
	_, markErr := s.store.MarkSourceIndexGenerationFailed(ctx, workflow.MarkSourceIndexGenerationFailedParams{GenerationID: generationID, FailureCode: safeFailureCode(code), FailureMessage: safeFailureMessage(code)})
	if markErr != nil {
		return workflow.SourceIndexGeneration{}, errors.Join(cause, ErrFailureFinalization)
	}
	return workflow.SourceIndexGeneration{}, cause
}

func (s *Supervisor) cleanup(generationID, nonce string) error {
	if err := sourceindex.ValidateIndexRoot(s.config.IndexRoot, s.config.ProtectedStorage); err != nil {
		return errors.New("unsafe index root")
	}
	if _, err := sourceindex.StagingRelativeDirectory(generationID, nonce); err != nil {
		return errors.New("invalid owned staging directory")
	}
	if err := fsatomic.RemoveOwnedGenerationAttempt(s.config.IndexRoot, generationID, nonce); err != nil {
		return fmt.Errorf("cannot remove owned staging directory: %w", err)
	}
	return nil
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
func safeFailureMessage(code string) string {
	switch safeFailureCode(code) {
	case indexerprotocol.FailureInvalidRequest:
		return "source-index worker rejected its request"
	case indexerprotocol.FailureUnsafePath:
		return "source-index worker rejected a storage path"
	case indexerprotocol.FailureSourceUnavailable:
		return "source-index worker could not access source"
	case indexerprotocol.FailureSourceMismatch:
		return "source-index worker found different source content"
	case indexerprotocol.FailureTreeInvalid:
		return "source-index worker found an invalid source tree"
	case indexerprotocol.FailureObjectInvalid:
		return "source-index worker found an invalid source object"
	case indexerprotocol.FailureContentReadFailed:
		return "source-index worker could not read source content"
	case indexerprotocol.FailureIndexBuildFailed:
		return "source-index worker could not build the index"
	case indexerprotocol.FailureArtifactWrite:
		return "source-index worker could not write index artifacts"
	case indexerprotocol.FailureInternal:
		return "source-index worker encountered an internal failure"
	case "indexer_start_failed":
		return "source-index worker could not start"
	case "indexer_output_exceeded":
		return "source-index worker response exceeded limits"
	case "indexer_protocol_failed":
		return "source-index worker response was invalid"
	case "cancelled":
		return "source-index build cancelled"
	case "publication_failed":
		return "source-index publication failed"
	case "build_options_mismatch":
		return "source-index build options did not match generation"
	default:
		return "source-index worker reported build failure"
	}
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

func (s *Supervisor) finishChild(out *boundedBuffer, waitErr error, generationID string) (indexerprotocol.BuildResponse, string, string, error) {
	response, parseErr := indexerprotocol.ParseBuildResponse(out.bytes())
	if parseErr != nil {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response is invalid", fmt.Errorf("%w: invalid response", ErrChildProtocol)
	}
	if response.GenerationID != "" && response.GenerationID != generationID {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response generation does not match", fmt.Errorf("%w: generation mismatch", ErrChildProtocol)
	}
	if waitErr == nil && response.Status == indexerprotocol.BuildStatusSuccess && response.Result != nil && response.Failure == nil {
		return response, "", "", nil
	}
	if waitErr != nil && response.Status == indexerprotocol.BuildStatusFailed && response.Failure != nil && response.Result == nil {
		return response, "", "", nil
	}
	return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer process and response disagree", fmt.Errorf("%w: exit mismatch", ErrChildProtocol)
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
