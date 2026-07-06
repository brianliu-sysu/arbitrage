package asset

import "github.com/ethereum/go-ethereum/common"

// MainnetWETH is the canonical wrapped ETH contract on Ethereum mainnet.
var MainnetWETH = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")

// IsMainnetWETH reports whether address is the mainnet WETH contract.
func IsMainnetWETH(address common.Address) bool {
	return address == MainnetWETH
}
