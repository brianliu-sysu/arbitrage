package poolsapp

import (
	"context"
	"math/big"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// BaseState is on-chain slot0 and liquidity without tick data.
type BaseState struct {
	SqrtPriceX96 *big.Int
	Tick         int32
	Liquidity    *big.Int
}

// HeadBlockReader returns the latest chain head block number.
type HeadBlockReader interface {
	LatestBlockNumber(ctx context.Context) (uint64, error)
}

// V4BaseStateReader loads on-chain base state for a V4 pool.
type V4BaseStateReader interface {
	ReadV4BaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*BaseState, error)
}

// V3BaseStateReader loads on-chain base state for a V3-style pool.
type V3BaseStateReader interface {
	ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BaseState, error)
}

// ChainReaders provides optional on-chain readers for pool diagnostics.
type ChainReaders struct {
	Head    HeadBlockReader
	V4      V4BaseStateReader
	V3      V3BaseStateReader
	Pancake V3BaseStateReader
}
