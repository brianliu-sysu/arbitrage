package contract

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

type PreparedBlock interface {
	Apply(context.Context) error
	Rollback(context.Context) error
}

// PreparedReorg contains protocol rollback state and prepares canonical replay
// blocks from logs fetched once by the shared runner.
type PreparedReorg interface {
	ReplayFrom() uint64
	PrepareBlock(context.Context, blockchain.BlockHeader, []RawLog) (PreparedBlock, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type BlockPreparer interface {
	PrepareBlock(context.Context, blockchain.BlockHeader, []RawLog) (PreparedBlock, error)
}

type BlockHandler interface {
	BlockPreparer
	HandleBlock(context.Context, blockchain.BlockHeader, []RawLog) error
}

type ReorgPreparer interface {
	PrepareReorg(context.Context, blockchain.Reorg) (PreparedReorg, error)
}

type HeadLogFetcher interface {
	FetchBlockLogs(context.Context, common.Hash) ([]RawLog, error)
}

type NamedHeadHandler struct {
	Name    string
	Handler BlockPreparer
}
