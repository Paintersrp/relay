package events

import (
	"log/slog"
	"sync"
	"time"
)

const (
	KindRunSummary     = "run.summary"
	KindStepAgent      = "step.agent"
	KindStepValidation = "step.validation"
	KindStepAudit      = "step.audit"
	KindStepCommit     = "step.commit"
	KindStepArtifacts  = "step.artifacts"
	KindToast          = "toast"
)

type RunEvent struct {
	RunID  int64  `json:"run_id"`
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
	Status string `json:"status,omitempty"`
	At     string `json:"at"`
}

type Hub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan RunEvent]struct{}
	log  *slog.Logger
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log}
}

func (h *Hub) Subscribe(runID int64) (<-chan RunEvent, func()) {
	if h == nil {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan RunEvent, 16)

	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[int64]map[chan RunEvent]struct{})
	}
	if h.subs[runID] == nil {
		h.subs[runID] = make(map[chan RunEvent]struct{})
	}
	h.subs[runID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			if subs, ok := h.subs[runID]; ok {
				delete(subs, ch)
				if len(subs) == 0 {
					delete(h.subs, runID)
				}
			}
			close(ch)
			h.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

func (h *Hub) Publish(event RunEvent) {
	if h == nil {
		return
	}
	if event.At == "" {
		event.At = time.Now().UTC().Format(time.RFC3339Nano)
	}

	h.mu.RLock()
	subs := h.subs[event.RunID]
	for ch := range subs {
		select {
		case ch <- event:
		default:
			if h.log != nil {
				h.log.Debug("dropping run event for slow subscriber", "run_id", event.RunID, "kind", event.Kind)
			}
		}
	}
	h.mu.RUnlock()
}
