package clv3sync

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// Services bundles CLMM V3 sync application services for wiring.
type Services struct {
	Lifecycle *syncapp.ProtocolLifecycle[common.Address]
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
	reorg := NewReorgRecoveryService(deps.Pools, deps.Bootstrap, snapshots, blockApply, readiness)
	blockConsumer := NewBlockConsumer(deps.Parser, blockApply, lifecycle, reorg)
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
