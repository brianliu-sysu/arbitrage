package clv3sync

import (
	"context"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

type SnapshotService = syncapp.SnapshotService[common.Address, marketclv3.Pool, marketclv3.Snapshot]

type snapshotProtocol struct{}

func (p *snapshotProtocol) PoolID(pool *marketclv3.Pool) common.Address { return pool.Address }
func (p *snapshotProtocol) NewSnapshot(pool *marketclv3.Pool, block uint64, createdAt time.Time) *marketclv3.Snapshot {
	return marketclv3.NewSnapshot(pool, block, createdAt)
}
func (p *snapshotProtocol) RestoreTo(snapshot *marketclv3.Snapshot, pool *marketclv3.Pool) {
	snapshot.RestoreTo(pool)
}
func (p *snapshotProtocol) BlockNumber(snapshot *marketclv3.Snapshot) uint64 {
	return snapshot.BlockNumber
}
func (p *snapshotProtocol) LastBlock(pool *marketclv3.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotProtocol) IsInitialized(pool *marketclv3.Pool) bool {
	return pool.State.IsInitialized()
}

func NewSnapshotService(snapshots SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return syncapp.NewSnapshotService(snapshots, policy, &snapshotProtocol{})
}

type SnapshotScheduler = syncapp.SnapshotScheduler[common.Address, marketclv3.Pool]

type snapshotSchedulerProtocol struct {
	pools     PoolRepository
	snapshots *SnapshotService
}

func (p *snapshotSchedulerProtocol) LoadPool(ctx context.Context, poolID common.Address) (*marketclv3.Pool, error) {
	return p.pools.Get(ctx, poolID)
}
func (p *snapshotSchedulerProtocol) CreateSnapshot(ctx context.Context, pool *marketclv3.Pool, block uint64) error {
	return p.snapshots.Create(ctx, pool, block)
}
func (p *snapshotSchedulerProtocol) PoolLastBlock(pool *marketclv3.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *snapshotSchedulerProtocol) FormatPoolID(poolID common.Address) string {
	return poolID.Hex()
}

func NewSnapshotScheduler(
	config Config,
	pools PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return syncapp.NewSnapshotScheduler(
		config.SnapshotFallback,
		lifecycle,
		&snapshotSchedulerProtocol{pools: pools, snapshots: snapshots},
	)
}
