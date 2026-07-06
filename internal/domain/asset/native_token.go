package asset

import "github.com/ethereum/go-ethereum/common"

const (
	NativeETHSymbol   = "ETH"
	NativeETHDecimals = 18
)

// IsNativeETH reports whether the address represents native ETH in Uniswap V4.
func IsNativeETH(address common.Address) bool {
	return address == (common.Address{})
}

// NativeETHToken returns metadata for native ETH (address zero).
func NativeETHToken() *Token {
	return &Token{
		Address: common.Address{},
		Symbol:  NativeETHSymbol,
		Decimal: NativeETHDecimals,
	}
}
