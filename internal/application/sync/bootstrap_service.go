package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// BootstrapService cold-starts a pool from chain state or snapshot.
type BootstrapService struct {
	pools    market.PoolRepository
	reader   PoolBootstrapReader
	snapshot *SnapshotService
}

func NewBootstrapService(
	pools market.PoolRepository,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
) *BootstrapService {
	return &BootstrapService{
		pools:    pools,
		reader:   reader,
		snapshot: snapshot,
	}
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*market.Pool, error) {
	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load pool: %w", err)
	}

	if pool == nil {
		data, err := s.reader.ReadBootstrapData(ctx, poolAddress, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap data: %w", err)
		}
		pool = market.NewPool(poolAddress, data.Token0, data.Token1, data.Fee, data.TickSpacing)
		pool.State = data.State.Clone()
		pool.Ticks = data.Ticks.Clone()
		pool.Bitmap = data.Bitmap.Clone()
		pool.Status = market.PoolStatusBootstrapping
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
		pool.State = data.State.Clone()
		pool.Ticks = data.Ticks.Clone()
		pool.Bitmap = data.Bitmap.Clone()
	}

	pool.LastBlockNumber = blockNumber
	pool.Status = market.PoolStatusCatchingUp
	if err := s.pools.Save(ctx, pool); err != nil {
		return nil, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}
