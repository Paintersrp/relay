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
	IsSourceIndexAuthorityActive(context.Context, sourceindex.GenerationIdentity) (bool, error)
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
type localBuild struct {
	cancel context.CancelFunc
	done   chan struct{}
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
	builds            map[string]localBuild
	locks             map[string]*generationLock
	queue             chan string
	wake              chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	done              chan struct{}
	doneOnce          sync.Once
	logger            *slog.Logger
}

func New(store Store, authority SourceAuthority, config Config) (*Manager, error) {
	if store == nil || authority == nil || !config.Enabled || config.BuildParallelism < 1 || config.BuildParallelism > 16 || config.QueryTimeout < time.Millisecond || config.QueryTimeout > time.Minute || config.FileLimitBytes != fixedFileLimit {
		return nil, errors.New("invalid source-index runtime configuration")
	}
	s, err := supervisor.New(store, authority, supervisor.Config{IndexRoot: config.IndexRoot, IndexerPath: config.IndexerPath, ProtectedStorage: config.ProtectedStorage})
	if err != nil {
		return nil, err
	}
	return &Manager{store: store, authority: authority, config: config, build: s, queued: map[string]bool{}, active: map[string]bool{}, builds: map[string]localBuild{}, locks: map[string]*generationLock{}, queue: make(chan string, 65536), wake: make(chan struct{}, 1), done: make(chan struct{}), logger: slog.Default()}, nil
}

// SetLogger supplies Relay's configured logger without changing composition.
func (m *Manager) SetLogger(logger *slog.Logger) {
	if logger != nil {
		m.logger = logger
	}
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
		m.mu.Lock()
		m.stopping = true
		m.mu.Unlock()
		return err
	}
	for range m.config.BuildParallelism {
		m.wg.Add(1)
		go m.worker()
	}
	m.wg.Add(1)
	go m.periodic()
	m.logger.Info("source_index_runtime_started")
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
		for _, build := range m.builds {
			build.cancel()
		}
		m.doneOnce.Do(func() { go func() { m.wg.Wait(); close(m.done) }() })
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		m.logger.Info("source_index_runtime_stopped")
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
		m.logger.Info("source_index_generation_queued", "generation_id", id)
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
	l := m.lock(id)
	l.mu.Lock()
	defer l.mu.Unlock()
	row, err := m.store.GetSourceIndexGeneration(m.ctx, id)
	if err != nil || row.State != workflowstore.SourceIndexGenerationPending {
		return
	}
	active, err := m.store.IsSourceIndexAuthorityActive(m.ctx, row.Identity)
	if err != nil {
		m.wakeReconciliation()
		return
	}
	if !active {
		if err := m.reconcileGeneration(m.ctx, row, false); err != nil {
			m.logger.Error("source_index_reconciliation_failed")
		}
		return
	}
	buildCtx, cancel := context.WithCancel(m.ctx)
	local := localBuild{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		cancel()
		return
	}
	m.builds[id] = local
	m.mu.Unlock()
	defer func() { cancel(); close(local.done); m.mu.Lock(); delete(m.builds, id); m.mu.Unlock() }()
	ready, err := m.build.BuildGeneration(buildCtx, id)
	if err == nil {
		m.logger.Info("source_index_generation_ready", "generation_id", id, "attempt_count", ready.AttemptCount)
		return
	}
	if errors.Is(err, supervisor.ErrPublicationAfterExposure) || errors.Is(err, supervisor.ErrPersistenceAfterPublication) {
		m.wakeReconciliation()
		current, e := m.store.GetSourceIndexGeneration(m.ctx, id)
		if e != nil {
			m.logger.Error("source_index_reconciliation_failed")
			return
		}
		if e = m.recoverBuilding(m.ctx, current); e != nil {
			m.logger.Error("source_index_reconciliation_failed")
			return
		}
	}
	current, e := m.store.GetSourceIndexGeneration(m.ctx, id)
	if e == nil {
		m.logger.Info("source_index_generation_failed", "generation_id", id, "attempt_count", current.AttemptCount, "failure_code", current.FailureCode)
	}
}
func (m *Manager) wakeReconciliation() {
	select {
	case m.wake <- struct{}{}:
	default:
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
		case <-m.wake:
		}
		if err := m.reconcile(m.ctx, false); err != nil {
			m.logger.Error("source_index_reconciliation_failed")
		}
	}
}

var _ Store = (*workflowstore.Store)(nil)
