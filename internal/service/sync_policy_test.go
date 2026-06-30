package service

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
)

func TestNeedsInitialFullSync(t *testing.T) {
	ps := pool.NewState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps}

	if !svc.needsInitialFullSync() {
		t.Fatal("empty pool should need initial full sync")
	}

	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	if !svc.needsInitialFullSync() {
		t.Fatal("pool with block but no ticks should need initial full sync")
	}

	ps.ReplaceTicks(map[int32]*pool.TickLiquidity{
		-100: {LiquidityNet: new(big.Int).Set(testLiq), LiquidityGross: new(big.Int).Set(testLiq)},
	})
	if svc.needsInitialFullSync() {
		t.Fatal("pool with block and ticks should not need initial full sync")
	}
}
