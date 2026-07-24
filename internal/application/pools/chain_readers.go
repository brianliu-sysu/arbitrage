package poolsapp

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// BaseState is on-chain slot0 and liquidity without tick data.
type BaseState = blockchain.BasePoolState

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

// BalancerStateReader loads on-chain Balancer pool state.
type BalancerStateReader interface {
	ReadBalancerState(ctx context.Context, poolID marketbalancer.PoolID, spec marketbalancer.PoolSpec, blockNumber uint64) (*marketbalancer.BootstrapData, error)
}

// BalancerStateBatchReader loads on-chain Balancer pool state for many pools in one batched request.
type BalancerStateBatchReader interface {
	ReadManyBalancerStates(ctx context.Context, inputs []marketbalancer.BootstrapInput, blockNumber uint64) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error)
}
