package blockchain

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type PoolsHeadReader struct {
	client *EthClient
}

func NewPoolsHeadReader(client *EthClient) *PoolsHeadReader {
	return &PoolsHeadReader{client: client}
}

func (r *PoolsHeadReader) LatestBlockNumber(ctx context.Context) (uint64, error) {
	if r == nil || r.client == nil {
		return 0, ErrClientClosed
	}
	header, err := r.client.GetLatestBlockHeader(ctx)
	if err != nil {
		return 0, err
	}
	return header.Number, nil
}

type CLV3ChainReader struct {
	reader *PoolReader
}

func NewCLV3ChainReader(reader *PoolReader) *CLV3ChainReader {
	return &CLV3ChainReader{reader: reader}
}

func (r *CLV3ChainReader) ReadV3BaseState(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
) (*domainchain.BasePoolState, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadBaseState(ctx, poolAddress, blockNumber)
}

func (r *CLV3ChainReader) ReadManyV3BaseStates(
	ctx context.Context,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*domainchain.BasePoolState, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadManyBaseStates(ctx, poolAddresses, blockNumber)
}

type V4ChainReader struct {
	reader *V4PoolReader
}

func NewV4ChainReader(reader *V4PoolReader) *V4ChainReader {
	return &V4ChainReader{reader: reader}
}

func (r *V4ChainReader) ReadV4BaseState(
	ctx context.Context,
	poolID marketv4.PoolID,
	blockNumber uint64,
) (*domainchain.BasePoolState, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadBaseState(ctx, poolID, blockNumber)
}

func (r *V4ChainReader) ReadManyV4BaseStates(
	ctx context.Context,
	poolIDs []marketv4.PoolID,
	blockNumber uint64,
) (map[marketv4.PoolID]*domainchain.BasePoolState, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadManyBaseStates(ctx, poolIDs, blockNumber)
}

type BalancerChainReader struct {
	reader *BalancerPoolReader
}

func NewBalancerChainReader(reader *BalancerPoolReader) *BalancerChainReader {
	return &BalancerChainReader{reader: reader}
}

func (r *BalancerChainReader) ReadBalancerState(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	spec marketbalancer.PoolSpec,
	blockNumber uint64,
) (*marketbalancer.BootstrapData, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadBootstrapData(ctx, poolID, spec, blockNumber)
}

func (r *BalancerChainReader) ReadManyBalancerStates(
	ctx context.Context,
	inputs []marketbalancer.BootstrapInput,
	blockNumber uint64,
) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	if r == nil || r.reader == nil {
		return nil, ErrClientClosed
	}
	return r.reader.ReadManyBootstrapData(ctx, inputs, blockNumber)
}
