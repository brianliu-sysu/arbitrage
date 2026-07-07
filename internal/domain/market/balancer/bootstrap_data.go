package balancer

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// BootstrapInput identifies a pool to read during batched bootstrap.
type BootstrapInput struct {
	PoolID PoolID
	Spec   PoolSpec
}

// BootstrapData is on-chain Balancer pool state read during cold bootstrap.
type BootstrapData struct {
	Spec              PoolSpec
	Tokens            []common.Address
	Balances          map[common.Address]*big.Int
	Weights           map[common.Address]*big.Int
	Amplification     *big.Int
	SwapFeePercentage *big.Int
	BlockNumber       uint64
}
