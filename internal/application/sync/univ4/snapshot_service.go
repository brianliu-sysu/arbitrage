package syncv4

import (
	"context"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[marketv4.PoolID, marketv4.Pool, marketv4.Snapshot]

type snapshotProtocol struct{}

func (p *snapshotProtocol) PoolID(pool *marketv4.Pool) marketv4.PoolID { return pool.ID }
func (p *snapshotProtocol) NewSnapshot(pool *marketv4.Pool, block uint64, createdAt time.Time) *marketv4.Snapshot {
	return marketv4.NewSnapshot(pool, block, createdAt)
}
func (p *snapshotProtocol) RestoreTo(snapshot *marketv4.Snapshot, pool *marketv4.Pool) {
	snapshot.RestoreTo(pool)
}
func (p *snapshotProtocol) BlockNumber(snapshot *marketv4.Snapshot) uint64 {
	return snapshot.BlockNumber
}
func (p *snapshotProtocol) LastBlock(pool *marketv4.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotProtocol) IsInitialized(pool *marketv4.Pool) bool {
	return pool.State.IsInitialized()
}

func NewSnapshotService(snapshots marketv4.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, &snapshotProtocol{})
}

type SnapshotScheduler = syncapp.SnapshotScheduler[marketv4.PoolID, marketv4.Pool]

type snapshotSchedulerProtocol struct {
	pools     marketv4.PoolRepository
	snapshots *SnapshotService
}

func (p *snapshotSchedulerProtocol) LoadPool(ctx context.Context, poolID marketv4.PoolID) (*marketv4.Pool, error) {
	return p.pools.Get(ctx, poolID)
}
func (p *snapshotSchedulerProtocol) CreateSnapshot(ctx context.Context, pool *marketv4.Pool, block uint64) error {
	return p.snapshots.Create(ctx, pool, block)
}
func (p *snapshotSchedulerProtocol) PoolLastBlock(pool *marketv4.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotSchedulerProtocol) FormatPoolID(poolID marketv4.PoolID) string {
	return poolID.String()
}

func NewSnapshotScheduler(
	config Config,
	pools marketv4.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(
		config.SnapshotFallback,
		lifecycle,
		&snapshotSchedulerProtocol{pools: pools, snapshots: snapshots},
	)
}
