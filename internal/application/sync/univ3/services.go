package syncv3

import (
	clv3sync "github.com/brianliu-sysu/uniswapv3/internal/application/sync/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
)

type (
	Config                  = clv3sync.Config
	RawLog                  = clv3sync.RawLog
	BlockReader             = clv3sync.BlockReader
	HeadSubscriber          = clv3sync.HeadSubscriber
	HealthProbe             = clv3sync.HealthProbe
	LogFilter               = clv3sync.LogFilter
	BootstrapData           = clv3sync.BootstrapData
	LogFetcher              = clv3sync.LogFetcher
	EventParser             = clv3sync.EventParser
	PoolBootstrapReader     = clv3sync.PoolBootstrapReader
	ChangedPoolsListener    = clv3sync.ChangedPoolsListener
	NopChangedPoolsListener = clv3sync.NopChangedPoolsListener
	SnapshotPolicy          = clv3sync.SnapshotPolicy
	ReadinessService        = clv3sync.ReadinessService
	Services                = clv3sync.Services
	SyncOrchestrator        = clv3sync.SyncOrchestrator
	BootstrapService        = clv3sync.BootstrapService
	BlockApplyService       = clv3sync.BlockApplyService
	ApplyBlockRequest       = clv3sync.ApplyBlockRequest
	ApplyBlockResult        = clv3sync.ApplyBlockResult
	CatchupService          = clv3sync.CatchupService
	HeadSyncService         = clv3sync.HeadSyncService
	SnapshotService         = clv3sync.SnapshotService
	ReorgRecoveryService    = clv3sync.ReorgRecoveryService
	PoolLifecycleService    = clv3sync.PoolLifecycleService
	SnapshotScheduler       = clv3sync.SnapshotScheduler
)

func DefaultConfig() Config {
	return clv3sync.DefaultConfig()
}

// ServiceDeps contains external dependencies required to construct Uniswap V3 sync services.
type ServiceDeps struct {
	Config      Config
	Pools       marketv3.PoolRepository
	Checkpoints blockchain.CheckpointRepository
	Snapshots   marketv3.SnapshotRepository
	Registry    marketv3.PoolRegistry
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
	return clv3sync.NewServices(adaptUniswapDeps(deps))
}
