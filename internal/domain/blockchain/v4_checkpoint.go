package blockchain

import (
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// V4Checkpoint records how far a V4 pool has been synced locally.
type V4Checkpoint struct {
	PoolID      marketv4.PoolID
	BlockNumber uint64
	BlockHash   common.Hash
}
