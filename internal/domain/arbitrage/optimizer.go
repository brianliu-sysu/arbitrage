package arbitrage

import (
	"context"
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
		iterations = 8
	}
	return &Optimizer{
		MinAmount:  cloneBigInt(minAmount),
		MaxAmount:  cloneBigInt(maxAmount),
		Iterations: iterations,
	}
}

// Optimize performs a bounded grid search over the input range.
func (o *Optimizer) Optimize(quoter AmountOutQuoter) (OptimizationResult, error) {
	return o.OptimizeContext(context.Background(), quoter)
}

// OptimizeContext performs a bounded grid search and stops when ctx is canceled.
func (o *Optimizer) OptimizeContext(ctx context.Context, quoter AmountOutQuoter) (OptimizationResult, error) {
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
		if err := ctx.Err(); err != nil {
			return OptimizationResult{}, err
		}
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

// ProbePositiveGrossProfit quotes min/mid/max amounts and reports whether any sample
// yields a positive gross profit. Callers use this as a cheap pre-screen before Optimize.
func (o *Optimizer) ProbePositiveGrossProfit(ctx context.Context, quoter AmountOutQuoter) (bool, error) {
	if quoter == nil {
		return false, fmt.Errorf("quoter is nil")
	}
	if o.MinAmount == nil || o.MaxAmount == nil {
		return false, fmt.Errorf("optimizer bounds are required")
	}
	if o.MinAmount.Sign() <= 0 || o.MaxAmount.Cmp(o.MinAmount) <= 0 {
		return false, fmt.Errorf("invalid optimizer bounds")
	}

	mid := new(big.Int).Add(o.MinAmount, o.MaxAmount)
	mid.Div(mid, big.NewInt(2))
	amounts := []*big.Int{
		new(big.Int).Set(o.MinAmount),
		mid,
		new(big.Int).Set(o.MaxAmount),
	}

	var lastQuoteErr error
	quoted := false
	for _, amount := range amounts {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		amountOut, err := quoter.QuoteAmountOut(new(big.Int).Set(amount))
		if err != nil {
			lastQuoteErr = err
			continue
		}
		quoted = true
		if amountOut != nil && amountOut.Cmp(amount) > 0 {
			return true, nil
		}
	}
	if !quoted && lastQuoteErr != nil {
		return false, fmt.Errorf("probe quote: %w", lastQuoteErr)
	}
	return false, nil
}
