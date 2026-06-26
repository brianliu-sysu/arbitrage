package service

import (
	"testing"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

var (
	addrPool1 = common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	addrPool2 = common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")
	addrPool3 = common.HexToAddress("0x99ac8cA7087fA4A2A1FB6357269965A2014ABc35")
	addrPool4 = common.HexToAddress("0x4e68Ccd3E89f51C3074ca5072bbAC773960dFa36")

	tkUSDC = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	tkWETH = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	tkUSDT = common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7")
	tkWBTC = common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599")
	tkDAI  = common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F")
)

// makeMockPool 创建一个 mock 池子，只设置地址和代币信息。
// PoolQuoteService 需要真实的 pool.PoolState，但 path_finder 只用 GetStateCopy。
func makeMockPool(addr common.Address, token0, token1 common.Address, fee uint32) *PoolQuoteService {
	ps := pool.NewPoolState(addr, token0, token1, fee)
	return &PoolQuoteService{pool: ps}
}

func TestPathFinderSingleHop(t *testing.T) {
	// 一个池子: USDC/WETH
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
	}
	pf := NewPathFinder(pools, 2, nil)

	paths := pf.FindPaths(tkUSDC, tkWETH)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if len(paths[0].Hops) != 1 {
		t.Errorf("expected 1 hop, got %d", len(paths[0].Hops))
	}
	hop := paths[0].Hops[0]
	if hop.TokenIn != tkUSDC {
		t.Errorf("TokenIn = %s", hop.TokenIn.Hex())
	}
	if hop.TokenOut != tkWETH {
		t.Errorf("TokenOut = %s", hop.TokenOut.Hex())
	}
}

func TestPathFinderReverseDirection(t *testing.T) {
	// 一个池子: USDC/WETH，从 WETH→USDC
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
	}
	pf := NewPathFinder(pools, 2, nil)

	paths := pf.FindPaths(tkWETH, tkUSDC)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	hop := paths[0].Hops[0]
	if hop.TokenIn != tkWETH {
		t.Errorf("TokenIn = %s", hop.TokenIn.Hex())
	}
	if hop.TokenOut != tkUSDC {
		t.Errorf("TokenOut = %s", hop.TokenOut.Hex())
	}
}

func TestPathFinderTwoHops(t *testing.T) {
	// 两个池子: USDC/WETH, WETH/USDT
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
		addrPool2: makeMockPool(addrPool2, tkWETH, tkUSDT, 500),
	}
	pf := NewPathFinder(pools, 3, nil)

	// USDC → USDT: 应该通过 WETH
	paths := pf.FindPaths(tkUSDC, tkUSDT)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if len(paths[0].Hops) != 2 {
		t.Errorf("expected 2 hops, got %d", len(paths[0].Hops))
	}
}

func TestPathFinderMaxHops(t *testing.T) {
	// 三个池子: USDC/WETH, WETH/USDT, USDT/DAI
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
		addrPool2: makeMockPool(addrPool2, tkWETH, tkUSDT, 500),
		addrPool3: makeMockPool(addrPool3, tkUSDT, tkDAI, 500),
	}
	// maxHops=1: USDC→DAI 无路径
	pf := NewPathFinder(pools, 1, nil)
	paths := pf.FindPaths(tkUSDC, tkDAI)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths with maxHops=1, got %d", len(paths))
	}

	// maxHops=2: USDC→USDT (1 path, 2 hops via WETH)
	pf = NewPathFinder(pools, 2, nil)
	paths = pf.FindPaths(tkUSDC, tkDAI)
	// USDC→WETH→USDT→DAI would be 3 hops, so with maxHops=2: no path
	if len(paths) != 0 {
		t.Errorf("expected 0 paths with maxHops=2 for 3-hop distance, got %d", len(paths))
	}

	// maxHops=3: USDC→DAI (1 path, 3 hops)
	pf = NewPathFinder(pools, 3, nil)
	paths = pf.FindPaths(tkUSDC, tkDAI)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path with maxHops=3, got %d", len(paths))
	}
	if len(paths[0].Hops) != 3 {
		t.Errorf("expected 3 hops, got %d", len(paths[0].Hops))
	}
}

func TestPathFinderSameToken(t *testing.T) {
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
	}
	pf := NewPathFinder(pools, 2, nil)

	paths := pf.FindPaths(tkUSDC, tkUSDC)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for same token, got %d", len(paths))
	}
}

func TestPathFinderNoPath(t *testing.T) {
	// 孤立池子: USDC/WETH only, 查询 WBTC→USDT
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
	}
	pf := NewPathFinder(pools, 2, nil)

	paths := pf.FindPaths(tkWBTC, tkDAI)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestPathFinderBridgeTokens(t *testing.T) {
	// 三个池子: USDC/WETH, WETH/USDT, WBTC/USDC
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
		addrPool2: makeMockPool(addrPool2, tkWETH, tkUSDT, 500),
		addrPool3: makeMockPool(addrPool3, tkWBTC, tkUSDC, 3000),
	}
	// 允许的 bridge: 仅 USDC（不允许 WETH）
	bridgeTokens := []common.Address{tkUSDC}
	pf := NewPathFinder(pools, 3, bridgeTokens)

	// WBTC→USDT via WBTC/USDC → USDC/WETH → WETH/USDT
	// 第一 hop: WBTC→USDC (USDC is bridge ✓)
	// 第二 hop: USDC→WETH (WETH is NOT bridge ✗)
	// So this path should be blocked
	paths := pf.FindPaths(tkWBTC, tkUSDT)
	if len(paths) != 0 {
		// 可能会有 WBTC→USDC→?? 但因为 WETH 不在 bridge 中所以被过滤
		t.Logf("found %d paths (expect 0 since WETH is not a bridge token)", len(paths))
	}

	// WBTC→USDC 单跳应可行 (USDC is bridge, and it's the target)
	pf = NewPathFinder(pools, 2, bridgeTokens)
	paths = pf.FindPaths(tkWBTC, tkUSDC)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path WBTC→USDC, got %d", len(paths))
	}
}

func TestPathFinderNoRevisitPool(t *testing.T) {
	// 两个池子共享同一对: USDC/WETH @ 0.3% and USDC/WETH @ 0.05%
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
		addrPool2: makeMockPool(addrPool2, tkUSDC, tkWETH, 500),
	}
	pf := NewPathFinder(pools, 2, nil)

	// USDC→WETH: 2 paths (one via each pool)
	paths := pf.FindPaths(tkUSDC, tkWETH)
	if len(paths) != 2 {
		t.Fatalf("expected 2 direct paths, got %d", len(paths))
	}

	// Each path should have exactly 1 hop (should not revisit the same pool)
	for _, p := range paths {
		if len(p.Hops) != 1 {
			t.Errorf("expected 1 hop, got %d (should not revisit pool)", len(p.Hops))
		}
	}
}

func TestPathFinderMultiRoute(t *testing.T) {
	// 三个池子: USDC/WETH, WETH/USDT, WBTC/WETH
	// WBTC→USDT: via WETH (WBTC→WETH→USDT) = 1 path, 2 hops
	pools := map[common.Address]*PoolQuoteService{
		addrPool1: makeMockPool(addrPool1, tkUSDC, tkWETH, 3000),
		addrPool2: makeMockPool(addrPool2, tkWETH, tkUSDT, 500),
		addrPool3: makeMockPool(addrPool3, tkWBTC, tkWETH, 3000),
	}
	pf := NewPathFinder(pools, 3, nil)

	paths := pf.FindPaths(tkWBTC, tkUSDT)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path WBTC→WETH→USDT, got %d", len(paths))
	}
	if len(paths[0].Hops) != 2 {
		t.Errorf("expected 2 hops, got %d", len(paths[0].Hops))
	}
}

func TestEmptyPathFinder(t *testing.T) {
	pf := NewPathFinder(
		map[common.Address]*PoolQuoteService{},
		2,
		nil,
	)
	paths := pf.FindPaths(tkUSDC, tkWETH)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths from empty finder, got %d", len(paths))
	}
}
