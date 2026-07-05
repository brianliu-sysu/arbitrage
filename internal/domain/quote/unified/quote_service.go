package unified

import (
	"fmt"
	"math/big"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// RoutePools holds loaded pool state keyed by protocol-specific identifiers.
type RoutePools struct {
	V3 map[common.Address]*marketv3.Pool
	V4 map[marketv4.PoolID]*marketv4.Pool
}

// QuoteService quotes swaps along unified routes by dispatching to V3 or V4 math.
type QuoteService struct {
	v3 *quoteuniv3.QuoteService
	v4 *quoteuniv4.QuoteService
}

func NewQuoteService(v3 *quoteuniv3.QuoteService, v4 *quoteuniv4.QuoteService) *QuoteService {
	if v3 == nil {
		v3 = quoteuniv3.NewQuoteService()
	}
	if v4 == nil {
		v4 = quoteuniv4.NewQuoteService()
	}
	return &QuoteService{v3: v3, v4: v4}
}

// QuoteExactInput quotes an exact-input swap on a single V3 pool.
func (s *QuoteService) QuoteExactInputV3(pool *marketv3.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.v3.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutput quotes an exact-output swap on a single V3 pool.
func (s *QuoteService) QuoteExactOutputV3(pool *marketv3.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.v3.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteExactInputV4 quotes an exact-input swap on a single V4 pool.
func (s *QuoteService) QuoteExactInputV4(pool *marketv4.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.v4.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutputV4 quotes an exact-output swap on a single V4 pool.
func (s *QuoteService) QuoteExactOutputV4(pool *marketv4.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.v4.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteRoute quotes an exact-input swap along a multi-hop unified route.
func (s *QuoteService) QuoteRoute(pools RoutePools, route Route, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return quoteshared.QuoteResult{}, fmt.Errorf("amountIn must be positive")
	}
	if len(route.Hops) == 0 {
		return quoteshared.QuoteResult{}, fmt.Errorf("route has no hops")
	}

	currentAmount := new(big.Int).Set(amountIn)
	totalFee := big.NewInt(0)
	var last quoteshared.QuoteResult

	for i, hop := range route.Hops {
		step, err := s.quoteHop(pools, hop, currentAmount)
		if err != nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("hop %d: %w", i, err)
		}

		totalFee.Add(totalFee, step.FeeAmount)
		currentAmount = step.AmountOut
		last = step
	}

	return quoteshared.NewQuoteResult(amountIn, currentAmount, totalFee, last.SqrtPriceX96, last.Tick), nil
}

func (s *QuoteService) quoteHop(pools RoutePools, hop RouteHop, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	switch hop.Version {
	case PoolVersionV3:
		pool := pools.V3[hop.PoolV3]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("v3 pool %s not found", hop.PoolV3.Hex())
		}
		return s.v3.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionV4:
		pool := pools.V4[hop.PoolV4]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("v4 pool %s not found", hop.PoolV4.String())
		}
		return s.v4.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	default:
		return quoteshared.QuoteResult{}, fmt.Errorf("unsupported pool version %d", hop.Version)
	}
}
