package protocol

import (
	"context"
	"fmt"
	"sync"
)

// PoolRegistry owns tracked pool membership.
type PoolRegistry[PoolID comparable] interface {
	ListTracked(context.Context) ([]PoolID, error)
	Register(context.Context, PoolID) error
	Unregister(context.Context, PoolID) error
}

// PoolBootstrapper initializes one pool at a target block.
type PoolBootstrapper[PoolID comparable] interface {
	Bootstrap(context.Context, PoolID, uint64) error
}

// BatchPoolBootstrapper initializes multiple pools in one operation.
type BatchPoolBootstrapper[PoolID comparable] interface {
	BootstrapAll(context.Context, []PoolID, uint64) error
}

// PoolAdmissionService registers, bootstraps, and activates pools for live sync.
type PoolAdmissionService[PoolID comparable] struct {
	readiness *ReadinessService[PoolID]
	registry  PoolRegistry[PoolID]
	bootstrap PoolBootstrapper[PoolID]

	mu     sync.RWMutex
	active map[PoolID]struct{}
}

func NewPoolAdmissionService[PoolID comparable](
	readiness *ReadinessService[PoolID],
	registry PoolRegistry[PoolID],
	bootstrap PoolBootstrapper[PoolID],
) *PoolAdmissionService[PoolID] {
	return &PoolAdmissionService[PoolID]{
		readiness: readiness,
		registry:  registry,
		bootstrap: bootstrap,
		active:    make(map[PoolID]struct{}),
	}
}

func (s *PoolAdmissionService[PoolID]) ListActive() []PoolID {
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

func (s *PoolAdmissionService[PoolID]) AdmitTrackedPools(ctx context.Context, blockNumber uint64) error {
	ids, err := s.registerTrackedPools(ctx)
	if err != nil {
		return err
	}
	if bootstrapper, ok := s.bootstrap.(BatchPoolBootstrapper[PoolID]); ok {
		return s.bootstrapBatch(ctx, bootstrapper, ids, blockNumber)
	}
	return s.bootstrapIndividually(ctx, ids, blockNumber)
}

func (s *PoolAdmissionService[PoolID]) registerTrackedPools(ctx context.Context) ([]PoolID, error) {
	ids, err := s.registry.ListTracked(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registry pools: %w", err)
	}
	// Pin listed pools into the mutable registry so later subgraph cache refreshes
	// cannot drop specs for pools that are already admitted to live sync.
	for _, id := range ids {
		if err := s.registry.Register(ctx, id); err != nil {
			return nil, fmt.Errorf("register pool %v: %w", id, err)
		}
	}
	return ids, nil
}

func (s *PoolAdmissionService[PoolID]) bootstrapBatch(
	ctx context.Context,
	bootstrapper BatchPoolBootstrapper[PoolID],
	ids []PoolID,
	blockNumber uint64,
) error {
	if err := bootstrapper.BootstrapAll(ctx, ids, blockNumber); err != nil {
		return err
	}
	s.activateAll(ids)
	return nil
}

func (s *PoolAdmissionService[PoolID]) bootstrapIndividually(
	ctx context.Context,
	ids []PoolID,
	blockNumber uint64,
) error {
	for _, id := range ids {
		if err := s.bootstrapAndActivate(ctx, id, blockNumber); err != nil {
			return err
		}
	}
	return nil
}

func (s *PoolAdmissionService[PoolID]) activateAll(ids []PoolID) {
	s.mu.Lock()
	for _, id := range ids {
		s.active[id] = struct{}{}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.readiness.SetPoolReady(id, false)
	}
}

func (s *PoolAdmissionService[PoolID]) bootstrapAndActivate(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.bootstrapInactive(ctx, poolID, blockNumber); err != nil {
		return err
	}
	s.activate(poolID)
	return nil
}

func (s *PoolAdmissionService[PoolID]) bootstrapInactive(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.bootstrap.Bootstrap(ctx, poolID, blockNumber); err != nil {
		return fmt.Errorf("bootstrap pool %v: %w", poolID, err)
	}
	s.readiness.SetPoolReady(poolID, false)
	return nil
}

func (s *PoolAdmissionService[PoolID]) activate(poolID PoolID) {
	s.mu.Lock()
	s.active[poolID] = struct{}{}
	s.mu.Unlock()
}

func (s *PoolAdmissionService[PoolID]) deactivate(poolID PoolID) {
	s.mu.Lock()
	delete(s.active, poolID)
	s.mu.Unlock()
	s.readiness.SetPoolReady(poolID, false)
}

func (s *PoolAdmissionService[PoolID]) registerAndBootstrapInactive(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	if err := s.registry.Register(ctx, poolID); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	if err := s.bootstrapInactive(ctx, poolID, blockNumber); err != nil {
		_ = s.registry.Unregister(ctx, poolID)
		return err
	}
	return nil
}

func (s *PoolAdmissionService[PoolID]) remove(ctx context.Context, poolID PoolID) error {
	s.deactivate(poolID)
	if err := s.registry.Unregister(ctx, poolID); err != nil {
		return fmt.Errorf("remove pool from registry: %w", err)
	}
	return nil
}
