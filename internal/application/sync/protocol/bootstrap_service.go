package protocol

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// BootstrapProtocol contains the required cold-start behavior for one protocol.
type BootstrapProtocol[PoolID comparable, Pool any, Data any] interface {
	IsNilPool(Pool) bool
	LoadPool(context.Context, PoolID) (Pool, error)
	SavePool(context.Context, Pool) error
	ReadChainData(context.Context, PoolID, uint64) (Data, error)
	NewPoolFromChain(PoolID, Data) (Pool, error)
	ApplyChainData(Pool, Data, uint64)
	IsInitialized(Pool) bool
	PoolLastBlock(Pool) uint64
	SetStatus(Pool, market.PoolStatus)
}

// SnapshotRestorer optionally restores a pool before falling back to chain state.
type SnapshotRestorer[Pool any] interface {
	RestoreSnapshot(context.Context, Pool) error
}

// BatchChainDataReader optionally batches cold-start chain reads.
type BatchChainDataReader[PoolID comparable, Data any] interface {
	ReadChainDataForMany(context.Context, []PoolID, uint64) (map[PoolID]Data, error)
}

// BootstrapService cold-starts a pool from chain state or snapshot.
type BootstrapService[PoolID comparable, Pool any, Data any] struct {
	staleBlockThreshold uint64
	protocol            BootstrapProtocol[PoolID, Pool, Data]
}

// NewBootstrapService builds a bootstrap service with protocol-specific behavior.
func NewBootstrapService[PoolID comparable, Pool any, Data any](
	staleBlockThreshold uint64,
	protocol BootstrapProtocol[PoolID, Pool, Data],
) *BootstrapService[PoolID, Pool, Data] {
	if staleBlockThreshold == 0 {
		staleBlockThreshold = DefaultConfig().BootstrapStaleBlockThreshold
	}
	return &BootstrapService[PoolID, Pool, Data]{
		staleBlockThreshold: staleBlockThreshold,
		protocol:            protocol,
	}
}

// Bootstrap loads or creates a pool and brings it to catching-up state.
func (s *BootstrapService[PoolID, Pool, Data]) Bootstrap(ctx context.Context, poolID PoolID, blockNumber uint64) (Pool, error) {
	return s.bootstrap(ctx, poolID, blockNumber, nil, false)
}

// BootstrapAll cold-starts many pools, batching chain reads when ReadChainDataForMany is configured.
func (s *BootstrapService[PoolID, Pool, Data]) BootstrapAll(ctx context.Context, poolIDs []PoolID, blockNumber uint64) error {
	var batchData map[PoolID]Data
	batchReader, supportsBatch := s.protocol.(BatchChainDataReader[PoolID, Data])
	if supportsBatch && len(poolIDs) > 0 {
		var err error
		batchData, err = batchReader.ReadChainDataForMany(ctx, poolIDs, blockNumber)
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

	pool, err := s.protocol.LoadPool(ctx, poolID)
	if err != nil {
		return zero, fmt.Errorf("load pool: %w", err)
	}

	var chainData Data
	chainApplied := false

	if s.protocol.IsNilPool(pool) {
		chainData, err = s.readChainData(ctx, poolID, blockNumber, preloaded, hasPreloaded)
		if err != nil {
			return zero, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool, err = s.protocol.NewPoolFromChain(poolID, chainData)
		if err != nil {
			return zero, err
		}
		s.protocol.ApplyChainData(pool, chainData, blockNumber)
		chainApplied = true
		s.protocol.SetStatus(pool, market.PoolStatusBootstrapping)
	} else {
		s.protocol.SetStatus(pool, market.PoolStatusBootstrapping)
		if restorer, ok := s.protocol.(SnapshotRestorer[Pool]); ok {
			if err := restorer.RestoreSnapshot(ctx, pool); err != nil {
				return zero, fmt.Errorf("restore snapshot: %w", err)
			}
		}
	}

	if !chainApplied && s.needsChainBootstrap(pool, blockNumber) {
		chainData, err = s.readChainData(ctx, poolID, blockNumber, preloaded, hasPreloaded)
		if err != nil {
			return zero, fmt.Errorf("read bootstrap data: %w", err)
		}
		s.protocol.ApplyChainData(pool, chainData, blockNumber)
	}

	s.protocol.SetStatus(pool, market.PoolStatusCatchingUp)
	if err := s.protocol.SavePool(ctx, pool); err != nil {
		return zero, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}

func (s *BootstrapService[PoolID, Pool, Data]) needsChainBootstrap(pool Pool, blockNumber uint64) bool {
	return !s.protocol.IsInitialized(pool) ||
		NeedsChainRebootstrap(
			s.protocol.PoolLastBlock(pool),
			blockNumber,
			s.staleBlockThreshold,
		)
}

func (s *BootstrapService[PoolID, Pool, Data]) readChainData(
	ctx context.Context,
	poolID PoolID,
	blockNumber uint64,
	preloaded *Data,
	hasPreloaded bool,
) (Data, error) {
	var zero Data
	if hasPreloaded && preloaded != nil {
		return *preloaded, nil
	}
	data, err := s.protocol.ReadChainData(ctx, poolID, blockNumber)
	if err != nil {
		return zero, err
	}
	return data, nil
}
