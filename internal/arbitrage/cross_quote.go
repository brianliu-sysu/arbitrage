package arbitrage

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/quote"
	"github.com/brianliu-sysu/arbitrage/internal/router"
	"github.com/ethereum/go-ethereum/common"
)

// CrossQuoter 跨池最优路径报价。
type CrossQuoter struct {
	cache  *pool.Cache
	router *router.PathFinder
	logger logx.Logger
}

// NewCrossQuoter 创建跨池报价器。
func NewCrossQuoter(cache *pool.Cache, pf *router.PathFinder, logger logx.Logger) *CrossQuoter {
	return &CrossQuoter{cache: cache, router: pf, logger: logger}
}

// Quote 搜索最优路径并报价。
func (q *CrossQuoter) Quote(amountIn *big.Int, tokenIn, tokenOut common.Address) (*quote.Result, error) {
	if q.router == nil {
		return nil, fmt.Errorf("path finder not initialized")
	}
	paths := q.router.FindPaths(tokenIn, tokenOut)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no swap path found from %s to %s", tokenIn.Hex(), tokenOut.Hex())
	}

	var best *quote.Result
	for _, path := range paths {
		current := new(big.Int).Set(amountIn)
		valid := true
		hops := make([]quote.Hop, len(path.Hops))

		for i, hop := range path.Hops {
			state, ok := q.cache.Get(hop.PoolAddr)
			if !ok {
				valid = false
				break
			}
			out, err := state.QuoteExactInput(current, hop.TokenIn)
			if err != nil {
				if q.logger != nil {
					q.logger.Error("cross-quote hop failed",
						"pool", hop.PoolAddr.Hex(),
						"tokenIn", hop.TokenIn.Hex(),
						"tokenOut", hop.TokenOut.Hex(),
						"err", err)
				}
				valid = false
				break
			}
			current = out
			hops[i] = quote.Hop{
				Pool:     hop.PoolAddr,
				TokenIn:  hop.TokenIn,
				TokenOut: hop.TokenOut,
			}
		}
		if !valid {
			continue
		}
		if best == nil || current.Cmp(best.AmountOut) > 0 {
			best = &quote.Result{
				Hops:      hops,
				AmountIn:  new(big.Int).Set(amountIn),
				AmountOut: current,
				TokenIn:   tokenIn,
				TokenOut:  tokenOut,
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("all paths failed to produce a quote")
	}
	return best, nil
}
