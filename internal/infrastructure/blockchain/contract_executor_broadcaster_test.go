package blockchain

import (
	"math/big"
	"testing"
)

func TestAddBuilderPaymentToGasPriceRoundsUp(t *testing.T) {
	got := addBuilderPaymentToGasPrice(big.NewInt(10), big.NewInt(101), 10)
	if got.Cmp(big.NewInt(21)) != 0 {
		t.Fatalf("expected gas price 21, got %s", got)
	}
}

func TestAddBuilderPaymentToGasPriceDoesNotMutateInput(t *testing.T) {
	base := big.NewInt(10)
	got := addBuilderPaymentToGasPrice(base, big.NewInt(100), 10)
	if base.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("base gas price mutated to %s", base)
	}
	if got.Cmp(big.NewInt(20)) != 0 {
		t.Fatalf("expected gas price 20, got %s", got)
	}
}
