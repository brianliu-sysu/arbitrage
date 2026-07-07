package balancer

import (
	"math/big"
	"testing"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func testTokens() (common.Address, common.Address) {
	return common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002")
}

func testWeightedPool(t *testing.T) *marketbalancer.Pool {
	t.Helper()
	tokenA, tokenB := testTokens()
	pool, err := marketbalancer.NewPool(
		marketbalancer.PoolID(common.HexToHash("0x1")),
		common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		common.HexToAddress("0x00000000000000000000000000000000000000bb"),
		marketbalancer.PoolTypeWeighted,
		[]common.Address{tokenA, tokenB},
	)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	pool.Balances[tokenA] = big.NewInt(1000)
	pool.Balances[tokenB] = big.NewInt(1000)
	pool.Weights[tokenA] = new(big.Int).Div(fixedOne, big.NewInt(2))
	pool.Weights[tokenB] = new(big.Int).Div(fixedOne, big.NewInt(2))
	pool.SwapFeePercentage = big.NewInt(0)
	return pool
}

func TestWeightedQuoteExactInput(t *testing.T) {
	tokenA, tokenB := testTokens()
	result, err := NewQuoteService().QuoteExactInput(testWeightedPool(t), tokenA, tokenB, big.NewInt(100))
	if err != nil {
		t.Fatalf("quote exact input: %v", err)
	}
	if result.AmountOut.Cmp(big.NewInt(90)) != 0 {
		t.Fatalf("expected amountOut 90, got %s", result.AmountOut)
	}
}

func TestWeightedQuoteExactOutput(t *testing.T) {
	tokenA, tokenB := testTokens()
	result, err := NewQuoteService().QuoteExactOutput(testWeightedPool(t), tokenA, tokenB, big.NewInt(90))
	if err != nil {
		t.Fatalf("quote exact output: %v", err)
	}
	if result.AmountIn.Cmp(big.NewInt(99)) != 0 {
		t.Fatalf("expected amountIn 99, got %s", result.AmountIn)
	}
}

func TestStableQuoteAppliesFee(t *testing.T) {
	tokenA, tokenB := testTokens()
	pool, err := marketbalancer.NewPool(
		marketbalancer.PoolID(common.HexToHash("0x2")),
		common.Address{},
		common.Address{},
		marketbalancer.PoolTypeStable,
		[]common.Address{tokenA, tokenB},
	)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	pool.Balances[tokenA] = big.NewInt(1000)
	pool.Balances[tokenB] = big.NewInt(1000)
	pool.Amplification = big.NewInt(1000)
	pool.SwapFeePercentage = new(big.Int).Div(fixedOne, big.NewInt(100))

	result, err := NewQuoteService().QuoteExactInput(pool, tokenA, tokenB, big.NewInt(100))
	if err != nil {
		t.Fatalf("quote stable: %v", err)
	}
	if result.AmountOut.Cmp(big.NewInt(97)) != 0 || result.FeeAmount.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected amountOut=97 fee=1, got amountOut=%s fee=%s", result.AmountOut, result.FeeAmount)
	}
}

func TestStableQuoteExactOutput(t *testing.T) {
	tokenA, tokenB := testTokens()
	pool, err := marketbalancer.NewPool(
		marketbalancer.PoolID(common.HexToHash("0x3")),
		common.Address{},
		common.Address{},
		marketbalancer.PoolTypeStable,
		[]common.Address{tokenA, tokenB},
	)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	pool.Balances[tokenA] = big.NewInt(1000)
	pool.Balances[tokenB] = big.NewInt(1000)
	pool.Amplification = big.NewInt(1000)
	pool.SwapFeePercentage = big.NewInt(0)

	result, err := NewQuoteService().QuoteExactOutput(pool, tokenA, tokenB, big.NewInt(97))
	if err != nil {
		t.Fatalf("quote stable exact output: %v", err)
	}
	if result.AmountIn.Cmp(big.NewInt(99)) != 0 {
		t.Fatalf("expected amountIn=99, got %s", result.AmountIn)
	}
}
