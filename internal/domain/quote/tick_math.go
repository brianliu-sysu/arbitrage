package quote

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

var (
	ErrInvalidTick       = errors.New("tick out of range")
	ErrInvalidSqrtRatio  = errors.New("sqrt ratio out of range")
	ErrInvalidMSBInput   = errors.New("invalid most significant bit input")
)

var (
	q32 = big.NewInt(1 << 32)

	minSqrtRatio = big.NewInt(4295128739)
	maxSqrtRatio = mustBigInt("1461446703485210103287273052203988822378723970342")

	magicSqrt10001 = mustBigInt("255738958999603826347141")
	magicTickLow   = mustBigInt("3402992956809132418596140100660247210")
	magicTickHigh  = mustBigInt("291339464771989622907027621153398088495")
)

func mustBigInt(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(fmt.Sprintf("invalid big.Int constant: %s", s))
	}
	return v
}

func mulShift(val, mulBy *big.Int) *big.Int {
	return new(big.Int).Rsh(new(big.Int).Mul(val, mulBy), 128)
}

// GetSqrtRatioAtTick returns sqrt(1.0001^tick) * 2^96.
func GetSqrtRatioAtTick(tick int32) (*big.Int, error) {
	if tick < market.MinTick || tick > market.MaxTick {
		return nil, ErrInvalidTick
	}

	absTick := tick
	if tick < 0 {
		absTick = -tick
	}

	var ratio *big.Int
	if absTick&0x1 != 0 {
		ratio = mustBigIntHex("fffcb933bd6fad37aa2d162d1a594001")
	} else {
		ratio = mustBigIntHex("100000000000000000000000000000000")
	}

	ratio = applyTickBit(ratio, absTick, 0x2, "fff97272373d413259a46990580e213a")
	ratio = applyTickBit(ratio, absTick, 0x4, "fff2e50f5f656932ef12357cf3c7fdcc")
	ratio = applyTickBit(ratio, absTick, 0x8, "ffe5caca7e10e4e61c3624eaa0941cd0")
	ratio = applyTickBit(ratio, absTick, 0x10, "ffcb9843d60f6159c9db58835c926644")
	ratio = applyTickBit(ratio, absTick, 0x20, "ff973b41fa98c081472e6896dfb254c0")
	ratio = applyTickBit(ratio, absTick, 0x40, "ff2ea16466c96a3843ec78b326b52861")
	ratio = applyTickBit(ratio, absTick, 0x80, "fe5dee046a99a2a811c461f1969c3053")
	ratio = applyTickBit(ratio, absTick, 0x100, "fcbe86c7900a88aedcffc83b479aa3a4")
	ratio = applyTickBit(ratio, absTick, 0x200, "f987a7253ac413176f2b074cf7815e54")
	ratio = applyTickBit(ratio, absTick, 0x400, "f3392b0822b70005940c7a398e4b70f3")
	ratio = applyTickBit(ratio, absTick, 0x800, "e7159475a2c29b7443b29c7fa6e889d9")
	ratio = applyTickBit(ratio, absTick, 0x1000, "d097f3bdfd2022b8845ad8f792aa5825")
	ratio = applyTickBit(ratio, absTick, 0x2000, "a9f746462d870fdf8a65dc1f90e061e5")
	ratio = applyTickBit(ratio, absTick, 0x4000, "70d869a156d2a1b890bb3df62baf32f7")
	ratio = applyTickBit(ratio, absTick, 0x8000, "31be135f97d08fd981231505542fcfa6")
	ratio = applyTickBit(ratio, absTick, 0x10000, "9aa508b5b7a84e1c677de54f3e99bc9")
	ratio = applyTickBit(ratio, absTick, 0x20000, "5d6af8dedb81196699c329225ee604")
	ratio = applyTickBit(ratio, absTick, 0x40000, "2216e584f5fa1ea926041bedfe98")
	ratio = applyTickBit(ratio, absTick, 0x80000, "48a170391f7dc42444e8fa2")

	if tick > 0 {
		ratio = new(big.Int).Div(maxUint256, ratio)
	}

	if new(big.Int).Rem(ratio, q32).Sign() > 0 {
		return new(big.Int).Add(new(big.Int).Div(ratio, q32), big.NewInt(1)), nil
	}
	return new(big.Int).Div(ratio, q32), nil
}

func mustBigIntHex(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic(fmt.Sprintf("invalid hex constant: %s", s))
	}
	return v
}

func applyTickBit(ratio *big.Int, absTick int32, mask int32, constantHex string) *big.Int {
	if absTick&mask != 0 {
		return mulShift(ratio, mustBigIntHex(constantHex))
	}
	return ratio
}

// GetTickAtSqrtRatio returns the greatest tick such that GetSqrtRatioAtTick(tick) <= sqrtRatioX96.
func GetTickAtSqrtRatio(sqrtRatioX96 *big.Int) (int32, error) {
	if sqrtRatioX96.Cmp(minSqrtRatio) < 0 || sqrtRatioX96.Cmp(maxSqrtRatio) >= 0 {
		return 0, ErrInvalidSqrtRatio
	}

	sqrtRatioX128 := new(big.Int).Lsh(sqrtRatioX96, 32)
	msb, err := mostSignificantBit(sqrtRatioX128)
	if err != nil {
		return 0, err
	}

	var r *big.Int
	if msb >= 128 {
		r = new(big.Int).Rsh(sqrtRatioX128, uint(msb-127))
	} else {
		r = new(big.Int).Lsh(sqrtRatioX128, uint(127-msb))
	}

	log2 := new(big.Int).Lsh(new(big.Int).Sub(big.NewInt(msb), big.NewInt(128)), 64)
	for i := 0; i < 14; i++ {
		r = new(big.Int).Rsh(new(big.Int).Mul(r, r), 127)
		f := new(big.Int).Rsh(r, 128)
		log2 = new(big.Int).Or(log2, new(big.Int).Lsh(f, uint(63-i)))
		r = new(big.Int).Rsh(r, uint(f.Int64()))
	}

	logSqrt10001 := new(big.Int).Mul(log2, magicSqrt10001)
	tickLow := int32(new(big.Int).Rsh(new(big.Int).Sub(logSqrt10001, magicTickLow), 128).Int64())
	tickHigh := int32(new(big.Int).Rsh(new(big.Int).Add(logSqrt10001, magicTickHigh), 128).Int64())
	if tickLow == tickHigh {
		return tickLow, nil
	}

	sqrtRatio, err := GetSqrtRatioAtTick(tickHigh)
	if err != nil {
		return 0, err
	}
	if sqrtRatio.Cmp(sqrtRatioX96) <= 0 {
		return tickHigh, nil
	}
	return tickLow, nil
}

func mostSignificantBit(x *big.Int) (int64, error) {
	if x.Sign() <= 0 {
		return 0, ErrInvalidMSBInput
	}
	if x.Cmp(maxUint256) > 0 {
		return 0, ErrInvalidMSBInput
	}

	var msb int64
	for _, power := range []int64{128, 64, 32, 16, 8, 4, 2, 1} {
		threshold := new(big.Int).Lsh(big.NewInt(1), uint(power))
		if x.Cmp(threshold) >= 0 {
			x = new(big.Int).Rsh(x, uint(power))
			msb += power
		}
	}
	return msb, nil
}
