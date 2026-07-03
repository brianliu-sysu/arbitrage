package blockchain

import (
	"github.com/ethereum/go-ethereum/common"
)

// Checkpoint records how far a pool has been synced locally.
type Checkpoint struct {
	PoolAddress common.Address
	BlockNumber uint64
	BlockHash   common.Hash
}
