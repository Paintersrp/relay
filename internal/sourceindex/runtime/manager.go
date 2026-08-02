package sourceindexruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/supervisor"
	workflowstore "relay/internal/store/workflow"
)

type Store interface {
	CreateOrResolveSourceIndexGeneration(context.Context, workflowstore.CreateOrResolveSourceIndexGenerationParams) (workflowstore.SourceIndexGeneration, bool, error)
	GetSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error)
	GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflowstore.SourceIndexGeneration, error)
	BeginSourceIndexGenerationBuild(context.Context, string) (workflowstore.SourceIndexGeneration, error)
	ListSourceIndexGenerations(context.Context) ([]workflowstore.SourceIndexGeneration, error)
	ListActiveSourceIndexAuthorities(context.Context) ([]workflowstore.ActiveSourceIndexAuthority, error)
	MarkSourceIndexGenerationReady(context.Context, workflowstore.MarkSourceIndexGenerationReadyParams) (workflowstore.SourceIndexGeneration, error)
	MarkSourceIndexGenerationFailed(context.Context, workflowstore.MarkSourceIndexGenerationFailedParams) (workflowstore.SourceIndexGeneration, error)
	RetrySourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error)
	RetireSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error)
	ReactivateSourceIndexGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error)
}

type SourceAuthority interface{ supervisor.SourceAuthority }
type builder interface {
	BuildGeneration(context.Context, string) (workflowstore.SourceIndexGeneration, error)
}

type generationLock struct {
	mu       sync.RWMutex
	retiring bool
}
type Manager struct {
	store             Store
	authority         SourceAuthority
	config            Config
	build             builder
	mu                sync.Mutex
	started, stopping bool
	queued            map[string]bool
	active            map[string]bool
	repair            map[string]bool
	locks             map[string]*generationLock
	queue             chan string
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

func New(store Store, authority SourceAuthority, config Config) (*Manager, error) {
	if store == nil || authority == nil || !config.Enabled || config.BuildParallelism < 1 || config.BuildParallelism > 16 || config.QueryTimeout < time.Millisecond || config.QueryTimeout > time.Minute || config.FileLimitBytes != fixedFileLimit {
		return nil, errors.New("invalid source-index runtime configuration")
	}
	s, err := supervisor.New(store, authority, supervisor.Config{IndexRoot: config.IndexRoot, IndexerPath: config.IndexerPath, ProtectedStorage: config.ProtectedStorage})
	if err != nil {
		return nil, err
	}
	return &Manager{store: store, authority: authority, config: config, build: s, queued: map[string]bool{}, active: map[string]bool{}, repair: map[string]bool{}, locks: map[string]*generationLock{}, queue: make(chan string, 65536)}, nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("source-index runtime already started")
	}
	m.started = true
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	if err := m.reconcile(m.ctx, true); err != nil {
		m.cancel()
		return err
	}
	for range m.config.BuildParallelism {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.periodic()
	slog.Info("source_index_runtime_started")
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	if !m.stopping {
		m.stopping = true
		m.cancel()
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("source_index_runtime_stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) lock(id string) *generationLock {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.locks[id]
	if l == nil {
		l = &generationLock{}
		m.locks[id] = l
	}
	return l
}
func (m *Manager) enqueue(id string) {
	m.mu.Lock()
	if m.stopping || m.queued[id] {
		m.mu.Unlock()
		return
	}
	m.queued[id] = true
	q := m.queue
	ctx := m.ctx
	m.mu.Unlock()
	select {
	case q <- id:
		slog.Info("source_index_generation_queued", "generation_id", id)
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.queued, id)
		m.mu.Unlock()
	}
}
func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case id := <-m.queue:
			m.runBuild(id)
		}
	}
}
func (m *Manager) runBuild(id string) {
	defer func() { m.mu.Lock(); delete(m.queued, id); m.mu.Unlock() }()
	m.mu.Lock()
	active := m.active[id]
	m.mu.Unlock()
	if !active {
		return
	}
	l := m.lock(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	row, err := m.store.GetSourceIndexGeneration(m.ctx, id)
	if err != nil || row.State != workflowstore.SourceIndexGenerationPending {
		return
	}
	ready, err := m.build.BuildGeneration(m.ctx, id)
	if err == nil {
		slog.Info("source_index_generation_ready", "generation_id", id, "attempt_count", ready.AttemptCount)
		return
	}
	if errors.Is(err, supervisor.ErrPublicationAfterExposure) || errors.Is(err, supervisor.ErrPersistenceAfterPublication) {
		_ = m.reconcileGeneration(m.ctx, row, true)
	}
	current, e := m.store.GetSourceIndexGeneration(context.Background(), id)
	if e == nil {
		slog.Info("source_index_generation_failed", "generation_id", id, "attempt_count", current.AttemptCount, "failure_code", current.FailureCode)
	}
}
func (m *Manager) periodic() {
	defer m.wg.Done()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			if err := m.reconcile(m.ctx, false); err != nil {
				slog.Error("source_index_reconciliation_failed")
			}
		}
	}
}

var _ Store = (*workflowstore.Store)(nil)
