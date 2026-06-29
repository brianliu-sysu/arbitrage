package service

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewMultiChainService(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	if mcs == nil {
		t.Fatal("NewMultiChainService returned nil")
	}
	if len(mcs.chains) != 0 {
		t.Errorf("expected empty chains, got %d", len(mcs.chains))
	}
}

func TestMultiChainAddChain(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)

	err := mcs.AddChain("ethereum", ms)
	if err != nil {
		t.Fatalf("AddChain: %v", err)
	}

	// Duplicate add
	err = mcs.AddChain("ethereum", ms)
	if err == nil {
		t.Error("expected error for duplicate chain")
	}
}

func TestMultiChainGetChain(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	_ = mcs.AddChain("polygon", ms)

	svc, ok := mcs.GetChain("polygon")
	if !ok {
		t.Fatal("GetChain should return ok for registered chain")
	}
	if svc != ms {
		t.Error("GetChain returned wrong service")
	}

	_, ok = mcs.GetChain("nonexistent")
	if ok {
		t.Error("GetChain should return !ok for unknown chain")
	}
}

func TestMultiChainChainNames(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	_ = mcs.AddChain("ethereum", NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil))
	_ = mcs.AddChain("polygon", NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil))
	_ = mcs.AddChain("arbitrum", NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil))

	names := mcs.ChainNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 chain names, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"ethereum", "polygon", "arbitrum"} {
		if !nameSet[expected] {
			t.Errorf("missing chain name %q", expected)
		}
	}
}

func TestMultiChainStopAll(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	_ = mcs.AddChain("ethereum", NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil))
	_ = mcs.AddChain("polygon", NewMultiPoolService("", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil))

	// Should not panic
	mcs.StopAll()
}

func TestMultiChainSetOnPriceUpdateAll(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	ms := NewMultiPoolService("ethereum", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	_ = mcs.AddChain("ethereum", ms)

	ch := make(chan string, 1)
	mcs.SetOnPriceUpdateAll(func(chain string, poolAddr common.Address, price0In1, price1In0 float64, tick int32) {
		ch <- chain
	})

	if ms.onPriceUpdate == nil {
		t.Error("onPriceUpdate should be set on chain service")
	}
}

func TestMultiChainGetAllPoolInfoFlat(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms := NewMultiPoolService("ethereum", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()
	_ = mcs.AddChain("ethereum", ms)

	info := mcs.GetAllPoolInfoFlat()
	if len(info) != 1 {
		t.Fatalf("expected 1 pool info, got %d", len(info))
	}
	if info[0]["chain"] != "ethereum" {
		t.Errorf("chain field = %v, want ethereum", info[0]["chain"])
	}
}

func TestMultiChainGetPrice(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	two := big.NewInt(2)
	sqrtP := new(big.Int).Mul(two, two96) // price = 4
	ps.UpdateFromSwap(sqrtP, 100, testLiq, 20000)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms := NewMultiPoolService("ethereum", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()
	_ = mcs.AddChain("ethereum", ms)

	// Valid chain and pool
	p0, p1, tick, ok := mcs.GetPrice("ethereum", addr)
	if !ok {
		t.Fatal("GetPrice should succeed")
	}
	if p0 != 4.0 {
		t.Errorf("Price0In1 = %f, want 4.0", p0)
	}
	if p1 != 0.25 {
		t.Errorf("Price1In0 = %f, want 0.25", p1)
	}
	if tick != 100 {
		t.Errorf("tick = %d, want 100", tick)
	}

	// Unknown chain
	_, _, _, ok = mcs.GetPrice("nonexistent", addr)
	if ok {
		t.Error("GetPrice should return !ok for unknown chain")
	}

	// Unknown pool
	_, _, _, ok = mcs.GetPrice("ethereum", common.Address{})
	if ok {
		t.Error("GetPrice should return !ok for unknown pool")
	}
}

func TestMultiChainQuoteExactInput(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms := NewMultiPoolService("ethereum", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()
	_ = mcs.AddChain("ethereum", ms)

	// Unknown chain
	_, err := mcs.QuoteExactInput("nonexistent", addr, big.NewInt(1000), tkUSDC)
	if err == nil {
		t.Error("expected error for unknown chain")
	}

	// Valid chain and pool
	out, err := mcs.QuoteExactInput("ethereum", addr, big.NewInt(1000), tkUSDC)
	if err != nil {
		t.Fatalf("QuoteExactInput: %v", err)
	}
	if out.Sign() <= 0 {
		t.Errorf("amountOut = %s, want positive", out)
	}
}

func TestMultiChainCrossQuote(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	// Unknown chain
	_, err := mcs.CrossQuote("nonexistent", big.NewInt(1), common.Address{}, common.Address{})
	if err == nil {
		t.Error("expected error for unknown chain")
	}
}

func TestMultiChainEmptyChainNames(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())
	names := mcs.ChainNames()
	if len(names) != 0 {
		t.Errorf("expected empty chain names, got %d", len(names))
	}
}

func TestMultiChainGetAllPoolInfo(t *testing.T) {
	mcs := NewMultiChainService(logx.Nop())

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms := NewMultiPoolService("ethereum", "", "", 2, nil, 100, common.Address{}, common.Address{}, common.Address{}, logx.Nop(), nil, nil, nil)
	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()
	_ = mcs.AddChain("ethereum", ms)

	info := mcs.GetAllPoolInfo()
	if len(info) != 1 {
		t.Fatalf("expected 1 pool info, got %d", len(info))
	}
}
