package market

import (
	"math/big"
)

const (
	MinTick = -887272
	MaxTick = 887272
)

// PoolState holds the mutable on-chain state of a Uniswap V3 pool.
type PoolState struct {
	SqrtPriceX96         *big.Int
	Tick                 int32
	Liquidity            *big.Int
	FeeGrowthGlobal0X128 *big.Int
	FeeGrowthGlobal1X128 *big.Int
}

func NewPoolState() PoolState {
	return PoolState{
		SqrtPriceX96:         big.NewInt(0),
		Tick:                 0,
		Liquidity:            big.NewInt(0),
		FeeGrowthGlobal0X128: big.NewInt(0),
		FeeGrowthGlobal1X128: big.NewInt(0),
	}
}

func (s PoolState) Clone() PoolState {
	return PoolState{
		SqrtPriceX96:         cloneInt(s.SqrtPriceX96),
		Tick:                 s.Tick,
		Liquidity:            cloneInt(s.Liquidity),
		FeeGrowthGlobal0X128: cloneInt(s.FeeGrowthGlobal0X128),
		FeeGrowthGlobal1X128: cloneInt(s.FeeGrowthGlobal1X128),
	}
}

func (s PoolState) IsInitialized() bool {
	return s.SqrtPriceX96 != nil && s.SqrtPriceX96.Sign() > 0
}

func cloneInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(v)
}
