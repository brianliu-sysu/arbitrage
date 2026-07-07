package blockchain

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// PoolsChainReaders adapts blockchain clients for pool diagnostics.
type PoolsChainReaders struct {
	client   *EthClient
	v4       *V4PoolReader
	v3       *PoolReader
	pancake  *PoolReader
	balancer *BalancerPoolReader
}

func NewPoolsChainReaders(client *EthClient, v3, pancake *PoolReader, v4 *V4PoolReader, balancer *BalancerPoolReader) *PoolsChainReaders {
	return &PoolsChainReaders{client: client, v3: v3, pancake: pancake, v4: v4, balancer: balancer}
}

func (r *PoolsChainReaders) PancakeReader() PancakeChainReader {
	return PancakeChainReader{inner: r}
}

func (r *PoolsChainReaders) LatestBlockNumber(ctx context.Context) (uint64, error) {
	header, err := r.client.GetLatestBlockHeader(ctx)
	if err != nil {
		return 0, err
	}
	return header.Number, nil
}

func (r *PoolsChainReaders) ReadV4BaseState(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*domainchain.BasePoolState, error) {
	return r.readV4BaseState(ctx, r.v4, poolID, blockNumber)
}

func (r *PoolsChainReaders) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*domainchain.BasePoolState, error) {
	return r.readCLV3BaseState(ctx, r.v3, poolAddress, blockNumber)
}

func (r *PoolsChainReaders) ReadManyV3BaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*domainchain.BasePoolState, error) {
	return r.readManyCLV3BaseStates(ctx, r.v3, poolAddresses, blockNumber)
}

func (r *PoolsChainReaders) ReadManyV4BaseStates(
	ctx context.Context,
	poolIDs []marketv4.PoolID,
	blockNumber uint64,
) (map[marketv4.PoolID]*domainchain.BasePoolState, error) {
	if r.v4 == nil {
		return nil, ErrClientClosed
	}
	states, err := r.v4.ReadManyBaseStates(ctx, poolIDs, blockNumber)
	if err != nil {
		return nil, err
	}
	return states, nil
}

func (r *PoolsChainReaders) ReadBalancerState(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	spec marketbalancer.PoolSpec,
	blockNumber uint64,
) (*marketbalancer.BootstrapData, error) {
	if r.balancer == nil {
		return nil, ErrClientClosed
	}
	return r.balancer.ReadBootstrapData(ctx, poolID, spec, blockNumber)
}

func (r *PoolsChainReaders) ReadManyBalancerStates(
	ctx context.Context,
	inputs []marketbalancer.BootstrapInput,
	blockNumber uint64,
) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	if r.balancer == nil {
		return nil, ErrClientClosed
	}
	return r.balancer.ReadManyBootstrapData(ctx, inputs, blockNumber)
}

func (r *PoolsChainReaders) readCLV3BaseState(
	ctx context.Context,
	reader *PoolReader,
	poolAddress common.Address,
	blockNumber uint64,
) (*domainchain.BasePoolState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	state, err := reader.ReadBaseState(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (r *PoolsChainReaders) readManyCLV3BaseStates(
	ctx context.Context,
	reader *PoolReader,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*domainchain.BasePoolState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	states, err := reader.ReadManyBaseStates(ctx, poolAddresses, blockNumber)
	if err != nil {
		return nil, err
	}
	return states, nil
}

func (r *PoolsChainReaders) readV4BaseState(
	ctx context.Context,
	reader *V4PoolReader,
	poolID marketv4.PoolID,
	blockNumber uint64,
) (*domainchain.BasePoolState, error) {
	if reader == nil {
		return nil, ErrClientClosed
	}
	state, err := reader.ReadBaseState(ctx, poolID, blockNumber)
	if err != nil {
		return nil, err
	}
	return state, nil
}

type PancakeChainReader struct{ inner *PoolsChainReaders }

func (r PancakeChainReader) ReadV3BaseState(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*domainchain.BasePoolState, error) {
	return r.inner.readCLV3BaseState(ctx, r.inner.pancake, poolAddress, blockNumber)
}

func (r PancakeChainReader) ReadManyV3BaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*domainchain.BasePoolState, error) {
	return r.inner.readManyCLV3BaseStates(ctx, r.inner.pancake, poolAddresses, blockNumber)
}
