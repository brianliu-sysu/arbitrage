package balancersync

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"go.uber.org/zap"
)

// Services bundles the Balancer sync application services for wiring.
type Services struct {
	Lifecycle *syncapp.ProtocolLifecycle[marketbalancer.PoolID]
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
	bootstrap := NewBootstrapService(deps.Pools, deps.Registry, deps.Bootstrap, snapshots, deps.Config.BootstrapStaleBlockThreshold)
	lifecycle := NewPoolLifecycleService(readiness, deps.Registry, bootstrap)
	blockApply := NewBlockApplyService(deps.Pools, deps.Checkpoints, snapshots, readiness, deps.Registry, deps.Bootstrap, deps.Listener)
	catchup := NewCatchupService(deps.Config, deps.Pools, deps.Checkpoints, deps.Registry, deps.Fetcher, deps.Parser, blockApply, lifecycle, deps.Blocks)
	reorg := NewReorgRecoveryService(deps.Pools, deps.Registry, deps.Bootstrap, snapshots, blockApply, readiness)
	blockConsumer := NewBlockConsumer(deps.Parser, blockApply, lifecycle, deps.Registry, reorg)
	scheduler := NewSnapshotScheduler(deps.Config, deps.Pools, snapshots, lifecycle)
	orchestrator := syncapp.NewSyncOrchestrator(deps.Blocks, lifecycle, catchup, blockConsumer, blockApply, scheduler, readiness)

	return &Services{
		Lifecycle: syncapp.NewProtocolLifecycle(
			lifecycle,
			blockConsumer,
			readiness,
			orchestrator,
			blockApply,
		),
	}
}

func (s *Services) SetListener(listener ChangedPoolsListener) {
	s.Lifecycle.SetListener(listener)
}

func (s *Services) SetLogger(logger *zap.Logger) {
	s.Lifecycle.SetLogger(logger)
}
