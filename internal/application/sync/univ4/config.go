package syncv4

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type Config = syncapp.Config

func DefaultConfig() Config {
	return syncapp.DefaultConfig()
}

type RawLog = syncapp.RawLog
type BlockReader = syncapp.BlockReader
type HeadSubscriber = syncapp.HeadSubscriber
type HealthProbe = syncapp.HealthProbe

// LogFilter selects PoolManager logs for tracked V4 pools within a block range.
type LogFilter struct {
	PoolIDs   []marketv4.PoolID
	FromBlock uint64
	ToBlock   uint64
}

// BootstrapData is on-chain V4 pool state read during cold bootstrap.
type BootstrapData struct {
	Key    marketv4.PoolKey
	State  market.PoolState
	Ticks  market.TickTable
	Bitmap market.TickBitmap
}

// LogFetcher fetches raw PoolManager logs from the chain.
type LogFetcher interface {
	FetchLogs(ctx context.Context, filter LogFilter) ([]RawLog, error)
}

// EventParser converts raw logs into V4 domain pool events.
type EventParser interface {
	ParsePoolEvents(logs []RawLog) ([]marketv4.PoolEvent, error)
}

// PoolBootstrapReader reads live V4 pool state from the chain.
type PoolBootstrapReader interface {
	ReadBootstrapData(ctx context.Context, poolID marketv4.PoolID, key marketv4.PoolKey, blockNumber uint64) (*BootstrapData, error)
}

// ChangedPoolsListener receives pools updated after a block is applied.
type ChangedPoolsListener interface {
	OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketv4.PoolID) error
}

// NopChangedPoolsListener ignores pool change notifications.
type NopChangedPoolsListener struct{}

func (NopChangedPoolsListener) OnPoolsChanged(context.Context, uint64, []marketv4.PoolID) error {
	return nil
}

// ServiceDeps contains external dependencies required to construct V4 sync services.
type ServiceDeps struct {
	Config      Config
	Pools       marketv4.PoolRepository
	Checkpoints blockchain.V4CheckpointRepository
	Snapshots   marketv4.SnapshotRepository
	Registry    marketv4.PoolRegistry
	Fetcher     LogFetcher
	Parser      EventParser
	Blocks      BlockReader
	Bootstrap   PoolBootstrapReader
	Subscriber  HeadSubscriber
	Health      []HealthProbe
	Listener    ChangedPoolsListener
}
