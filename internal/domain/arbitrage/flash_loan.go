package arbitrage

import (
	"fmt"
	"math/big"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
)

// FlashLoanProtocol identifies a flash-loan source considered for execution.
type FlashLoanProtocol string

const (
	FlashLoanProtocolBalancer FlashLoanProtocol = "balancer"
	FlashLoanProtocolUniv3    FlashLoanProtocol = "univ3"
	FlashLoanProtocolUniv4    FlashLoanProtocol = "univ4"
)

var flashLoanFeeDenominator = big.NewInt(1_000_000)

// FlashLoanOption configures the fee charged by one flash-loan source in ppm.
type FlashLoanOption struct {
	Protocol     FlashLoanProtocol
	PoolRef      PoolRef
	FeePPM       *big.Int
	BorrowToken0 bool // univ3 only: true when route.TokenIn is pool token0
}

// FlashLoanQuote is the selected flash-loan source and fee for a borrow amount.
type FlashLoanQuote struct {
	Protocol     FlashLoanProtocol
	PoolRef      PoolRef
	Amount       *big.Int
	Fee          *big.Int
	FeePPM       *big.Int
	BorrowToken0 bool // univ3 only
}

// DefaultFlashLoanOptions compares protocol-level flash-loan sources.
// Uniswap V3 options are pool-specific and should be added from route pool state.
func DefaultFlashLoanOptions() []FlashLoanOption {
	return []FlashLoanOption{
		{Protocol: FlashLoanProtocolBalancer, FeePPM: big.NewInt(0)},
		{Protocol: FlashLoanProtocolUniv4, FeePPM: big.NewInt(0)},
	}
}

// FlashLoanOptionsForRoute adds route-local Uniswap V3 flash-loan candidates.
func FlashLoanOptionsForRoute(route quoteunified.Route, pools quoteunified.RoutePools, base []FlashLoanOption) []FlashLoanOption {
	options := append([]FlashLoanOption(nil), base...)
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		if key := option.PoolRef.Key(); key != "" {
			seen[key] = struct{}{}
		}
	}

	for _, hop := range route.Hops {
		if hop.Version != quoteunified.PoolVersionV3 {
			continue
		}
		pool := pools.V3[hop.PoolV3]
		if pool == nil {
			continue
		}
		if pool.Token0 != route.TokenIn && pool.Token1 != route.TokenIn {
			continue
		}
		poolRef := PoolRefFromV3(pool.Address)
		if _, ok := seen[poolRef.Key()]; ok {
			continue
		}
		seen[poolRef.Key()] = struct{}{}
		options = append(options, FlashLoanOption{
			Protocol:     FlashLoanProtocolUniv3,
			PoolRef:      poolRef,
			FeePPM:       new(big.Int).SetUint64(uint64(pool.Fee)),
			BorrowToken0: pool.Token0 == route.TokenIn,
		})
	}
	return options
}

// SelectBestFlashLoan returns the lowest-fee flash-loan option for amount.
func SelectBestFlashLoan(amount *big.Int, options []FlashLoanOption) (FlashLoanQuote, error) {
	if amount == nil || amount.Sign() <= 0 {
		return FlashLoanQuote{}, fmt.Errorf("flash loan amount must be positive")
	}
	if len(options) == 0 {
		options = DefaultFlashLoanOptions()
	}

	var best FlashLoanQuote
	for _, option := range options {
		quote, err := option.Quote(amount)
		if err != nil {
			return FlashLoanQuote{}, err
		}
		if best.Protocol == "" || quote.Fee.Cmp(best.Fee) < 0 {
			best = quote
		}
	}
	if best.Protocol == "" {
		return FlashLoanQuote{}, fmt.Errorf("no flash loan options configured")
	}
	return best, nil
}

// Quote calculates the fee for this flash-loan option.
func (o FlashLoanOption) Quote(amount *big.Int) (FlashLoanQuote, error) {
	if o.Protocol == "" {
		return FlashLoanQuote{}, fmt.Errorf("flash loan protocol is required")
	}
	if amount == nil || amount.Sign() <= 0 {
		return FlashLoanQuote{}, fmt.Errorf("flash loan amount must be positive")
	}
	feePPM := cloneBigInt(o.FeePPM)
	if feePPM.Sign() < 0 {
		return FlashLoanQuote{}, fmt.Errorf("flash loan fee ppm must be non-negative")
	}
	fee := divUpBig(new(big.Int).Mul(amount, feePPM), flashLoanFeeDenominator)
	return FlashLoanQuote{
		Protocol:     o.Protocol,
		PoolRef:      o.PoolRef,
		Amount:       cloneBigInt(amount),
		Fee:          fee,
		FeePPM:       feePPM,
		BorrowToken0: o.BorrowToken0,
	}, nil
}

func divUpBig(numerator, denominator *big.Int) *big.Int {
	result := new(big.Int).Div(numerator, denominator)
	if new(big.Int).Mod(numerator, denominator).Sign() > 0 {
		result.Add(result, big.NewInt(1))
	}
	return result
}
