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
	inner   *syncapp.BootstrapService[marketv4.PoolID, *marketv4.Pool, *BootstrapData]
	logger  *zap.Logger
}

func NewBootstrapService(
	pools marketv4.PoolRepository,
	registry marketv4.PoolRegistry,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	svc := &BootstrapService{logger: zap.NewNop()}
	svc.inner = syncapp.NewBootstrapService(staleBlockThreshold, syncapp.BootstrapHooks[marketv4.PoolID, *marketv4.Pool, *BootstrapData]{
		IsNilPool: func(pool *marketv4.Pool) bool { return pool == nil },
		IsNilData: func(data *BootstrapData) bool { return data == nil },
		LoadPool:  pools.Get,
		SavePool:  pools.Save,
		RestoreSnapshot: func(ctx context.Context, pool *marketv4.Pool) error {
			if snapshot == nil {
				return nil
			}
			_, err := snapshot.RestorePool(ctx, pool)
			return err
		},
		ReadChainData: func(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*BootstrapData, error) {
			key, err := registry.GetKey(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("resolve pool key: %w", err)
			}
			data, err := reader.ReadBootstrapData(ctx, poolID, key, blockNumber)
			if err != nil {
				return nil, fmt.Errorf("read bootstrap data: %w", err)
			}
			return data, nil
		},
		NewPoolFromChain: func(poolID marketv4.PoolID, data *BootstrapData) (*marketv4.Pool, error) {
			return marketv4.NewPool(poolID, data.Key), nil
		},
		UpdatePoolFromChain: func(pool *marketv4.Pool, data *BootstrapData) {
			pool.Key = data.Key
			applyBootstrapData(pool, data)
		},
		IsInitialized: func(pool *marketv4.Pool) bool { return pool.State.IsInitialized() },
		PoolLastBlock: func(pool *marketv4.Pool) uint64 { return pool.LastBlockNumber },
		SetStatus:     func(pool *marketv4.Pool, status market.PoolStatus) { pool.Status = status },
		SetLastBlockOnChainBootstrap: func(pool *marketv4.Pool, data *BootstrapData, _ uint64) {
			pool.LastBlockNumber = data.BlockNumber
		},
		OnChainBootstrap: func(poolID marketv4.PoolID, data *BootstrapData) {
			svc.logChainBootstrap(poolID, data)
		},
	})
	return svc
}

// SetLogger configures bootstrap logging.
func (s *BootstrapService) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s.logger = logger
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*marketv4.Pool, error) {
	return s.inner.Bootstrap(ctx, poolID, blockNumber)
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
