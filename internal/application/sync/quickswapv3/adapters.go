package syncquickswapv3

import (
	"context"

	clv3sync "github.com/brianliu-sysu/uniswapv3/internal/application/sync/clv3"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

type poolRepositoryAdapter struct {
	inner marketquick.PoolRepository
}

func (a *poolRepositoryAdapter) Save(ctx context.Context, pool *marketclv3.Pool) error {
	return a.inner.Save(ctx, &marketquick.Pool{Pool: *pool})
}

func (a *poolRepositoryAdapter) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	pool, err := a.inner.Get(ctx, address)
	if err != nil || pool == nil {
		return nil, err
	}
	return &pool.Pool, nil
}

func (a *poolRepositoryAdapter) Delete(ctx context.Context, address common.Address) error {
	return a.inner.Delete(ctx, address)
}

func (a *poolRepositoryAdapter) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return a.inner.AdvanceSyncProgress(ctx, address, blockNumber)
}

func (a *poolRepositoryAdapter) AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
	return a.inner.AdvanceSyncProgressMany(ctx, addresses, blockNumber)
}

type snapshotRepositoryAdapter struct {
	inner marketquick.SnapshotRepository
}

func (a *snapshotRepositoryAdapter) Save(ctx context.Context, snapshot *marketclv3.Snapshot) error {
	return a.inner.Save(ctx, snapshot)
}

func (a *snapshotRepositoryAdapter) GetLatest(ctx context.Context, poolAddress common.Address) (*marketclv3.Snapshot, error) {
	return a.inner.GetLatest(ctx, poolAddress)
}

func (a *snapshotRepositoryAdapter) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketclv3.Snapshot, error) {
	return a.inner.GetAtBlock(ctx, poolAddress, blockNumber)
}

func (a *snapshotRepositoryAdapter) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return a.inner.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}

func newQuickSwapPool(address, token0, token1 common.Address, fee uint32, tickSpacing int32) *marketclv3.Pool {
	return &marketquick.NewPool(address, token0, token1, fee, tickSpacing).Pool
}

func adaptQuickSwapDeps(deps ServiceDeps) clv3sync.ServiceDeps {
	return clv3sync.ServiceDeps{
		Config:      deps.Config,
		Pools:       &poolRepositoryAdapter{inner: deps.Pools},
		Checkpoints: deps.Checkpoints,
		Snapshots:   &snapshotRepositoryAdapter{inner: deps.Snapshots},
		Registry:    deps.Registry,
		NewPool:     newQuickSwapPool,
		Fetcher:     deps.Fetcher,
		Parser:      deps.Parser,
		Blocks:      deps.Blocks,
		Bootstrap:   deps.Bootstrap,
		Listener:    deps.Listener,
	}
}
