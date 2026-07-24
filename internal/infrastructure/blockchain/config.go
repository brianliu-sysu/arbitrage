package blockchain

import "github.com/ethereum/go-ethereum/common"

// Config holds chain-wide RPC and multicall settings.
type Config struct {
	RPCURL           string
	WSURL            string
	MulticallAddress common.Address
}

type Univ3Config struct {
	FactoryAddress common.Address
}

type Univ4Config struct {
	PoolManagerAddress common.Address
	StateViewAddress   common.Address
}

type BalancerConfig struct {
	VaultAddress   common.Address
	VaultV3Address common.Address
}
