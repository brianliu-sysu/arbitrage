package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// BootstrapHooks configures protocol-specific cold-start behavior.
type BootstrapHooks[PoolID comparable, Pool any, Data any] struct {
	IsNilPool                    func(Pool) bool
	IsNilData                    func(Data) bool
	LoadPool                     func(context.Context, PoolID) (Pool, error)
	SavePool                     func(context.Context, Pool) error
	RestoreSnapshot              func(context.Context, Pool) error
	ReadChainData                func(context.Context, PoolID, uint64) (Data, error)
	ReadChainDataForMany         func(context.Context, []PoolID, uint64) (map[PoolID]Data, error)
	NewPoolFromChain             func(PoolID, Data) (Pool, error)
	UpdatePoolFromChain          func(Pool, Data)
	IsInitialized                func(Pool) bool
	PoolLastBlock                func(Pool) uint64
	SetStatus                    func(Pool, market.PoolStatus)
	SetLastBlockOnChainBootstrap func(Pool, Data, uint64)
	OnChainBootstrap             func(PoolID, Data)
}

// BootstrapService cold-starts a pool from chain state or snapshot.
type BootstrapService[PoolID comparable, Pool any, Data any] struct {
	staleBlockThreshold uint64
	hooks               BootstrapHooks[PoolID, Pool, Data]
}

// NewBootstrapService builds a bootstrap service with protocol hooks.
func NewBootstrapService[PoolID comparable, Pool any, Data any](
	staleBlockThreshold uint64,
	hooks BootstrapHooks[PoolID, Pool, Data],
) *BootstrapService[PoolID, Pool, Data] {
	if staleBlockThreshold == 0 {
		staleBlockThreshold = DefaultConfig().BootstrapStaleBlockThreshold
	}
	return &BootstrapService[PoolID, Pool, Data]{
		staleBlockThreshold: staleBlockThreshold,
		hooks:               hooks,
	}
}

// Bootstrap loads or creates a pool and brings it to catching-up state.
func (s *BootstrapService[PoolID, Pool, Data]) Bootstrap(ctx context.Context, poolID PoolID, blockNumber uint64) (Pool, error) {
	return s.bootstrap(ctx, poolID, blockNumber, nil, false)
}

// BootstrapAll cold-starts many pools, batching chain reads when ReadChainDataForMany is configured.
func (s *BootstrapService[PoolID, Pool, Data]) BootstrapAll(ctx context.Context, poolIDs []PoolID, blockNumber uint64) error {
	var batchData map[PoolID]Data
	if s.hooks.ReadChainDataForMany != nil && len(poolIDs) > 0 {
		var err error
		batchData, err = s.hooks.ReadChainDataForMany(ctx, poolIDs, blockNumber)
		if err != nil {
			return fmt.Errorf("read bootstrap data: %w", err)
		}
	}
	for _, poolID := range poolIDs {
		var preloaded *Data
		hasPreloaded := false
		if batchData != nil {
			if data, ok := batchData[poolID]; ok {
				preloaded = &data
				hasPreloaded = true
			}
		}
		if _, err := s.bootstrap(ctx, poolID, blockNumber, preloaded, hasPreloaded); err != nil {
			return err
		}
	}
	return nil
}

func (s *BootstrapService[PoolID, Pool, Data]) bootstrap(
	ctx context.Context,
	poolID PoolID,
	blockNumber uint64,
	preloaded *Data,
	hasPreloaded bool,
) (Pool, error) {
	var zero Pool

	pool, err := s.hooks.LoadPool(ctx, poolID)
	if err != nil {
		return zero, fmt.Errorf("load pool: %w", err)
	}

	var chainData Data
	chainBootstrapped := false

	if s.hooks.IsNilPool(pool) {
		chainData, err = s.readChainData(ctx, poolID, blockNumber, preloaded, hasPreloaded)
		if err != nil {
			return zero, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool, err = s.hooks.NewPoolFromChain(poolID, chainData)
		if err != nil {
			return zero, err
		}
		s.hooks.UpdatePoolFromChain(pool, chainData)
		s.hooks.SetStatus(pool, market.PoolStatusBootstrapping)
		chainBootstrapped = true
	} else {
		s.hooks.SetStatus(pool, market.PoolStatusBootstrapping)
		if s.hooks.RestoreSnapshot != nil {
			if err := s.hooks.RestoreSnapshot(ctx, pool); err != nil {
				return zero, fmt.Errorf("restore snapshot: %w", err)
			}
		}
	}

	if !s.hooks.IsInitialized(pool) {
		chainData, err = s.readChainData(ctx, poolID, blockNumber, preloaded, hasPreloaded)
		if err != nil {
			return zero, fmt.Errorf("read bootstrap data: %w", err)
		}
		s.hooks.UpdatePoolFromChain(pool, chainData)
		chainBootstrapped = true
	} else if NeedsChainRebootstrap(s.hooks.PoolLastBlock(pool), blockNumber, s.staleBlockThreshold) {
		chainData, err = s.readChainData(ctx, poolID, blockNumber, preloaded, hasPreloaded)
		if err != nil {
			return zero, fmt.Errorf("read bootstrap data: %w", err)
		}
		s.hooks.UpdatePoolFromChain(pool, chainData)
		chainBootstrapped = true
	}

	if chainBootstrapped {
		s.hooks.SetLastBlockOnChainBootstrap(pool, chainData, blockNumber)
		if s.hooks.OnChainBootstrap != nil && (s.hooks.IsNilData == nil || !s.hooks.IsNilData(chainData)) {
			s.hooks.OnChainBootstrap(poolID, chainData)
		}
	}
	s.hooks.SetStatus(pool, market.PoolStatusCatchingUp)
	if err := s.hooks.SavePool(ctx, pool); err != nil {
		return zero, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}

func (s *BootstrapService[PoolID, Pool, Data]) readChainData(
	ctx context.Context,
	poolID PoolID,
	blockNumber uint64,
	preloaded *Data,
	hasPreloaded bool,
) (Data, error) {
	var zero Data
	if hasPreloaded && preloaded != nil && (s.hooks.IsNilData == nil || !s.hooks.IsNilData(*preloaded)) {
		return *preloaded, nil
	}
	data, err := s.hooks.ReadChainData(ctx, poolID, blockNumber)
	if err != nil {
		return zero, err
	}
	return data, nil
}
