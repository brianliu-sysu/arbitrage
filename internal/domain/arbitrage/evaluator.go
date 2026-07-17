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
	// GasCost is denominated in Route.TokenIn, not wei unless the route starts with native ETH or wrapped native ETH.
	GasCost               *big.Int
	CoinbasePaymentBPS    uint16
	SettlementSlippageBPS uint16
	FlashLoan             FlashLoanQuote
	QuoteSteps            []OpportunityQuoteStep
}

// EvaluationResult is the profit outcome of a route evaluation.
type EvaluationResult struct {
	AmountIn        *big.Int
	AmountOut       *big.Int
	GrossProfit     *big.Int
	NetProfit       *big.Int
	CoinbasePayment *big.Int
	FlashLoan       FlashLoanQuote
	QuoteSteps      []OpportunityQuoteStep
	Profitable      bool
	Accepted        bool
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
	profitAfterFlashLoan := new(big.Int).Sub(grossProfit, flashLoanFee)
	profitAfterSlippage := cloneBigInt(profitAfterFlashLoan)
	settlementSlippageBPS := input.SettlementSlippageBPS
	if settlementSlippageBPS > 10_000 {
		settlementSlippageBPS = 10_000
	}
	if profitAfterSlippage.Sign() > 0 && settlementSlippageBPS > 0 {
		profitAfterSlippage.Mul(profitAfterSlippage, new(big.Int).SetUint64(uint64(10_000-settlementSlippageBPS)))
		profitAfterSlippage.Div(profitAfterSlippage, big.NewInt(10_000))
	}
	coinbasePayment := new(big.Int)
	if profitAfterSlippage.Sign() > 0 && input.CoinbasePaymentBPS > 0 {
		coinbasePayment.Mul(profitAfterSlippage, new(big.Int).SetUint64(uint64(input.CoinbasePaymentBPS)))
		coinbasePayment.Div(coinbasePayment, big.NewInt(10_000))
	}
	netProfit := new(big.Int).Sub(profitAfterSlippage, coinbasePayment)
	netProfit.Sub(netProfit, gasCost)

	profitable := grossProfit.Sign() > 0 && netProfit.Sign() > 0
	accepted := profitable && input.Strategy.MeetsMinimumProfit(netProfit)

	return EvaluationResult{
		AmountIn:        amountIn,
		AmountOut:       amountOut,
		GrossProfit:     grossProfit,
		NetProfit:       netProfit,
		CoinbasePayment: coinbasePayment,
		FlashLoan:       input.FlashLoan,
		QuoteSteps:      cloneQuoteSteps(input.QuoteSteps),
		Profitable:      profitable,
		Accepted:        accepted,
	}
}
