package asset

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestIsNativeETH(t *testing.T) {
	if !IsNativeETH(common.Address{}) {
		t.Fatal("expected zero address to represent native ETH")
	}
	if IsNativeETH(common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")) {
		t.Fatal("WETH is not native ETH in V4")
	}
}

func TestNativeETHToken(t *testing.T) {
	token := NativeETHToken()
	if token.Symbol != "ETH" || token.Decimal != 18 {
		t.Fatalf("unexpected native token metadata: %#v", token)
	}
}
