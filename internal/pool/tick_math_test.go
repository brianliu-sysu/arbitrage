package pool

import (
	"math/big"
	"testing"
)

func TestGetSqrtRatioAtTick_Zero(t *testing.T) {
	// tick 0 → sqrtPriceX96 = 2^96
	result := GetSqrtRatioAtTick(0)
	expected := new(big.Int).Lsh(big.NewInt(1), 96)
	if result.Cmp(expected) != 0 {
		t.Errorf("GetSqrtRatioAtTick(0) = %s, want %s (2^96)", result, expected)
	}
}

func TestGetSqrtRatioAtTick_Monotonic(t *testing.T) {
	// tick i+1 的 sqrtPriceX96 必须严格大于 tick i
	prev := GetSqrtRatioAtTick(-1000)
	for tick := int32(-999); tick <= 1000; tick++ {
		cur := GetSqrtRatioAtTick(tick)
		if cur.Cmp(prev) <= 0 {
			t.Errorf("GetSqrtRatioAtTick(%d) = %s <= GetSqrtRatioAtTick(%d) = %s",
				tick, cur, tick-1, prev)
			break
		}
		prev = cur
	}
}

func TestGetSqrtRatioAtTick_Bounds(t *testing.T) {
	// tick = TickMin (-887272) → sqrtPriceX96 应接近 MinSqrtRatio
	minResult := GetSqrtRatioAtTick(TickMin)
	// MinSqrtRatio = 4295128740；浮点计算允许 ±5 的误差
	diffMin := new(big.Int).Sub(minResult, MinSqrtRatio)
	if diffMin.Sign() < 0 {
		diffMin.Abs(diffMin)
	}
	tolerance := big.NewInt(5)
	if diffMin.Cmp(tolerance) > 0 {
		t.Errorf("GetSqrtRatioAtTick(%d) = %s, MinSqrtRatio = %s, diff = %s > tolerance = %s",
			TickMin, minResult, MinSqrtRatio, diffMin, tolerance)
	}

	// tick = TickMax (887272) → sqrtPriceX96 应接近 MaxSqrtRatio
	maxResult := GetSqrtRatioAtTick(TickMax)
	diffMax := new(big.Int).Sub(maxResult, MaxSqrtRatio)
	if diffMax.Sign() < 0 {
		diffMax.Abs(diffMax)
	}
	// MaxSqrtRatio 约 1.46e48；使用相对容差（百万分之一的相对误差）
	relTol := new(big.Int).Div(MaxSqrtRatio, big.NewInt(1_000_000))
	if diffMax.Cmp(relTol) > 0 {
		t.Errorf("GetSqrtRatioAtTick(%d) = %s, MaxSqrtRatio = %s, diff = %s > relTol = %s",
			TickMax, maxResult, MaxSqrtRatio, diffMax, relTol)
	}
}

func TestGetSqrtRatioAtTick_Symmetry(t *testing.T) {
	// sqrtPriceX96(t) * sqrtPriceX96(-t) ≈ 2^192
	// 即：sqrt(1.0001^t) * 2^96 * sqrt(1.0001^(-t)) * 2^96 = 2^192
	two192 := new(big.Int).Lsh(big.NewInt(1), 192)

	for _, tick := range []int32{1, 10, 100, 1000, 10000, 100000} {
		pos := GetSqrtRatioAtTick(tick)
		neg := GetSqrtRatioAtTick(-tick)
		product := new(big.Int).Mul(pos, neg)

		// 由于浮点舍入，允许 1% 的误差（tick 越大误差越大，但相对误差极小）
		diff := new(big.Int).Sub(product, two192)
		if diff.Sign() < 0 {
			diff.Abs(diff)
		}

		// 允许的绝对误差：two192 的 0.01%
		tolerance := new(big.Int).Div(two192, big.NewInt(10000))
		if diff.Cmp(tolerance) > 0 {
			t.Errorf("GetSqrtRatioAtTick(%d) * GetSqrtRatioAtTick(%d) = %s, "+
				"diff from 2^192 = %s > tolerance = %s",
				tick, -tick, product, diff, tolerance)
		}
	}
}

func TestGetSqrtRatioAtTick_KnownValues(t *testing.T) {
	// 手动验证几个 tick 值，确保价格在合理范围内
	// sqrtPriceX96(0) = 2^96 ≈ 7.92e28
	q96 := new(big.Int).Lsh(big.NewInt(1), 96)

	r0 := GetSqrtRatioAtTick(0)
	if r0.Cmp(q96) != 0 {
		t.Errorf("tick 0: got %s, want %s", r0, q96)
	}

	// tick > 0 → sqrtPriceX96 > 2^96
	r100 := GetSqrtRatioAtTick(100)
	if r100.Cmp(q96) <= 0 {
		t.Errorf("tick 100: got %s, should be > 2^96 = %s", r100, q96)
	}

	// tick < 0 → sqrtPriceX96 < 2^96
	rNeg100 := GetSqrtRatioAtTick(-100)
	if rNeg100.Cmp(q96) >= 0 {
		t.Errorf("tick -100: got %s, should be < 2^96 = %s", rNeg100, q96)
	}
}
