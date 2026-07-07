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
	return r.readV4BaseState(ctx, r.v4, poolID, blockNumber)
}

func (r *PoolsChainReaders) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*poolsapp.BaseState, error) {
	return r.readCLV3BaseState(ctx, r.v3, poolAddress, blockNumber)
}

func (r *PoolsChainReaders) ReadManyV3BaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*poolsapp.BaseState, error) {
	return r.readManyCLV3BaseStates(ctx, r.v3, poolAddresses, blockNumber)
}

func (r *PoolsChainReaders) ReadManyV4BaseStates(
	ctx context.Context,
	poolIDs []marketv4.PoolID,
	blockNumber uint64,
) (map[marketv4.PoolID]*poolsapp.BaseState, error) {
	if r.v4 == nil {
		return nil, ErrClientClosed
	}
	states, err := r.v4.ReadManyBaseStates(ctx, poolIDs, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsV4BaseStateMap(states), nil
}

func (r *PoolsChainReaders) readCLV3BaseState(
	ctx context.Context,
	reader *PoolReader,
	poolAddress common.Address,
	blockNumber uint64,
) (*poolsapp.BaseState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	state, err := reader.ReadBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsBaseState(state), nil
}

func (r *PoolsChainReaders) readManyCLV3BaseStates(
	ctx context.Context,
	reader *PoolReader,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*poolsapp.BaseState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	states, err := reader.ReadManyBaseStates(ctx, poolAddresses, blockNumber)
	if err != nil {
		return nil, err
	}
	return toPoolsBaseStateMap(states), nil
}

func (r *PoolsChainReaders) readV4BaseState(
	ctx context.Context,
	reader *V4PoolReader,
	poolID marketv4.PoolID,
	blockNumber uint64,
) (*poolsapp.BaseState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	state, err := reader.ReadBaseState(ctx, poolID, blockNumber)
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

func toPoolsBaseStateMap(states map[common.Address]*BasePoolState) map[common.Address]*poolsapp.BaseState {
	out := make(map[common.Address]*poolsapp.BaseState, len(states))
	for address, state := range states {
		out[address] = toPoolsBaseState(state)
	}
	return out
}

func toPoolsV4BaseStateMap(states map[marketv4.PoolID]*BasePoolState) map[marketv4.PoolID]*poolsapp.BaseState {
	out := make(map[marketv4.PoolID]*poolsapp.BaseState, len(states))
	for poolID, state := range states {
		out[poolID] = toPoolsBaseState(state)
	}
	return out
}

type pancakeChainReader struct{ inner *PoolsChainReaders }

func (r pancakeChainReader) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*poolsapp.BaseState, error) {
	return r.inner.readCLV3BaseState(ctx, r.inner.pancake, poolAddress, blockNumber)
}

func (r pancakeChainReader) ReadManyV3BaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*poolsapp.BaseState, error) {
	return r.inner.readManyCLV3BaseStates(ctx, r.inner.pancake, poolAddresses, blockNumber)
}
