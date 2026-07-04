package syncapp

import (
	"context"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

// Config holds sync-related runtime settings shared by V3 and V4.
type Config struct {
	CatchupBatchSize             uint64
	CatchupPoolGroupSize         uint64
	CatchupBlockSpan             uint64
	CatchupHeaderConcurrency     int
	BootstrapStaleBlockThreshold uint64
	SnapshotInterval             uint64
	SnapshotFallback             time.Duration
	ReorgMaxDepth                uint64
}

func DefaultConfig() Config {
	return Config{
		CatchupBatchSize:             2000,
		CatchupPoolGroupSize:         100,
		CatchupBlockSpan:             100,
		CatchupHeaderConcurrency:     16,
		BootstrapStaleBlockThreshold: 1000,
		SnapshotInterval:             5000,
		SnapshotFallback:             10 * time.Minute,
		ReorgMaxDepth:                128,
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

// HeadSubscriber delivers new canonical block headers.
type HeadSubscriber interface {
	SubscribeNewHead(ctx context.Context) (<-chan blockchain.BlockHeader, error)
}

// BlockReader reads block headers for catchup and reorg recovery.
type BlockReader interface {
	GetBlockHeader(ctx context.Context, blockNumber uint64) (blockchain.BlockHeader, error)
	GetLatestBlockHeader(ctx context.Context) (blockchain.BlockHeader, error)
}

// HealthProbe checks a single external dependency.
type HealthProbe interface {
	Name() string
	Ping(ctx context.Context) error
}
