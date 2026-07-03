package quote

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// QuoteService quotes swaps against pool state using Uniswap V3 math.
type QuoteService struct{}

func NewQuoteService() *QuoteService {
	return &QuoteService{}
}

// QuoteExactInput quotes an exact-input swap on a single pool.
func (s *QuoteService) QuoteExactInput(pool *market.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (QuoteResult, error) {
	if pool == nil {
		return QuoteResult{}, fmt.Errorf("pool is nil")
	}
	if amountIn == nil || amountIn.Sign() <= 0 {
		return QuoteResult{}, fmt.Errorf("amountIn must be positive")
	}

	zeroForOne, err := resolveSwapDirection(pool, tokenIn, tokenOut)
	if err != nil {
		return QuoteResult{}, err
	}

	return s.swap(pool, zeroForOne, true, new(big.Int).Set(amountIn), nil)
}

// QuoteExactOutput quotes an exact-output swap on a single pool.
func (s *QuoteService) QuoteExactOutput(pool *market.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (QuoteResult, error) {
	if pool == nil {
		return QuoteResult{}, fmt.Errorf("pool is nil")
	}
	if amountOut == nil || amountOut.Sign() <= 0 {
		return QuoteResult{}, fmt.Errorf("amountOut must be positive")
	}

	zeroForOne, err := resolveSwapDirection(pool, tokenIn, tokenOut)
	if err != nil {
		return QuoteResult{}, err
	}

	result, err := s.swap(pool, zeroForOne, false, new(big.Int).Set(amountOut), nil)
	if err != nil {
		return QuoteResult{}, err
	}

	return QuoteResult{
		AmountIn:     result.AmountIn,
		AmountOut:    new(big.Int).Set(amountOut),
		FeeAmount:    result.FeeAmount,
		SqrtPriceX96: result.SqrtPriceX96,
		Tick:         result.Tick,
	}, nil
}

// QuoteRoute quotes an exact-input swap along a multi-hop route.
func (s *QuoteService) QuoteRoute(pools map[common.Address]*market.Pool, route Route, amountIn *big.Int) (QuoteResult, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return QuoteResult{}, fmt.Errorf("amountIn must be positive")
	}
	if len(route.Hops) == 0 {
		return QuoteResult{}, fmt.Errorf("route has no hops")
	}

	currentAmount := new(big.Int).Set(amountIn)
	totalFee := big.NewInt(0)
	var last QuoteResult

	for i, hop := range route.Hops {
		pool := pools[hop.PoolAddress]
		if pool == nil {
			return QuoteResult{}, fmt.Errorf("pool %s not found", hop.PoolAddress.Hex())
		}

		step, err := s.QuoteExactInput(pool, hop.TokenIn, hop.TokenOut, currentAmount)
		if err != nil {
			return QuoteResult{}, fmt.Errorf("hop %d: %w", i, err)
		}

		totalFee.Add(totalFee, step.FeeAmount)
		currentAmount = step.AmountOut
		last = step
	}

	return QuoteResult{
		AmountIn:     cloneBigInt(amountIn),
		AmountOut:    cloneBigInt(currentAmount),
		FeeAmount:    cloneBigInt(totalFee),
		SqrtPriceX96: cloneBigInt(last.SqrtPriceX96),
		Tick:         last.Tick,
	}, nil
}

type swapState struct {
	amountSpecifiedRemaining *big.Int
	amountCalculated         *big.Int
	sqrtPriceX96             *big.Int
	tick                     int32
	liquidity                *big.Int
}

func (s *QuoteService) swap(
	pool *market.Pool,
	zeroForOne bool,
	exactInput bool,
	amountSpecified *big.Int,
	sqrtPriceLimitX96 *big.Int,
) (QuoteResult, error) {
	if !pool.State.IsInitialized() {
		return QuoteResult{}, fmt.Errorf("pool is not initialized")
	}

	limit, err := resolveSqrtPriceLimit(zeroForOne, sqrtPriceLimitX96)
	if err != nil {
		return QuoteResult{}, err
	}

	state := swapState{
		sqrtPriceX96: new(big.Int).Set(pool.State.SqrtPriceX96),
		tick:         pool.State.Tick,
		liquidity:    new(big.Int).Set(pool.State.Liquidity),
	}
	if exactInput {
		state.amountSpecifiedRemaining = new(big.Int).Set(amountSpecified)
		state.amountCalculated = big.NewInt(0)
	} else {
		state.amountSpecifiedRemaining = new(big.Int).Neg(amountSpecified)
		state.amountCalculated = big.NewInt(0)
	}

	totalFee := big.NewInt(0)
	for state.amountSpecifiedRemaining.Sign() != 0 && state.sqrtPriceX96.Cmp(limit) != 0 {
		if err := s.runSwapStep(pool, &state, zeroForOne, exactInput, limit, totalFee); err != nil {
			return QuoteResult{}, err
		}
	}

	var amountIn, amountOut *big.Int
	if exactInput {
		amountIn = new(big.Int).Set(amountSpecified)
		amountOut = new(big.Int).Set(state.amountCalculated)
	} else {
		amountOut = new(big.Int).Set(amountSpecified)
		amountIn = new(big.Int).Set(state.amountCalculated)
	}

	return QuoteResult{
		AmountIn:     amountIn,
		AmountOut:    amountOut,
		FeeAmount:    totalFee,
		SqrtPriceX96: new(big.Int).Set(state.sqrtPriceX96),
		Tick:         state.tick,
	}, nil
}

func (s *QuoteService) runSwapStep(
	pool *market.Pool,
	state *swapState,
	zeroForOne bool,
	exactInput bool,
	sqrtPriceLimitX96 *big.Int,
	totalFee *big.Int,
) error {
	sqrtPriceStartX96 := new(big.Int).Set(state.sqrtPriceX96)

	tickNext, initialized, err := pool.Bitmap.NextInitializedTick(state.tick, pool.TickSpacing, zeroForOne)
	if err != nil {
		return fmt.Errorf("find next initialized tick: %w", err)
	}
	if tickNext < market.MinTick {
		tickNext = market.MinTick
	} else if tickNext > market.MaxTick {
		tickNext = market.MaxTick
	}

	sqrtPriceNextX96, err := GetSqrtRatioAtTick(tickNext)
	if err != nil {
		return fmt.Errorf("sqrt ratio at tick %d: %w", tickNext, err)
	}

	sqrtRatioTargetX96 := sqrtPriceNextX96
	if zeroForOne {
		if sqrtPriceNextX96.Cmp(sqrtPriceLimitX96) < 0 {
			sqrtRatioTargetX96 = sqrtPriceLimitX96
		}
	} else if sqrtPriceNextX96.Cmp(sqrtPriceLimitX96) > 0 {
		sqrtRatioTargetX96 = sqrtPriceLimitX96
	}

	step, err := ComputeSwapStep(
		state.sqrtPriceX96,
		sqrtRatioTargetX96,
		state.liquidity,
		state.amountSpecifiedRemaining,
		pool.Fee,
	)
	if err != nil {
		return fmt.Errorf("compute swap step: %w", err)
	}

	if exactInput {
		state.amountSpecifiedRemaining.Sub(state.amountSpecifiedRemaining, step.AmountIn)
		state.amountSpecifiedRemaining.Sub(state.amountSpecifiedRemaining, step.FeeAmount)
		state.amountCalculated.Add(state.amountCalculated, step.AmountOut)
	} else {
		state.amountSpecifiedRemaining.Add(state.amountSpecifiedRemaining, step.AmountOut)
		state.amountCalculated.Add(state.amountCalculated, step.AmountIn)
		state.amountCalculated.Add(state.amountCalculated, step.FeeAmount)
	}
	totalFee.Add(totalFee, step.FeeAmount)
	state.sqrtPriceX96 = step.SqrtRatioNextX96

	if state.sqrtPriceX96.Cmp(sqrtPriceNextX96) == 0 {
		if initialized {
			tickData, ok := pool.Ticks.Get(tickNext)
			if !ok {
				return fmt.Errorf("initialized tick %d missing from tick table", tickNext)
			}
			liquidityNet := new(big.Int).Set(tickData.LiquidityNet)
			if zeroForOne {
				liquidityNet.Neg(liquidityNet)
			}
			state.liquidity, err = AddDelta(state.liquidity, liquidityNet)
			if err != nil {
				return fmt.Errorf("cross tick %d: %w", tickNext, err)
			}
		}
		if zeroForOne {
			state.tick = tickNext - 1
		} else {
			state.tick = tickNext
		}
	} else if state.sqrtPriceX96.Cmp(sqrtPriceStartX96) != 0 {
		state.tick, err = GetTickAtSqrtRatio(state.sqrtPriceX96)
		if err != nil {
			return fmt.Errorf("tick at sqrt ratio: %w", err)
		}
	}

	return nil
}

func resolveSwapDirection(pool *market.Pool, tokenIn, tokenOut common.Address) (bool, error) {
	switch {
	case tokenIn == pool.Token0 && tokenOut == pool.Token1:
		return true, nil
	case tokenIn == pool.Token1 && tokenOut == pool.Token0:
		return false, nil
	default:
		return false, fmt.Errorf("tokens %s/%s do not match pool %s/%s", tokenIn.Hex(), tokenOut.Hex(), pool.Token0.Hex(), pool.Token1.Hex())
	}
}

func resolveSqrtPriceLimit(zeroForOne bool, sqrtPriceLimitX96 *big.Int) (*big.Int, error) {
	if sqrtPriceLimitX96 != nil {
		if sqrtPriceLimitX96.Sign() <= 0 {
			return nil, fmt.Errorf("sqrt price limit must be positive")
		}
		return new(big.Int).Set(sqrtPriceLimitX96), nil
	}
	if zeroForOne {
		return new(big.Int).Add(minSqrtRatio, big.NewInt(1)), nil
	}
	return new(big.Int).Sub(maxSqrtRatio, big.NewInt(1)), nil
}
