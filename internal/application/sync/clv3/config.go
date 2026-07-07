package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type Config = syncapp.Config

func DefaultConfig() Config {
	return syncapp.DefaultConfig()
}

type RawLog = syncapp.RawLog
type BlockReader = syncapp.BlockReader
type HeadSubscriber = syncapp.HeadSubscriber
type HealthProbe = syncapp.HealthProbe

// LogFilter selects logs for tracked CLMM V3 pools within a block range.
type LogFilter struct {
	PoolAddresses []common.Address
	FromBlock     uint64
	ToBlock       uint64
}

// BootstrapData is on-chain V3 pool state read during cold bootstrap.
type BootstrapData struct {
	Token0      common.Address
	Token1      common.Address
	Fee         uint32
	TickSpacing int32
	State       market.PoolState
	Ticks       market.TickTable
	Bitmap      market.TickBitmap
}

// LogFetcher fetches raw logs from the chain.
type LogFetcher interface {
	FetchLogs(ctx context.Context, filter LogFilter) ([]RawLog, error)
}

// EventParser converts raw logs into CLMM V3 domain pool events.
type EventParser interface {
	ParsePoolEvents(logs []RawLog) ([]marketclv3.PoolEvent, error)
}

// PoolBootstrapReader reads live V3 pool state from the chain.
type PoolBootstrapReader interface {
	ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BootstrapData, error)
}

// PoolRegistry defines which V3-style pools the system should track and sync.
type PoolRegistry interface {
	List(ctx context.Context) ([]common.Address, error)
	Add(ctx context.Context, address common.Address) error
	Remove(ctx context.Context, address common.Address) error
}

// PoolRepository persists CLMM V3 pool aggregates keyed by contract address.
type PoolRepository interface {
	Save(ctx context.Context, pool *marketclv3.Pool) error
	Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error)
	Delete(ctx context.Context, address common.Address) error
	AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error
	AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error
}

// SnapshotRepository stores CLMM V3 pool snapshots keyed by contract address.
type SnapshotRepository = syncapp.SnapshotRepository[common.Address, marketclv3.Snapshot]

// PoolFactory constructs a new pool aggregate for cold bootstrap.
type PoolFactory func(address, token0, token1 common.Address, fee uint32, tickSpacing int32) *marketclv3.Pool

// ChangedPoolsListener receives pools updated after a block is applied.
type ChangedPoolsListener interface {
	OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error
}

// NopChangedPoolsListener ignores pool change notifications.
type NopChangedPoolsListener struct{}

func (NopChangedPoolsListener) OnPoolsChanged(context.Context, uint64, []common.Address) error {
	return nil
}

// ServiceDeps contains external dependencies required to construct CLMM V3 sync services.
type ServiceDeps struct {
	Config      Config
	Pools       PoolRepository
	Checkpoints blockchain.CheckpointRepository
	Snapshots   SnapshotRepository
	Registry    PoolRegistry
	NewPool     PoolFactory
	Fetcher     LogFetcher
	Parser      EventParser
	Blocks      BlockReader
	Bootstrap   PoolBootstrapReader
	Subscriber  HeadSubscriber
	Health      []HealthProbe
	Listener    ChangedPoolsListener
}
