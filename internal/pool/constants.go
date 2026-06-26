// Package pool — Uniswap V3 常量。
package pool

import "math/big"

// MinSqrtRatio sqrtPriceX96 的下界（= 4295128739 + 1）。
var MinSqrtRatio = new(big.Int).Add(
	new(big.Int).SetUint64(4295128739),
	big.NewInt(1),
)

// MaxSqrtRatio sqrtPriceX96 的上界。
var MaxSqrtRatio = func() *big.Int {
	v := new(big.Int)
	v.SetString("1461446703485210103287273052203988822378723970342", 10)
	return v
}()
