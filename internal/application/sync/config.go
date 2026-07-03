package syncapp

import (
	"context"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// Config holds sync-related runtime settings.
type Config struct {
	CatchupBatchSize          uint64
	CatchupPoolGroupSize      uint64
	CatchupBlockSpan          uint64
	CatchupHeaderConcurrency  int
	SnapshotInterval          uint64
	SnapshotFallback     time.Duration
	ReorgMaxDepth        uint64
}

func DefaultConfig() Config {
	return Config{
		CatchupBatchSize:         2000,
		CatchupPoolGroupSize:     100,
		CatchupBlockSpan:         100,
		CatchupHeaderConcurrency: 16,
		SnapshotInterval:         5000,
		SnapshotFallback:     10 * time.Minute,
		ReorgMaxDepth:        128,
	}
}

// RawLog is a decoded-free log entry fetched from the chain.
type RawLog struct {
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	BlockNumber uint64
	BlockHash   common.Hash
	TxIndex     uint
	LogIndex    uint
}

// LogFilter selects logs for tracked pools within a block range.
type LogFilter struct {
	PoolAddresses []common.Address
	FromBlock     uint64
	ToBlock       uint64
}

// BootstrapData is on-chain pool state read during cold bootstrap.
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

// EventParser converts raw logs into domain pool events.
type EventParser interface {
	ParsePoolEvents(logs []RawLog) ([]market.PoolEvent, error)
}

// HeadSubscriber delivers new canonical block headers.
type HeadSubscriber interface {
	SubscribeNewHead(ctx context.Context) (<-chan blockchain.BlockHeader, error)
}

// BlockReader reads block headers for catchup and reorg recovery.
type BlockReader interface {
	GetBlockHeader(ctx context.Context, blockNumber uint64) (blockchain.BlockHeader, error)
	GetLatestBlockHeader(ctx context.Context) (blockchain.BlockHeader, error)
}

// PoolBootstrapReader reads live pool state from the chain.
type PoolBootstrapReader interface {
	ReadBootstrapData(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BootstrapData, error)
}

// HealthProbe checks a single external dependency.
type HealthProbe interface {
	Name() string
	Ping(ctx context.Context) error
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
