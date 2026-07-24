package balancersync

import (
	"context"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[marketbalancer.PoolID, marketbalancer.Pool, marketbalancer.Snapshot]

type snapshotProtocol struct{}

func (p *snapshotProtocol) PoolID(pool *marketbalancer.Pool) marketbalancer.PoolID { return pool.ID }
func (p *snapshotProtocol) NewSnapshot(pool *marketbalancer.Pool, block uint64, createdAt time.Time) *marketbalancer.Snapshot {
	return marketbalancer.NewSnapshot(pool, block, createdAt)
}
func (p *snapshotProtocol) RestoreTo(snapshot *marketbalancer.Snapshot, pool *marketbalancer.Pool) {
	snapshot.RestoreTo(pool)
}
func (p *snapshotProtocol) BlockNumber(snapshot *marketbalancer.Snapshot) uint64 {
	return snapshot.BlockNumber
}
func (p *snapshotProtocol) LastBlock(pool *marketbalancer.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotProtocol) IsInitialized(pool *marketbalancer.Pool) bool {
	return pool.IsInitialized()
}

func NewSnapshotService(snapshots marketbalancer.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, &snapshotProtocol{})
}

type SnapshotScheduler = syncapp.SnapshotScheduler[marketbalancer.PoolID, marketbalancer.Pool]

type snapshotSchedulerProtocol struct {
	pools     marketbalancer.PoolRepository
	snapshots *SnapshotService
}

func (p *snapshotSchedulerProtocol) LoadPool(ctx context.Context, poolID marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	return p.pools.Get(ctx, poolID)
}
func (p *snapshotSchedulerProtocol) CreateSnapshot(ctx context.Context, pool *marketbalancer.Pool, block uint64) error {
	return p.snapshots.Create(ctx, pool, block)
}
func (p *snapshotSchedulerProtocol) PoolLastBlock(pool *marketbalancer.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotSchedulerProtocol) FormatPoolID(poolID marketbalancer.PoolID) string {
	return poolID.String()
}

func NewSnapshotScheduler(
	config Config,
	pools marketbalancer.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(
		config.SnapshotFallback,
		lifecycle,
		&snapshotSchedulerProtocol{pools: pools, snapshots: snapshots},
	)
}
