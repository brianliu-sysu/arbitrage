package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"go.uber.org/zap"
)

// BootstrapService cold-starts a V4 pool from chain state or snapshot.
type BootstrapService struct {
	pools               marketv4.PoolRepository
	registry            marketv4.PoolRegistry
	reader              PoolBootstrapReader
	snapshot            *SnapshotService
	staleBlockThreshold uint64
	logger              *zap.Logger
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
		logger:              zap.NewNop(),
	}
}

// SetLogger configures bootstrap logging.
func (s *BootstrapService) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s.logger = logger
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

	var chainData *BootstrapData

	if pool == nil {
		chainData, err = s.readChainBootstrap(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, err
		}
		pool = marketv4.NewPool(poolID, key)
		applyBootstrapData(pool, chainData)
		pool.Status = market.PoolStatusBootstrapping
	} else {
		pool.Status = market.PoolStatusBootstrapping
		if s.snapshot != nil {
			if _, err := s.snapshot.RestorePool(ctx, pool); err != nil {
				return nil, fmt.Errorf("restore snapshot: %w", err)
			}
		}
	}

	if !pool.State.IsInitialized() {
		chainData, err = s.readChainBootstrap(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, err
		}
		pool.Key = chainData.Key
		applyBootstrapData(pool, chainData)
	} else if pool.LastBlockNumber < blockNumber || syncapp.NeedsChainRebootstrap(pool.LastBlockNumber, blockNumber, s.staleBlockThreshold) {
		chainData, err = s.readChainBootstrap(ctx, poolID, key, blockNumber)
		if err != nil {
			return nil, err
		}
		pool.Key = chainData.Key
		applyBootstrapData(pool, chainData)
	}

	if chainData != nil {
		pool.LastBlockNumber = chainData.BlockNumber
		s.logChainBootstrap(poolID, chainData)
	}
	pool.Status = market.PoolStatusCatchingUp
	if err := s.pools.Save(ctx, pool); err != nil {
		return nil, fmt.Errorf("save pool: %w", err)
	}
	return pool, nil
}

func (s *BootstrapService) readChainBootstrap(
	ctx context.Context,
	poolID marketv4.PoolID,
	key marketv4.PoolKey,
	blockNumber uint64,
) (*BootstrapData, error) {
	data, err := s.reader.ReadBootstrapData(ctx, poolID, key, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap data: %w", err)
	}
	return data, nil
}

func (s *BootstrapService) logChainBootstrap(poolID marketv4.PoolID, data *BootstrapData) {
	if s == nil || s.logger == nil || data == nil {
		return
	}
	fields := []zap.Field{
		zap.String("protocol", "univ4"),
		zap.String("pool", poolID.String()),
		zap.Uint64("block", data.BlockNumber),
		zap.Int32("tick", data.State.Tick),
	}
	if data.State.SqrtPriceX96 != nil {
		fields = append(fields, zap.String("sqrtPriceX96", data.State.SqrtPriceX96.String()))
	}
	if data.State.Liquidity != nil {
		fields = append(fields, zap.String("liquidity", data.State.Liquidity.String()))
	}
	s.logger.Info("chain bootstrap", fields...)
}

func applyBootstrapData(pool *marketv4.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}
