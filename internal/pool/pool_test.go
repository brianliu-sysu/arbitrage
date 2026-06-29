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

func TestNewState(t *testing.T) {
	ps := NewState(addrPool, addrUSDC, addrWETH, 3000)

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
	ps := NewState(addrPool, common.Address{}, common.Address{}, 0)
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

func TestUpdateFromSwap(t *testing.T) {
	ps := NewState(addrPool, addrUSDC, addrWETH, 3000)
	sqrtPrice := new(big.Int).Set(testSqrtPriceX96)
	liq := new(big.Int).Set(testLiquidity)

	ps.UpdateFromSwap(sqrtPrice, 0, liq, 0)

	if ps.Tick != 0 {
		t.Errorf("Tick = %d, want 0", ps.Tick)
	}

	if ps.Liquidity.Cmp(liq) != 0 {
		t.Errorf("Liquidity mismatch")
	}
	if ps.SqrtPriceX96.Cmp(sqrtPrice) != 0 {
		t.Errorf("SqrtPriceX96 mismatch")
	}
}

func TestUpdateFromSwapPriceCalculation(t *testing.T) {
	ps := NewState(addrPool, addrUSDC, addrWETH, 3000)

	// sqrtPriceX96 for price=4: sqrt(4) * 2^96 = 2 * 2^96
	two := big.NewInt(2)
	two96 := new(big.Int).Lsh(big.NewInt(1), 96)
	sqrtPrice := new(big.Int).Mul(two, two96)

	ps.UpdateFromSwap(sqrtPrice, 100, testLiquidity, 0)
	if ps.Tick != 100 {
		t.Errorf("Tick = %d, want 100", ps.Tick)
	}
}

func TestUpdateTickFromBurn_WithMissingLiquidityGross_DoesNotPanic(t *testing.T) {
	ps := NewState(addrPool, addrUSDC, addrWETH, 3000)
	ps.TickSpacing = 60
	ps.UpdateFromSwap(testSqrtPriceX96, 0, big.NewInt(1_000_000), 1)

	// 模拟旧快照恢复：只有 net，没有 gross。
	ps.ReplaceTicks(map[int32]*TickLiquidity{
		60: {
			LiquidityNet: big.NewInt(1000),
		},
	})

	// 这里此前会因为 LiquidityGross=nil 在 Burn 路径崩溃。
	ps.UpdateTickFromBurn(0, 60, big.NewInt(100), 2)
}
