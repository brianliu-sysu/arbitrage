package contract

import (
	"context"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

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

type RawLog = blockchain.RawLog

type HeadSubscriber interface {
	SubscribeNewHead(context.Context) (<-chan blockchain.BlockHeader, error)
}

type BlockReader interface {
	GetBlockHeader(context.Context, uint64) (blockchain.BlockHeader, error)
	GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error)
}

type BlockBatchReader interface {
	GetBlockHeaders(context.Context, []uint64) (map[uint64]blockchain.BlockHeader, error)
}

type CanonicalBlockReader interface {
	BlockBatchReader
	GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error)
}
