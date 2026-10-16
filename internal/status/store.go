// Package status keeps the latest check results and exposes local status and
// Prometheus-compatible HTTP endpoints.
package status

import (
	"sync"
	"time"

	"github.com/leedcee/uplinkwatch/internal/agent"
	"github.com/leedcee/uplinkwatch/internal/checker"
)

// FailureSeries identifies one runtime failure counter.
type FailureSeries struct {
	Probe  string
	Target string
	Stage  checker.Stage
	Reason checker.FailureReason
}

// Snapshot is an immutable view of the in-memory state.
type Snapshot struct {
	ProbeID      string                   `json:"probe_id"`
	UpdatedAt    time.Time                `json:"updated_at"`
	Results      []checker.Result         `json:"results"`
	Statistics   map[string]TargetStats   `json:"statistics"`
	FailureCount map[FailureSeries]uint64 `json:"-"`
}

// TargetStats contains runtime-only availability statistics. Counters reset
// whenever the agent process restarts.
type TargetStats struct {
	Checks          uint64    `json:"checks"`
	Successful      uint64    `json:"successful"`
	LastStateChange time.Time `json:"last_state_change"`
	lastReachable   bool
}

// Store keeps the latest result per target and runtime failure counters.
type Store struct {
	mu            sync.RWMutex
	probeID       string
	updatedAt     time.Time
	order         []string
	results       map[string]checker.Result
	statistics    map[string]TargetStats
	failureCounts map[FailureSeries]uint64
}

// NewStore creates an empty status store.
func NewStore(probeID string) *Store {
	return &Store{
		probeID:       probeID,
		results:       make(map[string]checker.Result),
		statistics:    make(map[string]TargetStats),
		failureCounts: make(map[FailureSeries]uint64),
	}
}

// Record stores an event and increments the appropriate failure counter.
func (s *Store) Record(event agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.results[event.Result.TargetName]; !exists {
		s.order = append(s.order, event.Result.TargetName)
	}
	s.results[event.Result.TargetName] = event.Result
	s.updatedAt = event.EmittedAt
	stats := s.statistics[event.Result.TargetName]
	if stats.Checks == 0 || stats.lastReachable != event.Result.Reachable {
		stats.LastStateChange = event.EmittedAt
	}
	stats.Checks++
	if event.Result.Reachable {
		stats.Successful++
	}
	stats.lastReachable = event.Result.Reachable
	s.statistics[event.Result.TargetName] = stats
	if event.Result.Failure != nil {
		series := FailureSeries{
			Probe:  event.ProbeID,
			Target: event.Result.TargetName,
			Stage:  event.Result.Failure.Stage,
			Reason: event.Result.Failure.Reason,
		}
		s.failureCounts[series]++
	}
}

// Snapshot returns the current state without sharing mutable maps.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := Snapshot{
		ProbeID:      s.probeID,
		UpdatedAt:    s.updatedAt,
		Results:      make([]checker.Result, 0, len(s.order)),
		Statistics:   make(map[string]TargetStats, len(s.statistics)),
		FailureCount: make(map[FailureSeries]uint64, len(s.failureCounts)),
	}
	for _, target := range s.order {
		snapshot.Results = append(snapshot.Results, s.results[target])
	}
	for series, count := range s.failureCounts {
		snapshot.FailureCount[series] = count
	}
	for target, stats := range s.statistics {
		snapshot.Statistics[target] = stats
	}
	return snapshot
}
