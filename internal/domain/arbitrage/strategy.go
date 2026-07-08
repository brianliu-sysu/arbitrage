package arbitrage

import (
	"fmt"
	"math/big"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

// StrategyKind identifies the arbitrage search pattern.
type StrategyKind string

const (
	StrategyKindCycle    StrategyKind = "cycle"
	StrategyKindTriangle StrategyKind = "triangle"
	StrategyKindSpread   StrategyKind = "spread"
)

const triangleHopCount = 3

// Strategy defines how opportunities are discovered and filtered.
type Strategy struct {
	ID              string
	Kind            StrategyKind
	StartToken      common.Address
	MaxHops         int
	MinNetProfitWei *big.Int
}

func NewCycleStrategy(id string, startToken common.Address, maxHops int, minNetProfitWei *big.Int) Strategy {
	if maxHops <= 0 {
		maxHops = 3
	}
	return Strategy{
		ID:              id,
		Kind:            StrategyKindCycle,
		StartToken:      startToken,
		MaxHops:         maxHops,
		MinNetProfitWei: cloneBigInt(minNetProfitWei),
	}
}

// NewTriangleStrategy builds a three-hop triangular arbitrage strategy: A->B->C->A.
func NewTriangleStrategy(id string, startToken common.Address, minNetProfitWei *big.Int) Strategy {
	return Strategy{
		ID:              id,
		Kind:            StrategyKindTriangle,
		StartToken:      startToken,
		MaxHops:         triangleHopCount,
		MinNetProfitWei: cloneBigInt(minNetProfitWei),
	}
}

// NewSpreadStrategy builds a two-hop cross-pool spread strategy: A->B->A across distinct pools.
func NewSpreadStrategy(id string, startToken common.Address, minNetProfitWei *big.Int) Strategy {
	return Strategy{
		ID:              id,
		Kind:            StrategyKindSpread,
		StartToken:      startToken,
		MaxHops:         spreadHopCount,
		MinNetProfitWei: cloneBigInt(minNetProfitWei),
	}
}

func (s Strategy) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("strategy id is required")
	}
	if s.StartToken == (common.Address{}) {
		return fmt.Errorf("strategy start token is required")
	}
	switch s.Kind {
	case StrategyKindCycle:
		if s.MaxHops <= 0 {
			return fmt.Errorf("strategy max hops must be positive")
		}
	case StrategyKindTriangle:
		if s.MaxHops != triangleHopCount {
			return fmt.Errorf("triangle strategy requires exactly %d hops", triangleHopCount)
		}
	case StrategyKindSpread:
		if s.MaxHops != spreadHopCount {
			return fmt.Errorf("spread strategy requires exactly %d hops", spreadHopCount)
		}
	default:
		return fmt.Errorf("unsupported strategy kind %q", s.Kind)
	}
	return nil
}

// MatchesStrategy reports whether a route satisfies the strategy constraints.
func MatchesStrategy(strategy Strategy, route quoteunified.Route) bool {
	return MatchesUnifiedStrategy(strategy, route)
}

// MeetsMinimumProfit reports whether net profit satisfies the strategy threshold.
func (s Strategy) MeetsMinimumProfit(netProfit *big.Int) bool {
	if netProfit == nil || netProfit.Sign() <= 0 {
		return false
	}
	if s.MinNetProfitWei == nil || s.MinNetProfitWei.Sign() <= 0 {
		return true
	}
	return netProfit.Cmp(s.MinNetProfitWei) >= 0
}
