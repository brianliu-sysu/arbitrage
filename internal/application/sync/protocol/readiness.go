package protocol

import "sync"

// ReadinessService tracks pool-level and system-level quote readiness.
type ReadinessService[PoolID comparable] struct {
	mu          sync.RWMutex
	poolReady   map[PoolID]bool
	systemReady bool
}

func NewReadinessService[PoolID comparable]() *ReadinessService[PoolID] {
	return &ReadinessService[PoolID]{
		poolReady: make(map[PoolID]bool),
	}
}

func (s *ReadinessService[PoolID]) SetPoolReady(poolID PoolID, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poolReady[poolID] = ready
}

func (s *ReadinessService[PoolID]) IsPoolReady(poolID PoolID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.poolReady[poolID]
}

func (s *ReadinessService[PoolID]) SetSystemReady(ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemReady = ready
}

func (s *ReadinessService[PoolID]) IsSystemReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.systemReady {
		return false
	}
	for _, ready := range s.poolReady {
		if !ready {
			return false
		}
	}
	return len(s.poolReady) > 0
}

func (s *ReadinessService[PoolID]) MarkPoolsReady(ids []PoolID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		s.poolReady[id] = true
	}
}

func (s *ReadinessService[PoolID]) PoolStatuses() map[PoolID]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make(map[PoolID]bool, len(s.poolReady))
	for id, ready := range s.poolReady {
		statuses[id] = ready
	}
	return statuses
}

// ReadinessSnapshot is a point-in-time view of sync readiness.
type ReadinessSnapshot struct {
	SystemReady bool
	ReadyPools  int
	TotalPools  int
}

// Snapshot returns the current readiness counters.
func (s *ReadinessService[PoolID]) Snapshot() ReadinessSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	readyPools := 0
	for _, ready := range s.poolReady {
		if ready {
			readyPools++
		}
	}
	return ReadinessSnapshot{
		SystemReady: s.systemReady,
		ReadyPools:  readyPools,
		TotalPools:  len(s.poolReady),
	}
}
