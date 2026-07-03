package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// SyncOrchestrator coordinates bootstrap, catchup, and live head sync startup.
type SyncOrchestrator struct {
	config     Config
	blocks     BlockReader
	lifecycle  *PoolLifecycleService
	catchup    *CatchupService
	headSync   *HeadSyncService
	scheduler  *SnapshotScheduler
	readiness  *ReadinessService
}

func NewSyncOrchestrator(
	config Config,
	blocks BlockReader,
	lifecycle *PoolLifecycleService,
	catchup *CatchupService,
	headSync *HeadSyncService,
	scheduler *SnapshotScheduler,
	readiness *ReadinessService,
) *SyncOrchestrator {
	return &SyncOrchestrator{
		config:    config,
		blocks:    blocks,
		lifecycle: lifecycle,
		catchup:   catchup,
		headSync:  headSync,
		scheduler: scheduler,
		readiness: readiness,
	}
}

// Start bootstraps pools, catches up to the current head, then runs live sync.
func (o *SyncOrchestrator) Start(ctx context.Context) error {
	latest, err := o.blocks.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("load latest block: %w", err)
	}

	if err := o.lifecycle.StartAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("start pools: %w", err)
	}

	if err := o.catchup.CatchUpAll(ctx, latest.Number); err != nil {
		return fmt.Errorf("catch up pools: %w", err)
	}

	o.markPoolsReady(ctx)
	o.headSync.SetLocalHead(latest)
	o.readiness.SetSystemReady(true)

	if o.scheduler != nil {
		go func() {
			_ = o.scheduler.Run(ctx)
		}()
	}

	return o.headSync.Run(ctx)
}

func (o *SyncOrchestrator) markPoolsReady(ctx context.Context) {
	for _, poolAddress := range o.lifecycle.ListActive() {
		o.readiness.SetPoolReady(poolAddress, true)
	}
	_ = ctx
}

// Services bundles the sync application services for wiring.
type Services struct {
	Config     Config
	Readiness  *ReadinessService
	Health     *HealthService
	Snapshot   *SnapshotService
	Bootstrap  *BootstrapService
	BlockApply *BlockApplyService
	Lifecycle  *PoolLifecycleService
	Catchup    *CatchupService
	Reorg      *ReorgRecoveryService
	HeadSync   *HeadSyncService
	Scheduler  *SnapshotScheduler
}

// ServiceDeps contains external dependencies required to construct sync services.
type ServiceDeps struct {
	Config      Config
	Pools       market.PoolRepository
	Checkpoints blockchain.CheckpointRepository
	Snapshots   market.SnapshotRepository
	Registry    market.PoolRegistry
	Fetcher     LogFetcher
	Parser      EventParser
	Blocks      BlockReader
	Bootstrap   PoolBootstrapReader
	Subscriber  HeadSubscriber
	Health      []HealthProbe
	Listener    ChangedPoolsListener
}

func NewServices(deps ServiceDeps) *Services {
	if deps.Config == (Config{}) {
		deps.Config = DefaultConfig()
	}

	readiness := NewReadinessService()
	snapshotPolicy := SnapshotPolicy{BlockInterval: deps.Config.SnapshotInterval}
	snapshots := NewSnapshotService(deps.Snapshots, snapshotPolicy)
	bootstrap := NewBootstrapService(deps.Pools, deps.Bootstrap, snapshots)
	lifecycle := NewPoolLifecycleService(deps.Registry, bootstrap, readiness)
	blockApply := NewBlockApplyService(deps.Pools, deps.Checkpoints, snapshots, readiness, deps.Listener)
	catchup := NewCatchupService(deps.Config, deps.Pools, deps.Checkpoints, deps.Fetcher, deps.Parser, blockApply, lifecycle, deps.Blocks)
	reorg := NewReorgRecoveryService(deps.Config, deps.Blocks, deps.Checkpoints, deps.Pools, snapshots, deps.Fetcher, deps.Parser, blockApply, readiness)
	headSync := NewHeadSyncService(deps.Fetcher, deps.Parser, blockApply, lifecycle, reorg, readiness, deps.Subscriber)
	scheduler := NewSnapshotScheduler(deps.Config, deps.Pools, snapshots, lifecycle)
	health := NewHealthService(deps.Health...)

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
	return NewSyncOrchestrator(s.Config, blocks, s.Lifecycle, s.Catchup, s.HeadSync, s.Scheduler, s.Readiness)
}
