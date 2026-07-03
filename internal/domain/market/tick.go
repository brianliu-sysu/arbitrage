package market

import "math/big"

// Tick represents liquidity state at a single price tick.
type Tick struct {
	Index          int32
	LiquidityGross *big.Int
	LiquidityNet   *big.Int
}

func NewTick(index int32) *Tick {
	return &Tick{
		Index:          index,
		LiquidityGross: big.NewInt(0),
		LiquidityNet:   big.NewInt(0),
	}
}

func (t *Tick) Clone() *Tick {
	if t == nil {
		return nil
	}
	return &Tick{
		Index:          t.Index,
		LiquidityGross: cloneInt(t.LiquidityGross),
		LiquidityNet:   cloneInt(t.LiquidityNet),
	}
}

func (t *Tick) IsInitialized() bool {
	return t.LiquidityGross.Sign() > 0
}
