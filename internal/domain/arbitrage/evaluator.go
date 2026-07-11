package arbitrage

import (
	"math/big"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

// EvaluationInput contains the data required to evaluate a candidate route.
type EvaluationInput struct {
	Strategy    Strategy
	BlockNumber uint64
	Route       quoteunified.Route
	AmountIn    *big.Int
	AmountOut   *big.Int
	GasCost     *big.Int
	FlashLoan   FlashLoanQuote
	QuoteSteps  []OpportunityQuoteStep
}

// EvaluationResult is the profit outcome of a route evaluation.
type EvaluationResult struct {
	AmountIn    *big.Int
	AmountOut   *big.Int
	GrossProfit *big.Int
	NetProfit   *big.Int
	FlashLoan   FlashLoanQuote
	QuoteSteps  []OpportunityQuoteStep
	Profitable  bool
	Accepted    bool
}

// Evaluator computes gross and net profit and applies strategy filters.
type Evaluator struct{}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// Evaluate calculates profit metrics for a candidate route.
func (e *Evaluator) Evaluate(input EvaluationInput) EvaluationResult {
	amountIn := cloneBigInt(input.AmountIn)
	amountOut := cloneBigInt(input.AmountOut)
	gasCost := cloneBigInt(input.GasCost)
	flashLoanFee := cloneBigInt(input.FlashLoan.Fee)

	grossProfit := new(big.Int).Sub(amountOut, amountIn)
	netProfit := new(big.Int).Sub(grossProfit, gasCost)
	netProfit.Sub(netProfit, flashLoanFee)

	profitable := grossProfit.Sign() > 0 && netProfit.Sign() > 0
	accepted := profitable && input.Strategy.MeetsMinimumProfit(netProfit)

	return EvaluationResult{
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		GrossProfit: grossProfit,
		NetProfit:   netProfit,
		FlashLoan:   input.FlashLoan,
		QuoteSteps:  cloneQuoteSteps(input.QuoteSteps),
		Profitable:  profitable,
		Accepted:    accepted,
	}
}
