package blockchain

import (
	"context"

	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// PoolsChainReaders adapts blockchain clients for pool diagnostics.
type PoolsChainReaders struct {
	client  *EthClient
	v4      *V4PoolReader
	v3      *PoolReader
	pancake *PoolReader
}

func NewPoolsChainReaders(client *EthClient, v3, pancake *PoolReader, v4 *V4PoolReader) *PoolsChainReaders {
	return &PoolsChainReaders{client: client, v3: v3, pancake: pancake, v4: v4}
}

func (r *PoolsChainReaders) AsChainReaders() *poolsapp.ChainReaders {
	if r == nil {
		return nil
	}
	return &poolsapp.ChainReaders{
		Head:    r,
		V4:      r,
		V3:      r,
		Pancake: pancakeChainReader{inner: r},
	}
}

func (r *PoolsChainReaders) LatestBlockNumber(ctx context.Context) (uint64, error) {
	header, err := r.client.GetLatestBlockHeader(ctx)
	if err != nil {
		return 0, err
	}
	return header.Number, nil
}

func (r *PoolsChainReaders) ReadV4BaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*poolsapp.BaseState, error) {
	if r.v4 == nil {
		return nil, ErrClientClosed
	}
	state, err := r.v4.ReadBaseState(ctx, poolID, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsBaseState(state), nil
}

func (r *PoolsChainReaders) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*poolsapp.BaseState, error) {
	if r.v3 == nil {
		return nil, ErrClientClosed
	}
	state, err := r.v3.ReadBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsBaseState(state), nil
}

func (r *PoolsChainReaders) readPancakeBaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*poolsapp.BaseState, error) {
	if r.pancake == nil {
		return nil, ErrClientClosed
	}
	state, err := r.pancake.ReadBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsBaseState(state), nil
}

func toPoolsBaseState(state *BasePoolState) *poolsapp.BaseState {
	if state == nil {
		return nil
	}
	return &poolsapp.BaseState{
		SqrtPriceX96: state.SqrtPriceX96,
		Tick:         state.Tick,
		Liquidity:    state.Liquidity,
	}
}

type pancakeChainReader struct{ inner *PoolsChainReaders }

func (r pancakeChainReader) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*poolsapp.BaseState, error) {
	return r.inner.readPancakeBaseState(ctx, poolAddress, blockNumber)
}
