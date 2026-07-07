package clv3sync

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/ethereum/go-ethereum/common"
)

type SyncOrchestrator = syncapp.SyncOrchestrator[common.Address]

func NewSyncOrchestrator(
	_ Config,
	blocks BlockReader,
	lifecycle *PoolLifecycleService,
	catchup *CatchupService,
	headSync *HeadSyncService,
	blockApply *BlockApplyService,
	scheduler *SnapshotScheduler,
	readiness *ReadinessService,
) *SyncOrchestrator {
	return syncapp.NewSyncOrchestrator(
		blocks,
		lifecycle,
		catchup,
		headSync,
		blockApply,
		scheduler,
		readiness,
	)
}

// Services bundles CLMM V3 sync application services for wiring.
type Services struct {
	Config     Config
	Readiness  *ReadinessService
	Health     *syncapp.HealthService
	Snapshot   *SnapshotService
	Bootstrap  *BootstrapService
	BlockApply *BlockApplyService
	Lifecycle  *PoolLifecycleService
	Catchup    *CatchupService
	Reorg      *ReorgRecoveryService
	HeadSync   *HeadSyncService
	Scheduler  *SnapshotScheduler
}

func NewServices(deps ServiceDeps) *Services {
	if deps.Config == (Config{}) {
		deps.Config = DefaultConfig()
	}
	if deps.Listener == nil {
		deps.Listener = NopChangedPoolsListener{}
	}

	readiness := NewReadinessService()
	snapshotPolicy := SnapshotPolicy{BlockInterval: deps.Config.SnapshotInterval}
	snapshots := NewSnapshotService(deps.Snapshots, snapshotPolicy)
	bootstrap := NewBootstrapService(deps.Pools, deps.NewPool, deps.Bootstrap, snapshots, deps.Config.BootstrapStaleBlockThreshold)
	lifecycle := NewPoolLifecycleService(deps.Registry, bootstrap, readiness)
	blockApply := NewBlockApplyService(deps.Pools, deps.Checkpoints, snapshots, readiness, deps.Listener)
	catchup := NewCatchupService(deps.Config, deps.Pools, deps.Checkpoints, deps.Fetcher, deps.Parser, blockApply, lifecycle, deps.Blocks)
	reorg := NewReorgRecoveryService(deps.Config, deps.Blocks, deps.Checkpoints, deps.Pools, deps.Bootstrap, snapshots, deps.Fetcher, deps.Parser, blockApply, readiness)
	headSync := NewHeadSyncService(deps.Fetcher, deps.Parser, blockApply, lifecycle, reorg, readiness, catchup, deps.Blocks, deps.Subscriber)
	scheduler := NewSnapshotScheduler(deps.Config, deps.Pools, snapshots, lifecycle)
	health := syncapp.NewHealthService(deps.Health...)

	return &Services{
		Config:     deps.Config,
		Readiness:  readiness,
		Health:     health,
		Snapshot:   snapshots,
		Bootstrap:  bootstrap,
		BlockApply: blockApply,
		Lifecycle:  lifecycle,
		Catchup:    catchup,
		Reorg:      reorg,
		HeadSync:   headSync,
		Scheduler:  scheduler,
	}
}

func (s *Services) NewOrchestrator(blocks BlockReader) *SyncOrchestrator {
	return NewSyncOrchestrator(s.Config, blocks, s.Lifecycle, s.Catchup, s.HeadSync, s.BlockApply, s.Scheduler, s.Readiness)
}
