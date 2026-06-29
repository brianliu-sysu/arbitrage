package utils

import "math/big"

// Go 语言规范：包加载时自动隐式调用，且保证单线程全局锁定执行
func init() {
	prec := uint(256)
	base := new(big.Float).SetPrec(prec).SetFloat64(1.0001)
	twoTo96 := new(big.Float).SetPrec(prec).SetInt(new(big.Int).Lsh(big.NewInt(1), 96))

	for tick := MinTick; tick <= MaxTick; tick++ {
		cacheIndex := tick + MaxTick

		// 快速幂计算 1.0001^tick
		p := powBigFloat(base, int64(tick), prec)
		sqrtP := new(big.Float).SetPrec(prec).Sqrt(p)
		resultFloat := new(big.Float).SetPrec(prec).Mul(sqrtP, twoTo96)

		resultInt := new(big.Int)
		resultFloat.Int(resultInt)

		// 此时创建的 resultInt 被死死锁在全局数组中
		SqrtPriceCache[cacheIndex] = resultInt
	}
}

var (
	X96 = new(big.Int).Lsh(big.NewInt(1), 96)

	// B10001 = 1.0001 * 2^128 (Uniswap 官方使用的定点数基数)
	// 这里为了方便演示，我们直接给出官方合约中二进制推导的硬编码实现逻辑
	MaxTick int32 = 887272
	MinTick int32 = -887272

	// 官方位逼近法所需的魔法数字常数 (X96 定点数下的边界)
	minSqrtRatio, _ = new(big.Int).SetString("4295128739", 10)
	maxSqrtRatio, _ = new(big.Int).SetString("1461446703485210103287273052203988822378723970342", 10)
)

// SqrtPriceMath_ComputeSwapStep 对应 Uniswap V3 的 SwapMath.computeSwapStep
// 返回：(最终价格 sqrtPriceAfterX96, 消耗的输入量 amountIn, 产出的输出量 amountOut)
func SqrtPriceMathComputeSwapStep(
	sqrtPriceCurrentX96 *big.Int,
	sqrtPriceTargetX96 *big.Int,
	liquidity *big.Int,
	amountRemaining *big.Int,
	zeroForOne bool,
) (sqrtPriceAfterX96, amountIn, amountOut *big.Int) {

	sqrtPriceAfterX96 = new(big.Int)
	amountIn = big.NewInt(0)
	amountOut = big.NewInt(0)

	// 判断价格移动方向
	// zeroForOne = true:  当前价格 >= 目标价格 (价格下跌)
	// zeroForOne = false: 当前价格 <= 目标价格 (价格上涨)
	if zeroForOne {
		// 计算从当前价格跌到目标价格，最多能吃掉多少 Token 0 (Amount0)
		// 公式：Δx = (L * ΔsqrtP) / (sqrtP_current * sqrtP_target)
		maxAmountIn := getAmount0Delta(sqrtPriceTargetX96, sqrtPriceCurrentX96, liquidity, true)

		if amountRemaining.Cmp(maxAmountIn) >= 0 {
			// 钱充足：直接把这一步灌满，价格到达目标边界
			sqrtPriceAfterX96.Set(sqrtPriceTargetX96)
			amountIn.Set(maxAmountIn)
			// 计算能换出多少 Token 1 (Amount1)
			// 公式：Δy = L * ΔsqrtP
			amountOut = getAmount1Delta(sqrtPriceTargetX96, sqrtPriceCurrentX96, liquidity, false)
		} else {
			// 钱不够：价格停在区间内部，根据剩余的输入资金，反向推算最终的价格
			sqrtPriceAfterX96 = getNextSqrtPriceFromInput(sqrtPriceCurrentX96, liquidity, amountRemaining, zeroForOne)
			amountIn.Set(amountRemaining)
			amountOut = getAmount1Delta(sqrtPriceAfterX96, sqrtPriceCurrentX96, liquidity, false)
		}
	} else {
		// 向右找（Token 1 换 Token 0，价格上涨）
		// 计算从当前价格涨到目标价格，最多能吃掉多少 Token 1 (Amount1)
		// 公式：Δy = L * ΔsqrtP
		maxAmountIn := getAmount1Delta(sqrtPriceCurrentX96, sqrtPriceTargetX96, liquidity, true)

		if amountRemaining.Cmp(maxAmountIn) >= 0 {
			// 钱充足
			sqrtPriceAfterX96.Set(sqrtPriceTargetX96)
			amountIn.Set(maxAmountIn)
			// 计算换出的 Token 0
			amountOut = getAmount0Delta(sqrtPriceCurrentX96, sqrtPriceTargetX96, liquidity, false)
		} else {
			// 钱不够
			sqrtPriceAfterX96 = getNextSqrtPriceFromInput(sqrtPriceCurrentX96, liquidity, amountRemaining, zeroForOne)
			amountIn.Set(amountRemaining)
			amountOut = getAmount0Delta(sqrtPriceAfterX96, sqrtPriceCurrentX96, liquidity, false)
		}
	}

	return sqrtPriceAfterX96, amountIn, amountOut
}

// ==========================================
//          以下为底层高精度数学公式支持
// ==========================================

// getAmount0Delta 计算 Token 0 的变化量 (向上或向下取整)
func getAmount0Delta(sqrtPriceAX96, sqrtPriceBX96 *big.Int, liquidity *big.Int, roundUp bool) *big.Int {
	// 确保 A < B
	if sqrtPriceAX96.Cmp(sqrtPriceBX96) > 0 {
		sqrtPriceAX96, sqrtPriceBX96 = sqrtPriceBX96, sqrtPriceAX96
	}

	// 公式：Δx = (L * 2^96 * (sqrtP_B - sqrtP_A)) / (sqrtP_B * sqrtP_A)
	numerator1 := new(big.Int).Lsh(liquidity, 96)
	numerator2 := new(big.Int).Sub(sqrtPriceBX96, sqrtPriceAX96)

	numerator := new(big.Int).Mul(numerator1, numerator2)
	denominator := new(big.Int).Mul(sqrtPriceBX96, sqrtPriceAX96)

	if roundUp {
		// 向上取整：(a + b - 1) / b
		return new(big.Int).Div(new(big.Int).Add(numerator, new(big.Int).Sub(denominator, big.NewInt(1))), denominator)
	}
	return new(big.Int).Div(numerator, denominator)
}

// getAmount1Delta 计算 Token 1 的变化量
func getAmount1Delta(sqrtPriceAX96, sqrtPriceBX96 *big.Int, liquidity *big.Int, roundUp bool) *big.Int {
	if sqrtPriceAX96.Cmp(sqrtPriceBX96) > 0 {
		sqrtPriceAX96, sqrtPriceBX96 = sqrtPriceBX96, sqrtPriceAX96
	}

	// 公式：Δy = L * (sqrtP_B - sqrtP_A) / 2^96
	numerator := new(big.Int).Mul(liquidity, new(big.Int).Sub(sqrtPriceBX96, sqrtPriceAX96))

	if roundUp {
		return new(big.Int).Div(new(big.Int).Add(numerator, new(big.Int).Sub(X96, big.NewInt(1))), X96)
	}
	return new(big.Int).Div(numerator, X96)
}

// getNextSqrtPriceFromInput 根据输入的代币数量，反推最终的价格
func getNextSqrtPriceFromInput(sqrtPriceCurrentX96 *big.Int, liquidity *big.Int, amountIn *big.Int, zeroForOne bool) *big.Int {
	if zeroForOne {
		// Token 0 换 Token 1 (价格下跌)，反推下落后的价格
		// 公式：sqrtP_after = (L * sqrtP * 2^96) / (L * 2^96 + amountIn * sqrtP)
		numerator1 := new(big.Int).Mul(liquidity, sqrtPriceCurrentX96)
		numerator := new(big.Int).Lsh(numerator1, 96)

		denominator1 := new(big.Int).Lsh(liquidity, 96)
		denominator2 := new(big.Int).Mul(amountIn, sqrtPriceCurrentX96)
		denominator := new(big.Int).Add(denominator1, denominator2)

		// 官方为了安全规定，价格下跌反推时结果需要向上取整
		return new(big.Int).Div(new(big.Int).Add(numerator, new(big.Int).Sub(denominator, big.NewInt(1))), denominator)
	} else {
		// Token 1 换 Token 0 (价格上涨)，反推上涨后的价格
		// 公式：sqrtP_after = sqrtP + (amountIn * 2^96) / L
		quotient := new(big.Int).Lsh(amountIn, 96)
		quotient.Div(quotient, liquidity)
		return new(big.Int).Add(sqrtPriceCurrentX96, quotient)
	}
}

// 全量静态数组查找表：物理下标寻址，O(1) 速度之王
// 空间大小：887272 * 2 + 1 = 1,774,545 个指针
var SqrtPriceCache [1774545]*big.Int

// SqrtPriceMath_GetSqrtPriceAtTick 极速查表法
func SqrtPriceMathGetSqrtPriceAtTick(tick int32) *big.Int {
	if tick < MinTick || tick > MaxTick {
		panic("tick out of range")
	}

	// 直接物理内存偏移寻址，耗时 < 1 纳秒
	return new(big.Int).Set(SqrtPriceCache[tick+MaxTick])
}

// ==========================================
//          高精度大数指数运算辅助函数
// ==========================================

// powBigFloat 实现 big.Float 的高精度幂运算 (x^n)
func powBigFloat(x *big.Float, n int64, prec uint) *big.Float {
	res := new(big.Float).SetPrec(prec).SetFloat64(1.0)
	if n == 0 {
		return res
	}

	absN := n
	if n < 0 {
		absN = -n
	}

	// 经典的快速幂算法 (Binary Exponentiation)
	base := new(big.Float).SetPrec(prec).Set(x)
	for absN > 0 {
		if absN&1 == 1 {
			res.Mul(res, base)
		}
		base.Mul(base, base)
		absN >>= 1
	}

	// 如果 tick 是负数，求倒数：1.0 / x^n
	if n < 0 {
		res.Quo(new(big.Float).SetPrec(prec).SetFloat64(1.0), res)
	}

	return res
}

// SqrtPriceMathGetTickAtSqrtPrice 还原 TickMath.getTickAtSqrtRatio
// 输入当前的 sqrtPriceX96，反推出对应的 int24 tick
func SqrtPriceMathGetTickAtSqrtPrice(sqrtPriceX96 *big.Int) int32 {
	low := int32(0)
	high := int32(len(SqrtPriceCache) - 1)

	for low <= high {
		mid := (low + high) / 2
		cmp := SqrtPriceCache[mid].Cmp(sqrtPriceX96)

		if cmp == 0 {
			return mid - MaxTick
		} else if cmp < 0 {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	// 向下取整
	return high - MaxTick
}
