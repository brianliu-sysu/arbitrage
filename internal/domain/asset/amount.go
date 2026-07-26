package asset

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseDecimalAmount converts a human-readable token amount to its smallest unit.
func ParseDecimalAmount(value string, decimals uint8) (*big.Int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("amount is required")
	}
	rat, ok := new(big.Rat).SetString(value)
	if !ok || rat.Sign() < 0 {
		return nil, fmt.Errorf("invalid decimal amount %q", value)
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	scaled := new(big.Rat).Mul(rat, new(big.Rat).SetInt(scale))
	if !scaled.IsInt() {
		return nil, fmt.Errorf("amount %q exceeds token precision %d", value, decimals)
	}
	return new(big.Int).Set(scaled.Num()), nil
}
