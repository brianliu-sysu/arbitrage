package quote

import (
	"errors"
	"math/big"
)

var (
	ErrSqrtPriceLessThanZero = errors.New("sqrt price must be positive")
	ErrLiquidityLessThanZero = errors.New("liquidity must be positive")
	ErrSqrtPriceInvariant    = errors.New("sqrt price math invariant violated")
)

var (
	q96       = new(big.Int).Lsh(big.NewInt(1), 96)
	maxUint160 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))
	maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

func multiplyIn256(x, y *big.Int) *big.Int {
	return new(big.Int).And(new(big.Int).Mul(x, y), maxUint256)
}

func addIn256(x, y *big.Int) *big.Int {
	return new(big.Int).And(new(big.Int).Add(x, y), maxUint256)
}

// GetAmount0Delta returns the token0 delta between two sqrt prices.
func GetAmount0Delta(sqrtRatioAX96, sqrtRatioBX96, liquidity *big.Int, roundUp bool) *big.Int {
	if sqrtRatioAX96.Cmp(sqrtRatioBX96) >= 0 {
		sqrtRatioAX96, sqrtRatioBX96 = sqrtRatioBX96, sqrtRatioAX96
	}

	numerator1 := new(big.Int).Lsh(liquidity, 96)
	numerator2 := new(big.Int).Sub(sqrtRatioBX96, sqrtRatioAX96)

	if roundUp {
		return MulDivRoundingUp(MulDivRoundingUp(numerator1, numerator2, sqrtRatioBX96), big.NewInt(1), sqrtRatioAX96)
	}
	return new(big.Int).Div(new(big.Int).Div(new(big.Int).Mul(numerator1, numerator2), sqrtRatioBX96), sqrtRatioAX96)
}

// GetAmount1Delta returns the token1 delta between two sqrt prices.
func GetAmount1Delta(sqrtRatioAX96, sqrtRatioBX96, liquidity *big.Int, roundUp bool) *big.Int {
	if sqrtRatioAX96.Cmp(sqrtRatioBX96) >= 0 {
		sqrtRatioAX96, sqrtRatioBX96 = sqrtRatioBX96, sqrtRatioAX96
	}

	delta := new(big.Int).Sub(sqrtRatioBX96, sqrtRatioAX96)
	if roundUp {
		return MulDivRoundingUp(liquidity, delta, q96)
	}
	return new(big.Int).Div(new(big.Int).Mul(liquidity, delta), q96)
}

// GetNextSqrtPriceFromInput returns the next sqrt price after consuming amountIn.
func GetNextSqrtPriceFromInput(sqrtPX96, liquidity, amountIn *big.Int, zeroForOne bool) (*big.Int, error) {
	if sqrtPX96.Sign() <= 0 {
		return nil, ErrSqrtPriceLessThanZero
	}
	if liquidity.Sign() <= 0 {
		return nil, ErrLiquidityLessThanZero
	}
	if zeroForOne {
		return getNextSqrtPriceFromAmount0RoundingUp(sqrtPX96, liquidity, amountIn, true)
	}
	return getNextSqrtPriceFromAmount1RoundingDown(sqrtPX96, liquidity, amountIn, true)
}

// GetNextSqrtPriceFromOutput returns the next sqrt price after producing amountOut.
func GetNextSqrtPriceFromOutput(sqrtPX96, liquidity, amountOut *big.Int, zeroForOne bool) (*big.Int, error) {
	if sqrtPX96.Sign() <= 0 {
		return nil, ErrSqrtPriceLessThanZero
	}
	if liquidity.Sign() <= 0 {
		return nil, ErrLiquidityLessThanZero
	}
	if zeroForOne {
		return getNextSqrtPriceFromAmount1RoundingDown(sqrtPX96, liquidity, amountOut, false)
	}
	return getNextSqrtPriceFromAmount0RoundingUp(sqrtPX96, liquidity, amountOut, false)
}

func getNextSqrtPriceFromAmount0RoundingUp(sqrtPX96, liquidity, amount *big.Int, add bool) (*big.Int, error) {
	if amount.Sign() == 0 {
		return new(big.Int).Set(sqrtPX96), nil
	}

	numerator1 := new(big.Int).Lsh(liquidity, 96)
	if add {
		product := multiplyIn256(amount, sqrtPX96)
		if new(big.Int).Div(product, amount).Cmp(sqrtPX96) == 0 {
			denominator := addIn256(numerator1, product)
			if denominator.Cmp(numerator1) >= 0 {
				return MulDivRoundingUp(numerator1, sqrtPX96, denominator), nil
			}
		}
		return MulDivRoundingUp(numerator1, big.NewInt(1), new(big.Int).Add(new(big.Int).Div(numerator1, sqrtPX96), amount)), nil
	}

	product := multiplyIn256(amount, sqrtPX96)
	if new(big.Int).Div(product, amount).Cmp(sqrtPX96) != 0 {
		return nil, ErrSqrtPriceInvariant
	}
	if numerator1.Cmp(product) <= 0 {
		return nil, ErrSqrtPriceInvariant
	}
	return MulDivRoundingUp(numerator1, sqrtPX96, new(big.Int).Sub(numerator1, product)), nil
}

func getNextSqrtPriceFromAmount1RoundingDown(sqrtPX96, liquidity, amount *big.Int, add bool) (*big.Int, error) {
	if add {
		var quotient *big.Int
		if amount.Cmp(maxUint160) <= 0 {
			quotient = new(big.Int).Div(new(big.Int).Lsh(amount, 96), liquidity)
		} else {
			quotient = new(big.Int).Div(new(big.Int).Mul(amount, q96), liquidity)
		}
		return new(big.Int).Add(sqrtPX96, quotient), nil
	}

	quotient := MulDivRoundingUp(amount, q96, liquidity)
	if sqrtPX96.Cmp(quotient) <= 0 {
		return nil, ErrSqrtPriceInvariant
	}
	return new(big.Int).Sub(sqrtPX96, quotient), nil
}
