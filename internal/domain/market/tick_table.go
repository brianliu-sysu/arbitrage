package market

import (
	"fmt"
	"math/big"
)

// TickTable stores initialized ticks keyed by tick index.
type TickTable struct {
	ticks map[int32]*Tick
}

func NewTickTable() TickTable {
	return TickTable{ticks: make(map[int32]*Tick)}
}

func (tt TickTable) Clone() TickTable {
	cloned := NewTickTable()
	for index, tick := range tt.ticks {
		cloned.ticks[index] = tick.Clone()
	}
	return cloned
}

func (tt *TickTable) Get(index int32) (*Tick, bool) {
	tick, ok := tt.ticks[index]
	return tick, ok
}

func (tt *TickTable) GetOrCreate(index int32) *Tick {
	if tick, ok := tt.ticks[index]; ok {
		return tick
	}
	tick := NewTick(index)
	tt.ticks[index] = tick
	return tick
}

func (tt TickTable) InitializedIndexes() []int32 {
	indexes := make([]int32, 0, len(tt.ticks))
	for index, tick := range tt.ticks {
		if tick.IsInitialized() {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

// Update applies a liquidity delta to a tick and returns whether initialization flipped.
func (tt *TickTable) Update(index int32, liquidityDelta *big.Int, upper bool) (flipped bool, err error) {
	if err = validateTick(index); err != nil {
		return false, err
	}
	if liquidityDelta.Sign() == 0 {
		return false, nil
	}

	tick := tt.GetOrCreate(index)
	liquidityGrossBefore := new(big.Int).Set(tick.LiquidityGross)

	liquidityGrossAfter := new(big.Int).Add(tick.LiquidityGross, liquidityDelta)
	if liquidityGrossAfter.Sign() < 0 {
		return false, fmt.Errorf("tick %d liquidity gross underflow", index)
	}
	tick.LiquidityGross = liquidityGrossAfter

	if upper {
		tick.LiquidityNet = new(big.Int).Sub(tick.LiquidityNet, liquidityDelta)
	} else {
		tick.LiquidityNet = new(big.Int).Add(tick.LiquidityNet, liquidityDelta)
	}

	flipped = (liquidityGrossAfter.Sign() == 0) != (liquidityGrossBefore.Sign() == 0)
	if liquidityGrossAfter.Sign() == 0 {
		delete(tt.ticks, index)
	}
	return flipped, nil
}

func validateTick(index int32) error {
	if index < MinTick || index > MaxTick {
		return fmt.Errorf("tick %d out of range [%d, %d]", index, MinTick, MaxTick)
	}
	return nil
}
