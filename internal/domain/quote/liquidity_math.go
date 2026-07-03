package quote

import (
	"fmt"
	"math/big"
)

// AddDelta applies a signed liquidity delta and returns the updated liquidity.
func AddDelta(liquidity, delta *big.Int) (*big.Int, error) {
	if delta.Sign() < 0 {
		result := new(big.Int).Sub(liquidity, new(big.Int).Neg(delta))
		if result.Sign() < 0 {
			return nil, fmt.Errorf("liquidity underflow")
		}
		return result, nil
	}
	return new(big.Int).Add(liquidity, delta), nil
}
