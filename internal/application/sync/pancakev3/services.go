package syncpancakev3

import (
	clv3sync "github.com/brianliu-sysu/uniswapv3/internal/application/sync/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
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

func NewReadinessService() *ReadinessService {
	return clv3sync.NewReadinessService()
}

func NewSnapshotService(snapshots marketpancake.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return clv3sync.NewSnapshotService(&snapshotRepositoryAdapter{inner: snapshots}, policy)
}

func NewBootstrapService(
	pools marketpancake.PoolRepository,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	return clv3sync.NewBootstrapService(
		&poolRepositoryAdapter{inner: pools},
		newPancakePool,
		reader,
		snapshot,
		staleBlockThreshold,
	)
}

func NewBlockApplyService(
	pools marketpancake.PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	return clv3sync.NewBlockApplyService(
		&poolRepositoryAdapter{inner: pools},
		checkpoints,
		snapshots,
		readiness,
		listener,
	)
}

func NewCatchupService(
	config Config,
	pools marketpancake.PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	blocks BlockReader,
) *CatchupService {
	return clv3sync.NewCatchupService(
		config,
		&poolRepositoryAdapter{inner: pools},
		checkpoints,
		fetcher,
		parser,
		blockApply,
		lifecycle,
		blocks,
	)
}

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	checkpoints blockchain.CheckpointRepository,
	pools marketpancake.PoolRepository,
	bootstrap PoolBootstrapReader,
	snapshots *SnapshotService,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return clv3sync.NewReorgRecoveryService(
		config,
		blocks,
		checkpoints,
		&poolRepositoryAdapter{inner: pools},
		bootstrap,
		snapshots,
		fetcher,
		parser,
		blockApply,
		readiness,
	)
}

func NewHeadSyncService(
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	reorg *ReorgRecoveryService,
	readiness *ReadinessService,
	subscriber HeadSubscriber,
) *HeadSyncService {
	return clv3sync.NewHeadSyncService(fetcher, parser, blockApply, lifecycle, reorg, readiness, subscriber)
}

func NewPoolLifecycleService(
	registry marketpancake.PoolRegistry,
	bootstrap *BootstrapService,
	readiness *ReadinessService,
) *PoolLifecycleService {
	return clv3sync.NewPoolLifecycleService(registry, bootstrap, readiness)
}

func NewSnapshotScheduler(
	config Config,
	pools marketpancake.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return clv3sync.NewSnapshotScheduler(config, &poolRepositoryAdapter{inner: pools}, snapshots, lifecycle)
}

func NewSyncOrchestrator(
	config Config,
	blocks BlockReader,
	lifecycle *PoolLifecycleService,
	catchup *CatchupService,
	headSync *HeadSyncService,
	blockApply *BlockApplyService,
	scheduler *SnapshotScheduler,
	readiness *ReadinessService,
) *SyncOrchestrator {
	return clv3sync.NewSyncOrchestrator(config, blocks, lifecycle, catchup, headSync, blockApply, scheduler, readiness)
}

// ServiceDeps contains external dependencies required to construct PancakeSwap V3 sync services.
type ServiceDeps struct {
	Config      Config
	Pools       marketpancake.PoolRepository
	Checkpoints blockchain.CheckpointRepository
	Snapshots   marketpancake.SnapshotRepository
	Registry    marketpancake.PoolRegistry
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
	return clv3sync.NewServices(adaptPancakeDeps(deps))
}
