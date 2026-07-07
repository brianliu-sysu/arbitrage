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

// V4BaseStateBatchReader loads on-chain base state for many V4 pools in one batched request.
type V4BaseStateBatchReader interface {
	ReadManyV4BaseStates(ctx context.Context, poolIDs []marketv4.PoolID, blockNumber uint64) (map[marketv4.PoolID]*BaseState, error)
}

// V3BaseStateReader loads on-chain base state for a V3-style pool.
type V3BaseStateReader interface {
	ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*BaseState, error)
}

// V3BaseStateBatchReader loads on-chain base state for many V3-style pools in one batched request.
type V3BaseStateBatchReader interface {
	ReadManyV3BaseStates(ctx context.Context, poolAddresses []common.Address, blockNumber uint64) (map[common.Address]*BaseState, error)
}

// ChainReaders provides optional on-chain readers for pool diagnostics.
type ChainReaders struct {
	Head    HeadBlockReader
	V4      V4BaseStateReader
	V3      V3BaseStateReader
	Pancake V3BaseStateReader
}
