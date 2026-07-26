package arbitrageapp

import (
	"github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage/routing"
	"github.com/ethereum/go-ethereum/common"
)

type ScanService = routing.ScanService

var NewScanService = routing.NewScanService

func dedupeStartTokens(tokens []common.Address) []common.Address {
	seen := make(map[common.Address]struct{}, len(tokens))
	out := make([]common.Address, 0, len(tokens))
	for _, token := range tokens {
		if token == (common.Address{}) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}
