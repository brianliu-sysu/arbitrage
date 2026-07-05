package shared

import (
	"fmt"
	"math/big"
)

// DefaultSqrtPriceLimit returns the default sqrt price limit for a swap direction.
func DefaultSqrtPriceLimit(zeroForOne bool, sqrtPriceLimitX96 *big.Int) (*big.Int, error) {
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
