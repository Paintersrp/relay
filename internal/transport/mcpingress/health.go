package mcpingress

import (
	"sync"
	"time"

	"relay/internal/transport/transporttrace"
)

type HealthState string

const (
	HealthStarting  HealthState = "starting"
	HealthHealthy   HealthState = "healthy"
	HealthUnhealthy HealthState = "unhealthy"
	HealthStopping  HealthState = "stopping"
	HealthStopped   HealthState = "stopped"
)

type HealthSnapshot struct {
	MappingID           MappingID                 `json:"mapping_id"`
	RoutePath           string                    `json:"route_path"`
	State               HealthState               `json:"state"`
	LastTransitionAt    string                    `json:"last_transition_at"`
	ConsecutiveFailures int                       `json:"consecutive_failure_count"`
	ListenerReady       bool                      `json:"listener_ready"`
	UpstreamReady       bool                      `json:"upstream_ready"`
	LastErrorClass      transporttrace.ErrorClass `json:"last_error_class"`
}

type healthTracker struct {
	mu                  sync.RWMutex
	mappingID           MappingID
	routePath           string
	state               HealthState
	lastTransition      time.Time
	consecutiveFailures int
	listenerReady       bool
	upstreamReady       bool
	traceReady          bool
	lastError           transporttrace.ErrorClass
	now                 func() time.Time
}

func newHealthTracker(mappingID MappingID, routePath string, now func() time.Time) *healthTracker {
	if now == nil {
		now = time.Now
	}
	return &healthTracker{
		mappingID:      mappingID,
		routePath:      routePath,
		state:          HealthStarting,
		lastTransition: now().UTC(),
		traceReady:     true,
		now:            now,
	}
}

func (tracker *healthTracker) Snapshot() HealthSnapshot {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return HealthSnapshot{
		MappingID:           tracker.mappingID,
		RoutePath:           tracker.routePath,
		State:               tracker.state,
		LastTransitionAt:    tracker.lastTransition.UTC().Format(time.RFC3339Nano),
		ConsecutiveFailures: tracker.consecutiveFailures,
		ListenerReady:       tracker.listenerReady,
		UpstreamReady:       tracker.upstreamReady,
		LastErrorClass:      tracker.lastError,
	}
}

func (tracker *healthTracker) listener(value bool, failure transporttrace.ErrorClass) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.listenerReady = value
	if !value {
		tracker.lastError = failure
	}
	tracker.recomputeLocked()
}

func (tracker *healthTracker) probeSuccess() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.upstreamReady = true
	tracker.consecutiveFailures = 0
	if tracker.traceReady {
		tracker.lastError = transporttrace.ErrorNone
	}
	tracker.recomputeLocked()
}

func (tracker *healthTracker) probeFailure(class transporttrace.ErrorClass) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.consecutiveFailures++
	if tracker.consecutiveFailures >= 3 {
		tracker.upstreamReady = false
		tracker.lastError = class
	}
	tracker.recomputeLocked()
}

func (tracker *healthTracker) traceFailure() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.traceReady = false
	tracker.lastError = transporttrace.ErrorInternalTransportFailure
	tracker.recomputeLocked()
}

func (tracker *healthTracker) traceSuccess() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.traceReady = true
	if tracker.listenerReady && tracker.upstreamReady {
		tracker.lastError = transporttrace.ErrorNone
	}
	tracker.recomputeLocked()
}

func (tracker *healthTracker) stopping() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.transitionLocked(HealthStopping)
}

func (tracker *healthTracker) stopped() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.listenerReady = false
	tracker.upstreamReady = false
	tracker.transitionLocked(HealthStopped)
}

func (tracker *healthTracker) recomputeLocked() {
	if tracker.state == HealthStopping || tracker.state == HealthStopped {
		return
	}
	next := HealthStarting
	if !tracker.listenerReady || !tracker.traceReady || (tracker.consecutiveFailures >= 3 && !tracker.upstreamReady) {
		next = HealthUnhealthy
	} else if tracker.listenerReady && tracker.upstreamReady {
		next = HealthHealthy
	}
	tracker.transitionLocked(next)
}

func (tracker *healthTracker) transitionLocked(next HealthState) {
	if tracker.state == next {
		return
	}
	tracker.state = next
	tracker.lastTransition = tracker.now().UTC()
}
