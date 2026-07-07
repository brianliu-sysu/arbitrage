package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type BootstrapService = syncapp.BootstrapService[common.Address, *marketclv3.Pool, *BootstrapData]

func NewBootstrapService(
	pools PoolRepository,
	newPool PoolFactory,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	return syncapp.NewBootstrapService(staleBlockThreshold, syncapp.BootstrapHooks[common.Address, *marketclv3.Pool, *BootstrapData]{
		IsNilPool: func(pool *marketclv3.Pool) bool { return pool == nil },
		IsNilData: func(data *BootstrapData) bool { return data == nil },
		LoadPool:  pools.Get,
		SavePool:  pools.Save,
		RestoreSnapshot: func(ctx context.Context, pool *marketclv3.Pool) error {
			if snapshot == nil {
				return nil
			}
			_, err := snapshot.RestorePool(ctx, pool)
			return err
		},
		ReadChainData: reader.ReadBootstrapData,
		NewPoolFromChain: func(poolAddress common.Address, data *BootstrapData) (*marketclv3.Pool, error) {
			return newPool(poolAddress, data.Token0, data.Token1, data.Fee, data.TickSpacing), nil
		},
		UpdatePoolFromChain: func(pool *marketclv3.Pool, data *BootstrapData) {
			pool.Token0 = data.Token0
			pool.Token1 = data.Token1
			pool.Fee = data.Fee
			pool.TickSpacing = data.TickSpacing
			applyBootstrapData(pool, data)
		},
		IsInitialized: func(pool *marketclv3.Pool) bool { return pool.State.IsInitialized() },
		PoolLastBlock: func(pool *marketclv3.Pool) uint64 { return pool.LastBlockNumber },
		SetStatus:     func(pool *marketclv3.Pool, status market.PoolStatus) { pool.Status = status },
		SetLastBlockOnChainBootstrap: func(pool *marketclv3.Pool, _ *BootstrapData, blockNumber uint64) {
			pool.LastBlockNumber = blockNumber
		},
	})
}

func applyBootstrapData(pool *marketclv3.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}
