package poolsapp

import (
	"math/big"
	"testing"
)

func TestImpliedPriceFromSqrtPriceX96(t *testing.T) {
	sqrtPriceX96, _ := new(big.Int).SetString("1182815765319608250048300092661", 10)
	price := impliedPrice(sqrtPriceX96, 18, 18)
	if price.Token1PerToken0 == "" || price.Token1PerToken0 == "0" {
		t.Fatalf("expected positive token1/token0 price, got %#v", price)
	}
	if price.Token0PerToken1 == "" || price.Token0PerToken1 == "0" {
		t.Fatalf("expected positive token0/token1 price, got %#v", price)
	}
}
