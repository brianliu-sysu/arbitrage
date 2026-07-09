package syncapp

import (
	"context"
	"fmt"
	"sync"
)

// LifecycleHooks configures protocol-specific pool lifecycle behavior.
type LifecycleHooks[PoolID comparable] struct {
	Bootstrap    func(context.Context, PoolID, uint64) error
	BootstrapAll func(context.Context, []PoolID, uint64) error
	ListTracked  func(context.Context) ([]PoolID, error)
	Register     func(context.Context, PoolID) error
	Unregister   func(context.Context, PoolID) error
}

// PoolLifecycleService manages tracked pools at runtime.
type PoolLifecycleService[PoolID comparable] struct {
	readiness *ReadinessService[PoolID]
	hooks     LifecycleHooks[PoolID]

	mu     sync.RWMutex
	active map[PoolID]struct{}
}

func NewPoolLifecycleService[PoolID comparable](
	readiness *ReadinessService[PoolID],
	hooks LifecycleHooks[PoolID],
) *PoolLifecycleService[PoolID] {
	return &PoolLifecycleService[PoolID]{
		readiness: readiness,
		hooks:     hooks,
		active:    make(map[PoolID]struct{}),
	}
}

func (s *PoolLifecycleService[PoolID]) ListActive() []PoolID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]PoolID, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	return ids
}

func (s *PoolLifecycleService[PoolID]) StartAll(ctx context.Context, blockNumber uint64) error {
	ids, err := s.hooks.ListTracked(ctx)
	if err != nil {
		return fmt.Errorf("list registry pools: %w", err)
	}
	if s.hooks.BootstrapAll != nil {
		if err := s.hooks.BootstrapAll(ctx, ids, blockNumber); err != nil {
			return err
		}
		s.mu.Lock()
		for _, id := range ids {
			s.active[id] = struct{}{}
		}
		s.mu.Unlock()
		for _, id := range ids {
			s.readiness.SetPoolReady(id, false)
		}
		return nil
	}
	for _, id := range ids {
		if err := s.Start(ctx, id, blockNumber); err != nil {
			return err
		}
	}
	return nil
}

func (s *PoolLifecycleService[PoolID]) Start(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.BootstrapInactive(ctx, poolID, blockNumber); err != nil {
		return err
	}
	s.Activate(poolID)
	return nil
}

// BootstrapInactive initializes pool state without adding it to the live head-sync active set.
func (s *PoolLifecycleService[PoolID]) BootstrapInactive(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.hooks.Bootstrap(ctx, poolID, blockNumber); err != nil {
		return fmt.Errorf("bootstrap pool %v: %w", poolID, err)
	}
	s.readiness.SetPoolReady(poolID, false)
	return nil
}

// Activate adds a bootstrapped pool to the live head-sync active set.
func (s *PoolLifecycleService[PoolID]) Activate(poolID PoolID) {
	s.mu.Lock()
	s.active[poolID] = struct{}{}
	s.mu.Unlock()
}

func (s *PoolLifecycleService[PoolID]) Stop(poolID PoolID) {
	s.mu.Lock()
	delete(s.active, poolID)
	s.mu.Unlock()
	s.readiness.SetPoolReady(poolID, false)
}

func (s *PoolLifecycleService[PoolID]) Add(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.hooks.Register(ctx, poolID); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	return s.Start(ctx, poolID, blockNumber)
}

// RegisterAndBootstrapInactive adds a pool to the registry and bootstraps it without activating live head sync.
func (s *PoolLifecycleService[PoolID]) RegisterAndBootstrapInactive(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.hooks.Register(ctx, poolID); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	if err := s.BootstrapInactive(ctx, poolID, blockNumber); err != nil {
		_ = s.hooks.Unregister(ctx, poolID)
		return err
	}
	return nil
}

func (s *PoolLifecycleService[PoolID]) Remove(ctx context.Context, poolID PoolID) error {
	s.Stop(poolID)
	if err := s.hooks.Unregister(ctx, poolID); err != nil {
		return fmt.Errorf("remove pool from registry: %w", err)
	}
	return nil
}
