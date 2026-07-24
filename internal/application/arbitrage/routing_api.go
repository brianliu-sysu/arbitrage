package arbitrageapp

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage/routing"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

const autoStartTokenCount = 3

var ErrNoPoolsAvailable = routing.ErrNoPoolsAvailable

type PoolEdgeSource = routing.PoolEdgeSource
type ScanService = routing.ScanService

var NewScanService = routing.NewScanService
var ResolveTriangleStartTokens = routing.ResolveTriangleStartTokens
var TopPoolOverlapTokens = routing.TopPoolOverlapTokens
var ResolveSpreadStartTokens = routing.ResolveSpreadStartTokens
var TokensWithParallelPools = routing.TokensWithParallelPools

func dedupeStartTokens(tokens []common.Address) []common.Address {
	return routing.DedupeStartTokens(tokens)
}

func BuildUnifiedPoolGraph(ctx context.Context, sources ...PoolEdgeSource) (quoteunified.PoolGraph, error) {
	return routing.BuildUnifiedPoolGraph(ctx, sources...)
}

func NewUniv3PoolEdgeSource(registry marketuniv3.PoolRegistry, repository marketuniv3.PoolRepository) PoolEdgeSource {
	return routing.NewUniv3PoolEdgeSource(registry, repository)
}

func NewPancakeV3PoolEdgeSource(registry marketpancake.PoolRegistry, repository marketpancake.PoolRepository) PoolEdgeSource {
	return routing.NewPancakeV3PoolEdgeSource(registry, repository)
}

func NewQuickSwapV3PoolEdgeSource(registry marketquick.PoolRegistry, repository marketquick.PoolRepository) PoolEdgeSource {
	return routing.NewQuickSwapV3PoolEdgeSource(registry, repository)
}

func NewUniv4PoolEdgeSource(registry marketuniv4.PoolRegistry, repository marketuniv4.PoolRepository) PoolEdgeSource {
	return routing.NewUniv4PoolEdgeSource(registry, repository)
}

func NewBalancerPoolEdgeSource(registry marketbalancer.PoolRegistry, repository marketbalancer.PoolRepository) PoolEdgeSource {
	return routing.NewBalancerPoolEdgeSource(registry, repository)
}
