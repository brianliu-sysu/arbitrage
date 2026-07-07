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
	NetProfit   *big.Int
	Payload     []byte
	CreatedAt   time.Time
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
		NetProfit:   cloneBigInt(evaluation.NetProfit),
		CreatedAt:   createdAt,
	}
	_ = o.EnsurePayload()
	return o
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
