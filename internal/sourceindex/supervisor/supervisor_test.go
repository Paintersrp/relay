package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexerprotocol"
	workflow "relay/internal/store/workflow"
)

func TestNewValidatesConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	indexer, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	authority := &testAuthority{}
	if _, err := New(store, authority, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); err != nil {
		t.Fatalf("New valid configuration: %v", err)
	}
	for _, config := range []Config{
		{IndexRoot: "relative", IndexerPath: indexer},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: "relative"},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: filepath.Join(root, "missing")},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: root},
		{IndexRoot: filepath.Join(root, "source"), IndexerPath: indexer, ProtectedStorage: sourceindex.ProtectedStorage{SourceVaultRoot: root}},
	} {
		if _, err := New(store, authority, config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Errorf("New(%+v) error = %v, want invalid configuration", config, err)
		}
	}
	if _, err := New(nil, authority, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("nil store error = %v", err)
	}
	if _, err := New(store, nil, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("nil authority error = %v", err)
	}
}

func TestCleanupRemovesOnlyExactOwnedStaging(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("anchored staging cleanup is supported on Linux")
	}
	root := t.TempDir()
	generationID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nonce := "0123456789abcdef0123456789abcdef"
	owned := filepath.Join(root, sourceindex.StagingDirectoryName, generationID+"-"+nonce)
	other := filepath.Join(root, sourceindex.StagingDirectoryName, generationID+"-"+"ffffffffffffffffffffffffffffffff")
	otherGeneration := strings.Repeat("b", 64)
	sameNonceOtherGeneration := filepath.Join(root, sourceindex.StagingDirectoryName, otherGeneration+"-"+nonce)
	private, err := sourceindex.PrivateBuildDirectory(root, generationID, nonce)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{owned, private, other, sameNonceOtherGeneration} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s := &Supervisor{config: Config{IndexRoot: root}}
	if err := s.cleanup(generationID, nonce); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Lstat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned staging remains: %v", err)
	}
	if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned private staging remains: %v", err)
	}
	for _, path := range []string{other, sameNonceOtherGeneration} {
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			t.Fatalf("unowned staging %s was changed: %v", path, err)
		}
	}
}

func TestCleanupRejectsSymlinkStagingParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("anchored staging cleanup is supported on Linux")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, sourceindex.StagingDirectoryName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	s := &Supervisor{config: Config{IndexRoot: root}}
	if err := s.cleanup("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("cleanup accepted a symlink staging parent")
	}
}

func TestSafeFailureMessageIsSupervisorOwned(t *testing.T) {
	for _, code := range []string{
		"invalid_request", "unsafe_path", "source_unavailable", "source_mismatch",
		"tree_invalid", "object_invalid", "content_read_failed", "index_build_failed",
		"artifact_write_failed", "verification_failed", "cancelled", "internal",
	} {
		message := safeFailureMessage(code)
		if message == "" || message == code {
			t.Fatalf("safeFailureMessage(%q) = %q", code, message)
		}
	}
	malicious := "/private/repository TOKEN=secret\nstack trace\ngit fetch origin"
	if got := safeFailureMessage(malicious); got == malicious || got != "source-index worker reported build failure" {
		t.Fatalf("untrusted child text reached persistence message: %q", got)
	}
}

type testStore struct{}
type testAuthority struct{}

func (*testStore) GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) MarkSourceIndexGenerationReady(context.Context, workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) MarkSourceIndexGenerationFailed(context.Context, workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testAuthority) AcquireSourceIndexLease(context.Context, sourceindex.GenerationIdentity) (SourceLease, error) {
	return nil, errors.New("unexpected call")
}

type finalizationStore struct {
	marks  int
	params workflow.MarkSourceIndexGenerationFailedParams
	ctx    context.Context
}

func (s *finalizationStore) GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) MarkSourceIndexGenerationReady(context.Context, workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) MarkSourceIndexGenerationFailed(ctx context.Context, params workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error) {
	s.marks++
	s.params, s.ctx = params, ctx
	return workflow.SourceIndexGeneration{}, nil
}

type finalizationLease struct{ closes int }

func (*finalizationLease) RepositoryPath() string { return "/retained/repository" }
func (l *finalizationLease) Close() error         { l.closes++; return nil }

func TestFailFinalizesAfterCallerCancellationAndCleansExactAttempt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-rooted cleanup is Linux-only")
	}
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	canonical, err := sourceindex.StagingDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	private, err := sourceindex.PrivateBuildDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{canonical, private} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store := &finalizationStore{}
	lease := &finalizationLease{}
	s := &Supervisor{store: store, config: Config{IndexRoot: root}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.fail(ctx, id, "cancelled", "ignored", lease, nonce, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("fail error = %v", err)
	}
	if store.marks != 1 || store.params.FailureCode != "cancelled" || store.ctx.Err() != nil {
		t.Fatalf("finalization = %#v marks=%d context=%v", store.params, store.marks, store.ctx.Err())
	}
	if lease.closes != 1 {
		t.Fatalf("lease closes = %d, want 1", lease.closes)
	}
	for _, path := range []string{canonical, private} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned attempt remains at %s: %v", path, err)
		}
	}
}

func TestFailPreservesUnsafeAttemptWhenCleanupFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-rooted cleanup is Linux-only")
	}
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	canonical, err := sourceindex.StagingDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(canonical, 0700); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(canonical, "unknown")
	if err := os.WriteFile(unsafe, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &finalizationStore{}
	lease := &finalizationLease{}
	s := &Supervisor{store: store, config: Config{IndexRoot: root}}
	if _, err := s.fail(context.Background(), id, "internal", "ignored", lease, nonce, errors.New("child failed")); !errors.Is(err, ErrFailureFinalization) {
		t.Fatalf("fail error = %v", err)
	}
	if store.marks != 0 || lease.closes != 1 {
		t.Fatalf("marks=%d closes=%d", store.marks, lease.closes)
	}
	if _, err := os.Lstat(unsafe); err != nil {
		t.Fatalf("unsafe content was not preserved: %v", err)
	}
}

type supervisorStore struct {
	mu                sync.Mutex
	generation        workflow.SourceIndexGeneration
	events            *[]string
	failed            []workflow.MarkSourceIndexGenerationFailedParams
	failedCtx         []context.Context
	failedCtxErr      []error
	failedDeadline    []time.Time
	failedHasDeadline []bool
	ready             int
	failMark          error
	failReady         error
}

func (s *supervisorStore) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.events = append(*s.events, event)
}
func (s *supervisorStore) GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error) {
	s.record("get")
	return s.generation, nil
}
func (s *supervisorStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error) {
	s.record("begin")
	return s.generation, nil
}
func (s *supervisorStore) MarkSourceIndexGenerationReady(_ context.Context, p workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error) {
	s.record("ready")
	s.mu.Lock()
	s.ready++
	s.mu.Unlock()
	return s.generation, s.failReady
}
func (s *supervisorStore) MarkSourceIndexGenerationFailed(ctx context.Context, p workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error) {
	s.record("failed")
	s.mu.Lock()
	s.failed = append(s.failed, p)
	s.failedCtx = append(s.failedCtx, ctx)
	s.failedCtxErr = append(s.failedCtxErr, ctx.Err())
	deadline, ok := ctx.Deadline()
	s.failedDeadline = append(s.failedDeadline, deadline)
	s.failedHasDeadline = append(s.failedHasDeadline, ok)
	s.mu.Unlock()
	return s.generation, s.failMark
}

type supervisorLease struct {
	mu         sync.Mutex
	events     *[]string
	repository string
	closes     int
	err        error
}

func (l *supervisorLease) RepositoryPath() string { return l.repository }
func (l *supervisorLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closes++
	*l.events = append(*l.events, "close")
	return l.err
}

type supervisorAuthority struct{ lease *supervisorLease }

func (a *supervisorAuthority) AcquireSourceIndexLease(context.Context, sourceindex.GenerationIdentity) (SourceLease, error) {
	*a.lease.events = append(*a.lease.events, "acquire")
	return a.lease, nil
}

type supervisorHarness struct {
	s                   *Supervisor
	store               *supervisorStore
	lease               *supervisorLease
	events              []string
	id, nonce           string
	cleaned             [][2]string
	verified, published int
}

func newSupervisorHarness(t *testing.T) *supervisorHarness {
	t.Helper()
	digest, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", strings.Repeat("1", 40), strings.Repeat("2", 40), digest)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	h := &supervisorHarness{id: id, nonce: strings.Repeat("a", 32)}
	h.store = &supervisorStore{generation: workflow.SourceIndexGeneration{GenerationID: id, Identity: identity, State: workflow.SourceIndexGenerationBuilding}, events: &h.events}
	h.lease = &supervisorLease{events: &h.events, repository: t.TempDir()}
	h.s = &Supervisor{store: h.store, authority: &supervisorAuthority{lease: h.lease}, config: Config{IndexRoot: filepath.Join(t.TempDir(), "index"), IndexerPath: filepath.Join(t.TempDir(), "indexer")}}
	h.s.nonce = func() (string, error) { return h.nonce, nil }
	h.s.cleaner = func(generationID, nonce string) error {
		h.events = append(h.events, "cleanup")
		h.cleaned = append(h.cleaned, [2]string{generationID, nonce})
		return nil
	}
	h.s.verifier = func(indexerprotocol.BuildRequest, string, indexerprotocol.BuildResult) (verifiedResult, error) {
		h.verified++
		return verifiedResult{strings.Repeat("3", 64), strings.Repeat("4", 64), strings.Repeat("5", 64)}, nil
	}
	h.s.publisher = func(string, string) (PublicationResult, error) {
		h.published++
		return PublicationResult{Exposed: true}, nil
	}
	return h
}

func successResponse(h *supervisorHarness) indexerprotocol.BuildResponse {
	return indexerprotocol.BuildResponse{Version: indexerprotocol.ProtocolVersion, Status: indexerprotocol.BuildStatusSuccess, GenerationID: h.id, Result: &indexerprotocol.BuildResult{StagingRelativeDirectory: sourceindex.StagingDirectoryName + "/" + h.id + "-" + h.nonce, GenerationManifestSHA256: strings.Repeat("3", 64), CoverageManifestSHA256: strings.Repeat("4", 64), ArtifactManifestSHA256: strings.Repeat("5", 64), CoverageCounts: sourceindex.CoverageCounts{Total: 1, IndexedText: 1}, ShardCount: 1}}
}

func assertOrdinaryFailure(t *testing.T, h *supervisorHarness, code string, err error, sentinel error) {
	t.Helper()
	if sentinel != nil && !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	if len(h.store.failed) != 1 || h.store.failed[0].GenerationID != h.id || h.store.failed[0].FailureCode != code || h.store.failed[0].FailureMessage != safeFailureMessage(code) {
		t.Fatalf("failed calls = %#v", h.store.failed)
	}
	if h.lease.closes != 1 || len(h.cleaned) != 1 || h.cleaned[0] != [2]string{h.id, h.nonce} {
		t.Fatalf("closes=%d cleaned=%v", h.lease.closes, h.cleaned)
	}
	if got := strings.Join(h.events[len(h.events)-3:], ","); got != "cleanup,close,failed" {
		t.Fatalf("finalization order = %s; all=%v", got, h.events)
	}
}

func TestSupervisorPreExposureFailureClassesSUP01SUP04ThroughSUP09SUP17ThroughSUP21(t *testing.T) {
	childCause := errors.New("start cause")
	cases := []struct {
		name, code string
		sentinel   error
		configure  func(*supervisorHarness)
	}{
		{"SUP-01 start", "indexer_start_failed", nil, func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return indexerprotocol.BuildResponse{}, "indexer_start_failed", "ignored", childCause
			}
		}},
		{"SUP-04 output", "indexer_output_exceeded", nil, func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				out := &boundedBuffer{limit: MaxIndexerResponseBytes}
				_, _ = out.Write(make([]byte, MaxIndexerResponseBytes+1))
				if !out.exceeded {
					panic("test output did not exceed supervisor limit")
				}
				return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "ignored", errors.New("overflow")
			}
		}},
		{"SUP-06 mismatch", "indexer_protocol_failed", ErrChildProtocol, func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				b, _ := indexerprotocol.MarshalBuildResponse(successResponse(h))
				out := &boundedBuffer{limit: MaxIndexerResponseBytes}
				_, _ = out.Write(b)
				return h.s.finishChild(out, nil, strings.Repeat("f", 64))
			}
		}},
		{"SUP-08 verification", "verification_failed", ErrStagedVerification, func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return successResponse(h), "", "", nil
			}
			h.s.verifier = func(indexerprotocol.BuildRequest, string, indexerprotocol.BuildResult) (verifiedResult, error) {
				h.verified++
				return verifiedResult{}, errors.New("bad staging")
			}
		}},
		{"SUP-09 publication", "publication_failed", ErrPublication, func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return successResponse(h), "", "", nil
			}
			h.s.publisher = func(string, string) (PublicationResult, error) {
				h.published++
				return PublicationResult{Exposed: false}, errors.New("publish")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSupervisorHarness(t)
			tc.configure(h)
			_, err := h.s.BuildGeneration(context.Background(), h.id)
			assertOrdinaryFailure(t, h, tc.code, err, tc.sentinel)
			if tc.name == "SUP-06 mismatch" && (h.verified != 0 || h.published != 0) {
				t.Fatalf("mismatch reached verify/publish")
			}
		})
	}

	malformed := []struct {
		name string
		raw  []byte
		wait error
	}{
		{"invalid JSON", []byte("{"), nil}, {"trailing JSON", []byte("{}{}"), nil}, {"unsupported version", []byte(`{"version":"future","status":"failed","failure":{"code":"internal","message":"x"}}`), errors.New("exit")}, {"missing fields", []byte(`{"version":"relay-source-index-v1","status":"success"}`), nil}, {"success without result", []byte(`{"version":"relay-source-index-v1","status":"success","generation_id":"` + strings.Repeat("1", 64) + `"}`), nil}, {"failure without details", []byte(`{"version":"relay-source-index-v1","status":"failed"}`), errors.New("exit")},
	}
	for _, tc := range malformed {
		t.Run("SUP-05 "+tc.name, func(t *testing.T) {
			h := newSupervisorHarness(t)
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				out := &boundedBuffer{limit: MaxIndexerResponseBytes}
				_, _ = out.Write(tc.raw)
				return h.s.finishChild(out, tc.wait, h.id)
			}
			_, err := h.s.BuildGeneration(context.Background(), h.id)
			assertOrdinaryFailure(t, h, "indexer_protocol_failed", err, ErrChildProtocol)
		})
	}

	for _, code := range []string{"invalid_request", "unsafe_path", "source_unavailable", "source_mismatch", "tree_invalid", "object_invalid", "content_read_failed", "index_build_failed", "artifact_write_failed", "internal", "cancelled"} {
		t.Run("SUP-07 "+code, func(t *testing.T) {
			h := newSupervisorHarness(t)
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return indexerprotocol.BuildResponse{Version: indexerprotocol.ProtocolVersion, Status: indexerprotocol.BuildStatusFailed, Failure: &indexerprotocol.BuildFailure{Code: code, Message: "secret arbitrary diagnostic"}}, "", "", nil
			}
			_, err := h.s.BuildGeneration(context.Background(), h.id)
			assertOrdinaryFailure(t, h, code, err, nil)
			if strings.Contains(h.store.failed[0].FailureMessage, "secret") {
				t.Fatal("child diagnostic persisted")
			}
		})
	}
}

func TestSupervisorCancellationAndForcedTerminationSUP02SUP03SUP16(t *testing.T) {
	for _, name := range []string{"SUP-02 cancellation", "SUP-03 forced termination"} {
		t.Run(name, func(t *testing.T) {
			h := newSupervisorHarness(t)
			entered := make(chan struct{})
			terminated := make(chan struct{})
			h.s.child = func(ctx context.Context, _ []byte, _ string) (indexerprotocol.BuildResponse, string, string, error) {
				close(entered)
				<-ctx.Done()
				close(terminated)
				return indexerprotocol.BuildResponse{}, "cancelled", "ignored", ctx.Err()
			}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() { _, err := h.s.BuildGeneration(ctx, h.id); result <- err }()
			<-entered
			cancel()
			err := <-result
			<-terminated
			assertOrdinaryFailure(t, h, "cancelled", err, context.Canceled)
			if h.store.failedCtxErr[0] != nil {
				t.Fatalf("finalization context cancelled at call: %v", h.store.failedCtxErr[0])
			}
			if !h.store.failedHasDeadline[0] || time.Until(h.store.failedDeadline[0]) <= 0 {
				t.Fatalf("finalization deadline = %v, %v", h.store.failedDeadline[0], h.store.failedHasDeadline[0])
			}
		})
	}
}

func TestSupervisorPublicationUncertaintySUP10ThroughSUP13SUP22(t *testing.T) {
	tests := []struct {
		name                   string
		configure              func(*supervisorHarness)
		sentinel               error
		cleanup, failed, ready int
	}{
		{"SUP-10 publication after exposure", func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return successResponse(h), "", "", nil
			}
			h.s.publisher = func(string, string) (PublicationResult, error) {
				return PublicationResult{Exposed: true}, errors.New("sync")
			}
		}, ErrPublicationAfterExposure, 0, 0, 0},
		{"SUP-11 ready persistence", func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return successResponse(h), "", "", nil
			}
			h.store.failReady = errors.New("ready")
		}, ErrPersistenceAfterPublication, 0, 0, 1},
		{"SUP-13 lease after exposure", func(h *supervisorHarness) {
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return successResponse(h), "", "", nil
			}
			h.lease.err = errors.New("close")
		}, ErrPublicationAfterExposure, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSupervisorHarness(t)
			tc.configure(h)
			_, err := h.s.BuildGeneration(context.Background(), h.id)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error=%v want %v", err, tc.sentinel)
			}
			if len(h.cleaned) != tc.cleanup || len(h.store.failed) != tc.failed || h.store.ready != tc.ready || h.lease.closes != 1 {
				t.Fatalf("cleanup=%d failed=%d ready=%d closes=%d", len(h.cleaned), len(h.store.failed), h.store.ready, h.lease.closes)
			}
		})
	}

	t.Run("SUP-12 lease before exposure", func(t *testing.T) {
		h := newSupervisorHarness(t)
		h.lease.err = errors.New("close")
		h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
			return indexerprotocol.BuildResponse{}, "indexer_start_failed", "", errors.New("start")
		}
		_, err := h.s.BuildGeneration(context.Background(), h.id)
		if !errors.Is(err, ErrFailureFinalization) || len(h.cleaned) != 1 || h.lease.closes != 1 || len(h.store.failed) != 0 {
			t.Fatalf("error=%v cleanup=%v closes=%d failed=%v", err, h.cleaned, h.lease.closes, h.store.failed)
		}
	})
}

func TestSupervisorFinalizationFailuresSUP14SUP15SUP18ThroughSUP22(t *testing.T) {
	t.Run("SUP-14 cleanup failure", func(t *testing.T) {
		h := newSupervisorHarness(t)
		h.s.cleaner = func(id, nonce string) error {
			h.events = append(h.events, "cleanup")
			h.cleaned = append(h.cleaned, [2]string{id, nonce})
			return errors.New("cleanup")
		}
		h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
			return indexerprotocol.BuildResponse{}, "indexer_start_failed", "", errors.New("start")
		}
		_, err := h.s.BuildGeneration(context.Background(), h.id)
		if !errors.Is(err, ErrFailureFinalization) || h.lease.closes != 1 || len(h.store.failed) != 0 || h.cleaned[0] != [2]string{h.id, h.nonce} {
			t.Fatalf("error=%v closes=%d failed=%v cleaned=%v", err, h.lease.closes, h.store.failed, h.cleaned)
		}
	})
	t.Run("SUP-15 persistence failure", func(t *testing.T) {
		h := newSupervisorHarness(t)
		h.store.failMark = errors.New("persist")
		h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
			return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "", ErrChildProtocol
		}
		_, err := h.s.BuildGeneration(context.Background(), h.id)
		if !errors.Is(err, ErrFailureFinalization) || !errors.Is(err, ErrChildProtocol) || len(h.store.failed) != 1 || len(h.cleaned) != 1 || h.lease.closes != 1 {
			t.Fatalf("error=%v failed=%d cleaned=%d closes=%d", err, len(h.store.failed), len(h.cleaned), h.lease.closes)
		}
	})
	for _, code := range []string{" bad", strings.Repeat("x", 129)} {
		t.Run("SUP-18 normalize", func(t *testing.T) {
			h := newSupervisorHarness(t)
			h.s.child = func(context.Context, []byte, string) (indexerprotocol.BuildResponse, string, string, error) {
				return indexerprotocol.BuildResponse{Status: indexerprotocol.BuildStatusFailed, Failure: &indexerprotocol.BuildFailure{Code: code, Message: "raw"}}, "", "", nil
			}
			_, _ = h.s.BuildGeneration(context.Background(), h.id)
			if len(h.store.failed) != 1 || h.store.failed[0].FailureCode != "indexer_process_failed" || h.store.failed[0].FailureMessage != "source-index worker reported build failure" {
				t.Fatalf("failed=%#v", h.store.failed)
			}
		})
	}
}
