package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[common.Address, marketclv3.Pool, marketclv3.Snapshot]

var clv3SnapshotOps = syncapp.SnapshotOps[common.Address, marketclv3.Pool, marketclv3.Snapshot]{
	PoolID:      func(pool *marketclv3.Pool) common.Address { return pool.Address },
	NewSnapshot: marketclv3.NewSnapshot,
	RestoreTo:   func(snapshot *marketclv3.Snapshot, pool *marketclv3.Pool) { snapshot.RestoreTo(pool) },
	BlockNumber: func(snapshot *marketclv3.Snapshot) uint64 { return snapshot.BlockNumber },
	LastBlock:   func(pool *marketclv3.Pool) uint64 { return pool.LastBlockNumber },
	IsInitialized: func(pool *marketclv3.Pool) bool {
		return pool.State.IsInitialized()
	},
}

func NewSnapshotService(snapshots SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, clv3SnapshotOps)
}

type SnapshotScheduler = syncapp.SnapshotScheduler[common.Address, marketclv3.Pool]

func NewSnapshotScheduler(
	config Config,
	pools PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(syncapp.SnapshotSchedulerDeps[common.Address, marketclv3.Pool]{
		FallbackInterval: config.SnapshotFallback,
		Lifecycle:        lifecycle,
		LoadPool: func(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
			return pools.Get(ctx, address)
		},
		CreateSnapshot: func(ctx context.Context, pool *marketclv3.Pool, blockNumber uint64) error {
			return snapshots.Create(ctx, pool, blockNumber)
		},
		PoolLastBlock: func(pool *marketclv3.Pool) uint64 { return pool.LastBlockNumber },
		FormatPoolID:  func(address common.Address) string { return address.Hex() },
	})
}
