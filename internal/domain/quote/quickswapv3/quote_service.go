package quickswapv3

import (
	"math/big"

	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	quoteclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/clv3"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	"github.com/ethereum/go-ethereum/common"
)

// QuoteService quotes swaps against QuickSwap V3 pool state.
type QuoteService struct {
	inner *quoteclv3.QuoteService
}

func NewQuoteService() *QuoteService {
	return &QuoteService{inner: quoteclv3.NewQuoteService()}
}

func (s *QuoteService) QuoteExactInput(pool *marketquick.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.inner.QuoteExactInput(&pool.Pool, tokenIn, tokenOut, amountIn)
}

func (s *QuoteService) QuoteExactOutput(pool *marketquick.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.inner.QuoteExactOutput(&pool.Pool, tokenIn, tokenOut, amountOut)
}
