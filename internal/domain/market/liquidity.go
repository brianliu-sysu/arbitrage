package market

import (
	"fmt"
	"math/big"
)

// ModifyLiquidity applies a liquidity delta to concentrated-liquidity pool state.
// Shared by Uniswap V3 mint/burn and V4 modifyLiquidity semantics.
func ModifyLiquidity(
	tickSpacing int32,
	state *PoolState,
	ticks *TickTable,
	bitmap *TickBitmap,
	tickLower, tickUpper int32,
	liquidityDelta *big.Int,
) error {
	if liquidityDelta == nil || liquidityDelta.Sign() == 0 {
		return fmt.Errorf("liquidity delta must be non-zero")
	}
	if err := validateTickSpacing(tickLower, tickSpacing); err != nil {
		return err
	}
	if err := validateTickSpacing(tickUpper, tickSpacing); err != nil {
		return err
	}
	if tickLower >= tickUpper {
		return fmt.Errorf("tickLower %d must be less than tickUpper %d", tickLower, tickUpper)
	}

	flippedLower, err := ticks.Update(tickLower, liquidityDelta, false)
	if err != nil {
		return fmt.Errorf("update lower tick: %w", err)
	}
	if flippedLower {
		if err := bitmap.FlipTick(tickLower, tickSpacing); err != nil {
			return fmt.Errorf("flip lower tick bitmap: %w", err)
		}
	}

	flippedUpper, err := ticks.Update(tickUpper, liquidityDelta, true)
	if err != nil {
		return fmt.Errorf("update upper tick: %w", err)
	}
	if flippedUpper {
		if err := bitmap.FlipTick(tickUpper, tickSpacing); err != nil {
			return fmt.Errorf("flip upper tick bitmap: %w", err)
		}
	}

	if state.Tick >= tickLower && state.Tick < tickUpper {
		state.Liquidity = new(big.Int).Add(state.Liquidity, liquidityDelta)
		if state.Liquidity.Sign() < 0 {
			return fmt.Errorf("pool liquidity underflow")
		}
	}
	return nil
}

func validateTickSpacing(tick, tickSpacing int32) error {
	if err := validateTick(tick); err != nil {
		return err
	}
	if tick%tickSpacing != 0 {
		return fmt.Errorf("tick %d is not aligned to spacing %d", tick, tickSpacing)
	}
	return nil
}

// ValidateTick checks that a tick index is within the allowed range.
func ValidateTick(index int32) error {
	return validateTick(index)
}
