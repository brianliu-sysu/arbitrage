package balancersync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[marketbalancer.PoolID, marketbalancer.Pool, marketbalancer.Snapshot]

var balancerSnapshotOps = syncapp.SnapshotOps[marketbalancer.PoolID, marketbalancer.Pool, marketbalancer.Snapshot]{
	PoolID:        func(pool *marketbalancer.Pool) marketbalancer.PoolID { return pool.ID },
	NewSnapshot:   marketbalancer.NewSnapshot,
	RestoreTo:     func(snapshot *marketbalancer.Snapshot, pool *marketbalancer.Pool) { snapshot.RestoreTo(pool) },
	BlockNumber:   func(snapshot *marketbalancer.Snapshot) uint64 { return snapshot.BlockNumber },
	LastBlock:     func(pool *marketbalancer.Pool) uint64 { return pool.LastBlockNumber },
	IsInitialized: func(pool *marketbalancer.Pool) bool { return pool.IsInitialized() },
}

func NewSnapshotService(snapshots marketbalancer.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, balancerSnapshotOps)
}

type SnapshotScheduler = syncapp.SnapshotScheduler[marketbalancer.PoolID, marketbalancer.Pool]

func NewSnapshotScheduler(
	config Config,
	pools marketbalancer.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(syncapp.SnapshotSchedulerDeps[marketbalancer.PoolID, marketbalancer.Pool]{
		FallbackInterval: config.SnapshotFallback,
		Lifecycle:        lifecycle,
		LoadPool: func(ctx context.Context, poolID marketbalancer.PoolID) (*marketbalancer.Pool, error) {
			return pools.Get(ctx, poolID)
		},
		CreateSnapshot: func(ctx context.Context, pool *marketbalancer.Pool, blockNumber uint64) error {
			return snapshots.Create(ctx, pool, blockNumber)
		},
		PoolLastBlock: func(pool *marketbalancer.Pool) uint64 { return pool.LastBlockNumber },
		FormatPoolID:  func(poolID marketbalancer.PoolID) string { return poolID.String() },
	})
}
