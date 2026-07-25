package arbitrageapp

import (
	"github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage/routing"
	"github.com/ethereum/go-ethereum/common"
)

const autoStartTokenCount = 3

type ScanService = routing.ScanService

var NewScanService = routing.NewScanService
var ResolveTriangleStartTokens = routing.ResolveTriangleStartTokens
var TopPoolOverlapTokens = routing.TopPoolOverlapTokens
var ResolveSpreadStartTokens = routing.ResolveSpreadStartTokens
var TokensWithParallelPools = routing.TokensWithParallelPools

func dedupeStartTokens(tokens []common.Address) []common.Address {
	return routing.DedupeStartTokens(tokens)
}
