package service

import (
	"github.com/brianliu-sysu/arbitrage/internal/logx"

	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestNewPoolQuoteServiceBasic(t *testing.T) {
	cfg := Config{
		PoolAddress:            common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		HealthCheckIntervalSec: 0,
		MaxBlockGapForFullSync: 100,
	}
	// With empty wsURL, dial will fail
	_, err := NewPoolQuoteService("invalid://url", "invalid://url", cfg, logx.Nop(), nil, nil)
	if err == nil {
		t.Log("unexpectedly created service with invalid URL")
	}
}

func TestConfigFields(t *testing.T) {
	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	cfg := Config{
		PoolAddress:            addr,
		HealthCheckIntervalSec: 60,
	}
	if cfg.PoolAddress != addr {
		t.Error("PoolAddress mismatch")
	}
	if cfg.HealthCheckIntervalSec != 60 {
		t.Error("HealthCheckIntervalSec mismatch")
	}
}

func TestSetOnPriceUpdate(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ch := make(chan float64, 1)
	svc.SetOnPriceUpdate(func(addr common.Address, p0, p1 float64, tick int32) {
		ch <- p0
	})

	// Set a price and trigger emitPriceUpdate
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	two := big.NewInt(2)
	sqrtP := new(big.Int).Mul(two, two96) // price = 4.0
	ps.UpdateFromSwap(sqrtP, 100, testLiq, 12345)

	// emitPriceUpdate should fire
	svc.emitPriceUpdate()

	select {
	case p0 := <-ch:
		if p0 != 4.0 {
			t.Errorf("price0 = %f, want 4.0", p0)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for price update callback")
	}
}

func TestEmitPriceUpdateNoCallback(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	// No callback set — should not panic
	svc.emitPriceUpdate()
}

func TestOnSwap(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	event := &pool.SwapEvent{
		SqrtPriceX96: testSQ96,
		Tick:         42,
		Liquidity:    testLiq,
		Amount0:      big.NewInt(-1000000),
		Amount1:      big.NewInt(500000000000000000),
		Raw:          types.Log{BlockNumber: 15000000},
	}

	called := false
	svc.SetOnPriceUpdate(func(addr common.Address, p0, p1 float64, tick int32) {
		called = true
		if tick != 42 {
			t.Errorf("tick = %d, want 42", tick)
		}
	})

	svc.OnSwap(event)

	if !called {
		t.Error("price update callback was not called")
	}
	p0, _, tick := svc.GetPrice()
	if p0 != 1.0 {
		t.Errorf("price0 = %f, want 1.0", p0)
	}
	if tick != 42 {
		t.Errorf("tick = %d, want 42", tick)
	}
}

func TestOnMint(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	amount := big.NewInt(2000000000000000000)
	event := &pool.MintEvent{
		Owner:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:    amount,
		TickLower: -200,
		TickUpper: 200,
		Raw:       types.Log{BlockNumber: 15000001},
	}

	svc.OnMint(event)

	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount = %d, want 2", ps.GetTickCount())
	}
	_, _, liquidity, _ := ps.GetRawState()
	wantLiquidity := new(big.Int).Add(testLiq, amount)
	if liquidity.Cmp(wantLiquidity) != 0 {
		t.Errorf("liquidity after active mint = %s, want %s", liquidity, wantLiquidity)
	}
}

func TestOnBurn(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	// First mint to populate ticks
	amt := big.NewInt(1000000)
	ps.UpdateTickFromMint(-100, 100, amt)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	burnAmt := big.NewInt(1000000) // burn the same position
	event := &pool.BurnEvent{
		Owner:     common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Amount:    burnAmt,
		TickLower: -100,
		TickUpper: 100,
		Raw:       types.Log{BlockNumber: 15000002},
	}

	svc.OnBurn(event)

	// After burning the entire position, ticks should be empty
	if ps.GetTickCount() != 0 {
		t.Errorf("GetTickCount after burn = %d, want 0", ps.GetTickCount())
	}
	_, _, liquidity, _ := ps.GetRawState()
	if liquidity.Cmp(testLiq) != 0 {
		t.Errorf("liquidity after active burn = %s, want %s", liquidity, testLiq)
	}
}

func TestOnError(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	// Should not panic
	svc.OnError(fmt.Errorf("test error"))
}

func TestOnReconnected(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	// OnReconnected with nil subscriber should log and return
	// (SyncStateFromRPC returns error with nil sub)
	svc.OnReconnected()
	// Should not panic
}

func TestStartAndStop(t *testing.T) {
	cfg := Config{
		PoolAddress:            common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		HealthCheckIntervalSec: 0,
		MaxBlockGapForFullSync: 100,
	}
	ps, err := NewPoolQuoteService("http://127.0.0.1:1", "http://127.0.0.1:1", cfg, logx.Nop(), nil, nil)
	if err == nil {
		// Start will fail to connect, but should not panic
		err := ps.Start(0)
		if err != nil {
			t.Logf("Start failed (expected): %v", err)
		}
		ps.Stop()
	}
}

func TestHealthCheckDisabled(t *testing.T) {
	// Verify that 0 interval disables health checks
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	svc.startHealthCheck()
	// Should not start a goroutine; just logs and returns
	// No way to verify directly, but should not panic
}

func TestHealthCheckEnabled(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{
		pool:                ps,
		healthCheckInterval: 10 * time.Millisecond,
	logger: logx.Nop(),
	}
	svc.startHealthCheck()

	// Let it run a cycle
	time.Sleep(30 * time.Millisecond)

	// Stop should cancel the health check goroutine
	svc.Stop()

	// Should not panic or hang
}

func TestHealthCheckWithNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{
		pool:                ps,
		healthCheckInterval: 10 * time.Millisecond,
	logger: logx.Nop(),
	}
	svc.startHealthCheck()
	time.Sleep(30 * time.Millisecond)
	svc.Stop()
	// runHealthCheck with nil subscriber should just return
}

func TestHealthCheckDivergence(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	// Different tick - runHealthCheck will call SyncStateFromRPC
	ps.UpdateFromSwap(testSQ96, 1, testLiq, 100)
	svc := &PoolQuoteService{
		pool:                ps,
		healthCheckInterval: 10 * time.Millisecond,
	logger: logx.Nop(),
	}
	svc.startHealthCheck()
	time.Sleep(30 * time.Millisecond)
	svc.Stop()
}

func TestSyncStateNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.SyncStateFromRPC()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestDoFullSyncNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.DoFullSync(0)
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestQuoteExactInputNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	out, err := svc.QuoteExactInput(big.NewInt(1000), tkUSDC)
	if err != nil {
		t.Fatalf("QuoteExactInput should not require subscriber: %v", err)
	}
	if out.Sign() <= 0 {
		t.Fatalf("amountOut = %s, want positive", out)
	}
}

func TestResolvePoolMetadataNilSubscriber(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}
	err := svc.ResolvePoolMetadata()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestTryBufferEventRespectsBufferingMode(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ev := bufferedEvent{swap: &pool.SwapEvent{Raw: types.Log{BlockNumber: 10}}}
	if svc.tryBufferEvent(ev) {
		t.Fatal("should not buffer when buffering mode is disabled")
	}
	if svc.bufferLen() != 0 {
		t.Fatalf("bufferLen = %d, want 0", svc.bufferLen())
	}

	svc.bufferingMode.Store(true)
	if !svc.tryBufferEvent(ev) {
		t.Fatal("should buffer when buffering mode is enabled")
	}
	if svc.bufferLen() != 1 {
		t.Fatalf("bufferLen = %d, want 1", svc.bufferLen())
	}
}

func TestDrainAndReplayDisablesBufferingAndAppliesEvents(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{
		pool:               ps,
		logger:             logx.Nop(),
		snapshotStartBlock: 100,
	}

	svc.bufferingMode.Store(true)
	swap := &pool.SwapEvent{
		SqrtPriceX96: testSQ96,
		Tick:         88,
		Liquidity:    testLiq,
		Raw:          types.Log{BlockNumber: 100},
	}
	svc.eventBuffer = []bufferedEvent{{swap: swap}}

	svc.drainAndReplay()

	if svc.bufferingMode.Load() {
		t.Fatal("buffering mode should be disabled after draining")
	}
	_, _, tick := svc.GetPrice()
	if tick != 88 {
		t.Fatalf("tick = %d, want 88", tick)
	}
	if svc.bufferLen() != 0 {
		t.Fatalf("bufferLen = %d, want 0", svc.bufferLen())
	}
}

