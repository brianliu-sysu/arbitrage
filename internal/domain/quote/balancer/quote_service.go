package balancer

import (
	"fmt"
	"math"
	"math/big"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	"github.com/ethereum/go-ethereum/common"
)

var fixedOne = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// QuoteService quotes swaps against Balancer weighted and stable pool state.
type QuoteService struct{}

func NewQuoteService() *QuoteService {
	return &QuoteService{}
}

func (s *QuoteService) QuoteExactInput(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	if err := validateQuoteInput(pool, tokenIn, tokenOut, amountIn); err != nil {
		return quoteshared.QuoteResult{}, err
	}

	amountAfterFee, feeAmount, err := subtractSwapFee(amountIn, pool.SwapFeePercentage)
	if err != nil {
		return quoteshared.QuoteResult{}, err
	}

	var amountOut *big.Int
	switch pool.Type {
	case marketbalancer.PoolTypeWeighted:
		amountOut, err = weightedOutGivenIn(pool, tokenIn, tokenOut, amountAfterFee)
	case marketbalancer.PoolTypeStable:
		amountOut, err = stableOutGivenIn(pool, tokenIn, tokenOut, amountAfterFee)
	default:
		err = fmt.Errorf("unsupported balancer pool type %q", pool.Type)
	}
	if err != nil {
		return quoteshared.QuoteResult{}, err
	}
	return quoteshared.NewQuoteResult(amountIn, amountOut, feeAmount, nil, 0), nil
}

func (s *QuoteService) QuoteExactOutput(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (quoteshared.QuoteResult, error) {
	if err := validateQuoteInput(pool, tokenIn, tokenOut, amountOut); err != nil {
		return quoteshared.QuoteResult{}, err
	}

	var amountAfterFee *big.Int
	var err error
	switch pool.Type {
	case marketbalancer.PoolTypeWeighted:
		amountAfterFee, err = weightedInGivenOut(pool, tokenIn, tokenOut, amountOut)
	case marketbalancer.PoolTypeStable:
		amountAfterFee, err = stableInGivenOut(pool, tokenIn, tokenOut, amountOut)
	default:
		err = fmt.Errorf("unsupported balancer pool type %q", pool.Type)
	}
	if err != nil {
		return quoteshared.QuoteResult{}, err
	}

	amountIn, feeAmount, err := addSwapFee(amountAfterFee, pool.SwapFeePercentage)
	if err != nil {
		return quoteshared.QuoteResult{}, err
	}
	return quoteshared.NewQuoteResult(amountIn, amountOut, feeAmount, nil, 0), nil
}

func validateQuoteInput(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amount *big.Int) error {
	if pool == nil {
		return fmt.Errorf("pool is nil")
	}
	if pool.Paused {
		return fmt.Errorf("pool is paused")
	}
	if tokenIn == tokenOut {
		return fmt.Errorf("tokenIn and tokenOut must differ")
	}
	if amount == nil || amount.Sign() <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	if _, ok := pool.Balances[tokenIn]; !ok {
		return fmt.Errorf("tokenIn %s is not part of pool %s", tokenIn.Hex(), pool.ID)
	}
	if _, ok := pool.Balances[tokenOut]; !ok {
		return fmt.Errorf("tokenOut %s is not part of pool %s", tokenOut.Hex(), pool.ID)
	}
	return nil
}

func subtractSwapFee(amountIn, swapFee *big.Int) (*big.Int, *big.Int, error) {
	fee, err := validateSwapFee(swapFee)
	if err != nil {
		return nil, nil, err
	}
	feeAmount := new(big.Int).Div(new(big.Int).Mul(amountIn, fee), fixedOne)
	amountAfterFee := new(big.Int).Sub(amountIn, feeAmount)
	if amountAfterFee.Sign() <= 0 {
		return nil, nil, fmt.Errorf("amount after fee must be positive")
	}
	return amountAfterFee, feeAmount, nil
}

func addSwapFee(amountAfterFee, swapFee *big.Int) (*big.Int, *big.Int, error) {
	fee, err := validateSwapFee(swapFee)
	if err != nil {
		return nil, nil, err
	}
	denominator := new(big.Int).Sub(fixedOne, fee)
	if denominator.Sign() <= 0 {
		return nil, nil, fmt.Errorf("swap fee must be less than 1e18")
	}
	amountIn := divUp(new(big.Int).Mul(amountAfterFee, fixedOne), denominator)
	feeAmount := new(big.Int).Sub(amountIn, amountAfterFee)
	return amountIn, feeAmount, nil
}

func validateSwapFee(swapFee *big.Int) (*big.Int, error) {
	if swapFee == nil {
		return big.NewInt(0), nil
	}
	if swapFee.Sign() < 0 || swapFee.Cmp(fixedOne) >= 0 {
		return nil, fmt.Errorf("swap fee must be in [0, 1e18)")
	}
	return new(big.Int).Set(swapFee), nil
}

func weightedOutGivenIn(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (*big.Int, error) {
	balanceIn := pool.Balances[tokenIn]
	balanceOut := pool.Balances[tokenOut]
	weightIn := pool.Weights[tokenIn]
	weightOut := pool.Weights[tokenOut]
	if balanceIn.Sign() <= 0 || balanceOut.Sign() <= 0 || weightIn == nil || weightOut == nil || weightIn.Sign() <= 0 || weightOut.Sign() <= 0 {
		return nil, fmt.Errorf("weighted pool has invalid balances or weights")
	}

	bIn := bigToFloat(balanceIn)
	bOut := bigToFloat(balanceOut)
	aIn := bigToFloat(amountIn)
	wIn := bigToFloat(weightIn)
	wOut := bigToFloat(weightOut)

	ratio := bIn / (bIn + aIn)
	power := math.Pow(ratio, wIn/wOut)
	out := bOut * (1 - power)
	if out <= 0 || math.IsNaN(out) || math.IsInf(out, 0) {
		return nil, fmt.Errorf("weighted quote produced invalid amountOut")
	}
	amountOut := floatToBigFloor(out)
	if amountOut.Sign() <= 0 || amountOut.Cmp(balanceOut) >= 0 {
		return nil, fmt.Errorf("insufficient weighted pool liquidity")
	}
	return amountOut, nil
}

func weightedInGivenOut(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (*big.Int, error) {
	balanceIn := pool.Balances[tokenIn]
	balanceOut := pool.Balances[tokenOut]
	weightIn := pool.Weights[tokenIn]
	weightOut := pool.Weights[tokenOut]
	if balanceIn.Sign() <= 0 || balanceOut.Sign() <= 0 || weightIn == nil || weightOut == nil || weightIn.Sign() <= 0 || weightOut.Sign() <= 0 {
		return nil, fmt.Errorf("weighted pool has invalid balances or weights")
	}
	if amountOut.Cmp(balanceOut) >= 0 {
		return nil, fmt.Errorf("insufficient weighted pool liquidity")
	}

	bIn := bigToFloat(balanceIn)
	bOut := bigToFloat(balanceOut)
	aOut := bigToFloat(amountOut)
	wIn := bigToFloat(weightIn)
	wOut := bigToFloat(weightOut)

	ratio := bOut / (bOut - aOut)
	power := math.Pow(ratio, wOut/wIn)
	in := bIn * (power - 1)
	if in <= 0 || math.IsNaN(in) || math.IsInf(in, 0) {
		return nil, fmt.Errorf("weighted quote produced invalid amountIn")
	}
	return floatToBigCeil(in), nil
}

func stableOutGivenIn(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountIn *big.Int) (*big.Int, error) {
	balances, indexIn, indexOut, err := stableBalances(pool, tokenIn, tokenOut)
	if err != nil {
		return nil, err
	}
	balanceOut := pool.Balances[tokenOut]
	if amountIn.Sign() <= 0 || balanceOut == nil || balanceOut.Sign() <= 0 {
		return nil, fmt.Errorf("stable pool has invalid balances")
	}

	invariant, err := stableInvariant(balances, pool.Amplification)
	if err != nil {
		return nil, err
	}
	balances[indexIn] += bigToFloat(amountIn)
	y, err := stableTokenBalanceGivenInvariant(balances, pool.Amplification, invariant, indexOut)
	if err != nil {
		return nil, err
	}
	out := bigToFloat(balanceOut) - y - 1
	if out <= 0 || math.IsNaN(out) || math.IsInf(out, 0) {
		return nil, fmt.Errorf("stable quote produced invalid amountOut")
	}
	amountOut := floatToBigFloor(out)
	if amountOut.Sign() <= 0 || amountOut.Cmp(balanceOut) >= 0 {
		return nil, fmt.Errorf("insufficient stable pool liquidity")
	}
	return amountOut, nil
}

func stableInGivenOut(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address, amountOut *big.Int) (*big.Int, error) {
	balanceOut := pool.Balances[tokenOut]
	if balanceOut == nil || amountOut.Cmp(balanceOut) >= 0 {
		return nil, fmt.Errorf("insufficient stable pool liquidity")
	}

	low := big.NewInt(1)
	high := new(big.Int).Set(amountOut)
	for i := 0; i < 128; i++ {
		quoted, err := stableOutGivenIn(pool, tokenIn, tokenOut, high)
		if err == nil && quoted.Cmp(amountOut) >= 0 {
			break
		}
		high.Mul(high, big.NewInt(2))
		if high.Cmp(pool.Balances[tokenIn]) > 0 {
			high = new(big.Int).Set(pool.Balances[tokenIn])
			break
		}
	}

	for low.Cmp(high) < 0 {
		mid := new(big.Int).Add(low, high)
		mid.Div(mid, big.NewInt(2))
		quoted, err := stableOutGivenIn(pool, tokenIn, tokenOut, mid)
		if err != nil || quoted.Cmp(amountOut) < 0 {
			low = new(big.Int).Add(mid, big.NewInt(1))
			continue
		}
		high = mid
	}
	quoted, err := stableOutGivenIn(pool, tokenIn, tokenOut, low)
	if err != nil || quoted.Cmp(amountOut) < 0 {
		return nil, fmt.Errorf("insufficient stable pool liquidity")
	}
	return low, nil
}

func stableBalances(pool *marketbalancer.Pool, tokenIn, tokenOut common.Address) ([]float64, int, int, error) {
	if pool.Amplification == nil || pool.Amplification.Sign() <= 0 {
		return nil, 0, 0, fmt.Errorf("stable pool has invalid amplification")
	}
	if len(pool.Tokens) < 2 {
		return nil, 0, 0, fmt.Errorf("stable pool must have at least two tokens")
	}
	balances := make([]float64, len(pool.Tokens))
	indexIn, indexOut := -1, -1
	for i, token := range pool.Tokens {
		balance := pool.Balances[token]
		if balance == nil || balance.Sign() <= 0 {
			return nil, 0, 0, fmt.Errorf("stable pool has invalid token balance")
		}
		balances[i] = bigToFloat(balance)
		if token == tokenIn {
			indexIn = i
		}
		if token == tokenOut {
			indexOut = i
		}
	}
	if indexIn < 0 || indexOut < 0 {
		return nil, 0, 0, fmt.Errorf("stable pool token indexes not found")
	}
	return balances, indexIn, indexOut, nil
}

func stableInvariant(balances []float64, amplification *big.Int) (float64, error) {
	sum := 0.0
	for _, balance := range balances {
		sum += balance
	}
	if sum <= 0 {
		return 0, fmt.Errorf("stable pool balances sum to zero")
	}
	n := float64(len(balances))
	ann := bigToFloat(amplification) * n
	if ann <= 0 {
		return 0, fmt.Errorf("stable pool amplification is invalid")
	}

	d := sum
	for i := 0; i < 255; i++ {
		dP := d
		for _, balance := range balances {
			dP = dP * d / (balance * n)
		}
		prev := d
		d = (ann*sum + dP*n) * d / ((ann-1)*d + (n+1)*dP)
		if math.Abs(d-prev) <= 1 {
			return d, nil
		}
	}
	return d, nil
}

func stableTokenBalanceGivenInvariant(balances []float64, amplification *big.Int, invariant float64, tokenIndex int) (float64, error) {
	n := float64(len(balances))
	ann := bigToFloat(amplification) * n
	if ann <= 0 || invariant <= 0 {
		return 0, fmt.Errorf("stable invariant inputs are invalid")
	}

	c := invariant
	sum := 0.0
	for i, balance := range balances {
		if i == tokenIndex {
			continue
		}
		sum += balance
		c = c * invariant / (balance * n)
	}
	c = c * invariant / (ann * n)
	b := sum + invariant/ann
	y := invariant
	for i := 0; i < 255; i++ {
		prev := y
		y = (y*y + c) / (2*y + b - invariant)
		if math.Abs(y-prev) <= 1 {
			return y, nil
		}
	}
	return y, nil
}

func bigToFloat(v *big.Int) float64 {
	f, _ := new(big.Float).SetPrec(256).SetInt(v).Float64()
	return f
}

func floatToBigFloor(v float64) *big.Int {
	if v <= 0 {
		return big.NewInt(0)
	}
	out, _ := new(big.Float).SetPrec(256).SetFloat64(v).Int(nil)
	return out
}

func floatToBigCeil(v float64) *big.Int {
	if v <= 0 {
		return big.NewInt(0)
	}
	f := new(big.Float).SetPrec(256).SetFloat64(v)
	out, _ := f.Int(nil)
	if new(big.Float).SetInt(out).Cmp(f) < 0 {
		out.Add(out, big.NewInt(1))
	}
	return out
}

func divUp(numerator, denominator *big.Int) *big.Int {
	result := new(big.Int).Div(numerator, denominator)
	if new(big.Int).Mod(numerator, denominator).Sign() > 0 {
		result.Add(result, big.NewInt(1))
	}
	return result
}
