package syncv4

import (
	"context"
	"fmt"
	"sync"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// PoolLifecycleService manages tracked V4 pools at runtime.
type PoolLifecycleService struct {
	registry  marketv4.PoolRegistry
	bootstrap *BootstrapService
	readiness *ReadinessService

	mu     sync.RWMutex
	active map[marketv4.PoolID]struct{}
}

func NewPoolLifecycleService(
	registry marketv4.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return &PoolLifecycleService{
		registry:  registry,
		bootstrap: bootstrap,
		readiness: readiness,
		active:    make(map[marketv4.PoolID]struct{}),
	}
}

func (s *PoolLifecycleService) ListActive() []marketv4.PoolID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]marketv4.PoolID, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	return ids
}

func (s *PoolLifecycleService) StartAll(ctx context.Context, blockNumber uint64) error {
	ids, err := s.registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list registry pools: %w", err)
	}
	for _, id := range ids {
		if err := s.Start(ctx, id, blockNumber); err != nil {
			return err
		}
	}
	return nil
}

func (s *PoolLifecycleService) Start(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	if _, err := s.bootstrap.Bootstrap(ctx, poolID, blockNumber); err != nil {
		return fmt.Errorf("bootstrap pool %s: %w", poolID, err)
	}

	s.mu.Lock()
	s.active[poolID] = struct{}{}
	s.mu.Unlock()

	s.readiness.SetPoolReady(poolID, false)
	return nil
}

func (s *PoolLifecycleService) Stop(poolID marketv4.PoolID) {
	s.mu.Lock()
	delete(s.active, poolID)
	s.mu.Unlock()
	s.readiness.SetPoolReady(poolID, false)
}

func (s *PoolLifecycleService) Add(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	key, err := s.registry.GetKey(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool key: %w", err)
	}
	if err := s.registry.Add(ctx, poolID, key); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	return s.Start(ctx, poolID, blockNumber)
}

func (s *PoolLifecycleService) Remove(ctx context.Context, poolID marketv4.PoolID) error {
	s.Stop(poolID)
	if err := s.registry.Remove(ctx, poolID); err != nil {
		return fmt.Errorf("remove pool from registry: %w", err)
	}
	return nil
}
