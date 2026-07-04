package market

import (
	"math/big"
	"testing"
)

func TestTickTableUpdateFlips(t *testing.T) {
	table := NewTickTable()
	delta := big.NewInt(100)

	flipped, err := table.Update(60, delta, false)
	if err != nil {
		t.Fatalf("update tick: %v", err)
	}
	if !flipped {
		t.Fatal("expected initialization flip on first update")
	}

	flipped, err = table.Update(60, new(big.Int).Neg(delta), false)
	if err != nil {
		t.Fatalf("remove tick liquidity: %v", err)
	}
	if !flipped {
		t.Fatal("expected de-initialization flip")
	}
	if _, ok := table.Get(60); ok {
		t.Fatal("tick should be removed when gross liquidity is zero")
	}
}
