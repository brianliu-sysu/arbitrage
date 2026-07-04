package syncv3

import (
	"context"
	"fmt"
	"sync"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/common"
)

// PoolLifecycleService manages tracked pools at runtime.
type PoolLifecycleService struct {
	registry  marketv3.PoolRegistry
	bootstrap *BootstrapService
	readiness *ReadinessService

	mu     sync.RWMutex
	active map[common.Address]struct{}
}

func NewPoolLifecycleService(
	registry marketv3.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return &PoolLifecycleService{
		registry:  registry,
		bootstrap: bootstrap,
		readiness: readiness,
		active:    make(map[common.Address]struct{}),
	}
}

func (s *PoolLifecycleService) ListActive() []common.Address {
	s.mu.RLock()
	defer s.mu.RUnlock()

	addresses := make([]common.Address, 0, len(s.active))
	for address := range s.active {
		addresses = append(addresses, address)
	}
	return addresses
}

func (s *PoolLifecycleService) StartAll(ctx context.Context, blockNumber uint64) error {
	addresses, err := s.registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list registry pools: %w", err)
	}
	for _, address := range addresses {
		if err := s.Start(ctx, address, blockNumber); err != nil {
			return err
		}
	}
	return nil
}

func (s *PoolLifecycleService) Start(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	if _, err := s.bootstrap.Bootstrap(ctx, poolAddress, blockNumber); err != nil {
		return fmt.Errorf("bootstrap pool %s: %w", poolAddress.Hex(), err)
	}

	s.mu.Lock()
	s.active[poolAddress] = struct{}{}
	s.mu.Unlock()

	s.readiness.SetPoolReady(poolAddress, false)
	return nil
}

func (s *PoolLifecycleService) Stop(poolAddress common.Address) {
	s.mu.Lock()
	delete(s.active, poolAddress)
	s.mu.Unlock()
	s.readiness.SetPoolReady(poolAddress, false)
}

func (s *PoolLifecycleService) Add(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	if err := s.registry.Add(ctx, poolAddress); err != nil {
		return fmt.Errorf("add pool to registry: %w", err)
	}
	return s.Start(ctx, poolAddress, blockNumber)
}

func (s *PoolLifecycleService) Remove(ctx context.Context, poolAddress common.Address) error {
	s.Stop(poolAddress)
	if err := s.registry.Remove(ctx, poolAddress); err != nil {
		return fmt.Errorf("remove pool from registry: %w", err)
	}
	return nil
}
