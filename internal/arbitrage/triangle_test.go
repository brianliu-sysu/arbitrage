package arbitrage

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

func TestTriangleScannerFindsProfitableCycle(t *testing.T) {
	base := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x2000000000000000000000000000000000000002")
	tokenC := common.HexToAddress("0x3000000000000000000000000000000000000003")
	poolAB := common.HexToAddress("0xa000000000000000000000000000000000000001")
	poolBC := common.HexToAddress("0xb000000000000000000000000000000000000002")
	poolCA := common.HexToAddress("0xc000000000000000000000000000000000000003")

	cache := pool.NewCache()
	cache.Set(poolAB, stateForTokens(poolAB, base, tokenB, 101))
	cache.Set(poolBC, stateForTokens(poolBC, tokenB, tokenC, 102))
	cache.Set(poolCA, stateForTokens(poolCA, tokenC, base, 103))

	quoteFn := func(poolAddr common.Address, amountIn *big.Int, _ common.Address) (*big.Int, error) {
		out := new(big.Int).Set(amountIn)
		switch poolAddr {
		case poolAB:
			out.Mul(out, big.NewInt(2))
		case poolBC:
			out.Mul(out, big.NewInt(2))
		case poolCA:
			out.Mul(out, big.NewInt(3))
			out.Div(out, big.NewInt(10))
		}
		return out, nil
	}

	scanner := newTriangleScannerWithQuoteFunc(cache, TriangleConfig{
		BaseTokens:       []common.Address{base},
		AmountCandidates: []*big.Int{big.NewInt(1000)},
		MinProfitBps:     1000,
		MaxResults:       5,
	}, quoteFn)

	opps := scanner.Scan()
	if len(opps) == 0 {
		t.Fatal("expected at least one opportunity")
	}
	opp := opps[0]
	if opp.AmountOut.String() != "1200" {
		t.Fatalf("amount out=%s want 1200", opp.AmountOut)
	}
	if opp.Profit.String() != "200" {
		t.Fatalf("profit=%s want 200", opp.Profit)
	}
	if opp.ProfitBps != 2000 {
		t.Fatalf("profit bps=%f want 2000", opp.ProfitBps)
	}
	if opp.BlockNumber != 103 {
		t.Fatalf("block=%d want 103", opp.BlockNumber)
	}
}

func TestTriangleScannerFiltersByMinProfit(t *testing.T) {
	base := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x2000000000000000000000000000000000000002")
	tokenC := common.HexToAddress("0x3000000000000000000000000000000000000003")
	poolAB := common.HexToAddress("0xa000000000000000000000000000000000000001")
	poolBC := common.HexToAddress("0xb000000000000000000000000000000000000002")
	poolCA := common.HexToAddress("0xc000000000000000000000000000000000000003")

	cache := pool.NewCache()
	cache.Set(poolAB, stateForTokens(poolAB, base, tokenB, 1))
	cache.Set(poolBC, stateForTokens(poolBC, tokenB, tokenC, 1))
	cache.Set(poolCA, stateForTokens(poolCA, tokenC, base, 1))

	scanner := newTriangleScannerWithQuoteFunc(cache, TriangleConfig{
		BaseTokens:       []common.Address{base},
		AmountCandidates: []*big.Int{big.NewInt(1000)},
		MinProfitBps:     1,
	}, func(_ common.Address, amountIn *big.Int, _ common.Address) (*big.Int, error) {
		return new(big.Int).Set(amountIn), nil
	})

	if opps := scanner.Scan(); len(opps) != 0 {
		t.Fatalf("opportunities=%d want 0", len(opps))
	}
}

func TestTriangleScannerSkipsLoadingPools(t *testing.T) {
	base := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tokenB := common.HexToAddress("0x2000000000000000000000000000000000000002")
	tokenC := common.HexToAddress("0x3000000000000000000000000000000000000003")
	poolAB := common.HexToAddress("0xa000000000000000000000000000000000000001")
	poolBC := common.HexToAddress("0xb000000000000000000000000000000000000002")
	poolCA := common.HexToAddress("0xc000000000000000000000000000000000000003")

	cache := pool.NewCache()
	loading := stateForTokens(poolAB, base, tokenB, 1)
	loading.BeginLoading()
	cache.Set(poolAB, loading)
	cache.Set(poolBC, stateForTokens(poolBC, tokenB, tokenC, 1))
	cache.Set(poolCA, stateForTokens(poolCA, tokenC, base, 1))

	scanner := newTriangleScannerWithQuoteFunc(cache, TriangleConfig{
		BaseTokens:       []common.Address{base},
		AmountCandidates: []*big.Int{big.NewInt(1000)},
	}, func(_ common.Address, amountIn *big.Int, _ common.Address) (*big.Int, error) {
		return new(big.Int).Mul(amountIn, big.NewInt(2)), nil
	})

	if opps := scanner.Scan(); len(opps) != 0 {
		t.Fatalf("opportunities=%d want 0", len(opps))
	}
}

func stateForTokens(addr, token0, token1 common.Address, block uint64) *pool.State {
	st := pool.NewState(addr, token0, token1, 3000)
	st.BlockNumber = block
	return st
}
