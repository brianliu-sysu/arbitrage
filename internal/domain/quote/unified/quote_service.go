package unified

import (
	"errors"
	"fmt"
	"math/big"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quotebalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/balancer"
	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	quotequickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/quickswapv3"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// ErrNonPositiveAmount indicates a hop received or produced a non-positive amount.
var ErrNonPositiveAmount = errors.New("amount must be positive")

// RoutePools holds loaded pool state keyed by protocol-specific identifiers.
type RoutePools struct {
	V3          map[common.Address]*marketv3.Pool
	PancakeV3   map[common.Address]*marketpancake.Pool
	QuickSwapV3 map[common.Address]*marketquick.Pool
	V4          map[marketv4.PoolID]*marketv4.Pool
	Balancer    map[marketbalancer.PoolID]*marketbalancer.Pool
}

// QuoteService quotes swaps along unified routes by dispatching to V3-style or V4 math.
type QuoteService struct {
	v3        *quoteuniv3.QuoteService
	pancake   *quotepancakev3.QuoteService
	quickSwap *quotequickswapv3.QuoteService
	v4        *quoteuniv4.QuoteService
	balancer  *quotebalancer.QuoteService
}

type RouteQuoteStep struct {
	Index        int
	Hop          RouteHop
	AmountIn     *big.Int
	AmountOut    *big.Int
	FeeAmount    *big.Int
	SqrtPriceX96 *big.Int
	Tick         int32
}

func NewQuoteService(v3 *quoteuniv3.QuoteService, pancake *quotepancakev3.QuoteService, v4 *quoteuniv4.QuoteService, balancer ...*quotebalancer.QuoteService) *QuoteService {
	if v3 == nil {
		v3 = quoteuniv3.NewQuoteService()
	}
	if pancake == nil {
		pancake = quotepancakev3.NewQuoteService()
	}
	if v4 == nil {
		v4 = quoteuniv4.NewQuoteService()
	}
	balancerSvc := quotebalancer.NewQuoteService()
	if len(balancer) > 0 && balancer[0] != nil {
		balancerSvc = balancer[0]
	}
	return &QuoteService{v3: v3, pancake: pancake, quickSwap: quotequickswapv3.NewQuoteService(), v4: v4, balancer: balancerSvc}
}

// QuoteExactInput quotes an exact-input swap on a single Uniswap V3 pool.
func (s *QuoteService) QuoteExactInputV3(pool *marketv3.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.v3.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutput quotes an exact-output swap on a single Uniswap V3 pool.
func (s *QuoteService) QuoteExactOutputV3(pool *marketv3.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.v3.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteExactInputPancakeV3 quotes an exact-input swap on a single PancakeSwap V3 pool.
func (s *QuoteService) QuoteExactInputPancakeV3(pool *marketpancake.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.pancake.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutputPancakeV3 quotes an exact-output swap on a single PancakeSwap V3 pool.
func (s *QuoteService) QuoteExactOutputPancakeV3(pool *marketpancake.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.pancake.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteExactInputQuickSwapV3 quotes an exact-input swap on a single QuickSwap V3 pool.
func (s *QuoteService) QuoteExactInputQuickSwapV3(pool *marketquick.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.quickSwap.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutputQuickSwapV3 quotes an exact-output swap on a single QuickSwap V3 pool.
func (s *QuoteService) QuoteExactOutputQuickSwapV3(pool *marketquick.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.quickSwap.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteExactInputV4 quotes an exact-input swap on a single V4 pool.
func (s *QuoteService) QuoteExactInputV4(pool *marketv4.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.v4.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutputV4 quotes an exact-output swap on a single V4 pool.
func (s *QuoteService) QuoteExactOutputV4(pool *marketv4.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.v4.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteExactInputBalancer quotes an exact-input swap on a single Balancer pool.
func (s *QuoteService) QuoteExactInputBalancer(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	return s.balancer.QuoteExactInput(pool, tokenIn, tokenOut, amountIn)
}

// QuoteExactOutputBalancer quotes an exact-output swap on a single Balancer pool.
func (s *QuoteService) QuoteExactOutputBalancer(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	return s.balancer.QuoteExactOutput(pool, tokenIn, tokenOut, amountOut)
}

// QuoteRoute quotes an exact-input swap along a multi-hop unified route.
func (s *QuoteService) QuoteRoute(pools RoutePools, route Route, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	steps, err := s.QuoteRouteSteps(pools, route, amountIn)
	if err != nil {
		return quoteshared.QuoteResult{}, err
	}
	totalFee := big.NewInt(0)
	for _, step := range steps {
		totalFee.Add(totalFee, step.FeeAmount)
	}
	last := steps[len(steps)-1]

	return quoteshared.NewQuoteResult(amountIn, last.AmountOut, totalFee, last.SqrtPriceX96, last.Tick), nil
}

func (s *QuoteService) QuoteRouteSteps(pools RoutePools, route Route, amountIn *big.Int) ([]RouteQuoteStep, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("%w", ErrNonPositiveAmount)
	}
	if len(route.Hops) == 0 {
		return nil, fmt.Errorf("route has no hops")
	}

	currentAmount := new(big.Int).Set(amountIn)
	steps := make([]RouteQuoteStep, 0, len(route.Hops))

	for i, hop := range route.Hops {
		if currentAmount == nil || currentAmount.Sign() <= 0 {
			return nil, fmt.Errorf("hop %d: %w", i, ErrNonPositiveAmount)
		}
		step, err := s.quoteHop(pools, hop, currentAmount)
		if err != nil {
			return nil, fmt.Errorf("hop %d: %w", i, err)
		}
		if step.AmountOut == nil || step.AmountOut.Sign() <= 0 {
			return nil, fmt.Errorf("hop %d: %w", i, ErrNonPositiveAmount)
		}

		steps = append(steps, RouteQuoteStep{
			Index:        i,
			Hop:          hop,
			AmountIn:     new(big.Int).Set(currentAmount),
			AmountOut:    cloneBigInt(step.AmountOut),
			FeeAmount:    cloneBigInt(step.FeeAmount),
			SqrtPriceX96: cloneBigInt(step.SqrtPriceX96),
			Tick:         step.Tick,
		})
		currentAmount = step.AmountOut
	}

	return steps, nil
}

func (s *QuoteService) quoteHop(pools RoutePools, hop RouteHop, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	switch hop.Version {
	case PoolVersionV3:
		pool := pools.V3[hop.PoolV3]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("univ3 pool %s not found", hop.PoolV3.Hex())
		}
		return s.v3.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionPancakeV3:
		pool := pools.PancakeV3[hop.PoolPancakeV3]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("pancakev3 pool %s not found", hop.PoolPancakeV3.Hex())
		}
		return s.pancake.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionQuickSwapV3:
		pool := pools.QuickSwapV3[hop.PoolQuickSwapV3]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("quickswapv3 pool %s not found", hop.PoolQuickSwapV3.Hex())
		}
		return s.quickSwap.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionV4:
		pool := pools.V4[hop.PoolV4]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("univ4 pool %s not found", hop.PoolV4.String())
		}
		return s.v4.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionBalancer:
		pool := pools.Balancer[hop.PoolBalancer]
		if pool == nil {
			return quoteshared.QuoteResult{}, fmt.Errorf("balancer pool %s not found", hop.PoolBalancer.String())
		}
		return s.balancer.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, amountIn)
	case PoolVersionWrapWETH, PoolVersionUnwrapWETH:
		return QuoteWETHBridge(hop, amountIn)
	default:
		return quoteshared.QuoteResult{}, fmt.Errorf("unsupported pool version %d", hop.Version)
	}
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
