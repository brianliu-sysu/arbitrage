package syncv3

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// ReadinessService tracks pool-level and system-level quote readiness.
type ReadinessService struct {
	mu          sync.RWMutex
	poolReady   map[common.Address]bool
	systemReady bool
}

func NewReadinessService() *ReadinessService {
	return &ReadinessService{
		poolReady: make(map[common.Address]bool),
	}
}

func (s *ReadinessService) SetPoolReady(poolAddress common.Address, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poolReady[poolAddress] = ready
}

func (s *ReadinessService) IsPoolReady(poolAddress common.Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.poolReady[poolAddress]
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

func (s *ReadinessService) MarkPoolsReady(addresses []common.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, address := range addresses {
		s.poolReady[address] = true
	}
}

func (s *ReadinessService) PoolStatuses() map[common.Address]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make(map[common.Address]bool, len(s.poolReady))
	for address, ready := range s.poolReady {
		statuses[address] = ready
	}
	return statuses
}
