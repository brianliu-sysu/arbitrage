package v4

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestComputePoolIDDeterministic(t *testing.T) {
	key := PoolKey{
		Currency0:   common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		Currency1:   common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		Fee:         3000,
		TickSpacing: 60,
		Hooks:       common.Address{},
	}

	id1, err := ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id: %v", err)
	}
	id2, err := ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id again: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected deterministic pool id, got %s vs %s", id1, id2)
	}
	if id1.Hash() == (common.Hash{}) {
		t.Fatal("expected non-zero pool id")
	}

	key.Fee = 500
	id3, err := ComputePoolID(key)
	if err != nil {
		t.Fatalf("compute pool id with different fee: %v", err)
	}
	if id3 == id1 {
		t.Fatal("expected different pool id for different fee tier")
	}
}
