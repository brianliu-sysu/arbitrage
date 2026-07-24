package protocol

import (
	"context"
	"fmt"
	"sync"
)

// PoolLifecycleProtocol defines protocol-specific pool admission behavior.
type PoolLifecycleProtocol[PoolID comparable] interface {
	Bootstrap(context.Context, PoolID, uint64) error
	ListTracked(context.Context) ([]PoolID, error)
	Register(context.Context, PoolID) error
	Unregister(context.Context, PoolID) error
}

type batchPoolBootstrapper[PoolID comparable] interface {
	BootstrapAll(context.Context, []PoolID, uint64) error
}

// PoolLifecycleService manages tracked pools at runtime.
type PoolLifecycleService[PoolID comparable] struct {
	readiness *ReadinessService[PoolID]
	protocol  PoolLifecycleProtocol[PoolID]

	mu     sync.RWMutex
	active map[PoolID]struct{}
}

func NewPoolLifecycleService[PoolID comparable](
	readiness *ReadinessService[PoolID],
	protocol PoolLifecycleProtocol[PoolID],
) *PoolLifecycleService[PoolID] {
	return &PoolLifecycleService[PoolID]{
		readiness: readiness,
		protocol:  protocol,
		active:    make(map[PoolID]struct{}),
	}
}

func (s *PoolLifecycleService[PoolID]) ListActive() []PoolID {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]PoolID, 0, len(s.active))
	for id := range s.active {
		ids = append(ids, id)
	}
	return ids
}

// List returns only pools admitted to live block consumption.
func (s *PoolLifecycleService[PoolID]) List(context.Context) ([]PoolID, error) {
	if s == nil {
		return nil, nil
	}
	return s.ListActive(), nil
}

func (s *PoolLifecycleService[PoolID]) StartAll(ctx context.Context, blockNumber uint64) error {
	ids, err := s.protocol.ListTracked(ctx)
	if err != nil {
		return fmt.Errorf("list registry pools: %w", err)
	}
	// Pin listed pools into the mutable registry so later subgraph cache refreshes
	// cannot drop specs for pools that are already admitted to live sync.
	for _, id := range ids {
		if err := s.protocol.Register(ctx, id); err != nil {
			return fmt.Errorf("register pool %v: %w", id, err)
		}
	}
	if bootstrapper, ok := s.protocol.(batchPoolBootstrapper[PoolID]); ok {
		if err := bootstrapper.BootstrapAll(ctx, ids, blockNumber); err != nil {
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
	if err := s.protocol.Bootstrap(ctx, poolID, blockNumber); err != nil {
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
	if err := s.protocol.Register(ctx, poolID); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	return s.Start(ctx, poolID, blockNumber)
}

// RegisterAndBootstrapInactive adds a pool to the registry and bootstraps it without activating live block consumption.
func (s *PoolLifecycleService[PoolID]) RegisterAndBootstrapInactive(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.protocol.Register(ctx, poolID); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	if err := s.BootstrapInactive(ctx, poolID, blockNumber); err != nil {
		_ = s.protocol.Unregister(ctx, poolID)
		return err
	}
	return nil
}

func (s *PoolLifecycleService[PoolID]) Remove(ctx context.Context, poolID PoolID) error {
	s.Stop(poolID)
	if err := s.protocol.Unregister(ctx, poolID); err != nil {
		return fmt.Errorf("remove pool from registry: %w", err)
	}
	return nil
}
