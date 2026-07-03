package quote

import "math/big"

// MulDiv computes floor(a * b / denominator).
func MulDiv(a, b, denominator *big.Int) *big.Int {
	return new(big.Int).Div(new(big.Int).Mul(a, b), denominator)
}

// MulDivRoundingUp computes ceil(a * b / denominator).
func MulDivRoundingUp(a, b, denominator *big.Int) *big.Int {
	product := new(big.Int).Mul(a, b)
	result := new(big.Int).Div(product, denominator)
	if new(big.Int).Rem(product, denominator).Sign() != 0 {
		result.Add(result, big.NewInt(1))
	}
	return result
}
