package poolsapp

import (
	"math/big"
	"strings"
)

// PriceInfo is a human-readable mid price derived from sqrtPriceX96.
type PriceInfo struct {
	Token1PerToken0 string `json:"token1PerToken0"`
	Token0PerToken1 string `json:"token0PerToken1"`
}

func impliedPrice(sqrtPriceX96 *big.Int, decimals0, decimals1 uint8) PriceInfo {
	if sqrtPriceX96 == nil || sqrtPriceX96.Sign() <= 0 {
		return PriceInfo{}
	}

	q96 := new(big.Int).Lsh(big.NewInt(1), 96)
	sqrtRat := new(big.Rat).SetFrac(new(big.Int).Set(sqrtPriceX96), q96)
	priceRat := new(big.Rat).Mul(sqrtRat, sqrtRat)

	decimalShift := int(decimals0) - int(decimals1)
	if decimalShift > 0 {
		priceRat.Mul(priceRat, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalShift)), nil)))
	} else if decimalShift < 0 {
		priceRat.Quo(priceRat, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-decimalShift)), nil)))
	}

	token1PerToken0 := formatPriceRat(priceRat)
	token0PerToken0 := new(big.Rat)
	if priceRat.Sign() > 0 {
		token0PerToken0.Inv(priceRat)
	}
	return PriceInfo{
		Token1PerToken0: token1PerToken0,
		Token0PerToken1: formatPriceRat(token0PerToken0),
	}
}

func formatPriceRat(value *big.Rat) string {
	if value == nil || value.Sign() == 0 {
		return "0"
	}
	f, _ := value.Float64()
	text := big.NewFloat(f).Text('f', 12)
	return strings.TrimRight(strings.TrimRight(text, "0"), ".")
}
