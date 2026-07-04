package v4

import (
	"github.com/ethereum/go-ethereum/common"
)

// PoolKey identifies a Uniswap V4 pool inside the singleton PoolManager.
type PoolKey struct {
	Currency0   common.Address
	Currency1   common.Address
	Fee         uint32
	TickSpacing int32
	Hooks       common.Address
}

func (k PoolKey) Clone() PoolKey {
	return k
}
