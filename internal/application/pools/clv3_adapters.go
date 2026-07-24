package poolsapp

import (
	"context"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

// CLV3Adapter handles protocols sharing concentrated-liquidity V3 state.
type CLV3Adapter struct {
	source clv3PoolSource
}

type univ3PoolRepositoryAdapter struct {
	pools marketuniv3.PoolRepository
}

func (a *univ3PoolRepositoryAdapter) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	pool, err := a.pools.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return pool.Pool.Clone(), nil
}

type pancakeV3PoolRepositoryAdapter struct {
	pools marketpancake.PoolRepository
}

func (a *pancakeV3PoolRepositoryAdapter) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	pool, err := a.pools.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return pool.Pool.Clone(), nil
}

func NewUniv3Adapter(
	pools marketuniv3.PoolRepository,
	registry marketuniv3.PoolRegistry,
	reader V3BaseStateReader,
) *CLV3Adapter {
	return &CLV3Adapter{source: clv3PoolSource{
		poolType: PoolTypeUniv3, listLabel: "univ3",
		registry: registry,
		pools:    &univ3PoolRepositoryAdapter{pools: pools},
		reader:   reader,
	}}
}

func NewPancakeV3Adapter(
	pools marketpancake.PoolRepository,
	registry marketpancake.PoolRegistry,
	reader V3BaseStateReader,
) *CLV3Adapter {
	return &CLV3Adapter{source: clv3PoolSource{
		poolType: PoolTypePancakeV3, listLabel: "pancakev3",
		registry: registry,
		pools:    &pancakeV3PoolRepositoryAdapter{pools: pools},
		reader:   reader,
	}}
}

func (a *CLV3Adapter) Type() string { return a.source.poolType }

func (a *CLV3Adapter) List(ctx context.Context) ([]PoolInfo, error) {
	items := make([]PoolInfo, 0)
	if err := appendCLV3PoolInfos(ctx, a.source, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (a *CLV3Adapter) Diagnostics(
	ctx context.Context,
	req DiagnosticsRequest,
	head uint64,
	resolve TokenMetadataResolver,
) (*DiagnosticsResponse, error) {
	return diagnosticsCLV3(ctx, a.source, req.PoolAddress, head, resolve)
}

func (a *CLV3Adapter) AppendMismatches(
	ctx context.Context,
	head uint64,
	resolve TokenMetadataResolver,
	items *[]DiagnosticsResponse,
) error {
	return appendMismatchingCLV3Pools(ctx, a.source, head, resolve, items)
}
