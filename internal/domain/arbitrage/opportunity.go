package arbitrage

import (
	"math/big"
	"time"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// OpportunityStatus represents the lifecycle stage of a discovered opportunity.
type OpportunityStatus string

const (
	OpportunityStatusDiscovered OpportunityStatus = "discovered"
	OpportunityStatusAccepted   OpportunityStatus = "accepted"
	OpportunityStatusRejected   OpportunityStatus = "rejected"
	OpportunityStatusExecuted   OpportunityStatus = "executed"
)

// Opportunity is an arbitrage opportunity discovered by the scanner.
type Opportunity struct {
	ID          string
	StrategyID  string
	Status      OpportunityStatus
	PoolRef     PoolRef
	PoolAddress common.Address
	BlockNumber uint64
	Route       quoteunified.Route
	AmountIn    *big.Int
	AmountOut   *big.Int
	GrossProfit *big.Int
	GasCost     *big.Int
	FlashLoan   FlashLoanQuote
	NetProfit   *big.Int
	QuoteSteps  []OpportunityQuoteStep
	Payload     []byte
	CreatedAt   time.Time
}

type OpportunityQuoteStep struct {
	Index     int
	Version   string
	TokenIn   common.Address
	TokenOut  common.Address
	AmountIn  *big.Int
	AmountOut *big.Int
	FeeAmount *big.Int
}

// NewOpportunity builds an opportunity from evaluation output.
func NewOpportunity(
	id string,
	strategy Strategy,
	blockNumber uint64,
	route quoteunified.Route,
	evaluation EvaluationResult,
	gas GasEstimate,
	createdAt time.Time,
) *Opportunity {
	poolRef := PoolRef{}
	poolAddress := common.Address{}
	if len(route.Hops) > 0 {
		poolRef = PoolRefFromHop(route.Hops[0])
		poolAddress = poolRef.PrimaryAddress()
	}

	o := &Opportunity{
		ID:          id,
		StrategyID:  strategy.ID,
		Status:      OpportunityStatusDiscovered,
		PoolRef:     poolRef,
		PoolAddress: poolAddress,
		BlockNumber: blockNumber,
		Route:       route,
		AmountIn:    cloneBigInt(evaluation.AmountIn),
		AmountOut:   cloneBigInt(evaluation.AmountOut),
		GrossProfit: cloneBigInt(evaluation.GrossProfit),
		GasCost:     cloneBigInt(gas.CostWei),
		FlashLoan:   cloneFlashLoanQuote(evaluation.FlashLoan),
		NetProfit:   cloneBigInt(evaluation.NetProfit),
		QuoteSteps:  cloneQuoteSteps(evaluation.QuoteSteps),
		CreatedAt:   createdAt,
	}
	_ = o.EnsurePayload()
	return o
}

func cloneQuoteSteps(steps []OpportunityQuoteStep) []OpportunityQuoteStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]OpportunityQuoteStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, OpportunityQuoteStep{
			Index:     step.Index,
			Version:   step.Version,
			TokenIn:   step.TokenIn,
			TokenOut:  step.TokenOut,
			AmountIn:  cloneBigInt(step.AmountIn),
			AmountOut: cloneBigInt(step.AmountOut),
			FeeAmount: cloneBigInt(step.FeeAmount),
		})
	}
	return out
}

// IsProfitable reports whether net profit is positive.
func (o *Opportunity) IsProfitable() bool {
	return o != nil && o.NetProfit != nil && o.NetProfit.Sign() > 0
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}

func cloneFlashLoanQuote(v FlashLoanQuote) FlashLoanQuote {
	return FlashLoanQuote{
		Protocol:     v.Protocol,
		PoolRef:      v.PoolRef,
		Amount:       cloneBigInt(v.Amount),
		Fee:          cloneBigInt(v.Fee),
		FeePPM:       cloneBigInt(v.FeePPM),
		BorrowToken0: v.BorrowToken0,
	}
}
