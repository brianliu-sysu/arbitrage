package syncv4

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[marketv4.PoolID, marketv4.Pool, marketv4.Snapshot]

var v4SnapshotOps = syncapp.SnapshotOps[marketv4.PoolID, marketv4.Pool, marketv4.Snapshot]{
	PoolID:      func(pool *marketv4.Pool) marketv4.PoolID { return pool.ID },
	NewSnapshot: marketv4.NewSnapshot,
	RestoreTo:   func(snapshot *marketv4.Snapshot, pool *marketv4.Pool) { snapshot.RestoreTo(pool) },
	BlockNumber: func(snapshot *marketv4.Snapshot) uint64 { return snapshot.BlockNumber },
	LastBlock:   func(pool *marketv4.Pool) uint64 { return pool.LastBlockNumber },
	IsInitialized: func(pool *marketv4.Pool) bool {
		return pool.State.IsInitialized()
	},
}

func NewSnapshotService(snapshots marketv4.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, v4SnapshotOps)
}

type SnapshotScheduler = syncapp.SnapshotScheduler[marketv4.PoolID, marketv4.Pool]

func NewSnapshotScheduler(
	config Config,
	pools marketv4.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(syncapp.SnapshotSchedulerDeps[marketv4.PoolID, marketv4.Pool]{
		FallbackInterval: config.SnapshotFallback,
		Lifecycle:        lifecycle,
		LoadPool: func(ctx context.Context, poolID marketv4.PoolID) (*marketv4.Pool, error) {
			return pools.Get(ctx, poolID)
		},
		CreateSnapshot: func(ctx context.Context, pool *marketv4.Pool, blockNumber uint64) error {
			return snapshots.Create(ctx, pool, blockNumber)
		},
		PoolLastBlock: func(pool *marketv4.Pool) uint64 { return pool.LastBlockNumber },
		FormatPoolID:  func(poolID marketv4.PoolID) string { return poolID.String() },
	})
}
