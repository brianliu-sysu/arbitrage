package pool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

var (
	addrPool = common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	addrUSDC = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	addrWETH = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
)

// sqrtPriceX96 for ETH/USDC at ~$2000: sqrt(2000) * 2^96 ≈ 3.5e34
// 1 ETH = 2000 USDC => price = 2000, sqrtPriceX96 = sqrt(2000) * 2^96
// sqrt(2000) ≈ 44.721, 2^96 ≈ 7.92e28, product ≈ 3.54e30
var testSqrtPriceX96 = func() *big.Int {
	// Using a value that gives a reasonable price for testing
	// sqrtPriceX96 = 79228162514264337593543950336 = 2^96 (price=1)
	v := new(big.Int).SetUint64(1)
	v.Lsh(v, 96) // 2^96
	return v
}()

var testLiquidity = big.NewInt(1000000000000000000) // 1e18

func TestNewPoolState(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)

	if ps.Address != addrPool {
		t.Errorf("Address = %s, want %s", ps.Address.Hex(), addrPool.Hex())
	}
	if ps.Token0 != addrUSDC {
		t.Errorf("Token0 = %s, want %s", ps.Token0.Hex(), addrUSDC.Hex())
	}
	if ps.Token1 != addrWETH {
		t.Errorf("Token1 = %s, want %s", ps.Token1.Hex(), addrWETH.Hex())
	}
	if ps.Fee != 3000 {
		t.Errorf("Fee = %d, want 3000", ps.Fee)
	}
	if ps.SqrtPriceX96.Sign() != 0 {
		t.Error("SqrtPriceX96 should be 0 initially")
	}
	if ps.Liquidity.Sign() != 0 {
		t.Error("Liquidity should be 0 initially")
	}
}

func TestSetTokens(t *testing.T) {
	ps := NewPoolState(addrPool, common.Address{}, common.Address{}, 0)
	ps.SetTokens(addrUSDC, addrWETH, 500)

	if ps.Token0 != addrUSDC {
		t.Errorf("Token0 = %s, want %s", ps.Token0.Hex(), addrUSDC.Hex())
	}
	if ps.Token1 != addrWETH {
		t.Errorf("Token1 = %s, want %s", ps.Token1.Hex(), addrWETH.Hex())
	}
	if ps.Fee != 500 {
		t.Errorf("Fee = %d, want 500", ps.Fee)
	}
}

func TestSetTokensWithInfo(t *testing.T) {
	ps := NewPoolState(addrPool, common.Address{}, common.Address{}, 0)
	ps.SetTokensWithInfo(addrUSDC, addrWETH, 500, &TokenInfo{
		Symbol:   "USDCx",
		Decimals: 6,
	}, &TokenInfo{
		Symbol:   "WETHx",
		Decimals: 18,
	})

	if ps.Token0Symbol != "USDCx" {
		t.Errorf("Token0Symbol = %s, want USDCx", ps.Token0Symbol)
	}
	if ps.Token1Symbol != "WETHx" {
		t.Errorf("Token1Symbol = %s, want WETHx", ps.Token1Symbol)
	}
	if ps.Token0Decimals != 6 {
		t.Errorf("Token0Decimals = %d, want 6", ps.Token0Decimals)
	}
	if ps.Token1Decimals != 18 {
		t.Errorf("Token1Decimals = %d, want 18", ps.Token1Decimals)
	}
}

func TestUpdateFromSwap(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	sqrtPrice := new(big.Int).Set(testSqrtPriceX96)
	liq := new(big.Int).Set(testLiquidity)

	ps.UpdateFromSwap(sqrtPrice, 0, liq, 12345)

	if ps.Tick != 0 {
		t.Errorf("Tick = %d, want 0", ps.Tick)
	}
	if ps.BlockNumber != 12345 {
		t.Errorf("BlockNumber = %d, want 12345", ps.BlockNumber)
	}
	if ps.Liquidity.Cmp(liq) != 0 {
		t.Errorf("Liquidity mismatch")
	}
	if ps.SqrtPriceX96.Cmp(sqrtPrice) != 0 {
		t.Errorf("SqrtPriceX96 mismatch")
	}
	// 2^96 => price = 1.0
	if ps.Price0In1 != 1.0 {
		t.Errorf("Price0In1 = %f, want 1.0", ps.Price0In1)
	}
	if ps.Price1In0 != 1.0 {
		t.Errorf("Price1In0 = %f, want 1.0", ps.Price1In0)
	}
}

func TestUpdateFromSwapPriceCalculation(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)

	// sqrtPriceX96 for price=4: sqrt(4) * 2^96 = 2 * 2^96
	two := big.NewInt(2)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	sqrtPrice := new(big.Int).Mul(two, two96)

	ps.UpdateFromSwap(sqrtPrice, 100, testLiquidity, 1)

	if ps.Price0In1 != 4.0 {
		t.Errorf("Price0In1 = %f, want 4.0", ps.Price0In1)
	}
	// Price1In0 = 1/4
	if ps.Price1In0 != 0.25 {
		t.Errorf("Price1In0 = %f, want 0.25", ps.Price1In0)
	}
	if ps.Tick != 100 {
		t.Errorf("Tick = %d, want 100", ps.Tick)
	}
}

func TestUpdateFromSwapZeroSqrtPrice(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(big.NewInt(0), 0, testLiquidity, 1)

	// recalcPrices should skip with zero sqrtPrice, prices stay at zero value
	if ps.Price0In1 != 0 {
		t.Errorf("Price0In1 should be 0, got %f", ps.Price0In1)
	}
}

func TestUpdateFromSwapZeroValues(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	// sqrtPrice of 0 should be safe - recalcPrices skips
	ps.UpdateFromSwap(big.NewInt(0), 0, testLiquidity, 1)
	if ps.Price0In1 != 0 {
		t.Errorf("Price0In1 should be 0, got %f", ps.Price0In1)
	}
}

func TestGetPrices(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 42, testLiquidity, 100)

	p0, p1, tick, bn := ps.GetPrices()

	if p0 != 1.0 {
		t.Errorf("price0In1 = %f, want 1.0", p0)
	}
	if p1 != 1.0 {
		t.Errorf("price1In0 = %f, want 1.0", p1)
	}
	if tick != 42 {
		t.Errorf("tick = %d, want 42", tick)
	}
	if bn != 100 {
		t.Errorf("blockNumber = %d, want 100", bn)
	}
}

func TestGetStateCopy(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.Token0Symbol = "USDC"
	ps.Token1Symbol = "WETH"
	ps.UpdateFromSwap(testSqrtPriceX96, 5, testLiquidity, 77)

	cp := ps.GetStateCopy()

	if cp.Address != ps.Address {
		t.Error("Address mismatch in copy")
	}
	if cp.Token0Symbol != "USDC" {
		t.Errorf("Token0Symbol = %s, want USDC", cp.Token0Symbol)
	}
	if cp.Token1Symbol != "WETH" {
		t.Errorf("Token1Symbol = %s, want WETH", cp.Token1Symbol)
	}
	if cp.Fee != 3000 {
		t.Errorf("Fee = %d, want 3000", cp.Fee)
	}
	if cp.Tick != 5 {
		t.Errorf("Tick = %d, want 5", cp.Tick)
	}
	if cp.BlockNumber != 77 {
		t.Errorf("BlockNumber = %d, want 77", cp.BlockNumber)
	}

	// Verify deep copy - mutate original
	ps.SqrtPriceX96.SetInt64(0)
	if cp.SqrtPriceX96.Sign() == 0 {
		t.Error("GetStateCopy did not deep copy SqrtPriceX96")
	}

	ps.Liquidity.SetInt64(0)
	if cp.Liquidity.Sign() == 0 {
		t.Error("GetStateCopy did not deep copy Liquidity")
	}
}

func TestRecalcPricesHighPrice(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)

	// sqrtPriceX96 for price=2000: sqrt(2000) * 2^96
	// sqrt(2000) ≈ 44.7213595
	// 2^96 as big.Float
	two96f := new(big.Float).SetInt(new(big.Int).Lsh(big.NewInt(1), 96))
	sqrt2000 := new(big.Float).SetFloat64(44.7213595)
	sqrtPrice := new(big.Float).Mul(sqrt2000, two96f)
	sqrtInt, _ := sqrtPrice.Int(nil)

	ps.UpdateFromSwap(sqrtInt, 0, testLiquidity, 1)

	// Should be approximately 2000
	if ps.Price0In1 < 1990 || ps.Price0In1 > 2010 {
		t.Errorf("Price0In1 = %f, want ~2000", ps.Price0In1)
	}
	if ps.Price1In0 <= 0 {
		t.Error("Price1In0 should be positive")
	}
}

func TestGetRawState(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	tick := int32(42)
	liq := big.NewInt(999999)
	block := uint64(15000000)

	ps.UpdateFromSwap(two96, tick, liq, block)

	sqrtPrice, gotTick, liquidity, gotBlock := ps.GetRawState()

	if sqrtPrice.Cmp(two96) != 0 {
		t.Error("sqrtPrice mismatch")
	}
	if gotTick != tick {
		t.Errorf("tick = %d, want %d", gotTick, tick)
	}
	if liquidity.Cmp(liq) != 0 {
		t.Error("liquidity mismatch")
	}
	if gotBlock != block {
		t.Errorf("block = %d, want %d", gotBlock, block)
	}

	// Verify deep copy: mutating original must not affect returned values
	ps.SqrtPriceX96.SetInt64(0)
	if sqrtPrice.Cmp(two96) != 0 {
		t.Error("GetRawState should return deep copy of sqrtPriceX96")
	}

	ps.Liquidity.SetInt64(0)
	if liquidity.Cmp(liq) != 0 {
		t.Error("GetRawState should return deep copy of Liquidity")
	}
}

// ---- Tick 流动性测试 ----

func TestUpdateTickFromMint(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	amount := big.NewInt(1000000)
	tickLower := int32(-200)
	tickUpper := int32(200)

	ps.UpdateTickFromMint(tickLower, tickUpper, amount)

	// tickLower: +amount
	if l := ps.GetTickLiquidity(tickLower); l.Cmp(amount) != 0 {
		t.Errorf("tickLower(%d) liquidity = %s, want %s", tickLower, l.String(), amount.String())
	}
	// tickUpper: -amount
	expectedNeg := new(big.Int).Neg(amount)
	if l := ps.GetTickLiquidity(tickUpper); l.Cmp(expectedNeg) != 0 {
		t.Errorf("tickUpper(%d) liquidity = %s, want %s", tickUpper, l.String(), expectedNeg.String())
	}
	// non-existent tick: 0
	if l := ps.GetTickLiquidity(0); l.Sign() != 0 {
		t.Errorf("uninitialized tick should be 0, got %s", l.String())
	}
	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount = %d, want 2", ps.GetTickCount())
	}
}

func TestUpdateTickFromBurn(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	amount := big.NewInt(800000)

	// First mint
	ps.UpdateTickFromMint(-100, 100, amount)
	// Then burn (opposite direction)
	ps.UpdateTickFromBurn(-100, 100, amount)

	// After mint+burn, both ticks should be back to 0 and removed from map
	if ps.GetTickCount() != 0 {
		t.Errorf("GetTickCount after mint+burn = %d, want 0", ps.GetTickCount())
	}
	if l := ps.GetTickLiquidity(-100); l.Sign() != 0 {
		t.Error("liquidity should be 0 after mint+burn cycle")
	}
	if l := ps.GetTickLiquidity(100); l.Sign() != 0 {
		t.Error("liquidity should be 0 after mint+burn cycle")
	}
}

func TestUpdateTickFromMintUpdatesActiveLiquidity(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	baseLiquidity := big.NewInt(1_000_000)
	amount := big.NewInt(500_000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, baseLiquidity, 100)

	ps.UpdateTickFromMint(-10, 10, amount)

	_, _, liquidity, _ := ps.GetRawState()
	want := new(big.Int).Add(baseLiquidity, amount)
	if liquidity.Cmp(want) != 0 {
		t.Fatalf("liquidity = %s, want %s", liquidity, want)
	}
}

func TestUpdateTickFromMintDoesNotUpdateLiquidityAtUpperBound(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	baseLiquidity := big.NewInt(1_000_000)
	ps.UpdateFromSwap(testSqrtPriceX96, 10, baseLiquidity, 100)

	ps.UpdateTickFromMint(-10, 10, big.NewInt(500_000))

	_, _, liquidity, _ := ps.GetRawState()
	if liquidity.Cmp(baseLiquidity) != 0 {
		t.Fatalf("liquidity = %s, want %s", liquidity, baseLiquidity)
	}
}

func TestUpdateTickFromBurnUpdatesActiveLiquidity(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	baseLiquidity := big.NewInt(1_000_000)
	amount := big.NewInt(500_000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, baseLiquidity, 100)
	ps.UpdateTickFromMint(-10, 10, amount)
	ps.UpdateTickFromBurn(-10, 10, amount)

	_, _, liquidity, _ := ps.GetRawState()
	if liquidity.Cmp(baseLiquidity) != 0 {
		t.Fatalf("liquidity = %s, want %s", liquidity, baseLiquidity)
	}
}

func TestMultipleMints(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)

	// Position 1: [-200, 200] with amount 1M
	ps.UpdateTickFromMint(-200, 200, big.NewInt(1000000))
	// Position 2: [-100, 100] with amount 500K
	ps.UpdateTickFromMint(-100, 100, big.NewInt(500000))

	// -100: +500000 (from Position 2)
	if l := ps.GetTickLiquidity(-100); l.Cmp(big.NewInt(500000)) != 0 {
		t.Errorf("tick -100: %s, want 500000", l.String())
	}
	// -200: +1000000 (from Position 1)
	if l := ps.GetTickLiquidity(-200); l.Cmp(big.NewInt(1000000)) != 0 {
		t.Errorf("tick -200: %s, want 1000000", l.String())
	}
	// 100: -500000 (from Position 2 upper)
	if l := ps.GetTickLiquidity(100); l.Cmp(big.NewInt(-500000)) != 0 {
		t.Errorf("tick 100: %s, want -500000", l.String())
	}
	// 200: -1000000 (from Position 1 upper)
	if l := ps.GetTickLiquidity(200); l.Cmp(big.NewInt(-1000000)) != 0 {
		t.Errorf("tick 200: %s, want -1000000", l.String())
	}

	if ps.GetTickCount() != 4 {
		t.Errorf("GetTickCount = %d, want 4", ps.GetTickCount())
	}
}

func TestTicksInStateCopy(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateTickFromMint(-50, 50, big.NewInt(100))
	ps.UpdateFromSwap(testSqrtPriceX96, 0, testLiquidity, 100)

	cp := ps.GetStateCopy()
	if cp.GetTickCount() != 2 {
		t.Errorf("copy GetTickCount = %d, want 2", cp.GetTickCount())
	}
	if l := cp.GetTickLiquidity(-50); l.Cmp(big.NewInt(100)) != 0 {
		t.Error("copy tick -50 liquidity mismatch")
	}
	// Verify deep copy
	ps.UpdateTickFromBurn(-50, 50, big.NewInt(100))
	if cp.GetTickCount() != 2 {
		t.Error("GetStateCopy should return independent copy")
	}
}

func TestGetTicksCopy(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateTickFromMint(-10, 10, big.NewInt(500))

	ticks := ps.GetTicksCopy()
	if len(ticks) != 2 {
		t.Errorf("len = %d, want 2", len(ticks))
	}
	// Mutate copy, original unchanged
	ticks[-10].LiquidityNet.SetInt64(0)
	if ps.GetTickLiquidity(-10).Sign() == 0 {
		t.Error("GetTicksCopy should return deep copy")
	}
}

func TestHumanPrice(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokensWithInfo(addrUSDC, addrWETH, 3000,
		&TokenInfo{Decimals: 6, Symbol: "USDC"},  // token0
		&TokenInfo{Decimals: 18, Symbol: "WETH"}, // token1
	)
	// ETH/USDC: token0=USDC(6dec), token1=WETH(18dec)
	ps.UpdateFromSwap(testSqrtPriceX96, 202669, testLiquidity, 100)

	hp := ps.HumanPrice()
	// 1 ETH ≈ 1580 USDC (depends on exact tick, but should be in range)
	if hp < 1000 || hp > 3000 {
		t.Errorf("HumanPrice = %f, expected ~1580 for ETH/USDC at tick=202669", hp)
	}
}

func TestHumanPriceZeroTick(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)

	if hp := ps.HumanPrice(); hp != 0 {
		t.Errorf("HumanPrice = %f at tick=0, want 0", hp)
	}
}

func TestQuoteExactInputLocalToken0ForToken1(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, big.NewInt(1_000_000_000), 100)

	out, err := ps.QuoteExactInput(big.NewInt(1_000), addrUSDC)
	if err != nil {
		t.Fatalf("QuoteExactInput: %v", err)
	}
	if out.Sign() <= 0 {
		t.Fatalf("amountOut = %s, want positive", out)
	}
	if out.Cmp(big.NewInt(997)) > 0 {
		t.Fatalf("amountOut = %s, want <= amount after fee", out)
	}
}

func TestQuoteExactInputLocalToken1ForToken0(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, big.NewInt(1_000_000_000), 100)

	out, err := ps.QuoteExactInput(big.NewInt(1_000), addrWETH)
	if err != nil {
		t.Fatalf("QuoteExactInput: %v", err)
	}
	if out.Sign() <= 0 {
		t.Fatalf("amountOut = %s, want positive", out)
	}
	if out.Cmp(big.NewInt(997)) > 0 {
		t.Fatalf("amountOut = %s, want <= amount after fee", out)
	}
}

func TestQuoteExactInputLocalInvalidToken(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, big.NewInt(1_000_000_000), 100)

	_, err := ps.QuoteExactInput(big.NewInt(1_000), common.HexToAddress("0x0000000000000000000000000000000000000001"))
	if err == nil {
		t.Fatal("expected invalid token error")
	}
}

func TestQuoteExactInputLocalUninitialized(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)

	_, err := ps.QuoteExactInput(big.NewInt(1_000), addrUSDC)
	if err == nil {
		t.Fatal("expected uninitialized state error")
	}
}

func TestSetTokensDefaults(t *testing.T) {
	ps := NewPoolState(addrPool, common.Address{}, common.Address{}, 0)
	ps.SetTokens(addrUSDC, addrWETH, 3000)

	// 未传入 TokenInfo 时，decimals 默认为 18，symbol 为地址缩写
	if ps.Token0Decimals != 18 {
		t.Errorf("Token0Decimals = %d, want 18 (default)", ps.Token0Decimals)
	}
	if ps.Token1Decimals != 18 {
		t.Errorf("Token1Decimals = %d, want 18 (default)", ps.Token1Decimals)
	}
	if ps.Token0Symbol == "" {
		t.Error("Token0Symbol should not be empty")
	}
	if ps.Token1Symbol == "" {
		t.Error("Token1Symbol should not be empty")
	}
}

func TestSetTickLiquidity(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTickLiquidity(100, big.NewInt(500000))
	if l := ps.GetTickLiquidity(100); l.Cmp(big.NewInt(500000)) != 0 {
		t.Errorf("tick 100 liquidity = %s, want 500000", l.String())
	}
	// Setting zero should be ignored (no entry created)
	ps.SetTickLiquidity(200, big.NewInt(0))
	if l := ps.GetTickLiquidity(200); l.Sign() != 0 {
		t.Errorf("tick 200 should be 0, got %s", l.String())
	}
}

func TestClearTicks(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateTickFromMint(-10, 10, big.NewInt(500))
	ps.ClearTicks()
	if ps.GetTickCount() != 0 {
		t.Errorf("GetTickCount after ClearTicks = %d, want 0", ps.GetTickCount())
	}
}

func TestReplaceTicks(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.UpdateTickFromMint(-10, 10, big.NewInt(100))
	// Replace with completely new tick map
	newTicks := map[int32]*TickLiquidity{
		5:  {LiquidityNet: big.NewInt(999)},
		-5: {LiquidityNet: big.NewInt(-999)},
	}
	ps.ReplaceTicks(newTicks)
	if ps.GetTickCount() != 2 {
		t.Errorf("GetTickCount after ReplaceTicks = %d, want 2", ps.GetTickCount())
	}
	if l := ps.GetTickLiquidity(5); l.Cmp(big.NewInt(999)) != 0 {
		t.Errorf("tick 5 = %s, want 999", l.String())
	}
	if l := ps.GetTickLiquidity(-10); l.Sign() != 0 {
		t.Error("old tick -10 should be gone after replace")
	}
}

func TestQuoteExactInputNilAmount(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, testLiquidity, 100)
	_, err := ps.QuoteExactInput(nil, addrUSDC)
	if err == nil {
		t.Error("expected error for nil amountIn")
	}
	_, err = ps.QuoteExactInput(big.NewInt(-1), addrUSDC)
	if err == nil {
		t.Error("expected error for negative amountIn")
	}
	_, err = ps.QuoteExactInput(big.NewInt(0), addrUSDC)
	if err == nil {
		t.Error("expected error for zero amountIn")
	}
}

func TestQuoteExactInputMaxFee(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 1_000_000) // fee >= 1e6
	ps.SetTokens(addrUSDC, addrWETH, 1_000_000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, testLiquidity, 100)
	_, err := ps.QuoteExactInput(big.NewInt(1000), addrUSDC)
	if err == nil {
		t.Error("expected error for invalid fee")
	}
}

func TestQuoteExactInputTinyAmount(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokens(addrUSDC, addrWETH, 3000)
	ps.UpdateFromSwap(testSqrtPriceX96, 0, testLiquidity, 100)
	// Very small amount that rounds to 0 after fee
	out, err := ps.QuoteExactInput(big.NewInt(1), addrUSDC)
	if err != nil {
		t.Fatalf("QuoteExactInput: %v", err)
	}
	if out.Sign() != 0 {
		t.Logf("tiny amount output = %s", out.String())
	}
}

func TestHumanPriceDifferentDecimals(t *testing.T) {
	// Token0=USDC(6), Token1=WETH(18): 1 ETH = 10^(18-6)/1.0001^tick USDC
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SetTokensWithInfo(addrUSDC, addrWETH, 3000,
		&TokenInfo{Decimals: 6, Symbol: "USDC"},
		&TokenInfo{Decimals: 18, Symbol: "WETH"},
	)
	// tick=0 early-returns 0; use tick=1 to verify calculation
	ps.UpdateFromSwap(testSqrtPriceX96, 1, testLiquidity, 100)
	hp := ps.HumanPrice()
	// 1 ETH = 10^(18-6) / 1.0001^1 ≈ 1e12 / 1.0001 ≈ 9.999e11
	if hp <= 0 {
		t.Errorf("HumanPrice at tick=1 with 6/18 decimals = %f, want positive", hp)
	}
}

func TestRecalcPricesWithNilSqrtPrice(t *testing.T) {
	ps := NewPoolState(addrPool, addrUSDC, addrWETH, 3000)
	ps.SqrtPriceX96 = nil
	ps.recalcPrices()
	// Should not panic, prices should be zero
	if ps.Price0In1 != 0 {
		t.Errorf("Price0In1 should be 0 with nil SqrtPriceX96")
	}
}

func TestSetTokensWithInfoPartial(t *testing.T) {
	ps := NewPoolState(addrPool, common.Address{}, common.Address{}, 0)
	// Only token0 info provided, token1 info is nil
	ps.SetTokensWithInfo(addrUSDC, addrWETH, 500,
		&TokenInfo{Symbol: "USDCx", Decimals: 6},
		nil,
	)
	if ps.Token0Symbol != "USDCx" {
		t.Errorf("Token0Symbol = %s", ps.Token0Symbol)
	}
	if ps.Token0Decimals != 6 {
		t.Errorf("Token0Decimals = %d", ps.Token0Decimals)
	}
	if ps.Token1Decimals != 18 {
		t.Errorf("Token1Decimals = %d, want 18 (default)", ps.Token1Decimals)
	}
	if ps.Token1Symbol == "" {
		t.Error("Token1Symbol should have default")
	}
}

func TestSetTokensWithInfoBothNil(t *testing.T) {
	ps := NewPoolState(addrPool, common.Address{}, common.Address{}, 0)
	ps.SetTokensWithInfo(addrUSDC, addrWETH, 500, nil, nil)
	if ps.Token0Decimals != 18 {
		t.Errorf("Token0Decimals = %d, want 18", ps.Token0Decimals)
	}
	if ps.Token1Decimals != 18 {
		t.Errorf("Token1Decimals = %d, want 18", ps.Token1Decimals)
	}
	if ps.Token0Symbol == "" || ps.Token1Symbol == "" {
		t.Error("symbols should have defaults")
	}
}

func TestTickMinMaxConstants(t *testing.T) {
	if TickMin >= TickMax {
		t.Error("TickMin should be less than TickMax")
	}
	if TickMin != -887272 {
		t.Errorf("TickMin = %d", TickMin)
	}
	if TickMax != 887272 {
		t.Errorf("TickMax = %d", TickMax)
	}
}
