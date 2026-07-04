package syncv4

import (
	"context"
	"fmt"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// BootstrapService cold-starts a V4 pool from chain state or snapshot.
type BootstrapService struct {
	pools               marketv4.PoolRepository
	registry            marketv4.PoolRegistry
	reader              PoolBootstrapReader
	snapshot            *SnapshotService
	staleBlockThreshold uint64
}

func NewBootstrapService(
	pools marketv4.PoolRepository,
	registry marketv4.PoolRegistry,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	if staleBlockThreshold == 0 {
		staleBlockThreshold = DefaultConfig().BootstrapStaleBlockThreshold
	}
	return &BootstrapService{
		pools:               pools,
		registry:            registry,
		reader:              reader,
		snapshot:            snapshot,
		staleBlockThreshold: staleBlockThreshold,
	}
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*marketv4.Pool, error) {
	key, err := s.registry.GetKey(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("resolve pool key: %w", err)
	}

	pool, err := s.pools.Get(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("load pool: %w", err)
	}

	chainBootstrapped := false

	if pool == nil {
		data, err := s.reader.ReadBootstrapData(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool = marketv4.NewPool(poolID, key)
		applyBootstrapData(pool, data)
		pool.Status = market.PoolStatusBootstrapping
		chainBootstrapped = true
	} else {
		pool.Status = market.PoolStatusBootstrapping
	}

	if s.snapshot != nil {
		if _, err := s.snapshot.RestorePool(ctx, pool); err != nil {
			return nil, fmt.Errorf("restore snapshot: %w", err)
		}
	}

	if !pool.State.IsInitialized() {
		data, err := s.reader.ReadBootstrapData(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool.Key = data.Key
		applyBootstrapData(pool, data)
		chainBootstrapped = true
	} else if needsChainRebootstrap(pool, blockNumber, s.staleBlockThreshold) {
		data, err := s.reader.ReadBootstrapData(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool.Key = data.Key
		applyBootstrapData(pool, data)
		chainBootstrapped = true
	}

	if chainBootstrapped {
		pool.LastBlockNumber = blockNumber
	}
	pool.Status = market.PoolStatusCatchingUp
	if err := s.pools.Save(ctx, pool); err != nil {
		return nil, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}

func applyBootstrapData(pool *marketv4.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}

func needsChainRebootstrap(pool *marketv4.Pool, blockNumber, threshold uint64) bool {
	if pool == nil || threshold == 0 || blockNumber <= pool.LastBlockNumber {
		return false
	}
	return blockNumber-pool.LastBlockNumber > threshold
}
