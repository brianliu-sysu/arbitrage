package pancakev3

import (
	"math/big"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

// QuoteService quotes swaps against PancakeSwap V3 pool state.
type QuoteService struct {
	inner *quoteclv3.QuoteService
}

func NewQuoteService() *QuoteService {
	return &QuoteService{inner: quoteclv3.NewQuoteService()}
}

func (s *QuoteService) QuoteExactInput(pool *marketpancake.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.inner.QuoteExactInput(&pool.Pool, tokenIn, tokenOut, amountIn)
}

func (s *QuoteService) QuoteExactOutput(pool *marketpancake.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.inner.QuoteExactOutput(&pool.Pool, tokenIn, tokenOut, amountOut)
}

func (s *QuoteService) QuoteRoute(pools map[common.Address]*marketpancake.Pool, route Route, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	clPools := make(map[common.Address]*marketclv3.Pool, len(pools))
	for address, pool := range pools {
		if pool != nil {
			clPools[address] = &pool.Pool
		}
	}
	return s.inner.QuoteRoute(clPools, route, amountIn)
}
