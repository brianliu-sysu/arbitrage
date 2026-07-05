package shared

import "math/big"

// QuoteResult is the outcome of quoting a pool or route.
type QuoteResult struct {
	AmountIn     *big.Int
	AmountOut    *big.Int
	FeeAmount    *big.Int
	SqrtPriceX96 *big.Int
	Tick         int32
}

// NewQuoteResult creates a quote result with cloned big.Int values.
func NewQuoteResult(amountIn, amountOut, feeAmount, sqrtPriceX96 *big.Int, tick int32) QuoteResult {
	return QuoteResult{
		AmountIn:     cloneBigInt(amountIn),
		AmountOut:    cloneBigInt(amountOut),
		FeeAmount:    cloneBigInt(feeAmount),
		SqrtPriceX96: cloneBigInt(sqrtPriceX96),
		Tick:         tick,
	}
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
