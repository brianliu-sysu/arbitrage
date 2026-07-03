package quote

import "math/big"

var feeDenominator = big.NewInt(1_000_000)

// SwapStepResult holds the outcome of a single swap step.
type SwapStepResult struct {
	SqrtRatioNextX96 *big.Int
	AmountIn         *big.Int
	AmountOut        *big.Int
	FeeAmount        *big.Int
}

// ComputeSwapStep computes a single swap step toward the target sqrt price.
// amountRemaining is positive for exact input and negative for exact output.
func ComputeSwapStep(
	sqrtRatioCurrentX96,
	sqrtRatioTargetX96,
	liquidity,
	amountRemaining *big.Int,
	feePips uint32,
) (SwapStepResult, error) {
	zeroForOne := sqrtRatioCurrentX96.Cmp(sqrtRatioTargetX96) >= 0
	exactIn := amountRemaining.Sign() >= 0

	result := SwapStepResult{
		AmountIn:  big.NewInt(0),
		AmountOut: big.NewInt(0),
	}

	feePipsBig := big.NewInt(int64(feePips))
	feeComplement := new(big.Int).Sub(feeDenominator, feePipsBig)

	if exactIn {
		amountRemainingLessFee := new(big.Int).Div(new(big.Int).Mul(amountRemaining, feeComplement), feeDenominator)
		if zeroForOne {
			result.AmountIn = GetAmount0Delta(sqrtRatioTargetX96, sqrtRatioCurrentX96, liquidity, true)
		} else {
			result.AmountIn = GetAmount1Delta(sqrtRatioCurrentX96, sqrtRatioTargetX96, liquidity, true)
		}
		if amountRemainingLessFee.Cmp(result.AmountIn) >= 0 {
			result.SqrtRatioNextX96 = new(big.Int).Set(sqrtRatioTargetX96)
		} else {
			next, err := GetNextSqrtPriceFromInput(sqrtRatioCurrentX96, liquidity, amountRemainingLessFee, zeroForOne)
			if err != nil {
				return SwapStepResult{}, err
			}
			result.SqrtRatioNextX96 = next
		}
	} else {
		negatedRemaining := new(big.Int).Neg(amountRemaining)
		if zeroForOne {
			result.AmountOut = GetAmount1Delta(sqrtRatioTargetX96, sqrtRatioCurrentX96, liquidity, false)
		} else {
			result.AmountOut = GetAmount0Delta(sqrtRatioCurrentX96, sqrtRatioTargetX96, liquidity, false)
		}
		if negatedRemaining.Cmp(result.AmountOut) >= 0 {
			result.SqrtRatioNextX96 = new(big.Int).Set(sqrtRatioTargetX96)
		} else {
			next, err := GetNextSqrtPriceFromOutput(sqrtRatioCurrentX96, liquidity, negatedRemaining, zeroForOne)
			if err != nil {
				return SwapStepResult{}, err
			}
			result.SqrtRatioNextX96 = next
		}
	}

	max := sqrtRatioTargetX96.Cmp(result.SqrtRatioNextX96) == 0
	if zeroForOne {
		if !(max && exactIn) {
			result.AmountIn = GetAmount0Delta(result.SqrtRatioNextX96, sqrtRatioCurrentX96, liquidity, true)
		}
		if !(max && !exactIn) {
			result.AmountOut = GetAmount1Delta(result.SqrtRatioNextX96, sqrtRatioCurrentX96, liquidity, false)
		}
	} else {
		if !(max && exactIn) {
			result.AmountIn = GetAmount1Delta(sqrtRatioCurrentX96, result.SqrtRatioNextX96, liquidity, true)
		}
		if !(max && !exactIn) {
			result.AmountOut = GetAmount0Delta(sqrtRatioCurrentX96, result.SqrtRatioNextX96, liquidity, false)
		}
	}

	if !exactIn && result.AmountOut.Cmp(new(big.Int).Neg(amountRemaining)) > 0 {
		result.AmountOut = new(big.Int).Neg(amountRemaining)
	}

	if exactIn && result.SqrtRatioNextX96.Cmp(sqrtRatioTargetX96) != 0 {
		result.FeeAmount = new(big.Int).Sub(amountRemaining, result.AmountIn)
	} else {
		result.FeeAmount = MulDivRoundingUp(result.AmountIn, feePipsBig, feeComplement)
	}

	return result, nil
}
