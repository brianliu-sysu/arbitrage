package blockchain

import (
	"github.com/ethereum/go-ethereum/common"
)

// Config holds RPC and Uniswap contract addresses.
type Config struct {
	RPCURL               string
	WSURL                string
	FactoryAddress       common.Address
	MulticallAddress     common.Address
	PoolManagerAddress   common.Address
	StateViewAddress     common.Address
	BalancerVaultAddress   common.Address
	BalancerVaultV3Address common.Address
}

func DefaultConfig(rpcURL string) Config {
	return Config{
		RPCURL:           rpcURL,
		FactoryAddress:   common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"),
		MulticallAddress: common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"),
	}
}
