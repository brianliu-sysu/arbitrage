package service

import (
	"github.com/brianliu-sysu/arbitrage/internal/logx"

	"math/big"
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

func TestNewMultiPoolService(t *testing.T) {
	ms := NewMultiPoolService("ethereum", "wss://test.com", "https://test.com", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)
	if ms == nil {
		t.Fatal("NewMultiPoolService returned nil")
	}
	if ms.wsEndpoint != "wss://test.com" {
		t.Errorf("wsEndpoint = %s", ms.wsEndpoint)
	}
	if ms.maxHops != 2 {
		t.Errorf("maxHops = %d, want 2", ms.maxHops)
	}
	if len(ms.services) != 0 {
		t.Errorf("services should be empty")
	}
	if ms.pathFinder != nil {
		t.Error("pathFinder should be nil before pools are added")
	}
}

func TestMultiPoolBasic(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	if info := ms.GetAllPoolInfo(); len(info) != 0 {
		t.Errorf("expected 0 pools, got %d", len(info))
	}

	_, _, _, ok := ms.GetPrice(common.Address{})
	if ok {
		t.Error("GetPrice should return false for unknown pool")
	}
}

func TestMultiPoolSetOnPriceUpdate(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)
	ms.SetOnPriceUpdate(func(addr common.Address, p0, p1 float64, tick int32) {
		// callback set
	})
	if ms.onPriceUpdate == nil {
		t.Error("onPriceUpdate should be set")
	}
	// Should not panic
	ms.SetOnPriceUpdate(nil)
}

func TestMultiPoolQuoteExactInputNotFound(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	_, err := ms.QuoteExactInput(common.Address{}, big.NewInt(1), common.Address{})
	if err == nil {
		t.Error("QuoteExactInput should error for unknown pool")
	}
}

func TestMultiPoolCrossQuoteNoPathFinder(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	_, err := ms.CrossQuote(big.NewInt(1), common.Address{}, common.Address{})
	if err == nil {
		t.Error("CrossQuote should error with no path finder")
	}
}

func TestMultiPoolGetPrice(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	two := big.NewInt(2)
	sqrtP := new(big.Int).Mul(two, two96) // price = 4
	ps.UpdateFromSwap(sqrtP, 100, testLiq, 20000000)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()

	p0, p1, tick, ok := ms.GetPrice(addr)
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
}

func TestMultiPoolGetAllPoolInfo(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	ps.Token0Symbol = "USDC"
	ps.Token1Symbol = "WETH"
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()

	info := ms.GetAllPoolInfo()
	if len(info) != 1 {
		t.Fatalf("expected 1 pool in info, got %d", len(info))
	}
	if info[0]["token0"] != tkUSDC.Hex() {
		t.Errorf("token0 mismatch")
	}
	if info[0]["token1"] != tkWETH.Hex() {
		t.Errorf("token1 mismatch")
	}
	if info[0]["fee"] != uint32(3000) {
		t.Errorf("fee mismatch")
	}
}

func TestRebuildPathFinder(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	addr1 := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	addr2 := common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")

	ps1 := pool.NewPoolState(addr1, tkUSDC, tkWETH, 3000)
	ps1.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mock1 := &PoolQuoteService{pool: ps1}

	ps2 := pool.NewPoolState(addr2, tkWETH, tkUSDT, 500)
	ps2.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mock2 := &PoolQuoteService{pool: ps2}

	ms.mu.Lock()
	ms.services[addr1] = mock1
	ms.services[addr2] = mock2
	ms.mu.Unlock()

	ms.rebuildPathFinder()

	if ms.pathFinder == nil {
		t.Fatal("pathFinder should not be nil after rebuild")
	}

	// Find paths between USDC and USDT
	paths := ms.pathFinder.FindPaths(tkUSDC, tkUSDT)
	if len(paths) != 1 {
		t.Errorf("expected 1 path USDC→WETH→USDT, got %d", len(paths))
	}
}

func TestMultiPoolCrossQuoteTwoHopLocal(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, []common.Address{tkWETH}, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	addr1 := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	addr2 := common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")

	ps1 := pool.NewPoolState(addr1, tkUSDC, tkWETH, 3000)
	ps1.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mock1 := &PoolQuoteService{pool: ps1, logger: logx.Nop()}

	ps2 := pool.NewPoolState(addr2, tkWETH, tkUSDT, 500)
	ps2.UpdateFromSwap(testSQ96, 0, testLiq, 10)
	mock2 := &PoolQuoteService{pool: ps2, logger: logx.Nop()}

	ms.mu.Lock()
	ms.services[addr1] = mock1
	ms.services[addr2] = mock2
	ms.mu.Unlock()
	ms.rebuildPathFinder()

	amountIn := big.NewInt(1_000_000)
	result, err := ms.CrossQuote(amountIn, tkUSDC, tkUSDT)
	if err != nil {
		t.Fatalf("CrossQuote two-hop local failed: %v", err)
	}
	if len(result.Hops) != 2 {
		t.Fatalf("hops = %d, want 2", len(result.Hops))
	}
	if result.Hops[0].TokenIn != tkUSDC || result.Hops[0].TokenOut != tkWETH {
		t.Fatalf("first hop mismatch: %+v", result.Hops[0])
	}
	if result.Hops[1].TokenIn != tkWETH || result.Hops[1].TokenOut != tkUSDT {
		t.Fatalf("second hop mismatch: %+v", result.Hops[1])
	}
	if result.AmountOut.Sign() <= 0 {
		t.Fatalf("amountOut = %s, want positive", result.AmountOut)
	}
	if result.AmountOut.Cmp(amountIn) >= 0 {
		t.Fatalf("amountOut = %s, want less than amountIn after fees", result.AmountOut)
	}
}

func TestStopAll(t *testing.T) {
	ms := NewMultiPoolService("", "", "", 2, nil, 100, common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"), common.Address{}, common.Address{}, logx.Nop(), nil)

	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	ps := pool.NewPoolState(addr, tkUSDC, tkWETH, 3000)
	// PoolQuoteService with nil subscriber should still Stop safely
	mockPQS := &PoolQuoteService{pool: ps, logger: logx.Nop()}

	ms.mu.Lock()
	ms.services[addr] = mockPQS
	ms.mu.Unlock()

	// Should not panic
	ms.StopAll()
}

func TestQuoteResultTypes(t *testing.T) {
	addr := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	qr := &QuoteResult{
		Hops: []QuoteHop{
			{Pool: addr, TokenIn: tkUSDC, TokenOut: tkWETH},
		},
		AmountIn:  big.NewInt(1000000),
		AmountOut: big.NewInt(500000000),
		TokenIn:   tkUSDC,
		TokenOut:  tkWETH,
	}

	if len(qr.Hops) != 1 {
		t.Errorf("expected 1 hop")
	}
	if qr.AmountIn.Cmp(big.NewInt(1000000)) != 0 {
		t.Error("AmountIn mismatch")
	}
}

func TestPoolQuoteServiceNilSubscriber(t *testing.T) {
	cfg := Config{
		PoolAddress:            common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		HealthCheckIntervalSec: 0,
	}
	ps, err := NewPoolQuoteService("", "", cfg, logx.Nop(), nil)
	// Empty wsURL will fail to dial
	if err == nil {
		t.Log("NewPoolQuoteService succeeded with empty URL")
		ps.Stop() // should be safe with nil subscriber
	}
}

func TestPoolQuoteServiceGetPoolInfo(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	ps.UpdateFromSwap(two96, 0, testLiq, 100)
	ps.Token0Symbol = "USDC"
	ps.Token1Symbol = "WETH"

	svc := &PoolQuoteService{pool: ps}
	info := svc.GetPoolInfo()

	if info["address"] != addrPool1.Hex() {
		t.Errorf("address mismatch")
	}
	if info["fee"] != uint32(3000) {
		t.Errorf("fee mismatch")
	}
}

func TestPoolQuoteServiceGetPrice(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 42, testLiq, 100)

	svc := &PoolQuoteService{pool: ps}
	p0, p1, tick := svc.GetPrice()

	if p0 != 1.0 {
		t.Errorf("Price0In1 = %f, want 1.0", p0)
	}
	if p1 != 1.0 {
		t.Errorf("Price1In0 = %f, want 1.0", p1)
	}
	if tick != 42 {
		t.Errorf("tick = %d, want 42", tick)
	}
}

func TestPoolQuoteServiceSyncStateNilSub(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps}
	err := svc.SyncStateFromRPC()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

func TestPoolQuoteServiceQuoteNilSub(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	ps.UpdateFromSwap(testSQ96, 0, testLiq, 100)
	svc := &PoolQuoteService{pool: ps}
	out, err := svc.QuoteExactInput(big.NewInt(1000000), tkUSDC)
	if err != nil {
		t.Fatalf("QuoteExactInput should not require subscriber: %v", err)
	}
	if out.Sign() <= 0 {
		t.Fatalf("amountOut = %s, want positive", out)
	}
}

func TestPoolQuoteServiceResolvePoolMetadataNilSub(t *testing.T) {
	ps := pool.NewPoolState(addrPool1, tkUSDC, tkWETH, 3000)
	svc := &PoolQuoteService{pool: ps}
	err := svc.ResolvePoolMetadata()
	if err == nil {
		t.Error("expected error with nil subscriber")
	}
}

var testSQ96 = new(big.Int).Lsh(big.NewInt(1), 96) // 2^96
var testLiq = big.NewInt(1000000000000000000)
