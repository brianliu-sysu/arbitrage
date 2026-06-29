// Package pool — Uniswap V3 tick 到 sqrtPriceX96 的转换。
package pool

import (
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/utils"
)

// tickMathCache 缓存 sqrt(1.0001)^(2^i) 的预计算值（i = 0..19）。
//
// TickMax = 887272 < 2^20 = 1048576，因此 20 个 bit 足以分解任意合法 tick。
// 使用 big.Float 512-bit 精度，保证 160-bit sqrtPriceX96 的精确转换。
const tickMathPrec = 512

var tickMathCache [20]*big.Float

func init() {
	// sqrt(10001) with high precision
	sqrt10001 := new(big.Float).SetPrec(tickMathPrec).SetInt64(10001)
	sqrt10001.Sqrt(sqrt10001)

	// sqrt(1.0001) = sqrt(10001/10000) = sqrt(10001) / 100
	oneHundred := new(big.Float).SetPrec(tickMathPrec).SetInt64(100)
	sqrtRat1_0001 := new(big.Float).SetPrec(tickMathPrec).Quo(sqrt10001, oneHundred)

	// Precompute: tickMathCache[i] = sqrt(1.0001)^(2^i)
	tickMathCache[0] = new(big.Float).SetPrec(tickMathPrec).Copy(sqrtRat1_0001)
	for i := 1; i < 20; i++ {
		tickMathCache[i] = new(big.Float).SetPrec(tickMathPrec).Mul(
			tickMathCache[i-1], tickMathCache[i-1])
	}
}

// GetSqrtRatioAtTick 返回指定 tick 对应的 sqrtPriceX96。
//
// 公式：sqrtPriceX96 = sqrt(1.0001^tick) * 2^96
//
// tick 必须在 [TickMin, TickMax] 范围内，即 [-887272, 887272]。
func GetSqrtRatioAtTick(tick int32) *big.Int {
	result := new(big.Float).SetPrec(tickMathPrec).SetInt64(1)

	absTick := tick
	if tick < 0 {
		absTick = -tick
	}

	// 二进制分解：absTick = Σ bit_i * 2^i
	// sqrt(1.0001)^absTick = ∏_{bit_i=1} tickMathCache[i]
	ui := uint32(absTick)
	for i := 0; i < 20; i++ {
		if ui&(1<<i) != 0 {
			result.Mul(result, tickMathCache[i])
		}
	}

	// 负 tick 取倒数
	if tick < 0 {
		one := new(big.Float).SetPrec(tickMathPrec).SetInt64(1)
		result.Quo(one, result)
	}

	// 乘以 2^96
	q96Float := new(big.Float).SetPrec(tickMathPrec).SetInt(utils.X96)
	result.Mul(result, q96Float)

	// 四舍五入（正数 +0.5 后截断）
	half := new(big.Float).SetPrec(tickMathPrec).SetFloat64(0.5)
	result.Add(result, half)
	intVal, _ := result.Int(nil)
	return intVal
}
