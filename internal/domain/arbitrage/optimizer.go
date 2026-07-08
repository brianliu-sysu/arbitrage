package arbitrage

import (
	"fmt"
	"math/big"
)

// AmountOutQuoter quotes output amount for a given input amount.
type AmountOutQuoter interface {
	QuoteAmountOut(amountIn *big.Int) (*big.Int, error)
}

// OptimizationResult holds the best input amount found by the optimizer.
type OptimizationResult struct {
	AmountIn    *big.Int
	AmountOut   *big.Int
	GrossProfit *big.Int
}

// Optimizer searches for an input amount that maximizes gross profit.
type Optimizer struct {
	MinAmount  *big.Int
	MaxAmount  *big.Int
	Iterations int
}

func NewOptimizer(minAmount, maxAmount *big.Int, iterations int) *Optimizer {
	if iterations <= 0 {
		iterations = 16
	}
	return &Optimizer{
		MinAmount:  cloneBigInt(minAmount),
		MaxAmount:  cloneBigInt(maxAmount),
		Iterations: iterations,
	}
}

// Optimize performs a bounded grid search over the input range.
func (o *Optimizer) Optimize(quoter AmountOutQuoter) (OptimizationResult, error) {
	if quoter == nil {
		return OptimizationResult{}, fmt.Errorf("quoter is nil")
	}
	if o.MinAmount == nil || o.MaxAmount == nil {
		return OptimizationResult{}, fmt.Errorf("optimizer bounds are required")
	}
	if o.MinAmount.Sign() <= 0 || o.MaxAmount.Cmp(o.MinAmount) <= 0 {
		return OptimizationResult{}, fmt.Errorf("invalid optimizer bounds")
	}

	best := OptimizationResult{
		AmountIn:    big.NewInt(0),
		AmountOut:   big.NewInt(0),
		GrossProfit: big.NewInt(0),
	}

	rangeSize := new(big.Int).Sub(o.MaxAmount, o.MinAmount)
	step := new(big.Int).Div(rangeSize, big.NewInt(int64(o.Iterations)))
	if step.Sign() == 0 {
		step = big.NewInt(1)
	}

	current := new(big.Int).Set(o.MinAmount)
	var lastQuoteErr error
	for i := 0; i <= o.Iterations; i++ {
		if current.Cmp(o.MaxAmount) > 0 {
			current.Set(o.MaxAmount)
		}

		amountOut, err := quoter.QuoteAmountOut(new(big.Int).Set(current))
		if err != nil {
			lastQuoteErr = fmt.Errorf("quote amount %s: %w", current, err)
			current.Add(current, step)
			continue
		}

		grossProfit := new(big.Int).Sub(amountOut, current)
		if grossProfit.Cmp(best.GrossProfit) > 0 {
			best = OptimizationResult{
				AmountIn:    new(big.Int).Set(current),
				AmountOut:   new(big.Int).Set(amountOut),
				GrossProfit: grossProfit,
			}
		}

		current.Add(current, step)
	}
	if best.AmountIn.Sign() == 0 && lastQuoteErr != nil {
		return OptimizationResult{}, lastQuoteErr
	}

	return best, nil
}
