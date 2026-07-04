package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// BootstrapService cold-starts a pool from chain state or snapshot.
type BootstrapService struct {
	pools                 market.PoolRepository
	reader                PoolBootstrapReader
	snapshot              *SnapshotService
	staleBlockThreshold   uint64
}

func NewBootstrapService(
	pools market.PoolRepository,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	if staleBlockThreshold == 0 {
		staleBlockThreshold = DefaultConfig().BootstrapStaleBlockThreshold
	}
	return &BootstrapService{
		pools:               pools,
		reader:              reader,
		snapshot:            snapshot,
		staleBlockThreshold: staleBlockThreshold,
	}
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*market.Pool, error) {
	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load pool: %w", err)
	}

	chainBootstrapped := false

	if pool == nil {
		data, err := s.reader.ReadBootstrapData(ctx, poolAddress, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool = market.NewPool(poolAddress, data.Token0, data.Token1, data.Fee, data.TickSpacing)
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
		data, err := s.reader.ReadBootstrapData(ctx, poolAddress, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool.Token0 = data.Token0
		pool.Token1 = data.Token1
		pool.Fee = data.Fee
		pool.TickSpacing = data.TickSpacing
		applyBootstrapData(pool, data)
		chainBootstrapped = true
	} else if needsChainRebootstrap(pool, blockNumber, s.staleBlockThreshold) {
		data, err := s.reader.ReadBootstrapData(ctx, poolAddress, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool.Token0 = data.Token0
		pool.Token1 = data.Token1
		pool.Fee = data.Fee
		pool.TickSpacing = data.TickSpacing
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

func applyBootstrapData(pool *market.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}

func needsChainRebootstrap(pool *market.Pool, blockNumber, threshold uint64) bool {
	if pool == nil || threshold == 0 || blockNumber <= pool.LastBlockNumber {
		return false
	}
	return blockNumber-pool.LastBlockNumber > threshold
}
