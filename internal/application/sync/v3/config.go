package syncv3

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
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

// LogFilter selects logs for tracked V3 pools within a block range.
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

// EventParser converts raw logs into V3 domain pool events.
type EventParser interface {
	ParsePoolEvents(logs []RawLog) ([]marketv3.PoolEvent, error)
}

// PoolBootstrapReader reads live V3 pool state from the chain.
type PoolBootstrapReader interface {
	ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BootstrapData, error)
}

// ChangedPoolsListener receives pools updated after a block is applied.
type ChangedPoolsListener interface {
	OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error
}

// NopChangedPoolsListener ignores pool change notifications.
type NopChangedPoolsListener struct{}

func (NopChangedPoolsListener) OnPoolsChanged(context.Context, uint64, []common.Address) error {
	return nil
}

// ServiceDeps contains external dependencies required to construct V3 sync services.
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
