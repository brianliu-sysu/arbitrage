package syncv4

import (
	"sync"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

// ReadinessService tracks V4 pool-level and system-level quote readiness.
type ReadinessService struct {
	mu          sync.RWMutex
	poolReady   map[marketv4.PoolID]bool
	systemReady bool
}

func NewReadinessService() *ReadinessService {
	return &ReadinessService{
		poolReady: make(map[marketv4.PoolID]bool),
	}
}

func (s *ReadinessService) SetPoolReady(poolID marketv4.PoolID, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poolReady[poolID] = ready
}

func (s *ReadinessService) IsPoolReady(poolID marketv4.PoolID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.poolReady[poolID]
}

func (s *ReadinessService) SetSystemReady(ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemReady = ready
}

func (s *ReadinessService) IsSystemReady() bool {
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

func (s *ReadinessService) MarkPoolsReady(ids []marketv4.PoolID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		s.poolReady[id] = true
	}
}

func (s *ReadinessService) PoolStatuses() map[marketv4.PoolID]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make(map[marketv4.PoolID]bool, len(s.poolReady))
	for id, ready := range s.poolReady {
		statuses[id] = ready
	}
	return statuses
}
